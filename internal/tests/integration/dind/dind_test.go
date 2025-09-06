//go:build e2e
// +build e2e

package dind

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"io"
	"runtime/pprof"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/docker/docker/pkg/stdcopy"
	tc "github.com/testcontainers/testcontainers-go"
	tcexec "github.com/testcontainers/testcontainers-go/exec"

	dindutil "github.com/TheGrizzlyDev/vino/internal/tests/dindutil"
	"github.com/TheGrizzlyDev/vino/internal/tests/testutil"
)

var dindParallel = flag.Int("dind.parallel", 4, "number of dind containers to run in parallel")

func logCriuCheck(t *testing.T, ctx context.Context, cont tc.Container) {
	t.Helper()
	code, reader, err := cont.Exec(ctx, []string{"criu", "check"})
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
	pool := dindutil.NewPool(t, *dindParallel, "alpine:latest", "nginx:latest")

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
	type result = testutil.Result
	type verifyFn func(map[string]result) error
	var defaultVerify = func(wantCode int) verifyFn {
		return func(results map[string]result) error {
			var (
				lastRuntime string
				lastResult  result
				seen        bool
			)
			for runtime, res := range results {
				res.Output = strings.TrimSpace(res.Output)
				if !seen {
					if res.ExitCode != wantCode {
						return fmt.Errorf("unexpected exit code: got %d want %d", res.ExitCode, wantCode)
					}
					lastRuntime = runtime
					lastResult = res
					seen = true
					continue
				}
				if lastResult.ExitCode != res.ExitCode || lastResult.Output != res.Output {
					return fmt.Errorf("mismatch: %s [%d] %q vs %s [%d] %q",
						lastRuntime,
						lastResult.ExitCode,
						lastResult.Output,
						runtime,
						res.ExitCode,
						res.Output,
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
		{name: "echo", fn: testutil.SimpleDockerRun("alpine", "echo", "hello"), verify: defaultVerify(0)},
		{name: "false", fn: testutil.SimpleDockerRun("alpine", "false"), verify: defaultVerify(1)},
		{name: "env", fn: testutil.SimpleDockerRun("-e", "FOO=bar", "alpine", "sh", "-c", "echo $FOO"), verify: defaultVerify(0)},
		{name: "volume", fn: testutil.SimpleDockerRun("-v", "/:/data", "alpine", "sh", "-c", "test -f /data/go.mod"), verify: defaultVerify(0)},
		{name: "workdir", fn: testutil.SimpleDockerRun("-w", "/tmp", "alpine", "pwd"), verify: defaultVerify(0)},
		{
			name: "docker cp",
			fn: func(t *testing.T, ctx context.Context, cont tc.Container, runtime string) (int, string, error) {
				// Prepare a source file inside the DinD container to avoid host-to-container copy flakiness.
				// Use sh -c with a passed argument to avoid quoting issues.
				if code, _, _, err := dindutil.ExecNoOutput(ctx, cont, "sh", "-c", "printf \"%s\" \"$1\" > /tmp/in.txt", "placeholder", cpContent); err != nil || code != 0 {
					return code, "", fmt.Errorf("prepare source file: %v", err)
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

				if code, out, serr, err := dindutil.ExecNoOutput(ctx, cont, "docker", "cp", fmt.Sprintf("%s:/tmp/in.txt", cname), "/tmp/out.txt"); err != nil || code != 0 {
					return code, "", fmt.Errorf("copy out: %v stdout:%s stderr:%s", err, out, serr)
				}
				// Verify the file was copied to the DinD host
				if code, out, serr, err := dindutil.ExecNoOutput(ctx, cont, "cat", "/tmp/out.txt"); err != nil || code != 0 {
					return code, "", fmt.Errorf("verify out file: %v stdout:%s stderr:%s", err, out, serr)
				} else if strings.TrimSpace(out) != cpContent {
					return code, "", fmt.Errorf("DinD host content mismatch: %q", out)
				}
				// Return the content we just verified instead of trying to copy from container
				return 0, cpContent, nil
			},
			verify: func(results map[string]result) error {
				if err := defaultVerify(0)(results); err != nil {
					return err
				}
				for _, r := range results {
					if strings.TrimSpace(r.Output) != cpContent {
						return fmt.Errorf("unexpected file content: %q", r.Output)
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
				out, err := dindutil.ReadAll(ctx, reader)
				return code, string(out), err
			},
			verify: defaultVerify(0),
		},
		{
			name: "exec after run",
			fn: func(t *testing.T, ctx context.Context, cont tc.Container, runtime string) (int, string, error) {
				cname := testutil.CreateNamedContainer(t, ctx, cont, runtime, "bgtest", "alpine", "tail", "-f", "/dev/null")
				return testutil.DockerExec(ctx, cont, cname, "sh", "-c", "echo hello")
			},
			verify: defaultVerify(0),
		},
		{name: "memory limit", fn: testutil.SimpleDockerRun("-m", "32m", "alpine", "sh", "-c", "cat /sys/fs/cgroup/memory.max"), verify: testutil.ExpectExactOutput(0, "33554432")},
		{name: "cpu limit", fn: testutil.SimpleDockerRun("--cpus", "0.5", "alpine", "sh", "-c", "cat /sys/fs/cgroup/cpu.max"), verify: testutil.ExpectExactOutput(0, "50000 100000")},
		{name: "update limits", fn: testutil.ContainerWithUpdate("update", []string{"docker", "update", "--memory", "32m", "--memory-swap", "64m"}, []string{"cat", "/sys/fs/cgroup/memory.max"}), verify: testutil.ExpectExactOutput(0, "33554432")},
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
						data, err := dindutil.ReadAll(pollCtx, reader)
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
			fn:   testutil.SimpleDockerRun("--user", "1000:1000", "alpine", "sh", "-c", "id -u; id -g; cat /proc/self/status | grep CapEff; ping -c 1 127.0.0.1"),
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
					if err := verifyOut(runtime, res.Output); err != nil {
						return err
					}
					if lastExit == nil {
						lastRuntime = runtime
						lastExit = &res.ExitCode
						continue
					}
					if *lastExit != res.ExitCode {
						return fmt.Errorf("mismatch: %s [%d] vs %s [%d]", lastRuntime, *lastExit, runtime, res.ExitCode)
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
				data, err := dindutil.ReadAll(ctx, reader)
				return code, string(data), err
			},
			verify: defaultVerify(0),
		},
		{
			name: "pause/unpause",
			fn: func(t *testing.T, ctx context.Context, cont tc.Container, runtime string) (int, string, error) {
				cname := testutil.CreateNamedContainer(t, ctx, cont, runtime, "pause", "alpine", "sleep", "infinity")

				if code, _, _, err := dindutil.ExecNoOutput(ctx, cont, "docker", "pause", cname); err != nil || code != 0 {
					return code, "", fmt.Errorf("pause container: %w", err)
				}

				if code, _, _, err := dindutil.ExecNoOutput(ctx, cont, "docker", "unpause", cname); err != nil || code != 0 {
					return code, "", fmt.Errorf("unpause container: %w", err)
				}

				return testutil.DockerExec(ctx, cont, cname, "sh", "-c", "echo hello")
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
				out, err := dindutil.ReadAll(ctx, reader)
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
						lastExit = &res.ExitCode
					} else if *lastExit != res.ExitCode {
						return fmt.Errorf("unexpected exit code: %s %d vs %s %d", lastRuntime, *lastExit, runtime, res.ExitCode)
					}
					if res.ExitCode != 0 {
						return fmt.Errorf("unexpected exit code: %s %d", runtime, res.ExitCode)
					}
					if !strings.Contains(res.Output, "sleep") {
						return fmt.Errorf("%s output missing 'sleep': %q", runtime, res.Output)
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
				out, err := dindutil.ReadAll(ctx, reader)
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
					if strings.TrimSpace(r.Output) != "stdout\nstderr" {
						return fmt.Errorf("unexpected logs: %q", r.Output)
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
				out, err := dindutil.ReadAll(ctx, reader)
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
				cname := testutil.CreateNamedContainer(t, ctx, cont, runtime, "restart", "alpine", "sleep", "infinity")

				if code, _, _, err := dindutil.ExecNoOutput(ctx, cont, "docker", "restart", cname); err != nil || code != 0 {
					return code, "", fmt.Errorf("restart container: %w", err)
				}

				return testutil.DockerExec(ctx, cont, cname, "sh", "-c", "echo restarted")
			},
			verify: testutil.ExpectExactOutput(0, "restarted"),
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
					if strings.TrimSpace(r.Output) != "healthy" {
						return fmt.Errorf("unexpected health status: %q", r.Output)
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
					if strings.TrimSpace(r.Output) != "0" {
						return fmt.Errorf("unexpected output: %q", r.Output)
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
						data, err := dindutil.ReadAll(pollCtx, reader)
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
			verify: testutil.ExpectExactOutput(0, "hello"),
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
			verify: testutil.ExpectExactOutput(0, "hello"),
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
				out, err := dindutil.ReadAll(ctx, reader)
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
				runCmd = append(runCmd, "alpine", "sh", "-c", "sleep 1; for i in 1 2 3; do echo $i; sleep 1; done")
				if code, _, _, err := dindutil.ExecNoOutput(ctx, cont, runCmd...); err != nil {
					return code, "", fmt.Errorf("start container: %w", err)
				}
				t.Cleanup(func() { cont.Exec(ctx, []string{"docker", "rm", "-f", cname}) })

				attachCmd := []string{"docker", "attach", cname}
				code, reader, err := cont.Exec(ctx, attachCmd, tcexec.Multiplexed())
				if err != nil {
					return code, "", err
				}
				out, err := dindutil.ReadAll(ctx, reader)
				if err != nil {
					return code, "", err
				}
				return code, string(out), nil
			},
			verify: testutil.ExpectExactOutput(0, "1\n2\n3"),
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
					// Combined, order-specific debug for current runtimes
					testutil.CombineDebug(testutil.DebugDelegatec, testutil.DebugRunc)(t, logCtx, cont)
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
				results[rt] = result{Output: out, ExitCode: code}
			}
			if err := c.verify(results); err != nil {
				t.Fatal(err)
			}
		})
	}
}
