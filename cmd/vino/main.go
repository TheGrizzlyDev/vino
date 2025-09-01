package main

import (
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"reflect"

	"github.com/TheGrizzlyDev/vino/internal/pkg/runc"
	"github.com/TheGrizzlyDev/vino/internal/pkg/vino"
)

const (
	HOOK_SUBCOMMAND = "vino-mount-devices-hook"
)

type VinoOptions struct {
	DelegatePath string `runc_flag:"--delegate_path" runc_group:"delegate"`
}

type VinoRuncCommand[T runc.Command] struct {
	Command T           `runc_embed:""`
	Options VinoOptions `runc_embed:""`
}

func (d VinoRuncCommand[T]) Slots() runc.Slot {
	return runc.Group{
		Unordered: []runc.Slot{
			runc.FlagGroup{Name: "delegate"},
		},
		Ordered: []runc.Slot{
			d.Command.Slots(),
		},
	}
}

type RuncCommands struct {
	Checkpoint *VinoRuncCommand[runc.Checkpoint]
	Restore    *VinoRuncCommand[runc.Restore]
	Create     *VinoRuncCommand[runc.Create]
	Run        *VinoRuncCommand[runc.Run]
	Start      *VinoRuncCommand[runc.Start]
	Delete     *VinoRuncCommand[runc.Delete]
	Pause      *VinoRuncCommand[runc.Pause]
	Resume     *VinoRuncCommand[runc.Resume]
	Kill       *VinoRuncCommand[runc.Kill]
	List       *VinoRuncCommand[runc.List]
	Ps         *VinoRuncCommand[runc.Ps]
	State      *VinoRuncCommand[runc.State]
	Events     *VinoRuncCommand[runc.Events]
	Exec       *VinoRuncCommand[runc.Exec]
	Spec       *VinoRuncCommand[runc.Spec]
	Update     *VinoRuncCommand[runc.Update]
	Features   *VinoRuncCommand[runc.Features]
}

func main() {
	fmt.Println(os.Args, os.Args[2:])
	if len(os.Args) < 2 {
		panic("you need to specify a subcommand")
	}
	switch os.Args[1] {
	case "runc":
		if err := RuncMain(os.Args[2:]); err != nil {
			panic(err)
		}
	case HOOK_SUBCOMMAND:
		log.Println("empty hook: not implemented")
	default:
		panic("this subcommand isn't currently supported")
	}
}

func RuncMain(args []string) error {
	cmds := RuncCommands{}
	if err := runc.ParseAny(&cmds, os.Args[2:]); err != nil {
		return fmt.Errorf("failed to parse args: %w", err)
	}

	var (
		cmd  runc.Command
		opts VinoOptions
	)

	v := reflect.ValueOf(cmds)
	for i := 0; i < v.NumField(); i++ {
		f := v.Field(i)
		if f.IsNil() {
			continue
		}
		opts = f.Elem().FieldByName("Options").Interface().(VinoOptions)
		cmd = f.Elem().FieldByName("Command").Interface().(runc.Command)
		break
	}

	if cmd == nil {
		return fmt.Errorf("no command specified")
	}

	cli, err := runc.NewDelegatingCliClient(opts.DelegatePath, runc.InheritStdin)
	if err != nil {
		return fmt.Errorf("failed to create delegating client: %w", err)
	}

	executablePath, err := os.Executable()

	if err != nil {
		return err
	}

	bundleRewriter := &vino.BundleRewriter{
		HookPath: executablePath,
		HookArgs: []string{HOOK_SUBCOMMAND},
	}
	processRewriter := &vino.ProcessRewriter{}

	w := runc.Wrapper{
		BundleRewriter:  bundleRewriter,
		ProcessRewriter: processRewriter,
		Delegate:        cli,
	}
	if err := w.Run(cmd); err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			os.Exit(ee.ExitCode())
		}
		return fmt.Errorf("command run failed: %w", err)
	}

	return nil
}
