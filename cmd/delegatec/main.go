package main

import (
	"context"
	"fmt"
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
}

func main() {
	cmds := Commands{}
	if err := runc.ParseAny(&cmds, os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
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

	if cmd == nil {
		fmt.Fprintln(os.Stderr, "no command specified")
		os.Exit(1)
	}

	cli, err := runc.NewDelegatingCliClient(delegatePath)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	execCmd, err := cli.Command(context.Background(), cmd)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	execCmd.Stdin = os.Stdin
	execCmd.Stdout = os.Stdout
	execCmd.Stderr = os.Stderr

	if err := execCmd.Run(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			os.Exit(exitErr.ExitCode())
		}
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	if execCmd.ProcessState != nil {
		os.Exit(execCmd.ProcessState.ExitCode())
	}
}
