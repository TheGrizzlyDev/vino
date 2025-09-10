package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"reflect"
	"strings"

	"github.com/TheGrizzlyDev/vino/internal/pkg/cli"
	"github.com/TheGrizzlyDev/vino/internal/pkg/runc"
	"github.com/TheGrizzlyDev/vino/internal/pkg/vino"
	"github.com/TheGrizzlyDev/vino/internal/pkg/vino/hook"
	"github.com/TheGrizzlyDev/vino/internal/pkg/vino/labels"
	"github.com/opencontainers/runtime-spec/specs-go"
)

const (
	HOOK_SUBCOMMAND          = "oci-runtime-hook"
	RUNC_SUBCOMMAND          = "runc"
	WINE_LAUNCHER_SUBCOMMAND = "wine-launcher"
	WINE_LAUNCHER_PATH       = "/run/wine-launcher"
)

type VinoOptions struct {
	DelegatePath string `cli_flag:"--delegate_path" cli_group:"delegate"`
}

type VinoRuncCommand[T cli.Command] struct {
	Command T           `cli_embed:""`
	Options VinoOptions `cli_embed:""`
}

func (d VinoRuncCommand[T]) Slots() cli.Slot {
	return cli.Group{
		Unordered: []cli.Slot{
			cli.FlagGroup{Name: "delegate"},
		},
		Ordered: []cli.Slot{
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
	case RUNC_SUBCOMMAND:
		if err := RuncMain(os.Args[2:]); err != nil {
			panic(err)
		}
	case HOOK_SUBCOMMAND:
		if err := HookMain(os.Args[2:]); err != nil {
			panic(err)
		}
	case WINE_LAUNCHER_SUBCOMMAND:
		RunWine(os.Args[2:])
	default:
		panic("this subcommand isn't currently supported")
	}
}

func RuncMain(args []string) error {
	cmds := RuncCommands{}
	if err := cli.ParseAny(&cmds, os.Args[2:]); err != nil {
		return fmt.Errorf("failed to parse args: %w", err)
	}

	var (
		cmd  cli.Command
		opts VinoOptions
	)

	v := reflect.ValueOf(cmds)
	for i := 0; i < v.NumField(); i++ {
		f := v.Field(i)
		if f.IsNil() {
			continue
		}
		opts = f.Elem().FieldByName("Options").Interface().(VinoOptions)
		cmd = f.Elem().FieldByName("Command").Interface().(cli.Command)
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
		HookPath:                executablePath,
		CreateContainerHookArgs: []string{HOOK_SUBCOMMAND, "create"},
		StartContainerHookArgs:  []string{HOOK_SUBCOMMAND, "start"},
		RebindPaths: map[string]string{
			executablePath: WINE_LAUNCHER_PATH,
		},
	}
	processRewriter := &vino.ProcessRewriter{
		WineLauncherPath: WINE_LAUNCHER_PATH,
		WineLauncherArgs: []string{WINE_LAUNCHER_SUBCOMMAND},
	}

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

func HookMain(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("no hook subcommand")
	}
	f, err := os.OpenFile("/var/log/vino-hook.log", os.O_RDWR|os.O_CREATE|os.O_APPEND, 0666)
	if err != nil {
		return fmt.Errorf("error opening log file: %v", err)
	}
	defer f.Close()
	log.SetOutput(f)

	var state specs.State
	if err := json.NewDecoder(os.Stdin).Decode(&state); err != nil {
		return fmt.Errorf("decode state: %w", err)
	}

	devs, mounts, err := labels.Parse(state.Annotations)
	if err != nil {
		return fmt.Errorf("parse annotations: %w", err)
	}

	hookEnv, err := hook.FromEnvironment()
	if err != nil {
		return err
	}

	switch args[0] {
	case "start":
		if err = hookEnv.ApplyDevices(devs); err != nil {
			return err
		}
		if err = hookEnv.ApplyMounts(mounts); err != nil {
			return err
		}
	}
	return nil
}

func RunWine(args []string) (int, error) {
	wine := "wine64"
	switch strings.ToLower(os.Getenv("WINEARCH")) {
	case "win32":
		wine = "wine"
	case "win64":
		wine = "wine64"
	}

	_, display := os.LookupEnv("DISPLAY")
	_, xdg := os.LookupEnv("XDG_RUNTIME_DIR")

	bin := wine
	if !(display || xdg) {
		bin = "xvfb-run"
		args = append([]string{"-a", wine}, args...)
	}

	cmd := exec.Command(bin, args...)
	cmd.Env = os.Environ()
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	err := cmd.Run()
	if err == nil {
		return 0, nil
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		if status, ok := ee.ProcessState.Sys().(interface{ ExitStatus() int }); ok {
			return status.ExitStatus(), ee
		}
		return 1, ee
	}
	return 127, err
}
