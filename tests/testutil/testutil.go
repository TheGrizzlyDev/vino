package testutil

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tc "github.com/testcontainers/testcontainers-go"

	dindutil "github.com/TheGrizzlyDev/vino/tests/dindutil"
)

type TestCase struct {
	Name        string
	Description string
	Runtime     string
	Setup       SetupFunc
	Execute     ExecuteFunc
	Verify      VerifyFunc
	Timeout     time.Duration
}

type SetupFunc func(*testing.T, context.Context, tc.Container) error
type ExecuteFunc func(*testing.T, context.Context, tc.Container) (int, string, error)
type VerifyFunc func(*testing.T, int, string, error) error
type DebugFunc func(*testing.T, context.Context, tc.Container)

type Result struct {
	ExitCode int
	Output   string
	Error    error
}

type TestRunner struct {
	Pool            *dindutil.Pool
	DefaultTimeout  time.Duration
	DefaultRuntime  string
	DebugOnFailure  bool
	CustomDebugFunc DebugFunc
}

func (tr *TestRunner) WithCustomDebug(debugFunc DebugFunc) *TestRunner {
	tr.CustomDebugFunc = debugFunc
	return tr
}

func (tr *TestRunner) RunTestCase(t *testing.T, testCase TestCase) {
	t.Helper()
	
	cont := tr.Pool.Acquire(t)
	
	timeout := testCase.Timeout
	if timeout == 0 {
		timeout = tr.DefaultTimeout
	}
	
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	t.Cleanup(func() {
		if t.Failed() && tr.DebugOnFailure {
			tr.logDebugInfo(t, context.Background(), cont)
		}
	})

	t.Logf("Running: %s", testCase.Description)

	if testCase.Setup != nil {
		if err := testCase.Setup(t, ctx, cont); err != nil {
			t.Fatalf("Setup failed: %v", err)
		}
	}

	exitCode, output, err := testCase.Execute(t, ctx, cont)

	if testCase.Verify != nil {
		if verifyErr := testCase.Verify(t, exitCode, output, err); verifyErr != nil {
			t.Fatalf("Verification failed: %v\nOutput: %s", verifyErr, output)
		}
	}
}

func (tr *TestRunner) RunTestCases(t *testing.T, testCases []TestCase) {
	for _, tc := range testCases {
		tc := tc
		t.Run(tc.Name, func(t *testing.T) {
			t.Parallel()
			tr.RunTestCase(t, tc)
		})
	}
}

func (tr *TestRunner) logDebugInfo(t *testing.T, ctx context.Context, cont tc.Container) {
	t.Helper()
	
	logCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	
	if tr.CustomDebugFunc != nil {
		tr.CustomDebugFunc(t, logCtx, cont)
		return
	}
	
	tr.logBasicDebugInfo(t, logCtx, cont)
}

func (tr *TestRunner) logBasicDebugInfo(t *testing.T, ctx context.Context, cont tc.Container) {
	t.Helper()
	
	name, _ := cont.Name(ctx)
	t.Logf("=== DEBUG INFO for container %s ===", name)
	
	if code, out, _, err := dindutil.ExecNoOutput(ctx, cont, "cat", "/etc/docker/daemon.json"); err == nil && code == 0 {
		t.Logf("Docker daemon config: %s", out)
	}
	
	if code, out, _, err := dindutil.ExecNoOutput(ctx, cont, "docker", "info", "--format", "{{.Runtimes}}"); err == nil && code == 0 {
		t.Logf("Available runtimes: %s", out)
	}
}

func DebugRunc(t *testing.T, ctx context.Context, cont tc.Container) {
	t.Helper()
	dindutil.LogRuncLogs(t, ctx, cont)
}

func DebugDelegatec(t *testing.T, ctx context.Context, cont tc.Container) {
	t.Helper()
	dindutil.LogDelegatecLogs(t, ctx, cont)
}

func DebugVino(t *testing.T, ctx context.Context, cont tc.Container) {
	t.Helper()
	
	name, _ := cont.Name(ctx)
	t.Logf("=== VINO DEBUG INFO for %s ===", name)
	
	if code, out, _, err := dindutil.ExecNoOutput(ctx, cont, "ls", "-la", "/usr/local/sbin/vino"); err == nil && code == 0 {
		t.Logf("Vino binary info: %s", out)
	}
	
	if code, out, _, err := dindutil.ExecNoOutput(ctx, cont, "which", "wine64"); err == nil && code == 0 {
		t.Logf("Wine64 location: %s", out)
	}
	
	cont.Exec(ctx, []string{"sh", "-c", "find /var/log -name '*vino*' 2>/dev/null | head -5 | while read f; do echo \"=== $f ===\"; head -20 \"$f\"; done"})
}

func CombineDebug(debugFuncs ...DebugFunc) DebugFunc {
	return func(t *testing.T, ctx context.Context, cont tc.Container) {
		for _, debugFunc := range debugFuncs {
			debugFunc(t, ctx, cont)
		}
	}
}

// Simple helper for creating named containers with cleanup
func CreateNamedContainer(t *testing.T, ctx context.Context, cont tc.Container, runtime, namePrefix string, args ...string) string {
	t.Helper()
	cname := fmt.Sprintf("%s-%d", namePrefix, time.Now().UnixNano())
	runCmd := []string{"docker", "run", "-d", "--name", cname}
	if runtime != "" {
		runCmd = append(runCmd, "--runtime", runtime)
	}
	runCmd = append(runCmd, args...)
	if code, _, _, err := dindutil.ExecNoOutput(ctx, cont, runCmd...); err != nil || code != 0 {
		t.Fatalf("failed to create container %s: %v", cname, err)
	}
	t.Cleanup(func() { cont.Exec(ctx, []string{"docker", "rm", "-f", cname}) })
	return cname
}

// Helper for executing docker exec commands
func DockerExec(ctx context.Context, cont tc.Container, containerName string, cmd ...string) (int, string, error) {
	execCmd := append([]string{"docker", "exec", containerName}, cmd...)
	code, out, _, err := dindutil.ExecNoOutput(ctx, cont, execCmd...)
	return code, out, err
}

// SimpleDockerRun creates a simple docker run execution function
func SimpleDockerRun(args ...string) func(*testing.T, context.Context, tc.Container, string) (int, string, error) {
	return func(_ *testing.T, ctx context.Context, cont tc.Container, runtime string) (int, string, error) {
		return dindutil.RunDocker(ctx, cont, runtime, args...)
	}
}

// ExpectExactOutput creates a verification function that checks for exact output match
func ExpectExactOutput(wantCode int, expectedOutput string) func(map[string]Result) error {
	return func(results map[string]Result) error {
		// First verify all results have the expected exit code
		for runtime, res := range results {
			if res.ExitCode != wantCode {
				return fmt.Errorf("%s: unexpected exit code: got %d want %d", runtime, res.ExitCode, wantCode)
			}
		}
		// Then check that at least one result has the expected output
		for _, r := range results {
			if strings.TrimSpace(r.Output) == expectedOutput {
				return nil
			}
		}
		return fmt.Errorf("no result matched expected output: %q", expectedOutput)
	}
}

// ContainerWithUpdate creates a container, runs update operations, then execs a command
func ContainerWithUpdate(namePrefix string, updateCmd []string, execCmd []string) func(*testing.T, context.Context, tc.Container, string) (int, string, error) {
	return func(t *testing.T, ctx context.Context, cont tc.Container, runtime string) (int, string, error) {
		cname := CreateNamedContainer(t, ctx, cont, runtime, namePrefix, "alpine", "tail", "-f", "/dev/null")
		
		// Run the update command
		updateArgs := append(updateCmd, cname)
		if code, _, _, err := dindutil.ExecNoOutput(ctx, cont, updateArgs...); err != nil || code != 0 {
			return code, "", fmt.Errorf("update failed: %w", err)
		}
		
		// Execute the final command
		return DockerExec(ctx, cont, cname, execCmd...)
	}
}

func ExpectSuccess() VerifyFunc {
	return func(t *testing.T, exitCode int, output string, err error) error {
		if err != nil {
			return fmt.Errorf("execution failed: %v", err)
		}
		if exitCode != 0 {
			return fmt.Errorf("unexpected exit code: got %d, want 0", exitCode)
		}
		return nil
	}
}

func ExpectSuccessWithOutput(expectedOutput string) VerifyFunc {
	return func(t *testing.T, exitCode int, output string, err error) error {
		if err := ExpectSuccess()(t, exitCode, output, err); err != nil {
			return err
		}
		if !strings.Contains(strings.TrimSpace(output), expectedOutput) {
			return fmt.Errorf("expected output to contain %q, got %q", expectedOutput, output)
		}
		return nil
	}
}

func ExpectExitCode(wantCode int) VerifyFunc {
	return func(t *testing.T, exitCode int, output string, err error) error {
		if err != nil {
			return fmt.Errorf("execution failed: %v", err)
		}
		if exitCode != wantCode {
			return fmt.Errorf("unexpected exit code: got %d, want %d", exitCode, wantCode)
		}
		return nil
	}
}

func BuildImageFromDockerfile(imageName, dockerfilePath string) SetupFunc {
	return func(t *testing.T, ctx context.Context, cont tc.Container) error {
		rootDir, err := filepath.Abs("../..")
		if err != nil {
			return fmt.Errorf("failed to get root dir: %v", err)
		}

		dockerfileFullPath := filepath.Join(rootDir, dockerfilePath)
		buildCmd := fmt.Sprintf("docker build -t %s -f %s %s", imageName, dockerfileFullPath, rootDir)
		
		t.Logf("Building image %s from %s", imageName, dockerfilePath)
		code, stdout, stderr, err := dindutil.ExecNoOutput(ctx, cont, "sh", "-c", buildCmd)
		if err != nil || code != 0 {
			return fmt.Errorf("failed to build %s: code=%d, err=%v\nstdout: %s\nstderr: %s", 
				imageName, code, err, stdout, stderr)
		}
		t.Logf("Successfully built image %s", imageName)
		return nil
	}
}

func CombineSetup(setups ...SetupFunc) SetupFunc {
	return func(t *testing.T, ctx context.Context, cont tc.Container) error {
		for _, setup := range setups {
			if err := setup(t, ctx, cont); err != nil {
				return err
			}
		}
		return nil
	}
}

func RunDockerCommand(runtime string, args ...string) ExecuteFunc {
	return func(t *testing.T, ctx context.Context, cont tc.Container) (int, string, error) {
		return dindutil.RunDocker(ctx, cont, runtime, args...)
	}
}

func RunDockerContainer(runtime, imageName string, cmd ...string) ExecuteFunc {
	return func(t *testing.T, ctx context.Context, cont tc.Container) (int, string, error) {
		args := append([]string{imageName}, cmd...)
		return dindutil.RunDocker(ctx, cont, runtime, args...)
	}
}

