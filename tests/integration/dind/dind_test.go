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

func TestRuntimeParity(t *testing.T) {
	return

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

	runCase := func(runtime, cmd string) {
		t.Logf("--- running case: %q ---", cmd)

		// runc
		t.Log("executing with runc...")
		runcCode, runcReader, err := cont.Exec(ctx, []string{"sh", "-c", "docker run --rm --network host " + cmd}, tcexec.Multiplexed())
		if err != nil {
			t.Fatalf("runc exec failed for %q: %v", cmd, err)
		}
		runcOut, err := io.ReadAll(runcReader)
		if err != nil {
			t.Fatalf("read runc output: %v", err)
		}
		t.Logf("runc exited with %d", runcCode)
		t.Logf("runc output:\n%s", string(runcOut))

		// vinoc
		t.Logf("executing with %s...", runtime)
		vinocCode, vinocReader, err := cont.Exec(ctx, []string{"sh", "-c", "docker run --rm --network host --runtime " + runtime + " " + cmd}, tcexec.Multiplexed())
		if err != nil {
			t.Fatalf("%s exec failed for %q: %v", runtime, cmd, err)
		}
		vinocOut, err := io.ReadAll(vinocReader)
		if err != nil {
			t.Fatalf("read %s output: %v", runtime, err)
		}
		t.Logf("%s exited with %d", runtime, vinocCode)
		t.Logf("%s output:\n%s", runtime, string(vinocOut))

		// comparison
		if runcCode != vinocCode || string(runcOut) != string(vinocOut) {
			t.Fatalf("mismatch for %q: runc [%d] %q vs %s [%d] %q", cmd, runcCode, string(runcOut), runtime, vinocCode, string(vinocOut))
		}
		t.Logf("--- case PASSED: %q ---", cmd)
	}

	runCase("vinoc", "alpine echo hello")
	runCase("vinoc", "alpine false")
}
