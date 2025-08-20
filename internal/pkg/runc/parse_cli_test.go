package runc

import (
	"reflect"
	"testing"
)

func TestParse_RoundTripExec(t *testing.T) {
	args := []string{"exec", "--tty", "cid", "--", "/bin/sh"}
	cmd, err := Parse(args)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	execCmd, ok := cmd.(Exec)
	if !ok {
		t.Fatalf("expected Exec, got %T", cmd)
	}
	if !execCmd.Tty || execCmd.ContainerID != "cid" || execCmd.Command != "/bin/sh" {
		t.Fatalf("unexpected parsed command: %#v", execCmd)
	}
	round, err := convertToCmdline(execCmd)
	if err != nil {
		t.Fatalf("convert error: %v", err)
	}
	if !reflect.DeepEqual(round, args) {
		t.Fatalf("roundtrip mismatch: got %v want %v", round, args)
	}
}

func TestParse_Kill(t *testing.T) {
	args := []string{"kill", "--all", "cid", "KILL"}
	cmd, err := Parse(args)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	killCmd, ok := cmd.(Kill)
	if !ok {
		t.Fatalf("expected Kill, got %T", cmd)
	}
	if !killCmd.All || killCmd.ContainerID != "cid" || killCmd.Signal != "KILL" {
		t.Fatalf("unexpected parsed command: %#v", killCmd)
	}
}
