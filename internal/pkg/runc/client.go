package runc

import (
	"context"
	"fmt"
	"os/exec"
)

type Command interface {
	Subcommand() string
	Groups() []string
}

type RunResult struct {
	Stdout   []byte
	Stderr   []byte
	ExitCode int
}

type Cli interface {
	private()
	Command(ctx context.Context, cmd Command) (*exec.Cmd, error)
}

func NewDelegatingCliClient(delegatePath string) (Cli, error) {
	if delegatePath == "" {
		return nil, fmt.Errorf("delegatingCliClient.Command: empty delegate path")
	}

	return &delegatingCliClient{delegate: delegatePath}, nil
}

type delegatingCliClient struct {
	delegate string
}

func (d *delegatingCliClient) private() {}

func (c *delegatingCliClient) Command(ctx context.Context, cmd Command) (*exec.Cmd, error) {
	args, err := convertToCmdline(cmd)
	if err != nil {
		return nil, err
	}
	return exec.CommandContext(ctx, c.delegate, args...), nil
}
