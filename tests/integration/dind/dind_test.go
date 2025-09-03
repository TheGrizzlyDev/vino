//go:build e2e
// +build e2e

package dind

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime/pprof"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/docker/docker/pkg/stdcopy"
	tc "github.com/testcontainers/testcontainers-go"
	tcexec "github.com/testcontainers/testcontainers-go/exec"

	dindutil "github.com/TheGrizzlyDev/vino/tests/dindutil"
)

var dindParallel = flag.Int("dind.parallel", 4, "number of dind containers to run in parallel")

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
	pool := dindutil.NewPool(t, *dindParallel)

	// requireCheckpointSupport verifies that the host supports container
	// checkpoint/restore by running `criu check` and `docker checkpoint ls`
	// on a dummy container. If either command fails or reports unsupported
	// features, the calling test is skipped. This helps maintainers avoid
	// spurious failures when the kernel or Docker daemon lacks CRIU support.
	requireCheckpointSupport := func(t *testing.T, ctx context.Context, cont tc.Container) {
		t.Helper()
		if code, out, serr, err := dindutil.ExecNoOutput(ctx, cont, "criu", "check"); err != nil || code != 0 || strings.Contains(strings.ToLower(out+serr), "unsupported") {
			t.Skipf("skipping checkpoint restore: criu check failed (exit %d): %v\nstdout:\n%s\nstderr:\n%s", code, err, out, serr)
		}

		cname := fmt.Sprintf("ckpt-precheck-%d", time.Now().UnixNano())
		runCmd := []string{"docker", "run", "-d", "--name", cname, "alpine", "sleep", "infinity"}
		if code, _, _, err := dindutil.ExecNoOutput(ctx, cont, runCmd...); err != nil || code != 0 {
			t.Skipf("skipping checkpoint restore: failed to start dummy container: %v", err)
		}
		t.Cleanup(func() { cont.Exec(ctx, []string{"docker", "rm", "-f", cname}) })

		if code, out, serr, err := dindutil.ExecNoOutput(ctx, cont, "docker", "checkpoint", "ls", cname); err != nil || code != 0 || strings.Contains(strings.ToLower(out+serr), "unsupported") {
			t.Skipf("skipping checkpoint restore: docker checkpoint ls failed (exit %d): %v\nstdout:\n%s\nstderr:\n%s", code, err, out, serr)
		}
	}

	type caseFn func(*testing.T, context.Context, tc.Container, string) (int, string, error)
	type result struct {
		stdout string
		exit   int
	}
	type verifyFn func(map[string]result) error
	var defaultVerify = func(wantCode int) verifyFn {
		return func(results map[string]result) error {
			var (
				lastRuntime string
				lastResult  result
				seen        bool
			)
			for runtime, res := range results {
				res.stdout = strings.TrimSpace(res.stdout)
				if !seen {
					if res.exit != wantCode {
						return fmt.Errorf("unexpected exit code: got %d want %d", res.exit, wantCode)
					}
					lastRuntime = runtime
					lastResult = res
					seen = true
					continue
				}
				if lastResult.exit != res.exit || lastResult.stdout != res.stdout {
					return fmt.Errorf("mismatch: %s [%d] %q vs %s [%d] %q",
						lastRuntime,
						lastResult.exit,
						lastResult.stdout,
						runtime,
						res.exit,
						res.stdout,
					)
				}
			}
			if !seen {
				return fmt.Errorf("no results")
			}
			return nil
		}
	}
	const cpContent = "hello from host"
	cases := []struct {
		name     string
		runtimes []string
		fn       caseFn
		verify   verifyFn
		pretest  func(*testing.T, context.Context, tc.Container)
	}{
		{
			name: "echo",
			fn: func(_ *testing.T, ctx context.Context, cont tc.Container, runtime string) (int, string, error) {
				return dindutil.RunDocker(ctx, cont, runtime, "alpine", "echo", "hello")
			},
			verify: defaultVerify(0),
		},
		{
			name: "false",
			fn: func(_ *testing.T, ctx context.Context, cont tc.Container, runtime string) (int, string, error) {
				return dindutil.RunDocker(ctx, cont, runtime, "alpine", "false")
			},
			verify: defaultVerify(1),
		},
		{
			name: "env",
			fn: func(_ *testing.T, ctx context.Context, cont tc.Container, runtime string) (int, string, error) {
				return dindutil.RunDocker(ctx, cont, runtime, "-e", "FOO=bar", "alpine", "sh", "-c", "echo $FOO")
			},
			verify: defaultVerify(0),
		},
		{
			name: "volume",
			fn: func(_ *testing.T, ctx context.Context, cont tc.Container, runtime string) (int, string, error) {
				return dindutil.RunDocker(ctx, cont, runtime, "-v", "/:/data", "alpine", "sh", "-c", "test -f /data/go.mod")
			},
			verify: defaultVerify(0),
		},
		{
			name: "workdir",
			fn: func(_ *testing.T, ctx context.Context, cont tc.Container, runtime string) (int, string, error) {
				return dindutil.RunDocker(ctx, cont, runtime, "-w", "/tmp", "alpine", "pwd")
			},
			verify: defaultVerify(0),
		},
		{
			name: "docker cp",
			fn: func(t *testing.T, ctx context.Context, cont tc.Container, runtime string) (int, string, error) {
				tmpDir := t.TempDir()
				hostFile := filepath.Join(tmpDir, "cp.txt")
				if err := os.WriteFile(hostFile, []byte(cpContent), 0o600); err != nil {
					return 0, "", fmt.Errorf("write temp file: %w", err)
				}
				if err := cont.CopyFileToContainer(ctx, hostFile, "/tmp/in.txt", 0o600); err != nil {
					return 0, "", fmt.Errorf("copy to container: %w", err)
				}

				cname := fmt.Sprintf("cp-%s-%d", runtime, time.Now().UnixNano())
				runCmd := []string{"docker", "run", "-d", "--name", cname}
				if runtime != "" {
					runCmd = append(runCmd, "--runtime", runtime)
				}
				runCmd = append(runCmd, "alpine", "sleep", "infinity")
				if code, _, _, err := dindutil.ExecNoOutput(ctx, cont, runCmd...); err != nil {
					return code, "", fmt.Errorf("start container: %w", err)
				}
				t.Cleanup(func() { cont.Exec(ctx, []string{"docker", "rm", "-f", cname}) })

				if code, _, _, err := dindutil.ExecNoOutput(ctx, cont, "docker", "cp", "/tmp/in.txt", fmt.Sprintf("%s:/tmp/in.txt", cname)); err != nil {
					return code, "", fmt.Errorf("copy in: %w", err)
				}

				if code, out, serr, err := dindutil.ExecNoOutput(ctx, cont, "docker", "exec", cname, "cat", "/tmp/in.txt"); err != nil || code != 0 {
					return code, "", fmt.Errorf("exec cat: %v stdout:%s stderr:%s", err, out, serr)
				} else if strings.TrimSpace(out) != cpContent {
					return code, "", fmt.Errorf("container content mismatch: %q", out)
				}

				if code, _, _, err := dindutil.ExecNoOutput(ctx, cont, "docker", "cp", fmt.Sprintf("%s:/tmp/in.txt", cname), "/tmp/out.txt"); err != nil {
					return code, "", fmt.Errorf("copy out: %w", err)
				}
				rc, err := cont.CopyFileFromContainer(ctx, "/tmp/out.txt")
				if err != nil {
					return 0, "", fmt.Errorf("copy from container: %w", err)
				}
				defer rc.Close()
				data, err := io.ReadAll(rc)
				if err != nil {
					return 0, "", err
				}
				return 0, string(data), nil
			},
			verify: func(results map[string]result) error {
				if err := defaultVerify(0)(results); err != nil {
					return err
				}
				for _, r := range results {
					if strings.TrimSpace(r.stdout) != cpContent {
						return fmt.Errorf("unexpected file content: %q", r.stdout)
					}
					break
				}
				return nil
			},
		},
		{
			name: "tty stdin",
			fn: func(_ *testing.T, ctx context.Context, cont tc.Container, runtime string) (int, string, error) {
				cname := fmt.Sprintf("ttytest-%s", runtime)
				run := fmt.Sprintf("printf 'hello\\n' | script -qec \"docker run -it --name %s", cname)
				if runtime != "" {
					run += fmt.Sprintf(" --runtime %s", runtime)
				}
				run += " alpine cat\" /dev/null"
				code, reader, err := cont.Exec(ctx, []string{"sh", "-c", run}, tcexec.Multiplexed())
				_, _, _, _ = dindutil.ExecNoOutput(ctx, cont, "docker", "rm", "-f", cname)
				if err != nil {
					return code, "", err
				}
				out, err := io.ReadAll(reader)
				return code, string(out), err
			},
			verify: defaultVerify(0),
		},
		{
			name: "exec after run",
			fn: func(t *testing.T, ctx context.Context, cont tc.Container, runtime string) (int, string, error) {
				cname := fmt.Sprintf("bgtest-%s-%d", runtime, time.Now().UnixNano())
				runCmd := []string{"docker", "run", "-d", "--name", cname}
				if runtime != "" {
					runCmd = append(runCmd, "--runtime", runtime)
				}
				runCmd = append(runCmd, "alpine", "tail", "-f", "/dev/null")
				if code, _, _, err := dindutil.ExecNoOutput(ctx, cont, runCmd...); err != nil {
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
				return dindutil.RunDocker(ctx, cont, runtime, cmd...)
			},
			verify: func(results map[string]result) error {
				if err := defaultVerify(0)(results); err != nil {
					return err
				}
				for _, r := range results {
					if strings.TrimSpace(r.stdout) != "33554432" {
						return fmt.Errorf("unexpected memory limit: %s", strings.TrimSpace(r.stdout))
					}
					break
				}
				return nil
			},
		},
		{
			name: "cpu limit",
			fn: func(_ *testing.T, ctx context.Context, cont tc.Container, runtime string) (int, string, error) {
				cmd := []string{"--cpus", "0.5", "alpine", "sh", "-c", "cat /sys/fs/cgroup/cpu.max"}
				return dindutil.RunDocker(ctx, cont, runtime, cmd...)
			},
			verify: func(results map[string]result) error {
				if err := defaultVerify(0)(results); err != nil {
					return err
				}
				for _, r := range results {
					if strings.TrimSpace(r.stdout) != "50000 100000" {
						return fmt.Errorf("unexpected cpu limit: %s", strings.TrimSpace(r.stdout))
					}
					break
				}
				return nil
			},
		},
		{
			name: "update limits",
			fn: func(t *testing.T, ctx context.Context, cont tc.Container, runtime string) (int, string, error) {
				cname := fmt.Sprintf("update-%s-%d", runtime, time.Now().UnixNano())
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
			verify: func(results map[string]result) error {
				if err := defaultVerify(0)(results); err != nil {
					return err
				}
				for _, r := range results {
					if strings.TrimSpace(r.stdout) != "33554432" {
						return fmt.Errorf("unexpected memory limit: %s", strings.TrimSpace(r.stdout))
					}
					break
				}
				return nil
			},
		},
		{
			name: "nginx port mapping",
			fn: func(t *testing.T, ctx context.Context, cont tc.Container, runtime string) (int, string, error) {
				cname := fmt.Sprintf("web-%s-%d", runtime, time.Now().UnixNano())
				code, _, err := dindutil.RunDocker(ctx, cont, runtime, "-d", "--name", cname, "-p", "8080:80", "nginx")
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
				return dindutil.RunDocker(ctx, cont, runtime, cmd...)
			},
			verify: func(results map[string]result) error {
				verifyOut := func(runtime, out string) error {
					if !strings.Contains(out, "1000\n1000") {
						return fmt.Errorf("unexpected uid/gid output: %q", out)
					}
					if !strings.Contains(out, "CapEff") {
						return fmt.Errorf("missing CapEff line: %q", out)
					}
					return nil
				}
				var (
					lastRuntime string
					lastExit    *int
				)
				for runtime, res := range results {
					if err := verifyOut(runtime, res.stdout); err != nil {
						return err
					}
					if lastExit == nil {
						lastRuntime = runtime
						lastExit = &res.exit
						continue
					}
					if *lastExit != res.exit {
						return fmt.Errorf("mismatch: %s [%d] vs %s [%d]", lastRuntime, *lastExit, runtime, res.exit)
					}
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
				cname := fmt.Sprintf("ckpt-%s-%d", runtime, time.Now().UnixNano())
				runCmd := []string{"docker", "run", "-d", "--name", cname}
				if runtime != "" {
					runCmd = append(runCmd, "--runtime", runtime)
				}
				runCmd = append(runCmd, "alpine", "sh", "-c", "echo hello > /checkpoint_file; tail -f /dev/null")
				if code, _, _, err := dindutil.ExecNoOutput(ctx, cont, runCmd...); err != nil {
					return code, "", fmt.Errorf("start container: %w", err)
				}
				t.Cleanup(func() { cont.Exec(ctx, []string{"docker", "rm", "-f", cname}) })
				t.Cleanup(func() { cont.Exec(ctx, []string{"docker", "checkpoint", "rm", cname, "ckpt"}) })

				code, out, serr, err := dindutil.ExecNoOutput(ctx, cont, "docker", "checkpoint", "create", "--debug", cname, "ckpt")
				t.Logf("docker checkpoint create stdout:\n%s", out)
				t.Logf("docker checkpoint create stderr:\n%s", serr)
				if err != nil {
					return code, "", fmt.Errorf("create checkpoint: %w", err)
				}

				if code, _, _, err := dindutil.ExecNoOutput(ctx, cont, "docker", "start", "--checkpoint", "ckpt", cname); err != nil {
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
				cname := fmt.Sprintf("pause-%s-%d", runtime, time.Now().UnixNano())
				runCmd := []string{"docker", "run", "-d", "--name", cname}
				if runtime != "" {
					runCmd = append(runCmd, "--runtime", runtime)
				}
				runCmd = append(runCmd, "alpine", "sleep", "infinity")
				if code, _, _, err := dindutil.ExecNoOutput(ctx, cont, runCmd...); err != nil {
					return code, "", fmt.Errorf("start container: %w", err)
				}
				t.Cleanup(func() { cont.Exec(ctx, []string{"docker", "rm", "-f", cname}) })

				if code, _, _, err := dindutil.ExecNoOutput(ctx, cont, "docker", "pause", cname); err != nil {
					return code, "", fmt.Errorf("pause container: %w", err)
				}

				if code, _, _, err := dindutil.ExecNoOutput(ctx, cont, "docker", "unpause", cname); err != nil {
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
				cname := fmt.Sprintf("top-%s-%d", runtime, time.Now().UnixNano())
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
			verify: func(results map[string]result) error {
				var (
					lastRuntime string
					lastExit    *int
				)
				for runtime, res := range results {
					if lastExit == nil {
						lastRuntime = runtime
						lastExit = &res.exit
					} else if *lastExit != res.exit {
						return fmt.Errorf("unexpected exit code: %s %d vs %s %d", lastRuntime, *lastExit, runtime, res.exit)
					}
					if res.exit != 0 {
						return fmt.Errorf("unexpected exit code: %s %d", runtime, res.exit)
					}
					if !strings.Contains(res.stdout, "sleep") {
						return fmt.Errorf("%s output missing 'sleep': %q", runtime, res.stdout)
					}
				}
				return nil
			},
		},
		{
			name: "container logs",
			fn: func(_ *testing.T, ctx context.Context, cont tc.Container, runtime string) (int, string, error) {
				cname := fmt.Sprintf("logtest-%s-%d", runtime, time.Now().UnixNano())
				runCmd := []string{"docker", "run", "--name", cname}
				if runtime != "" {
					runCmd = append(runCmd, "--runtime", runtime)
				}
				runCmd = append(runCmd, "alpine", "sh", "-c", "echo stdout; echo stderr >&2")
				code, reader, err := cont.Exec(ctx, runCmd, tcexec.Multiplexed())
				if reader != nil {
					io.Copy(io.Discard, reader)
				}
				t.Cleanup(func() { cont.Exec(ctx, []string{"docker", "rm", "-f", cname}) })
				if err != nil {
					return code, "", err
				}

				logCmd := []string{"docker", "logs", cname}
				lcode, reader, err := cont.Exec(ctx, logCmd, tcexec.Multiplexed())
				if err != nil {
					if reader != nil {
						io.Copy(io.Discard, reader)
					}
					return lcode, "", err
				}
				out, err := io.ReadAll(reader)
				if err != nil {
					return lcode, "", err
				}
				if lcode != 0 {
					return lcode, "", fmt.Errorf("docker logs exit %d: %s", lcode, strings.TrimSpace(string(out)))
				}
				return code, string(out), nil
			},
			verify: func(results map[string]result) error {
				if err := defaultVerify(0)(results); err != nil {
					return err
				}
				for _, r := range results {
					if strings.TrimSpace(r.stdout) != "stdout\nstderr" {
						return fmt.Errorf("unexpected logs: %q", r.stdout)
					}
					break
				}
				return nil
			},
		},
		{
			name: "kill sigkill",
			fn: func(t *testing.T, ctx context.Context, cont tc.Container, runtime string) (int, string, error) {
				cname := fmt.Sprintf("kill-kill-%s-%d", runtime, time.Now().UnixNano())
				runCmd := []string{"docker", "run", "-d", "--name", cname}
				if runtime != "" {
					runCmd = append(runCmd, "--runtime", runtime)
				}
				runCmd = append(runCmd, "alpine", "sleep", "infinity")
				if code, _, _, err := dindutil.ExecNoOutput(ctx, cont, runCmd...); err != nil {
					return code, "", fmt.Errorf("start container: %w", err)
				}
				t.Cleanup(func() { cont.Exec(ctx, []string{"docker", "rm", "-f", cname}) })

				if code, _, _, err := dindutil.ExecNoOutput(ctx, cont, "docker", "kill", "--signal", "SIGKILL", cname); err != nil {
					return code, "", fmt.Errorf("kill container: %w", err)
				}

				code, reader, err := cont.Exec(ctx, []string{"docker", "wait", cname}, tcexec.Multiplexed())
				if err != nil {
					return code, "", err
				}
				out, err := io.ReadAll(reader)
				if err != nil {
					return code, "", err
				}
				if code != 0 {
					return code, "", fmt.Errorf("docker wait exit %d: %s", code, strings.TrimSpace(string(out)))
				}
				exitCode, err := strconv.Atoi(strings.TrimSpace(string(out)))
				if err != nil {
					return code, "", fmt.Errorf("parse exit code: %w", err)
				}
				return exitCode, "", nil
			},
			verify: defaultVerify(137),
		},
		{
			name: "restart",
			fn: func(t *testing.T, ctx context.Context, cont tc.Container, runtime string) (int, string, error) {
				cname := fmt.Sprintf("restart-%s-%d", runtime, time.Now().UnixNano())
				runCmd := []string{"docker", "run", "-d", "--name", cname}
				if runtime != "" {
					runCmd = append(runCmd, "--runtime", runtime)
				}
				runCmd = append(runCmd, "alpine", "sleep", "infinity")
				if code, _, _, err := dindutil.ExecNoOutput(ctx, cont, runCmd...); err != nil {
					return code, "", fmt.Errorf("start container: %w", err)
				}
				t.Cleanup(func() { cont.Exec(ctx, []string{"docker", "rm", "-f", cname}) })

				if code, _, _, err := dindutil.ExecNoOutput(ctx, cont, "docker", "restart", cname); err != nil {
					return code, "", fmt.Errorf("restart container: %w", err)
				}

				execCmd := []string{"docker", "exec", cname, "sh", "-c", "echo restarted"}
				code, reader, err := cont.Exec(ctx, execCmd, tcexec.Multiplexed())
				if err != nil {
					return code, "", err
				}
				out, err := io.ReadAll(reader)
				return code, string(out), err
			},
			verify: func(results map[string]result) error {
				if err := defaultVerify(0)(results); err != nil {
					return err
				}
				for _, r := range results {
					if strings.TrimSpace(r.stdout) != "restarted" {
						return fmt.Errorf("unexpected output: %q", r.stdout)
					}
					break
				}
				return nil
			},
		},
		{
			name: "healthcheck",
			fn: func(t *testing.T, ctx context.Context, cont tc.Container, runtime string) (int, string, error) {
				cname := fmt.Sprintf("health-%s-%d", runtime, time.Now().UnixNano())
				runCmd := []string{"docker", "run", "-d", "--name", cname, "--health-cmd", "true", "--health-interval", "1s"}
				if runtime != "" {
					runCmd = append(runCmd, "--runtime", runtime)
				}
				runCmd = append(runCmd, "alpine", "sleep", "infinity")
				if code, _, _, err := dindutil.ExecNoOutput(ctx, cont, runCmd...); err != nil {
					return code, "", fmt.Errorf("start container: %w", err)
				}
				t.Cleanup(func() { cont.Exec(ctx, []string{"docker", "rm", "-f", cname}) })

				inspectCmd := []string{"docker", "inspect", "--format", "{{.State.Health.Status}}", cname}
				var status string
				for i := 0; i < 10; i++ {
					code, out, _, err := dindutil.ExecNoOutput(ctx, cont, inspectCmd...)
					if err == nil && code == 0 {
						s := strings.TrimSpace(out)
						if s == "healthy" || s == "unhealthy" {
							status = s
							break
						}
					}
					time.Sleep(time.Second)
				}
				if status == "" {
					return 1, "", fmt.Errorf("failed to get health status")
				}
				return 0, status, nil
			},
			verify: func(results map[string]result) error {
				if err := defaultVerify(0)(results); err != nil {
					return err
				}
				for _, r := range results {
					if strings.TrimSpace(r.stdout) != "healthy" {
						return fmt.Errorf("unexpected health status: %q", r.stdout)
					}
					break
				}
				return nil
			},
		},
		{
			name: "device mapping",
			fn: func(_ *testing.T, ctx context.Context, cont tc.Container, runtime string) (int, string, error) {
				cmd := []string{"--device", "/dev/null:/dev/testnull", "alpine", "sh", "-c", "echo hi > /dev/testnull && wc -c < /dev/testnull"}
				return dindutil.RunDocker(ctx, cont, runtime, cmd...)
			},
			verify: func(results map[string]result) error {
				if err := defaultVerify(0)(results); err != nil {
					return err
				}
				for _, r := range results {
					if strings.TrimSpace(r.stdout) != "0" {
						return fmt.Errorf("unexpected output: %q", r.stdout)
					}
					break
				}
				return nil
			},
		},
		{
			name: "host networking",
			fn: func(t *testing.T, ctx context.Context, cont tc.Container, runtime string) (int, string, error) {
				cname := fmt.Sprintf("hostnet-%s-%d", runtime, time.Now().UnixNano())
				runCmd := []string{"docker", "run", "-d", "--name", cname, "--network", "host"}
				if runtime != "" {
					runCmd = append(runCmd, "--runtime", runtime)
				}
				runCmd = append(runCmd, "hashicorp/http-echo", "-text", "hello", "-listen", ":8081")
				if code, _, _, err := dindutil.ExecNoOutput(ctx, cont, runCmd...); err != nil {
					return code, "", fmt.Errorf("start server: %w", err)
				}
				defer cont.Exec(ctx, []string{"docker", "rm", "-f", cname})

				pollCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
				defer cancel()

				for {
					code, reader, err := cont.Exec(pollCtx, []string{"curl", "-fsSL", "--max-time", "1", "http://localhost:8081"}, tcexec.Multiplexed())
					if err == nil && code == 0 {
						data, err := io.ReadAll(reader)
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
						return code, "", err
					case <-time.After(100 * time.Millisecond):
					}
				}
			},
			verify: func(results map[string]result) error {
				if err := defaultVerify(0)(results); err != nil {
					return err
				}
				for _, r := range results {
					if strings.TrimSpace(r.stdout) != "hello" {
						return fmt.Errorf("unexpected response: %q", r.stdout)
					}
					break
				}
				return nil
			},
		},
		{
			name: "docker commit",
			fn: func(t *testing.T, ctx context.Context, cont tc.Container, runtime string) (int, string, error) {
				cname := fmt.Sprintf("commit-%s-%d", runtime, time.Now().UnixNano())
				imgName := fmt.Sprintf("commit-img-%s-%d", runtime, time.Now().UnixNano())
				runCmd := []string{"docker", "run", "-d", "--name", cname}
				if runtime != "" {
					runCmd = append(runCmd, "--runtime", runtime)
				}
				runCmd = append(runCmd, "alpine", "sh", "-c", "echo hello > /committed && sleep infinity")
				if code, _, _, err := dindutil.ExecNoOutput(ctx, cont, runCmd...); err != nil {
					return code, "", fmt.Errorf("start container: %w", err)
				}
				t.Cleanup(func() {
					cont.Exec(ctx, []string{"docker", "rm", "-f", cname})
					cont.Exec(ctx, []string{"docker", "rmi", "-f", imgName})
				})
				if code, _, _, err := dindutil.ExecNoOutput(ctx, cont, "docker", "commit", cname, imgName); err != nil {
					return code, "", fmt.Errorf("commit container: %w", err)
				}
				return dindutil.RunDocker(ctx, cont, runtime, imgName, "cat", "/committed")
			},
			verify: func(results map[string]result) error {
				if err := defaultVerify(0)(results); err != nil {
					return err
				}
				for _, r := range results {
					if strings.TrimSpace(r.stdout) != "hello" {
						return fmt.Errorf("unexpected output: %q", r.stdout)
					}
					break
				}
				return nil
			},
		},
		{
			name: "wait exited",
			fn: func(t *testing.T, ctx context.Context, cont tc.Container, runtime string) (int, string, error) {
				cname := fmt.Sprintf("wait-exited-%d", time.Now().UnixNano())
				runCmd := []string{"docker", "run", "-d", "--name", cname}
				if runtime != "" {
					runCmd = append(runCmd, "--runtime", runtime)
				}
				// run a short-lived command with a known exit code
				runCmd = append(runCmd, "alpine", "sh", "-c", "exit 7")
				if code, _, _, err := dindutil.ExecNoOutput(ctx, cont, runCmd...); err != nil {
					return code, "", fmt.Errorf("start container: %w", err)
				}

				code, reader, err := cont.Exec(ctx, []string{"docker", "wait", cname}, tcexec.Multiplexed())
				if err != nil {
					return code, "", err
				}
				out, err := io.ReadAll(reader)
				if err != nil {
					return code, "", err
				}
				if code != 0 {
					return code, "", fmt.Errorf("docker wait exit %d: %s", code, strings.TrimSpace(string(out)))
				}
				exitCode, err := strconv.Atoi(strings.TrimSpace(string(out)))
				if err != nil {
					return code, "", fmt.Errorf("parse exit code: %w", err)
				}

				if rmCode, _, _, rmErr := dindutil.ExecNoOutput(ctx, cont, "docker", "rm", cname); rmErr != nil {
					return exitCode, "", fmt.Errorf("remove container exit %d: %w", rmCode, rmErr)
				}

				return exitCode, "", nil
			},
			verify: defaultVerify(7),
		},
		{
			name: "docker attach",
			fn: func(t *testing.T, ctx context.Context, cont tc.Container, runtime string) (int, string, error) {
				cname := fmt.Sprintf("attach-%d", time.Now().UnixNano())
				runCmd := []string{"docker", "run", "-d", "--name", cname}
				if runtime != "" {
					runCmd = append(runCmd, "--runtime", runtime)
				}
				runCmd = append(runCmd, "alpine", "sh", "-c", "for i in 1 2 3; do echo $i; sleep 1; done")
				if code, _, _, err := dindutil.ExecNoOutput(ctx, cont, runCmd...); err != nil {
					return code, "", fmt.Errorf("start container: %w", err)
				}
				t.Cleanup(func() { cont.Exec(ctx, []string{"docker", "rm", "-f", cname}) })

				attachCmd := []string{"docker", "attach", cname}
				code, reader, err := cont.Exec(ctx, attachCmd, tcexec.Multiplexed())
				if err != nil {
					return code, "", err
				}
				out, err := io.ReadAll(reader)
				if err != nil {
					return code, "", err
				}
				return code, string(out), nil
			},
			verify: func(results map[string]result) error {
				if err := defaultVerify(0)(results); err != nil {
					return err
				}
				for _, r := range results {
					if strings.TrimSpace(r.stdout) != "1\n2\n3" {
						return fmt.Errorf("unexpected output: %q", r.stdout)
					}
					break
				}
				return nil
			},
		},
	}

	var (
		pendingMu sync.Mutex
		pending   = make(map[string]time.Time)
	)
	dump := func(prefix string, force bool) {
		pendingMu.Lock()
		names := make([]string, 0, len(pending))
		for n, start := range pending {
			names = append(names, fmt.Sprintf("%s (%s)", n, time.Since(start).Truncate(time.Second)))
		}
		pendingMu.Unlock()
		if len(names) == 0 && !force {
			return
		}
		msg := prefix
		if len(names) > 0 {
			msg = fmt.Sprintf("%s: %s", prefix, strings.Join(names, ", "))
		}
		fmt.Printf("%s\n", msg)
		t.Log(msg)
		var buf bytes.Buffer
		if err := pprof.Lookup("goroutine").WriteTo(&buf, 2); err == nil {
			fmt.Printf("goroutine dump:\n%s\n", buf.String())
			t.Logf("goroutine dump:\n%s", buf.String())
		} else {
			fmt.Printf("goroutine dump failed: %v\n", err)
			t.Logf("goroutine dump failed: %v", err)
		}
	}
	monCtx, cancelMon := context.WithCancel(context.Background())
	defer cancelMon()
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-monCtx.Done():
				return
			case <-ticker.C:
				dump("pending tests", false)
			}
		}
	}()
	if d, ok := t.Deadline(); ok {
		go func() {
			timer := time.NewTimer(time.Until(d))
			defer timer.Stop()
			select {
			case <-monCtx.Done():
				return
			case <-timer.C:
				dump("test deadline reached", true)
			}
		}()
	}

	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			pendingMu.Lock()
			pending[c.name] = time.Now()
			pendingMu.Unlock()
			defer func() {
				pendingMu.Lock()
				delete(pending, c.name)
				pendingMu.Unlock()
			}()
			t.Parallel()

			cont := pool.Acquire(t)

			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			defer cancel()

			t.Cleanup(func() {
				if t.Failed() {
					logCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
					defer cancel()
					dindutil.LogDelegatecLogs(t, logCtx, cont)
					dindutil.LogRuncLogs(t, logCtx, cont)
					logCriuCheck(t, logCtx, cont)
				}
			})

			if c.pretest != nil {
				c.pretest(t, ctx, cont)
			}

			runtimes := c.runtimes
			if len(runtimes) == 0 {
				runtimes = []string{"runc", "delegatec"}
			}
			results := make(map[string]result, len(runtimes))
			for _, rt := range runtimes {
				code, out, err := c.fn(t, ctx, cont, rt)
				if err != nil {
					t.Fatalf("%s exec failed: %v", rt, err)
				}
				results[rt] = result{stdout: out, exit: code}
			}
			if err := c.verify(results); err != nil {
				t.Fatal(err)
			}
		})
	}
}
