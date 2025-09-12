package cli

import (
	"reflect"
	"testing"
)

type unorderedArgsCmd struct {
	A     bool     `cli_flag:"--a" cli_group:"g"`
	B     bool     `cli_flag:"--b" cli_group:"g"`
	First string   `cli_argument:"first"`
	Rest  []string `cli_argument:"rest"`
}

func (unorderedArgsCmd) Slots() Slot {
	return Group{
		Unordered: []Slot{
			FlagGroup{Name: "g"},
			Argument{Name: "first"},
			Arguments{Name: "rest"},
		},
	}
}

type unorderedArgsCmd2 struct {
	A     bool     `cli_flag:"--a" cli_group:"g"`
	B     bool     `cli_flag:"--b" cli_group:"g"`
	First string   `cli_argument:"first"`
	Rest  []string `cli_argument:"rest"`
}

func (unorderedArgsCmd2) Slots() Slot {
	return Group{
		Unordered: []Slot{
			FlagGroup{Name: "g"},
			Arguments{Name: "rest"},
			Argument{Name: "first"},
		},
	}
}

func TestUnorderedArguments(t *testing.T) {
	t.Parallel()
	if err := ValidateCommandTags(unorderedArgsCmd{}); err != nil {
		t.Fatalf("ValidateCommandTags: %v", err)
	}
	if err := ValidateCommandTags(unorderedArgsCmd2{}); err != nil {
		t.Fatalf("ValidateCommandTags2: %v", err)
	}

	cases := []struct {
		name string
		run  func(*testing.T)
	}{
		{
			name: "single before, rest after",
			run: func(t *testing.T) {
				args := []string{"one", "--a", "--b", "r1", "r2"}
				want := unorderedArgsCmd{A: true, B: true, First: "one", Rest: []string{"r1", "r2"}}
				var cmd unorderedArgsCmd
				if err := Parse(&cmd, args); err != nil {
					t.Fatalf("Parse: %v", err)
				}
				if !reflect.DeepEqual(cmd, want) {
					t.Fatalf("got %#v want %#v", cmd, want)
				}
			},
		},
		{
			name: "single between flags",
			run: func(t *testing.T) {
				args := []string{"--a", "one", "--b", "r1", "r2"}
				want := unorderedArgsCmd{A: true, B: true, First: "one", Rest: []string{"r1", "r2"}}
				var cmd unorderedArgsCmd
				if err := Parse(&cmd, args); err != nil {
					t.Fatalf("Parse: %v", err)
				}
				if !reflect.DeepEqual(cmd, want) {
					t.Fatalf("got %#v want %#v", cmd, want)
				}
			},
		},
		{
			name: "variadic before/between/after flags, single after",
			run: func(t *testing.T) {
				args := []string{"r0", "--a", "r1", "--b", "one", "r2"}
				want := unorderedArgsCmd2{A: true, B: true, First: "one", Rest: []string{"r0", "r1", "r2"}}
				var cmd unorderedArgsCmd2
				if err := Parse(&cmd, args); err != nil {
					t.Fatalf("Parse: %v", err)
				}
				if !reflect.DeepEqual(cmd, want) {
					t.Fatalf("got %#v want %#v", cmd, want)
				}
			},
		},
	}

	for _, tt := range cases {
		t.Run(tt.name, tt.run)
	}
}
