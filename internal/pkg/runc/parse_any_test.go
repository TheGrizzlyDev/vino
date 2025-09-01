package runc

import (
	cli "github.com/TheGrizzlyDev/vino/internal/pkg/cli"
	"testing"
)

type cmdUnion struct {
	Run   *Run
	Start *Start
}

type wrapperCmd[T cli.Command] struct {
	Command T `cli_embed:""`

	DelegatePath string `cli_flag:"--delegate_path" cli_group:"delegate"`
}

func (w wrapperCmd[T]) Slots() cli.Slot {
	return cli.Group{
		Unordered: []cli.Slot{cli.FlagGroup{Name: "delegate"}},
		Ordered:   []cli.Slot{w.Command.Slots()},
	}
}

func TestParseAny_Run(t *testing.T) {
	t.Parallel()
	var u cmdUnion
	args := []string{"run", "myid"}
	if err := cli.ParseAny(&u, args); err != nil {
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
	if err := cli.ParseAny(&u, args); err != nil {
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
	if err := cli.ParseAny(&u, []string{"bogus"}); err == nil {
		t.Fatalf("expected error, got nil")
	}
}

func TestParseAny_NoArgs(t *testing.T) {
	t.Parallel()
	var u cmdUnion
	if err := cli.ParseAny(&u, []string{}); err == nil {
		t.Fatalf("expected error, got nil")
	}
}

func TestParseAny_NilUnion(t *testing.T) {
	t.Parallel()
	if err := cli.ParseAny[*cmdUnion](nil, []string{"run", "cid"}); err == nil {
		t.Fatalf("expected error, got nil")
	}
}

// TestParseAny_CliEmbed verifies that fields tagged with cli_embed expand as
// if they were anonymously embedded and their flags are parsed.
func TestParseAny_CliEmbed(t *testing.T) {
	t.Parallel()

	type union struct {
		Run *wrapperCmd[Run]
	}

	var u union
	args := []string{"run", "--delegate_path", "/tmp/d", "--keep", "cid"}
	if err := cli.ParseAny(&u, args); err != nil {
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

// TestParseAny_CliEmbedEquals verifies that flags specified with --flag=value
// syntax are properly split and parsed when cli_embed is used.
func TestParseAny_CliEmbedEquals(t *testing.T) {
	t.Parallel()

	type union struct {
		Run *wrapperCmd[Run]
	}

	var u union
	args := []string{"run", "--delegate_path=/tmp/d", "--keep", "cid"}
	if err := cli.ParseAny(&u, args); err != nil {
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

func TestParseAny_SubcommandAfterFlags(t *testing.T) {
	t.Parallel()

	type union struct {
		Run *wrapperCmd[Run]
	}

	var u union
	args := []string{"--delegate_path", "/tmp/d", "run", "--keep", "cid"}
	if err := cli.ParseAny(&u, args); err != nil {
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

func TestParseAny_Features(t *testing.T) {
	t.Parallel()

	var u struct {
		Features *Features
	}
	if err := cli.ParseAny(&u, []string{"features"}); err != nil {
		t.Fatalf("ParseAny: %v", err)
	}
	if u.Features == nil {
		t.Fatalf("Features command not populated")
	}
}
