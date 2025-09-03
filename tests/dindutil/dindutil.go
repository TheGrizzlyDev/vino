package dindutil

import (
	"bufio"
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

const dockerCmdTimeout = 2 * time.Minute

type ExecError struct {
	Cmd      []string
	ExitCode int
	Stdout   string
	Stderr   string
	Err      error
}

func (e *ExecError) Error() string {
	parts := []string{fmt.Sprintf("exec %v (exit code %d)", e.Cmd, e.ExitCode)}
	if e.Stdout != "" {
		parts = append(parts, fmt.Sprintf("stdout: %q", e.Stdout))
	}
	if e.Stderr != "" {
		parts = append(parts, fmt.Sprintf("stderr: %q", e.Stderr))
	}
	if e.Err != nil {
		parts = append(parts, e.Err.Error())
	}
	return strings.Join(parts, ": ")
}

func ReadAll(ctx context.Context, r io.Reader) ([]byte, error) {
	var buf bytes.Buffer
	done := make(chan error, 1)
	go func() {
		_, err := io.Copy(&buf, r)
		done <- err
	}()
	select {
	case <-ctx.Done():
		return buf.Bytes(), ctx.Err()
	case err := <-done:
		return buf.Bytes(), err
	}
}

func readStdStreams(ctx context.Context, r io.Reader) (stdout, stderr bytes.Buffer, err error) {
	done := make(chan error, 1)
	go func() {
		_, e := stdcopy.StdCopy(&stdout, &stderr, r)
		done <- e
	}()
	select {
	case <-ctx.Done():
		return stdout, stderr, ctx.Err()
	case e := <-done:
		return stdout, stderr, e
	}
}

func logStreamLines(t *testing.T, container, runtime, stream string, data []byte) {
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		t.Logf("container=%s runtime=%s stream=%s ts=%s msg=%q", container, runtime, stream, time.Now().Format(time.RFC3339Nano), scanner.Text())
	}
	if err := scanner.Err(); err != nil {
		t.Logf("container=%s runtime=%s stream=%s ts=%s msg=%q", container, runtime, stream, time.Now().Format(time.RFC3339Nano), fmt.Sprintf("scanner error: %v", err))
	}
}

// BuildDindImage builds the DinD test image and schedules its removal.
func BuildDindImage(t *testing.T) string {
	t.Helper()
	rootDir, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatalf("failed to get root dir: %v", err)
	}
	image := "vino-dind-test"
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, "docker", "build", "-t", image, "-f", filepath.Join(rootDir, "tests/integration/dind/Dockerfile"), rootDir)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("failed to build image: %v\n%s", err, string(out))
	}
	t.Log(string(out))
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		_ = exec.CommandContext(ctx, "docker", "rmi", "-f", image).Run()
	})
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
				out, _ := ReadAll(ctx, logReader)
				t.Logf("container logs:\n%s", string(out))
			}
		}
		t.Fatalf("failed to start container: %v", err)
	}
	execCtx, cancel := context.WithTimeout(ctx, dockerCmdTimeout)
	defer cancel()
	if code, _, err := cont.Exec(execCtx, []string{"sh", "-c", "echo module test > /go.mod"}); err != nil || code != 0 {
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
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		if err := exec.CommandContext(ctx, "docker", "exec", name, "docker", "image", "inspect", img).Run(); err == nil {
			cancel()
			continue
		}
		cancel()
		// Ensure the image exists on the host. If it doesn't, pull it first.
		ctx, cancel = context.WithTimeout(context.Background(), time.Minute)
		if err := exec.CommandContext(ctx, "docker", "image", "inspect", img).Run(); err != nil {
			cancel()
			ctxPull, cancelPull := context.WithTimeout(context.Background(), 5*time.Minute)
			if out, err := exec.CommandContext(ctxPull, "docker", "pull", img).CombinedOutput(); err != nil {
				cancelPull()
				t.Fatalf("failed to pull image %s: %v\n%s", img, err, string(out))
			}
			cancelPull()
		} else {
			cancel()
		}

		ctxSave, cancelSave := context.WithTimeout(context.Background(), 5*time.Minute)
		cmd := exec.CommandContext(ctxSave, "sh", "-c", fmt.Sprintf("docker save %s | docker exec -i %s docker load", img, name))
		if out, err := cmd.CombinedOutput(); err != nil {
			cancelSave()
			t.Fatalf("failed to preload image %s: %v\n%s", img, err, string(out))
		}
		cancelSave()
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
	execCtx, cancel := context.WithTimeout(ctx, dockerCmdTimeout)
	defer cancel()
	code, reader, err := cont.Exec(execCtx, cmd, tcexec.Multiplexed())
	var stdout, stderr bytes.Buffer
	if reader != nil {
		stdout, stderr, err = readStdStreams(execCtx, reader)
	}
	if err != nil {
		return code, stdout.String(), &ExecError{Cmd: cmd, ExitCode: code, Stdout: stdout.String(), Stderr: stderr.String(), Err: err}
	}
	if code != 0 {
		return code, stdout.String(), &ExecError{Cmd: cmd, ExitCode: code, Stdout: stdout.String(), Stderr: stderr.String()}
	}
	return code, stdout.String(), nil
}

// ExecNoOutput executes a command inside the container and collects stdout and stderr.
func ExecNoOutput(ctx context.Context, cont tc.Container, args ...string) (int, string, string, error) {
	execCtx, cancel := context.WithTimeout(ctx, dockerCmdTimeout)
	defer cancel()
	code, reader, err := cont.Exec(execCtx, args, tcexec.Multiplexed())
	var stdout, stderr bytes.Buffer
	if reader != nil {
		stdout, stderr, err = readStdStreams(execCtx, reader)
	}
	if err != nil {
		return code, stdout.String(), stderr.String(), &ExecError{Cmd: args, ExitCode: code, Stdout: stdout.String(), Stderr: stderr.String(), Err: err}
	}
	if code != 0 {
		return code, stdout.String(), stderr.String(), &ExecError{Cmd: args, ExitCode: code, Stdout: stdout.String(), Stderr: stderr.String()}
	}
	return code, stdout.String(), stderr.String(), nil
}

// LogDelegatecLogs logs the contents of delegatec.log from the container.
func LogDelegatecLogs(t *testing.T, ctx context.Context, cont tc.Container) {
	t.Helper()
	name, _ := cont.Name(ctx)
	runtime := "delegatec"
	execCtx, cancel := context.WithTimeout(ctx, dockerCmdTimeout)
	defer cancel()
	code, reader, err := cont.Exec(execCtx, []string{"cat", "/var/log/delegatec.log"}, tcexec.Multiplexed())
	if err != nil {
		t.Logf("container=%s runtime=%s stream=setup ts=%s msg=%q", name, runtime, time.Now().Format(time.RFC3339Nano), fmt.Sprintf("failed to read delegatec.log: %v", err))
		return
	}
	if code != 0 {
		t.Logf("container=%s runtime=%s stream=setup ts=%s msg=%q", name, runtime, time.Now().Format(time.RFC3339Nano), fmt.Sprintf("exit code %d", code))
		return
	}
	stdout, stderr, err := readStdStreams(execCtx, reader)
	if err != nil {
		t.Logf("container=%s runtime=%s stream=setup ts=%s msg=%q", name, runtime, time.Now().Format(time.RFC3339Nano), fmt.Sprintf("split streams: %v", err))
		return
	}
	logStreamLines(t, name, runtime, "stdout", stdout.Bytes())
	logStreamLines(t, name, runtime, "stderr", stderr.Bytes())
}

// LogRuncLogs logs the runc logs from the container.
func LogRuncLogs(t *testing.T, ctx context.Context, cont tc.Container) {
	t.Helper()
	name, _ := cont.Name(ctx)
	runtime := "runc"
	cmd := []string{"sh", "-c", "find /var/run/docker/containerd/daemon -name log.json -exec cat {} +"}
	execCtx, cancel := context.WithTimeout(ctx, dockerCmdTimeout)
	defer cancel()
	code, reader, err := cont.Exec(execCtx, cmd, tcexec.Multiplexed())
	if err != nil {
		t.Logf("container=%s runtime=%s stream=setup ts=%s msg=%q", name, runtime, time.Now().Format(time.RFC3339Nano), fmt.Sprintf("failed to read runc log: %v", err))
		return
	}
	if code != 0 {
		t.Logf("container=%s runtime=%s stream=setup ts=%s msg=%q", name, runtime, time.Now().Format(time.RFC3339Nano), fmt.Sprintf("exit code %d", code))
		return
	}
	stdout, stderr, err := readStdStreams(execCtx, reader)
	if err != nil {
		t.Logf("container=%s runtime=%s stream=setup ts=%s msg=%q", name, runtime, time.Now().Format(time.RFC3339Nano), fmt.Sprintf("split streams: %v", err))
		return
	}
	if stdout.Len() == 0 && stderr.Len() == 0 {
		t.Logf("container=%s runtime=%s stream=setup ts=%s msg=%q", name, runtime, time.Now().Format(time.RFC3339Nano), "runc log empty")
		return
	}
	logStreamLines(t, name, runtime, "stdout", stdout.Bytes())
	logStreamLines(t, name, runtime, "stderr", stderr.Bytes())
}
