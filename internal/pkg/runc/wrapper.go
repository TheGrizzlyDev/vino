package runc

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	specs "github.com/opencontainers/runtime-spec/specs-go"
)

type BundleRewriter interface {
	RewriteBundle(*specs.Spec) error
}

type ProcessRewriter interface {
	RewriteProcess(*specs.Process) error
}

type Wrapper struct {
	BundleRewriter  *BundleRewriter
	ProcessRewriter *ProcessRewriter
	Delegate        Cli
}

func (w *Wrapper) Run(cmd Command) error {
	if w.Delegate == nil {
		return fmt.Errorf("wrapper: nil delegate")
	}

	// Bundle rewriting for commands that reference a bundle.
	if w.BundleRewriter != nil || w.ProcessRewriter != nil {
		var bundlePath string
		switch c := cmd.(type) {
		case Create:
			bundlePath = c.Bundle
		case *Create:
			bundlePath = c.Bundle
		case Run:
			bundlePath = c.Bundle
		case *Run:
			bundlePath = c.Bundle
		case Restore:
			bundlePath = c.Bundle
		case *Restore:
			bundlePath = c.Bundle
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
				if err := (*w.BundleRewriter).RewriteBundle(&spec); err != nil {
					return err
				}
			}
			if w.ProcessRewriter != nil && spec.Process != nil {
				if err := (*w.ProcessRewriter).RewriteProcess(spec.Process); err != nil {
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
		switch c := cmd.(type) {
		case Exec:
			execCopy := c
			if err := w.rewriteExec(&execCopy, &tmpProc); err != nil {
				return err
			}
			cmd = execCopy
		case *Exec:
			if err := w.rewriteExec(c, &tmpProc); err != nil {
				return err
			}
		}
	}
	if tmpProc != "" {
		defer os.Remove(tmpProc)
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
	files := make([]*os.File, maxFD+1)
	files[0] = os.Stdin
	files[1] = os.Stdout
	files[2] = os.Stderr
	for _, fd := range fds {
		files[fd] = os.NewFile(uintptr(fd), "")
	}

	attr := &os.ProcAttr{Files: files, Dir: execCmd.Dir, Env: execCmd.Env}
	if attr.Env == nil {
		attr.Env = os.Environ()
	}
	if execCmd.SysProcAttr != nil {
		attr.Sys = execCmd.SysProcAttr
	}
	proc, err := os.StartProcess(execCmd.Path, execCmd.Args, attr)
	if err != nil {
		return fmt.Errorf("start process: %w", err)
	}
	for _, fd := range fds {
		files[fd].Close()
	}
	state, err := proc.Wait()
	if err != nil {
		return fmt.Errorf("wait process: %w", err)
	}
	if code := state.ExitCode(); code != 0 {
		return &exec.ExitError{ProcessState: state}
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
		if err := (*w.ProcessRewriter).RewriteProcess(&p); err != nil {
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
	if err := (*w.ProcessRewriter).RewriteProcess(&p); err != nil {
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
		fds = append(fds, fd)
	}
	return fds, nil
}
