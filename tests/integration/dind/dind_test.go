//go:build e2e
// +build e2e

package dind

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/docker/docker/pkg/stdcopy"
	tc "github.com/testcontainers/testcontainers-go"
	tcexec "github.com/testcontainers/testcontainers-go/exec"
	"github.com/testcontainers/testcontainers-go/wait"
)

func logCriuCheck(t *testing.T, ctx context.Context, cont tc.Container) {
	t.Helper()
	code, reader, err := cont.Exec(ctx, []string{"criu", "check"}, tcexec.Multiplexed())
	if err != nil {
		t.Logf("failed to run criu check: %v", err)
		return
	}
	var stdout, stderr bytes.Buffer
	if reader != nil {
		stdcopy.StdCopy(&stdout, &stderr, reader)
	}
	t.Logf("criu check exit code %d\nstdout:\n%s\nstderr:\n%s", code, stdout.String(), stderr.String())
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

	// requireCheckpointSupport verifies that the host supports container
	// checkpoint/restore by running `criu check` and `docker checkpoint ls`
	// on a dummy container. If either command fails or reports unsupported
	// features, the calling test is skipped. This helps maintainers avoid
	// spurious failures when the kernel or Docker daemon lacks CRIU support.
	requireCheckpointSupport := func(t *testing.T, ctx context.Context, cont tc.Container) {
		t.Helper()
		if code, out, serr, err := ExecNoOutput(ctx, cont, "criu", "check"); err != nil || code != 0 || strings.Contains(strings.ToLower(out+serr), "unsupported") {
			t.Skipf("skipping checkpoint restore: criu check failed (exit %d): %v\nstdout:\n%s\nstderr:\n%s", code, err, out, serr)
		}

		cname := fmt.Sprintf("ckpt-precheck-%d", time.Now().UnixNano())
		runCmd := []string{"docker", "run", "-d", "--name", cname, "alpine", "sleep", "infinity"}
		if code, _, _, err := ExecNoOutput(ctx, cont, runCmd...); err != nil || code != 0 {
			t.Skipf("skipping checkpoint restore: failed to start dummy container: %v", err)
		}
		t.Cleanup(func() { cont.Exec(ctx, []string{"docker", "rm", "-f", cname}) })

		if code, out, serr, err := ExecNoOutput(ctx, cont, "docker", "checkpoint", "ls", cname); err != nil || code != 0 || strings.Contains(strings.ToLower(out+serr), "unsupported") {
			t.Skipf("skipping checkpoint restore: docker checkpoint ls failed (exit %d): %v\nstdout:\n%s\nstderr:\n%s", code, err, out, serr)
		}
	}

	type caseFn func(*testing.T, context.Context, tc.Container, string) (int, string, error)
	type verifyFn func(int, string, int, string) error
	var defaultVerify = func(wantCode int) verifyFn {
		return func(runcCode int, runcOut string, delegatecCode int, delegatecOut string) error {
			if runcCode != delegatecCode || runcOut != delegatecOut {
				return fmt.Errorf("mismatch: runc [%d] %q vs delegatec [%d] %q", runcCode, runcOut, delegatecCode, delegatecOut)
			}
			if runcCode != wantCode {
				return fmt.Errorf("unexpected exit code: got %d want %d", runcCode, wantCode)
			}
			return nil
		}
	}
	cases := []struct {
		name    string
		fn      caseFn
		verify  verifyFn
		pretest func(*testing.T, context.Context, tc.Container)
	}{
		{
			name: "echo",
			fn: func(_ *testing.T, ctx context.Context, cont tc.Container, runtime string) (int, string, error) {
				return RunDocker(ctx, cont, runtime, "alpine", "echo", "hello")
			},
			verify: defaultVerify(0),
		},
		{
			name: "false",
			fn: func(_ *testing.T, ctx context.Context, cont tc.Container, runtime string) (int, string, error) {
				return RunDocker(ctx, cont, runtime, "alpine", "false")
			},
			verify: defaultVerify(1),
		},
		{
			name: "env",
			fn: func(_ *testing.T, ctx context.Context, cont tc.Container, runtime string) (int, string, error) {
				return RunDocker(ctx, cont, runtime, "-e", "FOO=bar", "alpine", "sh", "-c", "echo $FOO")
			},
			verify: defaultVerify(0),
		},
		{
			name: "volume",
			fn: func(_ *testing.T, ctx context.Context, cont tc.Container, runtime string) (int, string, error) {
				return RunDocker(ctx, cont, runtime, "-v", "/:/data", "alpine", "sh", "-c", "test -f /data/go.mod")
			},
			verify: defaultVerify(0),
		},
		{
			name: "workdir",
			fn: func(_ *testing.T, ctx context.Context, cont tc.Container, runtime string) (int, string, error) {
				return RunDocker(ctx, cont, runtime, "-w", "/tmp", "alpine", "pwd")
			},
			verify: defaultVerify(0),
		},
		{
			name: "exec after run",
			fn: func(t *testing.T, ctx context.Context, cont tc.Container, runtime string) (int, string, error) {
				cname := fmt.Sprintf("bgtest-%d", time.Now().UnixNano())
				runCmd := []string{"docker", "run", "-d", "--name", cname}
				if runtime != "" {
					runCmd = append(runCmd, "--runtime", runtime)
				}
				runCmd = append(runCmd, "alpine", "tail", "-f", "/dev/null")
				if code, _, _, err := ExecNoOutput(ctx, cont, runCmd...); err != nil {
					return code, "", fmt.Errorf("start container: %w", err)
				}
				t.Cleanup(func() { cont.Exec(ctx, []string{"docker", "rm", "-f", cname}) })
				execCmd := []string{"docker", "exec", cname, "sh", "-c", "echo hello"}
				code, reader, err := cont.Exec(ctx, execCmd, tcexec.Multiplexed())
				if err != nil {
					return code, "", err
				}
				data, err := io.ReadAll(reader)
				return code, string(data), err
			},
			verify: defaultVerify(0),
		},
		{
			name: "memory limit",
			fn: func(_ *testing.T, ctx context.Context, cont tc.Container, runtime string) (int, string, error) {
				cmd := []string{"-m", "32m", "alpine", "sh", "-c", "cat /sys/fs/cgroup/memory.max"}
				return RunDocker(ctx, cont, runtime, cmd...)
			},
			verify: func(runcCode int, runcOut string, delegatecCode int, delegatecOut string) error {
				if err := defaultVerify(0)(runcCode, runcOut, delegatecCode, delegatecOut); err != nil {
					return err
				}
				if strings.TrimSpace(runcOut) != "33554432" {
					return fmt.Errorf("unexpected memory limit: %s", strings.TrimSpace(runcOut))
				}
				return nil
			},
		},
		{
			name: "cpu limit",
			fn: func(_ *testing.T, ctx context.Context, cont tc.Container, runtime string) (int, string, error) {
				cmd := []string{"--cpus", "0.5", "alpine", "sh", "-c", "cat /sys/fs/cgroup/cpu.max"}
				return RunDocker(ctx, cont, runtime, cmd...)
			},
			verify: func(runcCode int, runcOut string, delegatecCode int, delegatecOut string) error {
				if err := defaultVerify(0)(runcCode, runcOut, delegatecCode, delegatecOut); err != nil {
					return err
				}
				if strings.TrimSpace(runcOut) != "50000 100000" {
					return fmt.Errorf("unexpected cpu limit: %s", strings.TrimSpace(runcOut))
				}
				return nil
			},
		},
		{
			name: "update limits",
			fn: func(t *testing.T, ctx context.Context, cont tc.Container, runtime string) (int, string, error) {
				cname := fmt.Sprintf("update-%d", time.Now().UnixNano())
				runCmd := []string{"docker", "run", "-d", "--name", cname}
				if runtime != "" {
					runCmd = append(runCmd, "--runtime", runtime)
				}
				runCmd = append(runCmd, "alpine", "tail", "-f", "/dev/null")
				if code, reader, err := cont.Exec(ctx, runCmd, tcexec.Multiplexed()); err != nil || code != 0 {
					if err == nil {
						io.Copy(io.Discard, reader)
					}
					return code, "", fmt.Errorf("start container: %w", err)
				} else {
					io.Copy(io.Discard, reader)
				}
				t.Cleanup(func() { cont.Exec(ctx, []string{"docker", "rm", "-f", cname}) })

				updateCmd := []string{"docker", "update", "--memory", "32m", "--memory-swap", "64m", cname}
				code, reader, err := cont.Exec(ctx, updateCmd)
				var stderr bytes.Buffer
				if reader != nil {
					stdcopy.StdCopy(io.Discard, &stderr, reader)
				}
				if code != 0 {
					msg := strings.TrimSpace(stderr.String())
					if err != nil {
						if msg == "" {
							return code, "", fmt.Errorf("update container: %w", err)
						}
						return code, "", fmt.Errorf("update container: %s: %w", msg, err)
					}
					if msg == "" {
						msg = fmt.Sprintf("exit code %d", code)
					}
					return code, "", fmt.Errorf("update container: %s", msg)
				}
				if err != nil {
					return code, "", fmt.Errorf("update container: %w", err)
				}

				execCmd := []string{"docker", "exec", cname, "cat", "/sys/fs/cgroup/memory.max"}
				code, reader, err = cont.Exec(ctx, execCmd, tcexec.Multiplexed())
				if err != nil {
					return code, "", err
				}
				out, err := io.ReadAll(reader)
				return code, string(out), err
			},
			verify: func(runcCode int, runcOut string, delegatecCode int, delegatecOut string) error {
				if err := defaultVerify(0)(runcCode, runcOut, delegatecCode, delegatecOut); err != nil {
					return err
				}
				if strings.TrimSpace(runcOut) != "33554432" {
					return fmt.Errorf("unexpected memory limit: %s", strings.TrimSpace(runcOut))
				}
				return nil
			},
		},
		{
			name: "nginx port mapping",
			fn: func(t *testing.T, ctx context.Context, cont tc.Container, runtime string) (int, string, error) {
				cname := fmt.Sprintf("web-%d", time.Now().UnixNano())
				code, _, err := RunDocker(ctx, cont, runtime, "-d", "--name", cname, "-p", "8080:80", "nginx")
				if err != nil {
					return code, "", fmt.Errorf("start nginx: %w", err)
				}
				if code != 0 {
					return code, "", fmt.Errorf("start nginx: exit code %d", code)
				}
				cleanup := func() { cont.Exec(ctx, []string{"docker", "rm", "-f", cname}) }
				t.Cleanup(cleanup)

				pollCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
				defer cancel()

				for {
					code, reader, err := cont.Exec(pollCtx, []string{"curl", "-fsSL", "--max-time", "1", "http://localhost:8080"}, tcexec.Multiplexed())
					if err == nil && code == 0 {
						data, err := io.ReadAll(reader)
						cleanup()
						return code, string(data), err
					}
					if err == nil {
						io.Copy(io.Discard, reader)
					}
					select {
					case <-pollCtx.Done():
						if err == nil {
							err = pollCtx.Err()
						}
						cleanup()
						return code, "", err
					case <-time.After(100 * time.Millisecond):
					}
				}
			},
			verify: defaultVerify(0),
		},
		{
			name: "user and capabilities",
			fn: func(_ *testing.T, ctx context.Context, cont tc.Container, runtime string) (int, string, error) {
				cmd := []string{"--user", "1000:1000", "alpine", "sh", "-c", "id -u; id -g; cat /proc/self/status | grep CapEff; ping -c 1 127.0.0.1"}
				return RunDocker(ctx, cont, runtime, cmd...)
			},
			verify: func(runcCode int, runcOut string, delegatecCode int, delegatecOut string) error {
				verifyOut := func(runtime, out string) error {
					if !strings.Contains(out, "1000\n1000") {
						return fmt.Errorf("unexpected uid/gid output: %q", out)
					}
					if !strings.Contains(out, "CapEff") {
						return fmt.Errorf("missing CapEff line: %q", out)
					}
					return nil
				}
				if err := verifyOut("runc", runcOut); err != nil {
					return err
				}
				if err := verifyOut("delegatec", delegatecOut); err != nil {
					return err
				}
				if runcCode != delegatecCode {
					return fmt.Errorf("mismatch: runc [%d] vs delegatec [%d]", runcCode, delegatecCode)
				}
				return nil
			},
		},
		{
			name: "checkpoint restore",
			pretest: func(t *testing.T, ctx context.Context, cont tc.Container) {
				requireCheckpointSupport(t, ctx, cont)
			},
			fn: func(t *testing.T, ctx context.Context, cont tc.Container, runtime string) (int, string, error) {
				cname := fmt.Sprintf("ckpt-%d", time.Now().UnixNano())
				runCmd := []string{"docker", "run", "-d", "--name", cname}
				if runtime != "" {
					runCmd = append(runCmd, "--runtime", runtime)
				}
				runCmd = append(runCmd, "alpine", "sh", "-c", "echo hello > /checkpoint_file; tail -f /dev/null")
				if code, _, _, err := ExecNoOutput(ctx, cont, runCmd...); err != nil {
					return code, "", fmt.Errorf("start container: %w", err)
				}
				t.Cleanup(func() { cont.Exec(ctx, []string{"docker", "rm", "-f", cname}) })
				t.Cleanup(func() { cont.Exec(ctx, []string{"docker", "checkpoint", "rm", cname, "ckpt"}) })

				code, out, serr, err := ExecNoOutput(ctx, cont, "docker", "checkpoint", "create", "--debug", cname, "ckpt")
				t.Logf("docker checkpoint create stdout:\n%s", out)
				t.Logf("docker checkpoint create stderr:\n%s", serr)
				if err != nil {
					return code, "", fmt.Errorf("create checkpoint: %w", err)
				}

				if code, _, _, err := ExecNoOutput(ctx, cont, "docker", "start", "--checkpoint", "ckpt", cname); err != nil {
					return code, "", fmt.Errorf("start from checkpoint: %w", err)
				}

				execCmd := []string{"docker", "exec", cname, "cat", "/checkpoint_file"}
				code, reader, err := cont.Exec(ctx, execCmd, tcexec.Multiplexed())
				if err != nil {
					return code, "", err
				}
				data, err := io.ReadAll(reader)
				return code, string(data), err
			},
			verify: defaultVerify(0),
		},
		{
			name: "pause/unpause",
			fn: func(t *testing.T, ctx context.Context, cont tc.Container, runtime string) (int, string, error) {
				cname := fmt.Sprintf("pause-%d", time.Now().UnixNano())
				runCmd := []string{"docker", "run", "-d", "--name", cname}
				if runtime != "" {
					runCmd = append(runCmd, "--runtime", runtime)
				}
				runCmd = append(runCmd, "alpine", "sleep", "infinity")
				if code, _, _, err := ExecNoOutput(ctx, cont, runCmd...); err != nil {
					return code, "", fmt.Errorf("start container: %w", err)
				}
				t.Cleanup(func() { cont.Exec(ctx, []string{"docker", "rm", "-f", cname}) })

				if code, _, _, err := ExecNoOutput(ctx, cont, "docker", "pause", cname); err != nil {
					return code, "", fmt.Errorf("pause container: %w", err)
				}

				if code, _, _, err := ExecNoOutput(ctx, cont, "docker", "unpause", cname); err != nil {
					return code, "", fmt.Errorf("unpause container: %w", err)
				}

				execCmd := []string{"docker", "exec", cname, "sh", "-c", "echo hello"}
				code, reader, err := cont.Exec(ctx, execCmd, tcexec.Multiplexed())
				if err != nil {
					return code, "", err
				}
				out, err := io.ReadAll(reader)
				return code, string(out), err
			},
			verify: defaultVerify(0),
		},
		{
			name: "top",
			fn: func(t *testing.T, ctx context.Context, cont tc.Container, runtime string) (int, string, error) {
				cname := fmt.Sprintf("top-%d", time.Now().UnixNano())
				runCmd := []string{"docker", "run", "-d", "--name", cname}
				if runtime != "" {
					runCmd = append(runCmd, "--runtime", runtime)
				}
				runCmd = append(runCmd, "alpine", "sleep", "1000")
				if code, reader, err := cont.Exec(ctx, runCmd, tcexec.Multiplexed()); err != nil || code != 0 {
					if err == nil {
						io.Copy(io.Discard, reader)
					}
					return code, "", fmt.Errorf("start container: %w", err)
				} else {
					io.Copy(io.Discard, reader)
				}
				t.Cleanup(func() { cont.Exec(ctx, []string{"docker", "rm", "-f", cname}) })

				topCmd := []string{"docker", "top", cname, "-o", "pid,comm", "-A"}
				code, reader, err := cont.Exec(ctx, topCmd, tcexec.Multiplexed())
				if err != nil {
					return code, "", err
				}
				out, err := io.ReadAll(reader)
				return code, string(out), err
			},
			verify: func(runcCode int, runcOut string, delegatecCode int, delegatecOut string) error {
				if runcCode != delegatecCode || runcCode != 0 {
					return fmt.Errorf("unexpected exit code: runc %d delegatec %d", runcCode, delegatecCode)
				}
				for name, out := range map[string]string{"runc": runcOut, "delegatec": delegatecOut} {
					if !strings.Contains(out, "sleep") {
						return fmt.Errorf("%s output missing 'sleep': %q", name, out)
					}
				}
				return nil
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if c.pretest != nil {
				c.pretest(t, ctx, cont)
			}
			runcCode, runcOut, err := c.fn(t, ctx, cont, "runc")
			if err != nil {
				t.Fatalf("runc exec failed: %v", err)
			}
			t.Logf("runc exited with %d", runcCode)
			t.Logf("runc output:\n%s", runcOut)

			delegatecCode, delegatecOut, err := c.fn(t, ctx, cont, "delegatec")
			if err != nil {
				t.Fatalf("delegatec exec failed: %v", err)
			}
			t.Logf("delegatec exited with %d", delegatecCode)
			t.Logf("delegatec output:\n%s", delegatecOut)

			if err := c.verify(runcCode, runcOut, delegatecCode, delegatecOut); err != nil {
				LogDelegatecLogs(t, ctx, cont)
				LogRuncLogs(t, ctx, cont)
				logCriuCheck(t, ctx, cont)
				t.Fatal(err)
			}
		})
	}
}
