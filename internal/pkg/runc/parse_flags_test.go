package runc

import (
	"reflect"
	"testing"
)

func TestParseFlags_Exec_MixedOrder(t *testing.T) {
	t.Parallel()

	args := []string{
		"--tty",
		"--no-pivot",
		"--console-socket", "/s",
		"--apparmor", "prof",
		"--env", "FOO=1",
		"--cgroup", "cg",
		"cid",
		"--",
		"/bin/sh",
		"-c", "echo",
	}

	var cmd Exec
	if err := Parse(&cmd, args); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

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

	if !reflect.DeepEqual(cmd, expected) {
		t.Fatalf("parsed command mismatch:\n got: %#v\n want: %#v", cmd, expected)
	}
}

func TestParseFlags_Update_MixedOrder(t *testing.T) {
	t.Parallel()

	args := []string{
		"cid",
		"--memory", "1024",
		"--cpu-quota", "50000",
		"-r", "-",
		"--cpuset-cpus", "0-3",
		"--memory-swap", "2048",
	}

	var cmd Update
	if err := Parse(&cmd, args); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cmd.ContainerID != "cid" {
		t.Fatalf("ContainerID mismatch: got %q", cmd.ContainerID)
	}
	if cmd.ReadFromJSON != "-" {
		t.Fatalf("ReadFromJSON mismatch: %q", cmd.ReadFromJSON)
	}
	if cmd.CPUQuota == nil || *cmd.CPUQuota != 50000 {
		t.Fatalf("CPUQuota mismatch: %#v", cmd.CPUQuota)
	}
	if cmd.Memory == nil || *cmd.Memory != 1024 {
		t.Fatalf("Memory mismatch: %#v", cmd.Memory)
	}
	if cmd.MemorySwap == nil || *cmd.MemorySwap != 2048 {
		t.Fatalf("MemorySwap mismatch: %#v", cmd.MemorySwap)
	}
	if cmd.CPUSetCPUs != "0-3" {
		t.Fatalf("CPUSetCPUs mismatch: %q", cmd.CPUSetCPUs)
	}
}

func TestParseFlags_Exec_FlagAfterArgFails(t *testing.T) {
	t.Parallel()

	args := []string{"--tty", "cid", "--no-pivot", "--", "/bin/sh"}
	var cmd Exec
	if err := Parse(&cmd, args); err == nil {
		t.Fatalf("expected error, got nil")
	}
}
