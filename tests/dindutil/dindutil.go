package dindutil

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/docker/docker/pkg/stdcopy"
	tc "github.com/testcontainers/testcontainers-go"
	tcexec "github.com/testcontainers/testcontainers-go/exec"
	"github.com/testcontainers/testcontainers-go/wait"
)

// BuildDindImage builds the DinD test image and schedules its removal.
func BuildDindImage(t *testing.T) string {
	t.Helper()
	rootDir, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatalf("failed to get root dir: %v", err)
	}
	image := "vino-dind-test"
	cmd := exec.Command("docker", "build", "-t", image, "-f", filepath.Join(rootDir, "tests/integration/dind/Dockerfile"), rootDir)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("failed to build image: %v\n%s", err, string(out))
	}
	t.Log(string(out))
	t.Cleanup(func() { _ = exec.Command("docker", "rmi", "-f", image).Run() })
	return image
}

// StartDindContainer starts a DinD container using the provided image and name.
func StartDindContainer(ctx context.Context, t *testing.T, image, name string, reuse bool) tc.Container {
	t.Helper()
	req := tc.ContainerRequest{
		Image:      image,
		Name:       name,
		Privileged: true,
		WaitingFor: wait.ForLog("API listen on /var/run/docker.sock").WithStartupTimeout(2 * time.Minute),
	}
	gcr := tc.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	}
	if reuse {
		gcr.Reuse = true
	}
	cont, err := tc.GenericContainer(ctx, gcr)
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
	if code, _, err := cont.Exec(ctx, []string{"sh", "-c", "echo module test > /go.mod"}); err != nil || code != 0 {
		_ = cont.Terminate(ctx)
		t.Fatalf("failed to create go.mod in container: %v (exit code %d)", err, code)
	}
	return cont
}

// preloadImages loads the specified images into the DinD container if they are
// not already present. Images are copied from the host by piping `docker save`
// into `docker load` inside the container.
func preloadImages(t *testing.T, name string, images []string) {
	t.Helper()
	for _, img := range images {
		// Skip if the image already exists in the container.
		if err := exec.Command("docker", "exec", name, "docker", "image", "inspect", img).Run(); err == nil {
			continue
		}
		// Ensure the image exists on the host. If it doesn't, pull it first.
		if err := exec.Command("docker", "image", "inspect", img).Run(); err != nil {
			if out, err := exec.Command("docker", "pull", img).CombinedOutput(); err != nil {
				t.Fatalf("failed to pull image %s: %v\n%s", img, err, string(out))
			}
		}

		cmd := exec.Command("sh", "-c", fmt.Sprintf("docker save %s | docker exec -i %s docker load", img, name))
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("failed to preload image %s: %v\n%s", img, err, string(out))
		}
	}
}

// Pool manages a set of DinD containers for parallel tests.
type Pool struct {
	ch chan tc.Container
}

// NewPool builds the DinD image, starts count containers, preloads the provided
// images into each container, and returns a pool.
func NewPool(t *testing.T, count int, images ...string) *Pool {
	image := BuildDindImage(t)
	reuse := os.Getenv("TESTCONTAINERS_REUSE_ENABLE") == "true"
	p := &Pool{ch: make(chan tc.Container, count)}
	var wg sync.WaitGroup
	for i := 0; i < count; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			name := fmt.Sprintf("vino-dind-%d", i)
			cont := StartDindContainer(ctx, t, image, name, reuse)
			cancel()
			if len(images) > 0 {
				preloadImages(t, name, images)
			}
			p.ch <- cont
			if !reuse {
				t.Cleanup(func(cont tc.Container) func() {
					return func() {
						ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
						defer cancel()
						if err := cont.Terminate(ctx); err != nil {
							fmt.Printf("failed to terminate container: %v\n", err)
						}
					}
				}(cont))
			}
		}(i)
	}
	wg.Wait()
	return p
}

// Acquire obtains a container from the pool and schedules its release when the test ends.
func (p *Pool) Acquire(t *testing.T) tc.Container {
	cont := <-p.ch
	t.Cleanup(func() { p.Release(cont) })
	return cont
}

// Release returns a container to the pool.
func (p *Pool) Release(cont tc.Container) {
	p.ch <- cont
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
