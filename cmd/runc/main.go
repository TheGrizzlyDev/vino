package main

import (
	"context"
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
	}
}
