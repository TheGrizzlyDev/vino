package main

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"reflect"

	"github.com/TheGrizzlyDev/vino/internal/pkg/runc"
)

type DelegatecCmd[T runc.Command] struct {
	Command      T      `runc_embed:""`
	DelegatePath string `runc_flag:"--delegate_path" runc_group:"delegate"`
}

func (d *DelegatecCmd[T]) Subcommand() string {
	return d.Command.Subcommand()
}

func (d *DelegatecCmd[T]) Groups() []string {
	return append([]string{"delegate"}, d.Command.Groups()...)
}

type Commands struct {
	Checkpoint *DelegatecCmd[runc.Checkpoint]
	Restore    *DelegatecCmd[runc.Restore]
	Create     *DelegatecCmd[runc.Create]
	Run        *DelegatecCmd[runc.Run]
	Start      *DelegatecCmd[runc.Start]
	Delete     *DelegatecCmd[runc.Delete]
	Pause      *DelegatecCmd[runc.Pause]
	Resume     *DelegatecCmd[runc.Resume]
	Kill       *DelegatecCmd[runc.Kill]
	List       *DelegatecCmd[runc.List]
	Ps         *DelegatecCmd[runc.Ps]
	State      *DelegatecCmd[runc.State]
	Events     *DelegatecCmd[runc.Events]
	Exec       *DelegatecCmd[runc.Exec]
	Spec       *DelegatecCmd[runc.Spec]
	Update     *DelegatecCmd[runc.Update]
	Features   *DelegatecCmd[runc.Features]
}

func main() {
	f, err := os.OpenFile("/var/log/delegatec.log", os.O_RDWR|os.O_CREATE|os.O_APPEND, 0666)
	if err != nil {
		log.Fatalf("error opening file: %v", err)
	}
	defer f.Close()
	log.SetOutput(f)

	log.Printf("delegatec called with args: %v\n", os.Args)
	log.Printf("delegatec environment: %v\n", os.Environ())

	cmds := Commands{}
	if err := runc.ParseAny(&cmds, os.Args[1:]); err != nil {
		log.Printf("failed to parse args: %v\nenv: %v", err, os.Environ())
		fmt.Fprintf(os.Stderr, "failed to parse args: %v\nenv: %v", err, os.Environ())
		os.Exit(1)
	}

	var (
		cmd          runc.Command
		delegatePath string
	)

	v := reflect.ValueOf(cmds)
	for i := 0; i < v.NumField(); i++ {
		f := v.Field(i)
		if f.IsNil() {
			continue
		}
		delegatePath = f.Elem().FieldByName("DelegatePath").String()
		cmdIface := f.Elem().FieldByName("Command").Interface()
		cmd = cmdIface.(runc.Command)
		break
	}

	log.Printf("delegatec parsed command: %#v\n", cmd)
	log.Printf("delegatec delegate path: %s\n", delegatePath)

	if cmd == nil {
		fmt.Fprintln(os.Stderr, "no command specified")
		os.Exit(1)
	}

	cli, err := runc.NewDelegatingCliClient(delegatePath)
	if err != nil {
		log.Printf("failed to create delegating client: %v\nenv: %v", err, os.Environ())
		fmt.Fprintf(os.Stderr, "failed to create delegating client: %v\nenv: %v", err, os.Environ())
		os.Exit(1)
	}

	execCmd, err := cli.Command(context.Background(), cmd)
	if err != nil {
		log.Printf("failed to create command: %v\nenv: %v", err, os.Environ())
		fmt.Fprintf(os.Stderr, "failed to create command: %v\nenv: %v", err, os.Environ())
		os.Exit(1)
	}

	log.Printf("executing command: %s %v\n", execCmd.Path, execCmd.Args)

	var stdoutBuf, stderrBuf bytes.Buffer
	execCmd.Stdout = &stdoutBuf
	execCmd.Stderr = &stderrBuf

	if err := execCmd.Run(); err != nil {
		log.Printf("runc stdout: %s\n", stdoutBuf.String())
		log.Printf("runc stderr: %s\n", stderrBuf.String())

		os.Stdout.Write(stdoutBuf.Bytes())
		os.Stderr.Write(stderrBuf.Bytes())

		if exitErr, ok := err.(*exec.ExitError); ok {
			os.Exit(exitErr.ExitCode())
		}
		log.Printf("command execution failed: %v\nenv: %v", err, os.Environ())
		fmt.Fprintf(os.Stderr, "command execution failed: %v\nenv: %v", err, os.Environ())
		os.Exit(1)
	}

	os.Stdout.Write(stdoutBuf.Bytes())
	os.Stderr.Write(stderrBuf.Bytes())

	if execCmd.ProcessState != nil {
		os.Exit(execCmd.ProcessState.ExitCode())
	}
}
