//go:build !e2e
// +build !e2e

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

	execNoOutput := func(ctx context.Context, cont tc.Container, args ...string) (int, error) {
		code, reader, err := cont.Exec(ctx, args, tcexec.Multiplexed())
		var stderr bytes.Buffer
		if reader != nil {
			stdcopy.StdCopy(io.Discard, &stderr, reader)
		}
		if err != nil {
			if stderr.Len() > 0 {
				err = fmt.Errorf("%s: %w", strings.TrimSpace(stderr.String()), err)
			}
			return code, err
		}
		if code != 0 {
			msg := strings.TrimSpace(stderr.String())
			if msg == "" {
				msg = fmt.Sprintf("exit code %d", code)
			}
			return code, fmt.Errorf("%s", msg)
		}
		return code, nil
	}

	type caseFn func(context.Context, tc.Container, string) (int, string, error)
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
		name   string
		fn     caseFn
		verify verifyFn
	}{
		{
			name: "echo",
			fn: func(ctx context.Context, cont tc.Container, runtime string) (int, string, error) {
				return runDocker(ctx, cont, runtime, "alpine", "echo", "hello")
			},
			verify: defaultVerify(0),
		},
		{
			name: "false",
			fn: func(ctx context.Context, cont tc.Container, runtime string) (int, string, error) {
				return runDocker(ctx, cont, runtime, "alpine", "false")
			},
			verify: defaultVerify(1),
		},
		{
			name: "env",
			fn: func(ctx context.Context, cont tc.Container, runtime string) (int, string, error) {
				return runDocker(ctx, cont, runtime, "-e", "FOO=bar", "alpine", "sh", "-c", "echo $FOO")
			},
			verify: defaultVerify(0),
		},
		{
			name: "volume",
			fn: func(ctx context.Context, cont tc.Container, runtime string) (int, string, error) {
				return runDocker(ctx, cont, runtime, "-v", "/:/data", "alpine", "sh", "-c", "test -f /data/go.mod")
			},
			verify: defaultVerify(0),
		},
		{
			name: "workdir",
			fn: func(ctx context.Context, cont tc.Container, runtime string) (int, string, error) {
				return runDocker(ctx, cont, runtime, "-w", "/tmp", "alpine", "pwd")
			},
			verify: defaultVerify(0),
		},
		{
			name: "exec after run",
			fn: func(ctx context.Context, cont tc.Container, runtime string) (int, string, error) {
				cname := fmt.Sprintf("bgtest-%d", time.Now().UnixNano())
				runCmd := []string{"docker", "run", "-d", "--name", cname}
				if runtime != "" {
					runCmd = append(runCmd, "--runtime", runtime)
				}
				runCmd = append(runCmd, "alpine", "tail", "-f", "/dev/null")
				if code, err := execNoOutput(ctx, cont, runCmd...); err != nil {
					return code, "", fmt.Errorf("start container: %w", err)
				}
				defer cont.Exec(ctx, []string{"docker", "rm", "-f", cname})
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
			name: "memory limit",
			fn: func(ctx context.Context, cont tc.Container, runtime string) (int, string, error) {
				cmd := []string{"-m", "32m", "alpine", "sh", "-c", "cat /sys/fs/cgroup/memory.max"}
				return runDocker(ctx, cont, runtime, cmd...)
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
			fn: func(ctx context.Context, cont tc.Container, runtime string) (int, string, error) {
				cmd := []string{"--cpus", "0.5", "alpine", "sh", "-c", "cat /sys/fs/cgroup/cpu.max"}
				return runDocker(ctx, cont, runtime, cmd...)
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
			fn: func(ctx context.Context, cont tc.Container, runtime string) (int, string, error) {
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
				defer cont.Exec(ctx, []string{"docker", "rm", "-f", cname})

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
			verify: defaultVerify(0),
		},
		{
			name: "user and capabilities",
			fn: func(ctx context.Context, cont tc.Container, runtime string) (int, string, error) {
				cmd := []string{"--user", "1000:1000", "alpine", "sh", "-c", "id -u; id -g; cat /proc/self/status | grep CapEff; ping -c 1 127.0.0.1"}
				return runDocker(ctx, cont, runtime, cmd...)
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
			fn: func(ctx context.Context, cont tc.Container, runtime string) (int, string, error) {
				cname := fmt.Sprintf("ckpt-%d", time.Now().UnixNano())
				runCmd := []string{"docker", "run", "-d", "--name", cname}
				if runtime != "" {
					runCmd = append(runCmd, "--runtime", runtime)
				}
				runCmd = append(runCmd, "alpine", "sh", "-c", "echo hello > /checkpoint_file; tail -f /dev/null")
				if code, err := execNoOutput(ctx, cont, runCmd...); err != nil {
					return code, "", fmt.Errorf("start container: %w", err)
				}
				defer cont.Exec(ctx, []string{"docker", "rm", "-f", cname})
				defer cont.Exec(ctx, []string{"docker", "checkpoint", "rm", cname, "ckpt"})

				if code, err := execNoOutput(ctx, cont, "docker", "checkpoint", "create", cname, "ckpt"); err != nil {
					return code, "", fmt.Errorf("create checkpoint: %w", err)
				}

				if code, err := execNoOutput(ctx, cont, "docker", "start", "--checkpoint", "ckpt", cname); err != nil {
					return code, "", fmt.Errorf("start from checkpoint: %w", err)
				}

				execCmd := []string{"docker", "exec", cname, "cat", "/checkpoint_file"}
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
			name: "pause/unpause",
			fn: func(ctx context.Context, cont tc.Container, runtime string) (int, string, error) {
				cname := fmt.Sprintf("pause-%d", time.Now().UnixNano())
				runCmd := []string{"docker", "run", "-d", "--name", cname}
				if runtime != "" {
					runCmd = append(runCmd, "--runtime", runtime)
				}
				runCmd = append(runCmd, "alpine", "sleep", "infinity")
				if code, err := execNoOutput(ctx, cont, runCmd...); err != nil {
					return code, "", fmt.Errorf("start container: %w", err)
				}
				defer cont.Exec(ctx, []string{"docker", "rm", "-f", cname})

				if code, err := execNoOutput(ctx, cont, "docker", "pause", cname); err != nil {
					return code, "", fmt.Errorf("pause container: %w", err)
				}

				if code, err := execNoOutput(ctx, cont, "docker", "unpause", cname); err != nil {
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
			fn: func(ctx context.Context, cont tc.Container, runtime string) (int, string, error) {
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
				defer cont.Exec(ctx, []string{"docker", "rm", "-f", cname})

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

			if err := c.verify(runcCode, runcOut, delegatecCode, delegatecOut); err != nil {
				logDelegatecLogs(t, ctx, cont)
				logRuncLogs(t, ctx, cont)
				t.Fatal(err)
			}
		})
	}
}
