package cli

import (
	"reflect"
	"testing"
)

type CommandWithArgsAndFlags struct {
	Foo  string   `cli_flag:"--foo" cli_group:"common"`
	Args []string `cli_argument:"args"`
}

func (CommandWithArgsAndFlags) Slots() Slot {
	return Group{
		Unordered: []Slot{
			FlagGroup{Name: "common"},
			Arguments{Name: "args"},
		},
	}
}

func TestConvertMixedArgumentsAndFlags(t *testing.T) {
	args, err := ConvertToCmdline(CommandWithArgsAndFlags{
		Foo:  "bar",
		Args: []string{"baz"},
	})

	if err != nil {
		t.Fatal(err)
	}

	want := []string{"--foo", "bar", "baz"}
	if !reflect.DeepEqual(args, want) {
		t.Fatalf("got %#v want %#v", args, want)
	}
}
