package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strings"

	"github.com/TheGrizzlyDev/vino/internal/pkg/cli"
	"github.com/TheGrizzlyDev/vino/internal/pkg/runc"
	"github.com/TheGrizzlyDev/vino/internal/pkg/vino"
	"github.com/TheGrizzlyDev/vino/internal/pkg/vino/hook"
	"github.com/TheGrizzlyDev/vino/internal/pkg/vino/labels"
	"github.com/opencontainers/runtime-spec/specs-go"
)

const (
	VINO_AFTER_PIVOT_PATH = "/run/vino"
)

var (
	vinoHookCreateLogPath = "/var/log/vino-hook-create.log"
	vinoHookStartLogPath  = "/var/log/vino-hook-start.log"
	wineLauncherLogPath   = "/var/log/wine-launcher.log"
)

type CommonCommand struct {
	VinocLogPath *string  `cli_flag:"--vinoc_log_path" cli_group:"common"`
	VinoArgs     []string `cli_argument:"args"`
}

func (CommonCommand) Slots() cli.Slot {
	return cli.Group{
		Unordered: []cli.Slot{
			cli.FlagGroup{Name: "common"},
			cli.Arguments{Name: "args"},
		},
	}
}

type RuncCommand struct {
	DelegatePath string   `cli_flag:"--delegate_path" cli_group:"vinoc"`
	RuncArgs     []string `cli_argument:"args"`
}

func (RuncCommand) Slots() cli.Slot {
	return cli.Group{
		Ordered: []cli.Slot{
			cli.Subcommand{Value: "runc"},
			cli.FlagGroup{Name: "vinoc"},
			cli.Arguments{Name: "args"},
		},
	}
}

type HookCommand struct {
	HookArgs []string `cli_argument:"args"`
}

func (HookCommand) Slots() cli.Slot {
	return cli.Group{
		Unordered: []cli.Slot{},
		Ordered: []cli.Slot{
			cli.Subcommand{Value: "oci-runtime-hook"},
			cli.Arguments{Name: "args"},
		},
	}
}

type HookCreateCommand struct{}

func (HookCreateCommand) Slots() cli.Slot {
	return cli.Group{
		Ordered: []cli.Slot{
			cli.Subcommand{Value: "create"},
		},
	}
}

type HookStartCommand struct{}

func (HookStartCommand) Slots() cli.Slot {
	return cli.Group{
		Ordered: []cli.Slot{
			cli.Subcommand{Value: "start"},
		},
	}
}

type HookCommands struct {
	Create *HookCreateCommand
	Start  *HookStartCommand
}

type WineLauncherCommand struct {
	Args []string `cli_argument:"args"`
}

func (WineLauncherCommand) Slots() cli.Slot {
	return cli.Group{
		Unordered: []cli.Slot{},
		Ordered: []cli.Slot{
			cli.Subcommand{Value: "wine-launcher"},
			cli.Arguments{Name: "args"},
		},
	}
}

type VinocCommands struct {
	Runc     *RuncCommand
	Hook     *HookCommand
	Launcher *WineLauncherCommand
}

func main() {
	err := run(os.Args[1:])
	if err == nil {
		os.Exit(0)
	}

	log.Println(err)
	fmt.Println(err)

	var ee *exec.ExitError
	if errors.As(err, &ee) {
		if status, ok := ee.ProcessState.Sys().(interface{ ExitStatus() int }); ok {
			os.Exit(status.ExitStatus())
		}
	}
	os.Exit(1)
}

func run(args []string) error {
	var common CommonCommand
	if err := cli.Parse(&common, os.Args[1:]); err != nil {
		return err
	}
	if common.VinocLogPath != nil {
		f, err := os.OpenFile(*common.VinocLogPath, os.O_RDWR|os.O_CREATE|os.O_APPEND, 0666)
		if err != nil {
			return err
		}
		defer f.Close()
		log.SetOutput(f)
	}

	var vinocCommands VinocCommands
	if err := cli.ParseAny(&vinocCommands, common.VinoArgs); err != nil {
		return fmt.Errorf("cannot parse vino subcommand: %v", err)
	}

	switch {
	case vinocCommands.Hook != nil:
		return HookMain(*vinocCommands.Hook)
	case vinocCommands.Runc != nil:
		return RuncMain(*vinocCommands.Runc)
	case vinocCommands.Launcher != nil:
		return RunWine(*vinocCommands.Launcher)
	}

	return fmt.Errorf("subcommand not supported: %v", args)
}

func RuncMain(cmd RuncCommand) error {
	delegate, err := runc.NewDelegatingCliClient(cmd.DelegatePath, runc.InheritStdin)
	if err != nil {
		return fmt.Errorf("failed to create delegating client: %w", err)
	}

	executablePath, err := os.Executable()

	if err != nil {
		return err
	}

	hookStartArgs, err := cli.ConvertToCmdline(HookStartCommand{})
	if err != nil {
		return err
	}

	hookStartArgs, err = cli.ConvertToCmdline(HookCommand{HookArgs: hookStartArgs})
	if err != nil {
		return err
	}

	hookStartArgs, err = cli.ConvertToCmdline(CommonCommand{
		VinocLogPath: &vinoHookStartLogPath,
		VinoArgs:     hookStartArgs,
	})
	if err != nil {
		return err
	}

	hookCreateArgs, err := cli.ConvertToCmdline(HookCreateCommand{})
	if err != nil {
		return err
	}

	hookCreateArgs, err = cli.ConvertToCmdline(HookCommand{HookArgs: hookCreateArgs})
	if err != nil {
		return err
	}

	hookCreateArgs, err = cli.ConvertToCmdline(CommonCommand{
		VinocLogPath: &vinoHookCreateLogPath,
		VinoArgs:     hookCreateArgs,
	})
	if err != nil {
		return err
	}

	bundleRewriter := &vino.BundleRewriter{
		HookPathBeforePivot:     executablePath,
		HookPathAfterPivot:      VINO_AFTER_PIVOT_PATH,
		CreateContainerHookArgs: hookCreateArgs,
		StartContainerHookArgs:  hookStartArgs,
		RebindPaths: map[string]string{
			executablePath: VINO_AFTER_PIVOT_PATH,
		},
	}

	wineLauncherArgs, err := cli.ConvertToCmdline(WineLauncherCommand{})
	if err != nil {
		return err
	}

	wineLauncherArgs, err = cli.ConvertToCmdline(CommonCommand{
		VinocLogPath: &wineLauncherLogPath,
		VinoArgs:     wineLauncherArgs,
	})
	if err != nil {
		return err
	}

	processRewriter := &vino.ProcessRewriter{
		WineLauncherPath: VINO_AFTER_PIVOT_PATH,
		WineLauncherArgs: wineLauncherArgs,
	}

	w := runc.Wrapper{
		BundleRewriter:  bundleRewriter,
		ProcessRewriter: processRewriter,
		Delegate:        delegate,
	}

	if err := runc.RunWithArgs(&w, cmd.RuncArgs); err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return err
		}
		return fmt.Errorf("command run failed: %w", err)
	}

	return nil
}

func HookMain(cmd HookCommand) error {
	ctx := context.Background() // TODO move outside to main

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

	var hookCommands HookCommands
	if err := cli.ParseAny(&hookCommands, cmd.HookArgs); err != nil {
		return err
	}

	switch {
	case hookCommands.Start != nil:
		if err = hookEnv.ApplyDevices(devs); err != nil {
			return err
		}
		if err = hookEnv.ApplyMounts(mounts); err != nil {
			return err
		}

		if err := exec.CommandContext(ctx, "wineserver").Wait(); err != nil {
			return err
		}

		if err := exec.CommandContext(ctx, "wineboot").Wait(); err != nil {
			return err
		}
	}

	return nil
}

func RunWine(launcherCmd WineLauncherCommand) error {
	if strings.Index(launcherCmd.Args[0], "@") == 0 {
		// TODO: this code can be simplified a bit and merge most
		//       logic with the branch below
		bin := strings.TrimPrefix(launcherCmd.Args[0], "@")
		cmd := exec.Command(bin, launcherCmd.Args[1:]...)
		cmd.Env = os.Environ()
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr

		err := cmd.Run()
		if err != nil {
			return err
		}
		return nil
	}

	wine := "wine64"
	switch strings.ToLower(os.Getenv("WINEARCH")) {
	case "win32":
		wine = "wine"
	case "win64":
		wine = "wine64"
	}

	_, display := os.LookupEnv("DISPLAY")
	_, xdg := os.LookupEnv("XDG_RUNTIME_DIR")

	args := launcherCmd.Args
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
	if err != nil {
		return err
	}
	return nil
}
