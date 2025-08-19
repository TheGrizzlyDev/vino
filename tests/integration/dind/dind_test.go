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
	ctx := context.Background()

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
		t.Fatalf("failed to start container: %v", err)
	}
	defer func() {
		_ = cont.Terminate(ctx)
	}()

	runCase := func(cmd string) {
		runcCode, runcReader, err := cont.Exec(ctx, []string{"sh", "-c", "docker run --rm " + cmd}, tcexec.Multiplexed())
		if err != nil {
			t.Fatalf("runc exec failed for %q: %v", cmd, err)
		}
		vinocCode, vinocReader, err := cont.Exec(ctx, []string{"sh", "-c", "docker run --rm --runtime vinoc " + cmd}, tcexec.Multiplexed())
		if err != nil {
			t.Fatalf("vinoc exec failed for %q: %v", cmd, err)
		}
		runcOut, err := io.ReadAll(runcReader)
		if err != nil {
			t.Fatalf("read runc output: %v", err)
		}
		vinocOut, err := io.ReadAll(vinocReader)
		if err != nil {
			t.Fatalf("read vinoc output: %v", err)
		}
		if runcCode != vinocCode || string(runcOut) != string(vinocOut) {
			t.Fatalf("mismatch for %q: runc [%d] %q vs vinoc [%d] %q", cmd, runcCode, string(runcOut), vinocCode, string(vinocOut))
		}
	}

	runCase("alpine echo hello")
	runCase("alpine false")
}
