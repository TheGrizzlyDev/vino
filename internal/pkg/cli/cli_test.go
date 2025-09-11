package cli

import (
	"reflect"
	"testing"
)

type simpleCmd struct {
	Flag bool   `cli_flag:"--flag" cli_group:"g"`
	ID   string `cli_argument:"id"`
}

func (simpleCmd) Slots() Slot {
	return Group{
		Unordered: []Slot{FlagGroup{Name: "g"}},
		Ordered: []Slot{
			Subcommand{Value: "do"},
			Argument{Name: "id"},
		},
	}
}

type nestedCmd struct{}

func (nestedCmd) Slots() Slot {
	return Group{Ordered: []Slot{Group{Ordered: []Slot{Subcommand{Value: "inner"}}}}}
}

type noSubCmd struct{}

func (noSubCmd) Slots() Slot { return Group{} }

type badCmd struct {
	Flag bool `cli_flag:"--flag"`
}

func (badCmd) Slots() Slot {
	return Group{Ordered: []Slot{Subcommand{Value: "bad"}}}
}

type invalidCmd struct{}

func TestSubcommandOf_Basic(t *testing.T) {
	t.Parallel()
	if got := SubcommandOf(simpleCmd{}); got != "do" {
		t.Fatalf("SubcommandOf got %q want %q", got, "do")
	}
}

func TestSubcommandOf_Nested(t *testing.T) {
	t.Parallel()
	if got := SubcommandOf(nestedCmd{}); got != "inner" {
		t.Fatalf("SubcommandOf got %q want %q", got, "inner")
	}
}

func TestSubcommandOf_NoSubcommand(t *testing.T) {
	t.Parallel()
	if got := SubcommandOf(noSubCmd{}); got != "" {
		t.Fatalf("SubcommandOf got %q want empty", got)
	}
}

func TestSubcommandOf_Nil(t *testing.T) {
	t.Parallel()
	if got := SubcommandOf(nil); got != "" {
		t.Fatalf("SubcommandOf got %q want empty", got)
	}
}

func TestConvertToCmdline_Basic(t *testing.T) {
	t.Parallel()
	cmd := simpleCmd{Flag: true, ID: "abc"}
	argv, err := ConvertToCmdline(cmd)
	if err != nil {
		t.Fatalf("ConvertToCmdline: %v", err)
	}
	expected := []string{"do", "--flag", "abc"}
	if !reflect.DeepEqual(argv, expected) {
		t.Fatalf("argv mismatch\n  got: %v\n  want: %v", argv, expected)
	}
}

func TestParse_Basic(t *testing.T) {
	t.Parallel()
	var cmd simpleCmd
	args := []string{"--flag", "abc"}
	if err := Parse(&cmd, args); err != nil {
		t.Fatalf("Parse: %v", err)
	}
	expected := simpleCmd{Flag: true, ID: "abc"}
	if !reflect.DeepEqual(cmd, expected) {
		t.Fatalf("got %#v want %#v", cmd, expected)
	}
}

func TestParseAny_Basic(t *testing.T) {
	t.Parallel()
	var u struct{ Simple *simpleCmd }
	args := []string{"do", "--flag", "abc"}
	if err := ParseAny(&u, args); err != nil {
		t.Fatalf("ParseAny: %v", err)
	}
	if u.Simple == nil {
		t.Fatalf("Simple not set")
	}
	expected := simpleCmd{Flag: true, ID: "abc"}
	if !reflect.DeepEqual(*u.Simple, expected) {
		t.Fatalf("got %#v want %#v", *u.Simple, expected)
	}
}

func TestParseAny_Errors(t *testing.T) {
	t.Parallel()

	t.Run("nil union", func(t *testing.T) {
		t.Parallel()
		var u *struct{ Simple *simpleCmd }
		if err := ParseAny(u, []string{"do"}); err == nil {
			t.Fatalf("expected error")
		}
	})

	t.Run("missing subcommand", func(t *testing.T) {
		t.Parallel()
		var u struct{ Simple *simpleCmd }
		if err := ParseAny(&u, nil); err == nil {
			t.Fatalf("expected error")
		}
	})

	t.Run("no valid subcommand", func(t *testing.T) {
		t.Parallel()
		var u struct{ Simple *simpleCmd }
		if err := ParseAny(&u, []string{"bogus"}); err == nil {
			t.Fatalf("expected error")
		}
	})

	t.Run("field not command", func(t *testing.T) {
		t.Parallel()
		var u struct{ Bad *invalidCmd }
		if err := ParseAny(&u, []string{"bad"}); err == nil {
			t.Fatalf("expected error")
		}
	})
}

func TestValidateCommandTags_Nil(t *testing.T) {
	t.Parallel()
	if err := ValidateCommandTags(nil); err == nil {
		t.Fatalf("expected error")
	}
}

func TestValidateCommandTags_MissingGroup(t *testing.T) {
	t.Parallel()
	if err := ValidateCommandTags(badCmd{}); err == nil {
		t.Fatalf("expected error")
	}
}

func TestValidateCommandTags_OK(t *testing.T) {
	t.Parallel()
	if err := ValidateCommandTags(simpleCmd{}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateCommandTags_NoSubcommand(t *testing.T) {
	t.Parallel()
	if err := ValidateCommandTags(noSubCmd{}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
