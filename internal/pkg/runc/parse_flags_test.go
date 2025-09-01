package runc

import (
	cli "github.com/TheGrizzlyDev/vino/internal/pkg/cli"
	"reflect"
	"testing"
)

func TestParseFlags_Exec_ProcessFlag(t *testing.T) {
	t.Parallel()

	args := []string{"cid", "--process", "proc.json"}
	var cmd Exec
	if err := cli.Parse(&cmd, args); err != nil {
		t.Fatalf("Parse: %v", err)
	}
	expected := Exec{ContainerID: "cid", Process: "proc.json"}
	if !reflect.DeepEqual(cmd, expected) {
		t.Fatalf("got %#v want %#v", cmd, expected)
	}
}

// TestParseFlags_Docs_Run ensures that the example from the runc README
// `runc run mycontainerid` parses correctly.
func TestParseFlags_Docs_Run(t *testing.T) {
	t.Parallel()
	args := []string{"mycontainerid"}
	var cmd Run
	if err := cli.Parse(&cmd, args); err != nil {
		t.Fatalf("Parse: %v", err)
	}
	expected := Run{ContainerID: "mycontainerid"}
	if !reflect.DeepEqual(cmd, expected) {
		t.Fatalf("got %#v want %#v", cmd, expected)
	}
}

// TestParseFlags_Docs_Exec ensures that the exec example parses correctly.
func TestParseFlags_Docs_Exec(t *testing.T) {
	t.Parallel()
	args := []string{"-t", "mycontainerid", "--", "sh"}
	var cmd Exec
	if err := cli.Parse(&cmd, args); err != nil {
		t.Fatalf("Parse: %v", err)
	}
	expected := Exec{Tty: true, ContainerID: "mycontainerid", Command: "sh"}
	if !reflect.DeepEqual(cmd, expected) {
		t.Fatalf("got %#v want %#v", cmd, expected)
	}
}

// TestParseFlags_Docs_RunSystemd covers the systemd example
// `runc run -d --pid-file /run/mycontainerid.pid mycontainerid`.
func TestParseFlags_Docs_RunSystemd(t *testing.T) {
	t.Parallel()
	args := []string{"-d", "--pid-file", "/run/mycontainerid.pid", "mycontainerid"}
	var cmd Run
	if err := cli.Parse(&cmd, args); err != nil {
		t.Fatalf("Parse: %v", err)
	}
	expected := Run{
		DetachOpt:   DetachOpt{Detach: true},
		PidFileOpt:  PidFileOpt{PidFile: "/run/mycontainerid.pid"},
		ContainerID: "mycontainerid",
	}
	if !reflect.DeepEqual(cmd, expected) {
		t.Fatalf("got %#v want %#v", cmd, expected)
	}
}

// forEachPermutation calls f with every permutation of items.
func forEachPermutation(items [][]string, f func([][]string)) {
	if len(items) == 0 {
		f(nil)
		return
	}
	var permute func(int)
	permute = func(i int) {
		if i == len(items) {
			cp := make([][]string, len(items))
			copy(cp, items)
			f(cp)
			return
		}
		for j := i; j < len(items); j++ {
			items[i], items[j] = items[j], items[i]
			permute(i + 1)
			items[i], items[j] = items[j], items[i]
		}
	}
	permute(0)
}

// testPermutations verifies that Parse can handle any permutation of flags.
func testPermutations[T cli.Command](t *testing.T, before []string, flags [][]string, after []string, expected T) {
	forEachPermutation(flags, func(perm [][]string) {
		args := append([]string{}, before...)
		for _, p := range perm {
			args = append(args, p...)
		}
		args = append(args, after...)
		var cmd T
		cmdPtr, ok := any(&cmd).(cli.Command)
		if !ok {
			t.Fatalf("%T does not implement Command", cmd)
		}
		if err := cli.Parse(cmdPtr, args); err != nil {
			t.Fatalf("args %v: %v", args, err)
		}
		if !reflect.DeepEqual(cmd, expected) {
			t.Fatalf("args %v: got %#v want %#v", args, cmd, expected)
		}
	})
}

func TestParseFlags_Checkpoint_Permutations(t *testing.T) {
	t.Parallel()
	flags := [][]string{
		{"--image-path", "/img"},
		{"--tcp-established"},
		{"--manage-cgroups-mode", "soft"},
		{"--leave-running"},
	}
	after := []string{"cid"}
	expected := Checkpoint{
		ImagePath:         "/img",
		TcpEstablished:    true,
		ManageCgroupsMode: "soft",
		LeaveRunning:      true,
		ContainerID:       "cid",
	}
	testPermutations(t, nil, flags, after, expected)
}

func TestParseFlags_Restore_Permutations(t *testing.T) {
	t.Parallel()
	flags := [][]string{
		{"--bundle", "/b"},
		{"--console-socket", "/s"},
		{"--no-pivot"},
		{"--image-path", "/img"},
		{"--lsm-profile", "t:l"},
	}
	after := []string{"cid"}
	expected := Restore{
		BundleOpt:          BundleOpt{Bundle: "/b"},
		ConsoleSocketOpt:   ConsoleSocketOpt{ConsoleSocket: "/s"},
		PivotKeyringFDsOpt: PivotKeyringFDsOpt{NoPivot: true},
		ImagePath:          "/img",
		LSMProfile:         "t:l",
		ContainerID:        "cid",
	}
	testPermutations(t, nil, flags, after, expected)
}

func TestParseFlags_Create_Permutations(t *testing.T) {
	t.Parallel()
	flags := [][]string{
		{"--bundle", "/b"},
		{"--console-socket", "/s"},
		{"--no-pivot"},
		{"--pid-file", "/pid"},
	}
	after := []string{"cid"}
	expected := Create{
		BundleOpt:          BundleOpt{Bundle: "/b"},
		ConsoleSocketOpt:   ConsoleSocketOpt{ConsoleSocket: "/s"},
		PivotKeyringFDsOpt: PivotKeyringFDsOpt{NoPivot: true},
		PidFileOpt:         PidFileOpt{PidFile: "/pid"},
		ContainerID:        "cid",
	}
	testPermutations(t, nil, flags, after, expected)
}

func TestParseFlags_Run_Permutations(t *testing.T) {
	t.Parallel()
	flags := [][]string{
		{"--bundle", "/b"},
		{"--console-socket", "/s"},
		{"--no-pivot"},
		{"--detach"},
	}
	after := []string{"cid"}
	expected := Run{
		BundleOpt:          BundleOpt{Bundle: "/b"},
		ConsoleSocketOpt:   ConsoleSocketOpt{ConsoleSocket: "/s"},
		PivotKeyringFDsOpt: PivotKeyringFDsOpt{NoPivot: true},
		DetachOpt:          DetachOpt{Detach: true},
		ContainerID:        "cid",
	}
	testPermutations(t, nil, flags, after, expected)
}

func TestParseFlags_Start_Permutations(t *testing.T) {
	t.Parallel()
	expected := Start{ContainerID: "cid"}
	testPermutations(t, nil, nil, []string{"cid"}, expected)
}

func TestParseFlags_Delete_Permutations(t *testing.T) {
	t.Parallel()
	flags := [][]string{{"--force"}}
	after := []string{"cid"}
	expected := Delete{Force: true, ContainerID: "cid"}
	testPermutations(t, nil, flags, after, expected)
}

func TestParseFlags_Pause_Permutations(t *testing.T) {
	t.Parallel()
	expected := Pause{ContainerID: "cid"}
	testPermutations(t, nil, nil, []string{"cid"}, expected)
}

func TestParseFlags_Resume_Permutations(t *testing.T) {
	t.Parallel()
	expected := Resume{ContainerID: "cid"}
	testPermutations(t, nil, nil, []string{"cid"}, expected)
}

func TestParseFlags_Kill_Permutations(t *testing.T) {
	t.Parallel()
	flags := [][]string{{"--all"}}
	after := []string{"cid", "KILL"}
	expected := Kill{All: true, ContainerID: "cid", Signal: "KILL"}
	testPermutations(t, nil, flags, after, expected)
}

func TestParseFlags_List_Permutations(t *testing.T) {
	t.Parallel()
	flags := [][]string{{"--format", "json"}, {"--quiet"}}
	expected := List{FormatOpt: FormatOpt{Format: "json"}, Quiet: true}
	testPermutations(t, nil, flags, nil, expected)
}

func TestParseFlags_Ps_Permutations(t *testing.T) {
	t.Parallel()
	flags := [][]string{{"--format", "json"}}
	after := []string{"cid", "aux"}
	expected := Ps{FormatOpt: FormatOpt{Format: "json"}, ContainerID: "cid", PsArgs: []string{"aux"}}
	testPermutations(t, nil, flags, after, expected)
}

func TestParseFlags_State_Permutations(t *testing.T) {
	t.Parallel()
	expected := State{ContainerID: "cid"}
	testPermutations(t, nil, nil, []string{"cid"}, expected)
}

func TestParseFlags_Events_Permutations(t *testing.T) {
	t.Parallel()
	flags := [][]string{{"--interval", "5s"}, {"--stats"}}
	after := []string{"cid"}
	expected := Events{Interval: "5s", Stats: true, ContainerID: "cid"}
	testPermutations(t, nil, flags, after, expected)
}

func TestParseFlags_Exec_Permutations(t *testing.T) {
	t.Parallel()
	flags := [][]string{
		{"--tty"},
		{"--no-pivot"},
		{"--console-socket", "/s"},
		{"--apparmor", "prof"},
		{"--env", "FOO=1"},
		{"--cgroup", "cg"},
	}
	after := []string{"cid", "--", "/bin/sh", "-c", "echo"}
	expected := Exec{
		ConsoleSocketOpt:   ConsoleSocketOpt{ConsoleSocket: "/s"},
		PivotKeyringFDsOpt: PivotKeyringFDsOpt{NoPivot: true},
		Tty:                true,
		Env:                []string{"FOO=1"},
		AppArmor:           "prof",
		Cgroup:             "cg",
		ContainerID:        "cid",
		Command:            "/bin/sh",
		Args:               []string{"-c", "echo"},
	}
	testPermutations(t, nil, flags, after, expected)
}

func TestParseFlags_Spec_Permutations(t *testing.T) {
	t.Parallel()
	flags := [][]string{{"--bundle", "/b"}, {"--rootless"}}
	expected := Spec{BundleOpt: BundleOpt{Bundle: "/b"}, Rootless: true}
	testPermutations(t, nil, flags, nil, expected)
}

func TestParseFlags_Update_Permutations(t *testing.T) {
	t.Parallel()
	before := []string{"cid"}
	flags := [][]string{
		{"--memory", "1024"},
		{"--cpu-quota", "50000"},
		{"-r", "-"},
		{"--cpuset-cpus", "0-3"},
		{"--memory-swap", "2048"},
	}
	expected := Update{
		ContainerID:  "cid",
		ReadFromJSON: "-",
		CPUQuota:     func() *int64 { v := int64(50000); return &v }(),
		CPUSetCPUs:   "0-3",
		Memory:       func() *int64 { v := int64(1024); return &v }(),
		MemorySwap:   func() *int64 { v := int64(2048); return &v }(),
	}
	testPermutations(t, before, flags, nil, expected)
}
