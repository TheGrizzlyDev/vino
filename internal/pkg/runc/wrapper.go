package runc

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"

	"github.com/TheGrizzlyDev/vino/internal/pkg/cli"
	specs "github.com/opencontainers/runtime-spec/specs-go"
	"golang.org/x/sys/unix"
)

type BundleRewriter interface {
	RewriteBundle(*specs.Spec) error
}

type ProcessRewriter interface {
	RewriteProcess(*specs.Process) error
}

type Wrapper struct {
	BundleRewriter  BundleRewriter
	ProcessRewriter ProcessRewriter
	Delegate        Cli
}

type RuncCommands struct {
	Checkpoint *Checkpoint
	Restore    *Restore
	Create     *Create
	Run        *Run
	Start      *Start
	Delete     *Delete
	Pause      *Pause
	Resume     *Resume
	Kill       *Kill
	List       *List
	Ps         *Ps
	State      *State
	Events     *Events
	Exec       *Exec
	Spec       *Spec
	Update     *Update
	Features   *Features
}

func RunWithArgs(w *Wrapper, args []string) error {
	var cmds RuncCommands
	if err := cli.ParseAny(&cmds, args); err != nil {
		return err
	}
	return w.Run(cmds)
}

func (w *Wrapper) Run(cmds RuncCommands) error {
	if w.Delegate == nil {
		return fmt.Errorf("wrapper: nil delegate")
	}

	// Bundle rewriting for commands that reference a bundle.
	if w.BundleRewriter != nil || w.ProcessRewriter != nil {
		var bundlePath string
		switch {
		case cmds.Create != nil:
			bundlePath = cmds.Create.Bundle
		case cmds.Run != nil:
			bundlePath = cmds.Run.Bundle
		// TODO: check if we actually want to modify a restored bundle
		//		 or if it is aleady restored with the modifications
		case cmds.Restore != nil:
			bundlePath = cmds.Restore.Bundle
		}
		if bundlePath != "" {
			cfg := filepath.Join(bundlePath, "config.json")
			data, err := os.ReadFile(cfg)
			if err != nil {
				return fmt.Errorf("read bundle: %w", err)
			}
			var spec specs.Spec
			if err := json.Unmarshal(data, &spec); err != nil {
				return fmt.Errorf("unmarshal bundle: %w", err)
			}
			if w.BundleRewriter != nil {
				if err := w.BundleRewriter.RewriteBundle(&spec); err != nil {
					return err
				}
			}
			if w.ProcessRewriter != nil && spec.Process != nil {
				if err := w.ProcessRewriter.RewriteProcess(spec.Process); err != nil {
					return err
				}
			}
			out, err := json.MarshalIndent(&spec, "", "  ")
			if err != nil {
				return fmt.Errorf("marshal bundle: %w", err)
			}
			if err := os.WriteFile(cfg, out, 0o644); err != nil {
				return fmt.Errorf("write bundle: %w", err)
			}
		}
	}

	// Process rewriting for exec commands.
	var tmpProc string
	if w.ProcessRewriter != nil {
		switch {
		case cmds.Exec != nil:
			if err := w.rewriteExec(cmds.Exec, &tmpProc); err != nil {
				return err
			}
		}

		// TODO: rewrite process in bundle too
	}
	if tmpProc != "" {
		defer os.Remove(tmpProc)
	}

	v := reflect.ValueOf(cmds)

	var cmd cli.Command
	for i := 0; i < v.NumField(); i++ {
		f := v.Field(i)
		if f.IsNil() {
			continue
		}
		cmd = f.Interface().(cli.Command)
		break
	}

	ctx := context.Background()
	execCmd, err := w.Delegate.Command(ctx, cmd)
	if err != nil {
		return err
	}
	if execCmd.Stdin == nil {
		execCmd.Stdin = os.Stdin
	}
	if execCmd.Stdout == nil {
		execCmd.Stdout = os.Stdout
	}
	if execCmd.Stderr == nil {
		execCmd.Stderr = os.Stderr
	}

	fds, err := inheritedFDs()
	if err != nil {
		return err
	}
	maxFD := 2
	for _, fd := range fds {
		if fd > maxFD {
			maxFD = fd
		}
	}
	extra := make([]*os.File, maxFD-2)
	for _, fd := range fds {
		extra[fd-3] = os.NewFile(uintptr(fd), "")
	}
	if len(extra) > 0 {
		execCmd.ExtraFiles = extra
	}
	if err := execCmd.Start(); err != nil {
		for _, f := range extra {
			if f != nil {
				f.Close()
			}
		}
		return fmt.Errorf("start process: %w", err)
	}
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh)
	doneCh := make(chan struct{})
	go func() {
		defer close(doneCh)
		for s := range sigCh {
			_ = execCmd.Process.Signal(s)
		}
	}()
	defer func() {
		signal.Stop(sigCh)
		close(sigCh)
		<-doneCh
	}()
	for _, f := range extra {
		if f != nil {
			f.Close()
		}
	}
	if err := execCmd.Wait(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return exitErr
		}
		return fmt.Errorf("wait process: %w", err)
	}
	return nil
}

func (w *Wrapper) rewriteExec(c *Exec, tmpPath *string) error {
	if c.Process != "" {
		data, err := os.ReadFile(c.Process)
		if err != nil {
			return fmt.Errorf("read process: %w", err)
		}
		var p specs.Process
		if err := json.Unmarshal(data, &p); err != nil {
			return fmt.Errorf("unmarshal process: %w", err)
		}
		if err := w.ProcessRewriter.RewriteProcess(&p); err != nil {
			return err
		}
		out, err := json.MarshalIndent(&p, "", "  ")
		if err != nil {
			return fmt.Errorf("marshal process: %w", err)
		}
		if err := os.WriteFile(c.Process, out, 0o644); err != nil {
			return fmt.Errorf("write process: %w", err)
		}
		return nil
	}

	p := specs.Process{
		Cwd:      c.Cwd,
		Env:      c.Env,
		Args:     append([]string{c.Command}, c.Args...),
		Terminal: c.Tty,
	}
	if c.User != "" {
		parts := strings.SplitN(c.User, ":", 2)
		if uid, err := strconv.ParseUint(parts[0], 10, 32); err == nil {
			p.User.UID = uint32(uid)
		}
		if len(parts) > 1 {
			if gid, err := strconv.ParseUint(parts[1], 10, 32); err == nil {
				p.User.GID = uint32(gid)
			}
		}
	}
	if len(c.AdditionalGids) > 0 {
		p.User.AdditionalGids = make([]uint32, len(c.AdditionalGids))
		for i, g := range c.AdditionalGids {
			p.User.AdditionalGids[i] = uint32(g)
		}
	}
	if err := w.ProcessRewriter.RewriteProcess(&p); err != nil {
		return err
	}
	f, err := os.CreateTemp("", "process-*.json")
	if err != nil {
		return err
	}
	enc, err := json.MarshalIndent(&p, "", "  ")
	if err != nil {
		f.Close()
		os.Remove(f.Name())
		return err
	}
	if _, err := f.Write(enc); err != nil {
		f.Close()
		os.Remove(f.Name())
		return err
	}
	if err := f.Close(); err != nil {
		os.Remove(f.Name())
		return err
	}
	c.Process = f.Name()
	*tmpPath = f.Name()
	return nil
}

func inheritedFDs() ([]int, error) {
	dir, err := os.Open("/proc/self/fd")
	if err != nil {
		return nil, err
	}
	defer dir.Close()

	dirFD := int(dir.Fd())
	entries, err := dir.ReadDir(-1)
	if err != nil {
		return nil, err
	}
	var fds []int
	for _, e := range entries {
		fd, err := strconv.Atoi(e.Name())
		if err != nil || fd < 3 || fd == dirFD {
			continue
		}

		flags, err := unix.FcntlInt(uintptr(fd), unix.F_GETFD, 0)
		if err != nil {
			continue
		}
		if flags&unix.FD_CLOEXEC != 0 {
			continue
		}

		if link, err := os.Readlink(filepath.Join("/proc/self/fd", e.Name())); err == nil {
			if link == "anon_inode:[eventpoll]" || strings.HasPrefix(link, "pipe:") {
				continue
			}
		}

		fds = append(fds, fd)
	}
	return fds, nil
}
