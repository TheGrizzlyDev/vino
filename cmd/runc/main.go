package main

import (
	"context"
<<<<<<< ours
	"fmt"
	"os"
	"os/exec"

	runccli "github.com/TheGrizzlyDev/vino/internal/pkg/runc"
)

func main() {
	cmd, err := runccli.Parse(os.Args[1:])
	if err != nil {
		fmt.Fprintf(os.Stderr, "parse error: %v\n", err)
		os.Exit(1)
	}

	cli, err := runccli.NewDelegatingCliClient("runc")
	if err != nil {
		fmt.Fprintf(os.Stderr, "client error: %v\n", err)
		os.Exit(1)
	}

	execCmd, err := cli.Command(context.Background(), cmd)
	if err != nil {
		fmt.Fprintf(os.Stderr, "command error: %v\n", err)
		os.Exit(1)
	}
	execCmd.Stdin = os.Stdin
	execCmd.Stdout = os.Stdout
	execCmd.Stderr = os.Stderr

	if err := execCmd.Run(); err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			os.Exit(ee.ExitCode())
		}
		fmt.Fprintf(os.Stderr, "run error: %v\n", err)
		os.Exit(1)
=======
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"

	runcpkg "github.com/TheGrizzlyDev/vino/internal/pkg/runc"
)

func runCommand(runtime string, cmd runcpkg.Command) error {
	cli, err := runcpkg.NewDelegatingCliClient(runtime)
	if err != nil {
		return fmt.Errorf("create client: %w", err)
	}
	ecmd, err := cli.Command(context.Background(), cmd)
	if err != nil {
		return fmt.Errorf("build command: %w", err)
	}
	ecmd.Stdin = os.Stdin
	ecmd.Stdout = os.Stdout
	ecmd.Stderr = os.Stderr
	return ecmd.Run()
}

func checkpointMain(runtime string, args []string) error {
	var c runcpkg.Checkpoint
	if err := runcpkg.Parse(&c, args); err != nil {
		return fmt.Errorf("parse: %w", err)
	}
	return runCommand(runtime, c)
}

func restoreMain(runtime string, args []string) error {
	var c runcpkg.Restore
	if err := runcpkg.Parse(&c, args); err != nil {
		return fmt.Errorf("parse: %w", err)
	}
	return runCommand(runtime, c)
}

func createMain(runtime string, args []string) error {
	var c runcpkg.Create
	if err := runcpkg.Parse(&c, args); err != nil {
		return fmt.Errorf("parse: %w", err)
	}
	return runCommand(runtime, c)
}

func runMain(runtime string, args []string) error {
	var c runcpkg.Run
	if err := runcpkg.Parse(&c, args); err != nil {
		return fmt.Errorf("parse: %w", err)
	}
	return runCommand(runtime, c)
}

func startMain(runtime string, args []string) error {
	var c runcpkg.Start
	if err := runcpkg.Parse(&c, args); err != nil {
		return fmt.Errorf("parse: %w", err)
	}
	return runCommand(runtime, c)
}

func deleteMain(runtime string, args []string) error {
	var c runcpkg.Delete
	if err := runcpkg.Parse(&c, args); err != nil {
		return fmt.Errorf("parse: %w", err)
	}
	return runCommand(runtime, c)
}

func pauseMain(runtime string, args []string) error {
	var c runcpkg.Pause
	if err := runcpkg.Parse(&c, args); err != nil {
		return fmt.Errorf("parse: %w", err)
	}
	return runCommand(runtime, c)
}

func resumeMain(runtime string, args []string) error {
	var c runcpkg.Resume
	if err := runcpkg.Parse(&c, args); err != nil {
		return fmt.Errorf("parse: %w", err)
	}
	return runCommand(runtime, c)
}

func killMain(runtime string, args []string) error {
	var c runcpkg.Kill
	if err := runcpkg.Parse(&c, args); err != nil {
		return fmt.Errorf("parse: %w", err)
	}
	return runCommand(runtime, c)
}

func listMain(runtime string, args []string) error {
	var c runcpkg.List
	if err := runcpkg.Parse(&c, args); err != nil {
		return fmt.Errorf("parse: %w", err)
	}
	return runCommand(runtime, c)
}

func psMain(runtime string, args []string) error {
	var c runcpkg.Ps
	if err := runcpkg.Parse(&c, args); err != nil {
		return fmt.Errorf("parse: %w", err)
	}
	return runCommand(runtime, c)
}

func stateMain(runtime string, args []string) error {
	var c runcpkg.State
	if err := runcpkg.Parse(&c, args); err != nil {
		return fmt.Errorf("parse: %w", err)
	}
	return runCommand(runtime, c)
}

func eventsMain(runtime string, args []string) error {
	var c runcpkg.Events
	if err := runcpkg.Parse(&c, args); err != nil {
		return fmt.Errorf("parse: %w", err)
	}
	return runCommand(runtime, c)
}

func execMain(runtime string, args []string) error {
	var c runcpkg.Exec
	if err := runcpkg.Parse(&c, args); err != nil {
		return fmt.Errorf("parse: %w", err)
	}
	return runCommand(runtime, c)
}

func specMain(runtime string, args []string) error {
	var c runcpkg.Spec
	if err := runcpkg.Parse(&c, args); err != nil {
		return fmt.Errorf("parse: %w", err)
	}
	return runCommand(runtime, c)
}

func updateMain(runtime string, args []string) error {
	var c runcpkg.Update
	if err := runcpkg.Parse(&c, args); err != nil {
		return fmt.Errorf("parse: %w", err)
	}
	return runCommand(runtime, c)
}

func main() {
	runtimePath := flag.String("runtime", "runc", "path to runc compatible binary")
	flag.Parse()

	if flag.NArg() < 1 {
		log.Fatalf("usage: %s [flags] <subcommand> [args...]", os.Args[0])
	}

	subcmd := flag.Arg(0)
	args := flag.Args()[1:]

	var err error
	switch subcmd {
	case "checkpoint":
		err = checkpointMain(*runtimePath, args)
	case "restore":
		err = restoreMain(*runtimePath, args)
	case "create":
		err = createMain(*runtimePath, args)
	case "run":
		err = runMain(*runtimePath, args)
	case "start":
		err = startMain(*runtimePath, args)
	case "delete":
		err = deleteMain(*runtimePath, args)
	case "pause":
		err = pauseMain(*runtimePath, args)
	case "resume":
		err = resumeMain(*runtimePath, args)
	case "kill":
		err = killMain(*runtimePath, args)
	case "list":
		err = listMain(*runtimePath, args)
	case "ps":
		err = psMain(*runtimePath, args)
	case "state":
		err = stateMain(*runtimePath, args)
	case "events":
		err = eventsMain(*runtimePath, args)
	case "exec":
		err = execMain(*runtimePath, args)
	case "spec":
		err = specMain(*runtimePath, args)
	case "update":
		err = updateMain(*runtimePath, args)
	default:
		log.Fatalf("unknown subcommand %q", subcmd)
	}

	if err != nil {
		if ee, ok := err.(*exec.Error); ok {
			log.Fatalf("exec error: %v", ee)
		}
		if exitErr, ok := err.(*exec.ExitError); ok {
			os.Exit(exitErr.ExitCode())
		}
		log.Fatalf("%s: %v", subcmd, err)
>>>>>>> theirs
	}
}
