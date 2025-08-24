//go:build !e2e
// +build !e2e

package dind

import (
	"context"
	"fmt"
	"io"
	"path/filepath"
	"testing"
	"time"

	tc "github.com/testcontainers/testcontainers-go"
	tcexec "github.com/testcontainers/testcontainers-go/exec"
	"github.com/testcontainers/testcontainers-go/wait"
)

func logDelegatecLogs(t *testing.T, ctx context.Context, cont tc.Container) {
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

func logRuncLogs(t *testing.T, ctx context.Context, cont tc.Container) {
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

func TestRuntimeParity(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	// Build the image using the repository root as context.
	rootDir, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatalf("failed to get root dir: %v", err)
	}

	req := tc.ContainerRequest{
		FromDockerfile: tc.FromDockerfile{
			Context:       rootDir,
			Dockerfile:    "tests/integration/dind/Dockerfile",
			PrintBuildLog: true,
		},
		Privileged: true,
		WaitingFor: wait.ForLog("API listen on /var/run/docker.sock").WithStartupTimeout(2 * time.Minute),
	}

	cont, err := tc.GenericContainer(ctx, tc.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	if err != nil {
		if cont != nil {
			logReader, logErr := cont.Logs(ctx)
			if logErr == nil {
				out, _ := io.ReadAll(logReader)
				t.Logf("container logs:\n%s", string(out))
			}
		}
		t.Fatalf("failed to start container: %v", err)
	}
	defer func() {
		_ = cont.Terminate(ctx)
	}()

	// create a file to verify volume mounts
	if code, _, err := cont.Exec(ctx, []string{"sh", "-c", "echo module test > /go.mod"}); err != nil || code != 0 {
		t.Fatalf("failed to create go.mod in container: %v (exit code %d)", err, code)
	}

	runDocker := func(ctx context.Context, cont tc.Container, runtime string, args ...string) (int, string, error) {
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

	type caseFn func(context.Context, tc.Container, string) (int, string, error)
	cases := []struct {
		name     string
		fn       caseFn
		wantCode int
	}{
		{
			name: "echo",
			fn: func(ctx context.Context, cont tc.Container, runtime string) (int, string, error) {
				return runDocker(ctx, cont, runtime, "alpine", "echo", "hello")
			},
			wantCode: 0,
		},
		{
			name: "false",
			fn: func(ctx context.Context, cont tc.Container, runtime string) (int, string, error) {
				return runDocker(ctx, cont, runtime, "alpine", "false")
			},
			wantCode: 1,
		},
		{
			name: "env",
			fn: func(ctx context.Context, cont tc.Container, runtime string) (int, string, error) {
				return runDocker(ctx, cont, runtime, "-e", "FOO=bar", "alpine", "sh", "-c", "echo $FOO")
			},
			wantCode: 0,
		},
		{
			name: "volume",
			fn: func(ctx context.Context, cont tc.Container, runtime string) (int, string, error) {
				return runDocker(ctx, cont, runtime, "-v", "/:/data", "alpine", "sh", "-c", "test -f /data/go.mod")
			},
			wantCode: 0,
		},
		{
			name: "workdir",
			fn: func(ctx context.Context, cont tc.Container, runtime string) (int, string, error) {
				return runDocker(ctx, cont, runtime, "-w", "/tmp", "alpine", "pwd")
			},
			wantCode: 0,
		},
		{
			name: "nginx port mapping",
			fn: func(ctx context.Context, cont tc.Container, runtime string) (int, string, error) {
				cname := fmt.Sprintf("web-%d", time.Now().UnixNano())
				if code, _, err := runDocker(ctx, cont, runtime, "-d", "--name", cname, "-p", "8080:80", "nginx"); err != nil || code != 0 {
					return code, "", fmt.Errorf("start nginx: %w", err)
				}
				defer cont.Exec(ctx, []string{"docker", "rm", "-f", cname})
				time.Sleep(1 * time.Second)
				code, reader, err := cont.Exec(ctx, []string{"curl", "-fsSL", "http://localhost:8080"}, tcexec.Multiplexed())
				if err != nil {
					return code, "", err
				}
				out, err := io.ReadAll(reader)
				return code, string(out), err
			},
			wantCode: 0,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			runcCode, runcOut, err := c.fn(ctx, cont, "runc")
			if err != nil {
				t.Fatalf("runc exec failed: %v", err)
			}
			t.Logf("runc exited with %d", runcCode)
			t.Logf("runc output:\n%s", runcOut)

			delegatecCode, delegatecOut, err := c.fn(ctx, cont, "delegatec")
			if err != nil {
				t.Fatalf("delegatec exec failed: %v", err)
			}
			t.Logf("delegatec exited with %d", delegatecCode)
			t.Logf("delegatec output:\n%s", delegatecOut)

			if runcCode != delegatecCode || runcOut != delegatecOut {
				if delegatecCode != 0 {
					logDelegatecLogs(t, ctx, cont)
				}
				logRuncLogs(t, ctx, cont)
				t.Fatalf("mismatch: runc [%d] %q vs delegatec [%d] %q", runcCode, runcOut, delegatecCode, delegatecOut)
			}
			if runcCode != c.wantCode {
				t.Fatalf("unexpected exit code: got %d want %d", runcCode, c.wantCode)
			}
		})
	}
}
