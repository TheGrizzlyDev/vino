package dind

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/docker/docker/pkg/stdcopy"
	tc "github.com/testcontainers/testcontainers-go"
	tcexec "github.com/testcontainers/testcontainers-go/exec"
)

// LogDelegatecLogs logs the contents of delegatec.log from the container.
func LogDelegatecLogs(t *testing.T, ctx context.Context, cont tc.Container) {
	t.Helper()
	code, reader, err := cont.Exec(ctx, []string{"cat", "/var/log/delegatec.log"})
	if err != nil {
		t.Logf("failed to read delegatec.log: %v", err)
		return
	}
	if code != 0 {
		t.Logf("failed to read delegatec.log: exit code %d", code)
		return
	}
	out, _ := io.ReadAll(reader)
	t.Logf("--- delegatec.log ---\n%s\n--------------------", string(out))
}

// LogRuncLogs logs the runc logs from the container.
func LogRuncLogs(t *testing.T, ctx context.Context, cont tc.Container) {
	t.Helper()
	cmd := []string{"sh", "-c", "find /var/run/docker/containerd/daemon -name log.json -exec cat {} +"}
	code, reader, err := cont.Exec(ctx, cmd)
	if err != nil {
		t.Logf("failed to read runc log: %v", err)
		return
	}
	if code != 0 {
		t.Logf("failed to read runc log: exit code %d", code)
		return
	}
	out, _ := io.ReadAll(reader)
	if len(out) == 0 {
		t.Log("runc log empty")
		return
	}
	t.Logf("--- runc log ---\n%s\n----------------", string(out))
}

// RunDocker executes a docker command inside the container using the specified runtime.
func RunDocker(ctx context.Context, cont tc.Container, runtime string, args ...string) (int, string, error) {
	cmd := []string{"docker", "run", "--rm", "-q"}
	if runtime != "" {
		cmd = append(cmd, "--runtime", runtime)
	}
	cmd = append(cmd, args...)
	code, reader, err := cont.Exec(ctx, cmd, tcexec.Multiplexed())
	if err != nil {
		return code, "", err
	}
	out, err := io.ReadAll(reader)
	return code, string(out), err
}

// ExecNoOutput executes a command inside the container and collects stdout and stderr.
func ExecNoOutput(ctx context.Context, cont tc.Container, args ...string) (int, string, string, error) {
	code, reader, err := cont.Exec(ctx, args, tcexec.Multiplexed())
	var stdout, stderr bytes.Buffer
	if reader != nil {
		stdcopy.StdCopy(&stdout, &stderr, reader)
	}
	if err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = strings.TrimSpace(stdout.String())
		}
		if msg != "" {
			err = fmt.Errorf("%s: %w", msg, err)
		}
		return code, stdout.String(), stderr.String(), err
	}
	if code != 0 {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = strings.TrimSpace(stdout.String())
		}
		if msg == "" {
			msg = fmt.Sprintf("exit code %d", code)
		}
		return code, stdout.String(), stderr.String(), fmt.Errorf("%s", msg)
	}
	return code, stdout.String(), stderr.String(), nil
}
