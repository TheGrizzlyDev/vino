package runc

import (
	cli "github.com/TheGrizzlyDev/vino/internal/pkg/cli"
	"reflect"
	"strings"
	"testing"
)

/* -------------------------- shared constants/vars -------------------------- */

const (
	socketPath   = "/tmp/tty.sock"
	pidFilePath  = "/tmp/pid"
	workDir      = "/work"
	containerID  = "c1"
	userSpec     = "1000:1000"
	processJSON  = "proc.json"
	apparmor     = "docker-default"
	selinuxLbl   = "system_u:system_r:container_t:s0"
	cgroupPath   = "foo"
	shellPath    = "/bin/sh"
	delegatePath = "/tmp/delegate"
)

var (
	envVars     = []string{"FOO=1", "BAR=2"}
	additionalG = []uint{10, 20}
	execArgs    = []string{"-lc", "echo ok"}

	psForwardArgs = []string{"-o", "pid,comm", "-A"}

	updateCPUQuota int64  = 50000
	updateCPUPer   uint64 = 100000
	updateMem      int64  = 1 << 30 // 1GiB
	updateSwap     int64  = 2 << 30
)

/* ------------------------------- test helpers ------------------------------ */

func mustConvert(t *testing.T, cmd cli.Command) []string {
	t.Helper()
	argv, err := cli.ConvertToCmdline(cmd)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	return argv
}

func eq(t *testing.T, got, want []string) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("argv mismatch:\n  got:  %v\n  want: %v", got, want)
	}
}

func uintp(v uint) *uint { return &v }

/* ------------------------------- happy paths -------------------------------- */

func TestConvertToCmdline_Exec_Comprehensive(t *testing.T) {
	t.Parallel()

	cmd := Exec{
		ConsoleSocketOpt: ConsoleSocketOpt{ConsoleSocket: socketPath},
		DetachOpt:        DetachOpt{Detach: true},
		PidFileOpt:       PidFileOpt{PidFile: pidFilePath},
		PivotKeyringFDsOpt: PivotKeyringFDsOpt{
			NoPivot:      true,
			NoNewKeyring: true,
			PreserveFDs:  uintp(3),
		},
		Cwd:            workDir,
		Env:            envVars,
		Tty:            true,
		User:           userSpec,
		AdditionalGids: additionalG,
		Process:        processJSON,
		ProcessLabel:   selinuxLbl,
		AppArmor:       apparmor,
		NoNewPrivs:     true,
		Cap:            []string{"CAP_SYS_PTRACE"},
		IgnorePaused:   true,
		Cgroup:         cgroupPath,
		ContainerID:    containerID,
		Command:        shellPath,
		Args:           execArgs,
	}

	expected := []string{
		"exec",
		"--console-socket", socketPath,
		"--detach",
		"--pid-file", pidFilePath,
		"--ignore-paused",
		"--no-pivot",
		"--no-new-keyring",
		"--preserve-fds", "3",
		"--cwd", workDir,
		"--env", envVars[0],
		"--env", envVars[1],
		"--tty",
		"--user", userSpec,
		"--additional-gids", "10",
		"--additional-gids", "20",
		"--process", processJSON,
		"--process-label", selinuxLbl,
		"--apparmor", apparmor,
		"--no-new-privs",
		"--cap", "CAP_SYS_PTRACE",
		"--cgroup", cgroupPath,
		containerID, "--", shellPath, execArgs[0], execArgs[1],
	}

	got := mustConvert(t, cmd)
	eq(t, got, expected)
}

func TestConvertToCmdline_Kill_OrderAndOmissions(t *testing.T) {
	t.Parallel()

	withSignal := Kill{
		All:         true,
		ContainerID: "abc123",
		Signal:      "KILL",
	}
	expectedWithSignal := []string{"kill", "--all", "abc123", "KILL"}
	eq(t, mustConvert(t, withSignal), expectedWithSignal)

	withoutOptionals := Kill{
		All:         false,
		ContainerID: "abc123",
	}
	expectedWithout := []string{"kill", "abc123"}
	eq(t, mustConvert(t, withoutOptionals), expectedWithout)
}

func TestConvertToCmdline_Ps_ForwardArgsAndFormat(t *testing.T) {
	t.Parallel()

	cmd := Ps{
		FormatOpt:   FormatOpt{Format: "json"},
		ContainerID: "c9",
		PsArgs:      psForwardArgs,
	}
	expected := []string{"ps", "--format", "json", "c9", psForwardArgs[0], psForwardArgs[1], psForwardArgs[2]}
	eq(t, mustConvert(t, cmd), expected)
}

func TestConvertToCmdline_Update_SkipsZeroesAndHandlesNumerics(t *testing.T) {
	t.Parallel()

	cmd := Update{
		ContainerID:  "cid",
		ReadFromJSON: "-",
		CPUQuota:     &updateCPUQuota,
		CPUPeriod:    &updateCPUPer,
		CPUSetCPUs:   "0-3",
		Memory:       &updateMem,
		MemorySwap:   &updateSwap,
		// CPUShares, PidsLimit, BlkioWeight etc. omitted → should be skipped
	}
	expected := []string{
		"update",
		"cid",
		"-r", "-",
		"--cpu-quota", "50000",
		"--cpu-period", "100000",
		"--cpuset-cpus", "0-3",
		"--memory", "1073741824",
		"--memory-swap", "2147483648",
	}
	eq(t, mustConvert(t, cmd), expected)
}

func TestConvertToCmdline_Checkpoint_NumericAndSkipZero(t *testing.T) {
	t.Parallel()

	imagePath := "/images/cp"
	cmd := Checkpoint{
		ImagePath:   imagePath,
		StatusFD:    uintp(10),
		ContainerID: "X",
	}
	expected := []string{
		"checkpoint",
		"--image-path", imagePath,
		"--status-fd", "10",
		"X",
	}
	eq(t, mustConvert(t, cmd), expected)
}

func TestConvertToCmdline_SeparatorEmittedWhenGroupPresent(t *testing.T) {
	t.Parallel()

	onlyID := Exec{ContainerID: "only"}

	expected := []string{"exec", onlyID.ContainerID, "--"}

	eq(t, mustConvert(t, onlyID), expected)
}

func TestConvertToCmdline_EmbeddedOrder_Run(t *testing.T) {
	t.Parallel()

	cmd := Run{
		BundleOpt:        BundleOpt{Bundle: "/b"},
		ConsoleSocketOpt: ConsoleSocketOpt{ConsoleSocket: "/s"},
		PivotKeyringFDsOpt: PivotKeyringFDsOpt{
			NoPivot:     true,
			PreserveFDs: uintp(2),
		},
		NoSubreaper: true,
		Keep:        true,
		ContainerID: "C",
	}
	expected := []string{
		"run",
		"--bundle", "/b",
		"--console-socket", "/s",
		"--no-pivot",
		"--preserve-fds", "2",
		"--no-subreaper",
		"--keep",
		"C",
	}
	eq(t, mustConvert(t, cmd), expected)
}

func TestConvertToCmdline_Features(t *testing.T) {
	t.Parallel()

	cmd := Features{}
	expected := []string{"features"}
	eq(t, mustConvert(t, cmd), expected)
}

/* ---------------------------- negative test types --------------------------- */

// BadMissingGroupFlag: cli_flag present without cli_group.
type BadMissingGroupFlag struct {
	Oops bool `cli_flag:"--oops"`
}

func (BadMissingGroupFlag) Slots() cli.Slot {
	return cli.Group{Ordered: []cli.Slot{cli.Subcommand{Value: "bad-missing-group"}}}
}

// BadArgHasGroup: argument incorrectly sets cli_group.
type BadArgHasGroup struct {
	Thing string `cli_argument:"arg" cli_group:"nope"`
}

func (BadArgHasGroup) Slots() cli.Slot {
	return cli.Group{Ordered: []cli.Slot{cli.Subcommand{Value: "bad-arg-has-group"}, cli.Argument{Name: "arg"}}}
}

// BadGroupNotInGroupsList: flag references a group not listed in Slots().
type BadGroupNotInGroupsList struct {
	Flag string `cli_flag:"--flag" cli_group:"missing"`
}

func (BadGroupNotInGroupsList) Slots() cli.Slot {
	return cli.Group{Ordered: []cli.Slot{cli.Subcommand{Value: "bad-missing-in-list"}}, Unordered: []cli.Slot{cli.FlagGroup{Name: "global"}}}
}

// BadBothFlagAndArg: field illegally has both tags.
type BadBothFlagAndArg struct {
	Field string `cli_flag:"--flag" cli_group:"x" cli_argument:"arg"`
}

func (BadBothFlagAndArg) Slots() cli.Slot {
	return cli.Group{
		Unordered: []cli.Slot{cli.FlagGroup{Name: "x"}},
		Ordered:   []cli.Slot{cli.Subcommand{Value: "bad-both"}, cli.Argument{Name: "arg"}},
	}
}

// BadMultipleSeparatorsGroup: deprecated in Slots model.
// Removed: Multiple literal separators are allowed; no special-case validation.

// BadAltNoFlag: cli_flag_alternatives present without cli_flag.
type BadAltNoFlag struct {
	A bool `cli_flag_alternatives:"-a"`
}

func (BadAltNoFlag) Slots() cli.Slot {
	return cli.Group{Ordered: []cli.Slot{cli.Subcommand{Value: "bad-alt-no-flag"}}, Unordered: []cli.Slot{cli.FlagGroup{Name: "g"}}}
}

// BadAltInvalid: cli_flag_alternatives contains invalid flag.
type BadAltInvalid struct {
	Flag bool `cli_flag:"--flag" cli_flag_alternatives:"oops" cli_group:"g"`
}

func (BadAltInvalid) Slots() cli.Slot {
	return cli.Group{Ordered: []cli.Slot{cli.Subcommand{Value: "bad-alt-invalid"}}, Unordered: []cli.Slot{cli.FlagGroup{Name: "g"}}}
}

// SliceFlagsAndArgsStruct: simple type to check repeated emission.
type SliceFlagsAndArgsStruct struct {
	Fs []string `cli_flag:"--fs" cli_group:"g"`
	Is []int    `cli_argument:"a"`
}

func (SliceFlagsAndArgsStruct) Slots() cli.Slot {
	return cli.Group{Unordered: []cli.Slot{cli.FlagGroup{Name: "g"}}, Ordered: []cli.Slot{cli.Subcommand{Value: "slices"}, cli.Argument{Name: "a"}}}
}

/* ------------------------------ negative tests ------------------------------ */

func TestConvertToCmdline_Fails_MissingGroupForFlag(t *testing.T) {
	t.Parallel()

	_, err := cli.ConvertToCmdline(BadMissingGroupFlag{})
	if err == nil || !strings.Contains(err.Error(), "missing required cli_group") {
		t.Fatalf("expected missing cli_group error, got: %v", err)
	}
}

func TestConvertToCmdline_Fails_ArgHasGroup(t *testing.T) {
	t.Parallel()

	_, err := cli.ConvertToCmdline(BadArgHasGroup{})
	if err == nil {
		t.Fatalf("expected arg-has-group error, got nothing")
	}
}

func TestConvertToCmdline_Fails_GroupNotInGroupsList(t *testing.T) {
	t.Parallel()

	_, err := cli.ConvertToCmdline(BadGroupNotInGroupsList{})
	if err == nil || !strings.Contains(err.Error(), "not present in Slots()") {
		t.Fatalf("expected group-not-in-Slots error, got: %v", err)
	}
}

func TestConvertToCmdline_Fails_BothFlagAndArg(t *testing.T) {
	t.Parallel()

	_, err := cli.ConvertToCmdline(BadBothFlagAndArg{})
	if err == nil || !strings.Contains(err.Error(), "both cli_flag and cli_argument") {
		t.Fatalf("expected both-tags error, got: %v", err)
	}
}

// Removed: multiple literal separators are allowed by Slots model.

func TestConvertToCmdline_Fails_AltWithoutFlag(t *testing.T) {
	t.Parallel()

	_, err := cli.ConvertToCmdline(BadAltNoFlag{})
	if err == nil || !strings.Contains(err.Error(), "cli_flag_alternatives") {
		t.Fatalf("expected cli_flag_alternatives error, got: %v", err)
	}
}

func TestConvertToCmdline_Fails_InvalidAlt(t *testing.T) {
	t.Parallel()

	_, err := cli.ConvertToCmdline(BadAltInvalid{})
	if err == nil || !strings.Contains(err.Error(), "cli_flag_alternative") {
		t.Fatalf("expected invalid cli_flag_alternative error, got: %v", err)
	}
}

func TestConvertToCmdline_SliceFlagsAndArgs(t *testing.T) {
	t.Parallel()

	cmd := SliceFlagsAndArgsStruct{
		Fs: []string{"x", "y"},
		Is: []int{1, 2},
	}
	expected := []string{"slices", "--fs", "x", "--fs", "y", "1", "2"}
	eq(t, mustConvert(t, cmd), expected)
}

// delegateWrapper is a helper used to verify that cli_embed expands named
// fields when generating command lines.
type delegateWrapper[T cli.Command] struct {
	Command T `cli_embed:""`

	DelegatePath string `cli_flag:"--delegate_path" cli_group:"delegate"`
}

func (w delegateWrapper[T]) Slots() cli.Slot {
	return cli.Group{
		Unordered: []cli.Slot{cli.FlagGroup{Name: "delegate"}},
		Ordered:   []cli.Slot{w.Command.Slots()},
	}
}

// TestConvertToCmdline_CliEmbed ensures that fields tagged with cli_embed are
// traversed during command line conversion so that nested flags and arguments
// are emitted.
func TestConvertToCmdline_CliEmbed(t *testing.T) {
	t.Parallel()

	cmd := delegateWrapper[Run]{
		Command: Run{
			Keep:        true,
			ContainerID: containerID,
		},
		DelegatePath: delegatePath,
	}

	expected := []string{"run", "--delegate_path", delegatePath, "--keep", containerID}
	eq(t, mustConvert(t, cmd), expected)
}
