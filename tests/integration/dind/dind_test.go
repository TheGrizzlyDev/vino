//go:build !e2e
// +build !e2e

package dind

import (
	"context"
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

	runCase := func(runtime, cmd string, wantCode int) {
		t.Logf("--- running case: %q ---", cmd)

		// runc
		t.Log("executing with runc...")
		runcCode, runcReader, err := cont.Exec(ctx, []string{"sh", "-c", "docker run -q --rm " + cmd}, tcexec.Multiplexed())
		if err != nil {
			t.Fatalf("runc exec failed for %q: %v", cmd, err)
		}
		runcOut, err := io.ReadAll(runcReader)
		if err != nil {
			t.Fatalf("read runc output: %v", err)
		}
		t.Logf("runc exited with %d", runcCode)
		t.Logf("runc output:\n%s", string(runcOut))

		// delegatec
		t.Logf("executing with %s...", runtime)
		delegatecCode, delegatecReader, err := cont.Exec(ctx, []string{"sh", "-c", "docker run -q --rm --runtime " + runtime + " " + cmd}, tcexec.Multiplexed())
		if err != nil {
			t.Fatalf("%s exec failed for %q: %v", runtime, cmd, err)
		}
		delegatecOut, err := io.ReadAll(delegatecReader)
		if err != nil {
			t.Fatalf("read %s output: %v", runtime, err)
		}
		t.Logf("%s exited with %d", runtime, delegatecCode)
		t.Logf("%s output:\n%s", runtime, string(delegatecOut))

		// comparison
		if runcCode != delegatecCode || string(runcOut) != string(delegatecOut) {
			if delegatecCode != 0 {
				logDelegatecLogs(t, ctx, cont)
			}
			logRuncLogs(t, ctx, cont)
			t.Fatalf("mismatch for %q: runc [%d] %q vs %s [%d] %q", cmd, runcCode, string(runcOut), runtime, delegatecCode, string(delegatecOut))
		}
		if runcCode != wantCode {
			t.Fatalf("unexpected exit code for %q: runc [%d] vs %s [%d], want %d", cmd, runcCode, runtime, delegatecCode, wantCode)
		}
		t.Logf("--- case PASSED: %q ---", cmd)
	}
	cases := []struct {
		cmd      string
		wantCode int
	}{
		{"alpine echo hello", 0},
		{"alpine false", 1},
		{"-e FOO=bar alpine sh -c 'echo $FOO'", 0},
		{"-v $(pwd):/data alpine sh -c 'test -f /data/go.mod'", 0},
	}
	for _, c := range cases {
		runCase("delegatec", c.cmd, c.wantCode)
	}
}
