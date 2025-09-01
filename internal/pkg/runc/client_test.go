package runc

import (
	"context"
	cli "github.com/TheGrizzlyDev/vino/internal/pkg/cli"
	"os"
	"os/exec"
	"testing"
)

// Test that middleware in the delegating client is invoked for every command.
func TestDelegatingCliClient_MiddlewareInvoked(t *testing.T) {
	var called bool
	mw := func(next Forward) Forward {
		return func(ctx context.Context, cmd cli.Command) (*exec.Cmd, error) {
			called = true
			return next(ctx, cmd)
		}
	}
	cli, err := NewDelegatingCliClient("runc", mw)
	if err != nil {
		t.Fatalf("NewDelegatingCliClient: %v", err)
	}
	if _, err := cli.Command(context.Background(), Spec{}); err != nil {
		t.Fatalf("Command: %v", err)
	}
	if !called {
		t.Fatalf("middleware not invoked")
	}
}

// Test Only wrapper scopes middleware to a particular subcommand.
func TestOnlyMiddleware(t *testing.T) {
	var count int
	base := func(next Forward) Forward {
		return func(ctx context.Context, cmd cli.Command) (*exec.Cmd, error) {
			count++
			return next(ctx, cmd)
		}
	}
	mw := Only("spec", base)
	cli, err := NewDelegatingCliClient("runc", mw)
	if err != nil {
		t.Fatalf("NewDelegatingCliClient: %v", err)
	}
	// spec command should trigger
	if _, err := cli.Command(context.Background(), Spec{}); err != nil {
		t.Fatalf("Command spec: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected middleware to run once, got %d", count)
	}
	// update command should not trigger
	if _, err := cli.Command(context.Background(), Update{}); err != nil {
		t.Fatalf("Command update: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected middleware not to run for update, got %d", count)
	}
}

// Test that middleware type switches narrow behavior to relevant commands.
func TestMiddlewareTypeSwitch(t *testing.T) {
	mw := func(next Forward) Forward {
		return func(ctx context.Context, cmd cli.Command) (*exec.Cmd, error) {
			execCmd, err := next(ctx, cmd)
			if err != nil {
				return nil, err
			}
			switch cmd.(type) {
			case Run, *Run,
				Exec, *Exec,
				Restore, *Restore,
				Update, *Update:
				execCmd.Stdin = os.Stdin
			}
			return execCmd, nil
		}
	}
	cli, err := NewDelegatingCliClient("runc", mw)
	if err != nil {
		t.Fatalf("NewDelegatingCliClient: %v", err)
	}
	// Run should receive stdin
	cmd, err := cli.Command(context.Background(), Run{})
	if err != nil {
		t.Fatalf("Command run: %v", err)
	}
	if cmd.Stdin != os.Stdin {
		t.Fatalf("expected stdin to be set for run")
	}
	// Spec should not
	cmd2, err := cli.Command(context.Background(), Spec{})
	if err != nil {
		t.Fatalf("Command spec: %v", err)
	}
	if cmd2.Stdin != nil {
		t.Fatalf("expected nil stdin for spec")
	}
}
