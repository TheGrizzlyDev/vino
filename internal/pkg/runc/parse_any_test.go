package runc

import "testing"

type cmdUnion struct {
	Run   *Run
	Start *Start
}

type wrapperCmd[T Command] struct {
	Command T `runc_embed:""`

	DelegatePath string `runc_flag:"--delegate_path" runc_group:"delegate"`
}

func (w wrapperCmd[T]) Subcommand() string { return w.Command.Subcommand() }

func (w wrapperCmd[T]) Groups() []string {
	return append([]string{"delegate"}, w.Command.Groups()...)
}

func TestParseAny_Run(t *testing.T) {
	t.Parallel()
	var u cmdUnion
	args := []string{"run", "myid"}
	if err := ParseAny(&u, args); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if u.Run == nil {
		t.Fatalf("Run command not populated")
	}
	if u.Run.ContainerID != "myid" {
		t.Fatalf("Run.ContainerID got %q", u.Run.ContainerID)
	}
	if u.Start != nil {
		t.Fatalf("Start should be nil")
	}
}

func TestParseAny_Start(t *testing.T) {
	t.Parallel()
	var u cmdUnion
	args := []string{"start", "cid"}
	if err := ParseAny(&u, args); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if u.Start == nil {
		t.Fatalf("Start command not populated")
	}
	if u.Start.ContainerID != "cid" {
		t.Fatalf("Start.ContainerID got %q", u.Start.ContainerID)
	}
	if u.Run != nil {
		t.Fatalf("Run should be nil")
	}
}

func TestParseAny_InvalidSubcommand(t *testing.T) {
	t.Parallel()
	var u cmdUnion
	if err := ParseAny(&u, []string{"bogus"}); err == nil {
		t.Fatalf("expected error, got nil")
	}
}

func TestParseAny_NoArgs(t *testing.T) {
	t.Parallel()
	var u cmdUnion
	if err := ParseAny(&u, []string{}); err == nil {
		t.Fatalf("expected error, got nil")
	}
}

func TestParseAny_NilUnion(t *testing.T) {
	t.Parallel()
	if err := ParseAny[*cmdUnion](nil, []string{"run", "cid"}); err == nil {
		t.Fatalf("expected error, got nil")
	}
}

// TestParseAny_RuncEmbed verifies that fields tagged with runc_embed expand as
// if they were anonymously embedded and their flags are parsed.
func TestParseAny_RuncEmbed(t *testing.T) {
	t.Parallel()

	type union struct {
		Run *wrapperCmd[Run]
	}

	var u union
	args := []string{"run", "--delegate_path", "/tmp/d", "--keep", "cid"}
	if err := ParseAny(&u, args); err != nil {
		t.Fatalf("ParseAny: %v", err)
	}
	if u.Run == nil {
		t.Fatalf("Run command not populated")
	}
	if u.Run.DelegatePath != "/tmp/d" {
		t.Fatalf("DelegatePath got %q", u.Run.DelegatePath)
	}
	if !u.Run.Command.Keep {
		t.Fatalf("Keep flag not set")
	}
	if u.Run.Command.ContainerID != "cid" {
		t.Fatalf("ContainerID got %q", u.Run.Command.ContainerID)
	}
}
