package runc

import (
	"context"
	"fmt"
	"os/exec"
)

type Command interface {
    Slots() Slot
}

// subcommandOf walks a command's Slots() and returns the Subcommand value.
// Returns an empty string if none found (invalid).
func subcommandOf(cmd Command) string {
    var find func(Slot) (string, bool)
    find = func(s Slot) (string, bool) {
        switch v := s.(type) {
        case Subcommand:
            return v.Value, true
        case Group:
            for _, o := range v.Ordered {
                if name, ok := find(o); ok {
                    return name, true
                }
            }
        }
        return "", false
    }
    if cmd == nil {
        return ""
    }
    if name, ok := find(cmd.Slots()); ok {
        return name
    }
    return ""
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
