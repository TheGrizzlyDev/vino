package runc

import (
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

func mustConvert(t *testing.T, cmd Command) []string {
	t.Helper()
	argv, err := convertToCmdline(cmd)
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
			PreserveFDs:  3,
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
		StatusFD:    10,
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
			PreserveFDs: 2,
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

/* ---------------------------- negative test types --------------------------- */

// BadMissingGroupFlag: runc_flag present without runc_group.
type BadMissingGroupFlag struct {
	Oops bool `runc_flag:"--oops"`
}

func (BadMissingGroupFlag) Subcommand() string { return "bad-missing-group" }
func (BadMissingGroupFlag) Groups() []string   { return []string{"global", "common"} }

// BadArgHasGroup: argument incorrectly sets runc_group.
type BadArgHasGroup struct {
	Thing string `runc_argument:"arg" runc_group:"nope"`
}

func (BadArgHasGroup) Subcommand() string { return "bad-arg-has-group" }
func (BadArgHasGroup) Groups() []string   { return []string{"arg"} }

// BadGroupNotInGroupsList: flag references a group not listed in Groups().
type BadGroupNotInGroupsList struct {
	Flag string `runc_flag:"--flag" runc_group:"missing"`
}

func (BadGroupNotInGroupsList) Subcommand() string { return "bad-missing-in-list" }
func (BadGroupNotInGroupsList) Groups() []string   { return []string{"global"} }

// BadBothFlagAndArg: field illegally has both tags.
type BadBothFlagAndArg struct {
	Field string `runc_flag:"--flag" runc_group:"x" runc_argument:"arg"`
}

func (BadBothFlagAndArg) Subcommand() string { return "bad-both" }
func (BadBothFlagAndArg) Groups() []string   { return []string{"x", "arg"} }

// BadMultipleSeparatorsGroup: Groups() contains "--" twice.
type BadMultipleSeparatorsGroup struct {
	A string `runc_flag:"--a" runc_group:"x"`
}

func (BadMultipleSeparatorsGroup) Subcommand() string { return "bad-seps" }
func (BadMultipleSeparatorsGroup) Groups() []string   { return []string{"x", "--", "--"} }

// BadAltNoFlag: runc_flag_alternatives present without runc_flag.
type BadAltNoFlag struct {
	A bool `runc_flag_alternatives:"-a"`
}

func (BadAltNoFlag) Subcommand() string { return "bad-alt-no-flag" }
func (BadAltNoFlag) Groups() []string   { return []string{"g"} }

// BadAltInvalid: runc_flag_alternatives contains invalid flag.
type BadAltInvalid struct {
	Flag bool `runc_flag:"--flag" runc_flag_alternatives:"oops" runc_group:"g"`
}

func (BadAltInvalid) Subcommand() string { return "bad-alt-invalid" }
func (BadAltInvalid) Groups() []string   { return []string{"g"} }

// SliceFlagsAndArgsStruct: simple type to check repeated emission.
type SliceFlagsAndArgsStruct struct {
	Fs []string `runc_flag:"--fs" runc_group:"g"`
	Is []int    `runc_argument:"a"`
}

func (SliceFlagsAndArgsStruct) Subcommand() string { return "slices" }
func (SliceFlagsAndArgsStruct) Groups() []string   { return []string{"g", "a"} }

/* ------------------------------ negative tests ------------------------------ */

func TestConvertToCmdline_Fails_MissingGroupForFlag(t *testing.T) {
	t.Parallel()

	_, err := convertToCmdline(BadMissingGroupFlag{})
	if err == nil || !strings.Contains(err.Error(), "missing required runc_group") {
		t.Fatalf("expected missing runc_group error, got: %v", err)
	}
}

func TestConvertToCmdline_Fails_ArgHasGroup(t *testing.T) {
	t.Parallel()

	_, err := convertToCmdline(BadArgHasGroup{})
	if err == nil {
		t.Fatalf("expected arg-has-group error, got nothing")
	}
}

func TestConvertToCmdline_Fails_GroupNotInGroupsList(t *testing.T) {
	t.Parallel()

	_, err := convertToCmdline(BadGroupNotInGroupsList{})
	if err == nil || !strings.Contains(err.Error(), "not present in Groups()") {
		t.Fatalf("expected group-not-in-Groups error, got: %v", err)
	}
}

func TestConvertToCmdline_Fails_BothFlagAndArg(t *testing.T) {
	t.Parallel()

	_, err := convertToCmdline(BadBothFlagAndArg{})
	if err == nil || !strings.Contains(err.Error(), "both runc_flag and runc_argument") {
		t.Fatalf("expected both-tags error, got: %v", err)
	}
}

func TestConvertToCmdline_Fails_MultipleSeparatorsInGroups(t *testing.T) {
	t.Parallel()

	_, err := convertToCmdline(BadMultipleSeparatorsGroup{})
	if err == nil {
		t.Fatal("expected multiple-separators error, got nothing")
	}
}

func TestConvertToCmdline_Fails_AltWithoutFlag(t *testing.T) {
	t.Parallel()

	_, err := convertToCmdline(BadAltNoFlag{})
	if err == nil || !strings.Contains(err.Error(), "runc_flag_alternatives") {
		t.Fatalf("expected runc_flag_alternatives error, got: %v", err)
	}
}

func TestConvertToCmdline_Fails_InvalidAlt(t *testing.T) {
	t.Parallel()

	_, err := convertToCmdline(BadAltInvalid{})
	if err == nil || !strings.Contains(err.Error(), "runc_flag_alternative") {
		t.Fatalf("expected invalid runc_flag_alternative error, got: %v", err)
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

// delegateWrapper is a helper used to verify that runc_embed expands named
// fields when generating command lines.
type delegateWrapper[T Command] struct {
	Command T `runc_embed:""`

	DelegatePath string `runc_flag:"--delegate_path" runc_group:"delegate"`
}

func (w delegateWrapper[T]) Subcommand() string { return w.Command.Subcommand() }

func (w delegateWrapper[T]) Groups() []string {
	return append([]string{"delegate"}, w.Command.Groups()...)
}

// TestConvertToCmdline_RuncEmbed ensures that fields tagged with runc_embed are
// traversed during command line conversion so that nested flags and arguments
// are emitted.
func TestConvertToCmdline_RuncEmbed(t *testing.T) {
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
