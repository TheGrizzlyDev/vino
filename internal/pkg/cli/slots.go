package cli

var (
	_ Slot = FlagGroup{}
	_ Slot = Argument{}
	_ Slot = Arguments{}
	_ Slot = Literal{}
	_ Slot = Subcommand{}
	_ Slot = Group{}
)

type Slot interface{ slot() }

// FlagGroup represents a named collection of flags.
type FlagGroup struct {
	Name string // The name of the flag group (e.g., "global", "exec_flags")
}

func (FlagGroup) slot() {}

// Argument represents a single, strictly ordered positional argument.
type Argument struct {
	Name string // The name of the argument (e.g., "container_id")
}

func (Argument) slot() {}

// Arguments represents a variadic list of positional arguments.
type Arguments struct {
	Name string // The name of the variadic argument (e.g., "command_args")
}

func (Arguments) slot() {}

// Literal represents a specific string that must appear at its
// position in the argument sequence.
type Literal struct {
	Value string // The literal string, e.g., "--"
}

func (Literal) slot() {}

// Subcommand represents the literal string identifying a command.
type Subcommand struct {
	Value string // The literal string, e.g., "add", "remove"
}

func (Subcommand) slot() {}

// Group defines a segment of command-line arguments where flags and
// positional arguments can be mixed.
type Group struct {
	// Unordered contains FlagGroup, Argument, and Arguments slots. Flags
	// from these groups and positional arguments listed here may appear in
	// any order within this Group's scope.
	Unordered []Slot

	// Ordered contains Argument, Arguments, Literal, and Subcommand
	// slots. These must appear in the specified sequence.
	Ordered []Slot
}

func (Group) slot() {}
