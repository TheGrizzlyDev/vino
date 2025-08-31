package runc

import (
	"context"
	"fmt"
	"os"
	"os/exec"
)

type Command interface {
	Slots() Slot
}

// Forward represents the next command construction step in a middleware chain.
// Implementations are expected to return an *exec.Cmd ready for execution.
type Forward func(ctx context.Context, cmd Command) (*exec.Cmd, error)

// Middleware allows wrapping of command construction logic. Each middleware
// receives the next Forward in the chain and returns a new Forward that may
// inspect or modify the exec.Cmd before it's returned to the caller.
type Middleware func(next Forward) Forward

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

func NewDelegatingCliClient(delegatePath string, middleware ...Middleware) (Cli, error) {
	if delegatePath == "" {
		return nil, fmt.Errorf("delegatingCliClient.Command: empty delegate path")
	}

	return &delegatingCliClient{delegate: delegatePath, middleware: middleware}, nil
}

type delegatingCliClient struct {
	delegate   string
	middleware []Middleware
}

func (d *delegatingCliClient) private() {}

func (c *delegatingCliClient) Command(ctx context.Context, cmd Command) (*exec.Cmd, error) {
	forward := func(ctx context.Context, cmd Command) (*exec.Cmd, error) {
		args, err := convertToCmdline(cmd)
		if err != nil {
			return nil, err
		}
		return exec.CommandContext(ctx, c.delegate, args...), nil
	}
	for i := len(c.middleware) - 1; i >= 0; i-- {
		forward = c.middleware[i](forward)
	}
	return forward(ctx, cmd)
}

func requiresStdin(cmd Command) bool {
	switch cmd.(type) {
	case Run, *Run,
		Exec, *Exec,
		Restore, *Restore,
		Update, *Update:
		return true
	default:
		return false
	}
}

func InheritStdin(next Forward) Forward {
	return func(ctx context.Context, cmd Command) (*exec.Cmd, error) {
		execCmd, err := next(ctx, cmd)
		if err != nil {
			return nil, err
		}
		if requiresStdin(cmd) {
			execCmd.Stdin = os.Stdin
		}
		return execCmd, nil
	}
}

// Only returns a middleware that invokes mw only when the command's active
// subcommand matches the provided name.
func Only(subcmd string, mw Middleware) Middleware {
	return func(next Forward) Forward {
		wrapped := mw(next)
		return func(ctx context.Context, cmd Command) (*exec.Cmd, error) {
			if subcommandOf(cmd) == subcmd {
				return wrapped(ctx, cmd)
			}
			return next(ctx, cmd)
		}
	}
}
