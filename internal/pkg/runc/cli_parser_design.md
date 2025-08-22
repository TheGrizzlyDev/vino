# CLI Parser Design: Slot-Based Argument Parsing

## Goal

The primary goal of this design is to create a flexible, readable, and maintainable command-line interface (CLI) argument parser for `runc` (and similar Go CLIs). Specifically, we aim to:

1.  **Loosen Flag Ordering:** Allow flags to appear anywhere relative to positional arguments within a logical group, rather than enforcing a strict "flags-first" rule.
2.  **Enforce Positional Argument Ordering:** Maintain strict relative ordering for positional arguments.
3.  **Strict `--` Separator Semantics:** Ensure the `--` literal acts as a definitive separator, after which all subsequent tokens are treated as non-flag arguments, regardless of their appearance.
4.  **Clear and Declarative Command Definitions:** Make the parsing rules for each command explicit and easy to understand directly from its definition.
5.  **Composability and Reusability:** Facilitate the reuse of flag sets and argument patterns across different commands.

## Approach: Slot-Based Parsing

We are adopting a "Slot-Based Parsing" approach. This involves defining the expected structure of command-line arguments using a series of `Slot` types. Each `Command` will implement a `Slots()` method that returns a slice of these `Slot`s, declaratively outlining its parsing rules.

### Why this Approach?

This approach was chosen for several key reasons:

*   **Explicitness and Readability:** The `Slots()` method provides a clear, human-readable definition of a command's argument structure. Developers can quickly understand what arguments and flags a command expects and in what order.
*   **Flexibility:** It allows for complex parsing rules, such as intermingling flags and positional arguments, while maintaining strict ordering where necessary.
*   **Type Safety:** By using Go types for `Slot`s, we gain compile-time checks and better tooling support compared to string-based struct tags for defining complex parsing logic.
*   **Composability:** The introduction of `FlagGroup` allows for the reuse of common sets of flags across multiple commands, reducing redundancy and improving consistency.
*   **Maintainability:** Changes to parsing rules are localized within the `Slots()` method of the relevant command, making updates safer and easier.
*   **Clear Semantics:** It precisely defines the role of each part of the command line, including the strict behavior of the `--` separator.

### Core Concepts

#### `Slot` Interface

A marker interface that all parsing elements implement. It uses an
unexported `slot()` method so that only types within this package can
implement it, effectively sealing the interface.

```go
type Slot interface {
    slot()
}
```

#### Concrete `Slot` Types

1.  **`FlagGroup`**: Represents a collection of flags. Flags are associated with a `FlagGroup` via the `runc_group` struct tag on their corresponding fields. When a `FlagGroup` is encountered in a `Group`'s `Unordered` list, all flags belonging to that group can be consumed.

    ```go
    type FlagGroup struct {
        Name string // The name of the flag group (e.g., "global", "exec_flags")
    }

    func (FlagGroup) slot() {}
    ```

2.  **`Argument`**: Represents a single, strictly ordered positional argument.

    ```go
    type Argument struct {
        Name string // The name of the argument (e.g., "container_id")
    }

    func (Argument) slot() {}
    ```

3.  **`Arguments`**: Represents a variadic list of positional arguments. It consumes all remaining tokens until a `Literal{Value: "--"}` or the end of the command line. This is useful for collecting arguments to be passed to an external process.

    ```go
    type Arguments struct {
        Name string // The name of the variadic argument (e.g., "command_args")
    }

    func (Arguments) slot() {}
    ```

4.  **`Literal`**: Represents a specific string that must appear at its exact position in the argument sequence. This is primarily used for the `--` separator.

    ```go
    type Literal struct {
        Value string // The literal string, e.g., "--"
    }

    func (Literal) slot() {}
    ```

5.  **`Subcommand`**: Represents the literal string that identifies this command. It must be the first `Ordered` slot in the command's `Slots()` definition. `ParseAny` will use this to identify the correct command parser.

    ```go
    type Subcommand struct {
        Value string // The literal string, e.g., "add", "remove", "run_command"
    }

    func (Subcommand) slot() {}
    ```

6.  **`Group`**: Defines a segment of command-line arguments where flags and positional arguments can be mixed.

    *   `Unordered []Slot`: Contains `FlagGroup` slots. Flags associated with these groups can appear in any order within this `Group`'s scope.
    *   `Ordered []Slot`: Contains `Argument`, `Arguments`, `Literal`, and `Subcommand` slots. These must appear in the specified sequence.

    ```go
    type Group struct {
        Unordered []Slot // Contains FlagGroup slots
        Ordered   []Slot // Contains Argument, Arguments, Literal, and Subcommand slots in strict sequence
    }

    func (Group) slot() {}
    ```

#### `Command` Interface

Each command struct implements this interface, providing its parsing definition.

```go
type Command interface {
    Slots() []Slot // Defines the parsing structure for the command
}
```

### How it Works (Conceptual Parsing Flow)

The parsing process is orchestrated by two main functions: `ParseAny` and `Parse`.

1.  **`ParseAny` Function:**
    *   **Role:** Identifies the specific command being invoked based on its `Subcommand` slot and dispatches to the correct command parser.
    *   **Process:**
        *   It iterates through the command-line arguments (`args`) to find the *first non-flag argument*. This argument is the candidate for the command's name.
        *   It then iterates through a pre-defined registry of all known `Command` implementations (e.g., provided via a slice of command prototypes).
        *   For each `Command` prototype, `ParseAny` inspects its `Slots()` definition. It specifically looks for a `Subcommand` slot as the *first* element in the `Ordered` list of the command's initial `Group` (or as a top-level `Subcommand` slot if the command starts with one) that matches the identified command name candidate.
        *   Once a match is found, `ParseAny` instantiates the corresponding concrete `Command` struct (using reflection) and passes the *entire original `args` slice* (including the program name and any global flags that might precede the command name) to the generic `Parse` function.
        *   It then sets the populated `Command` struct into the appropriate field of the `cmdUnion` (also using reflection).
        *   If no matching `Subcommand` slot is found after checking all prototypes, `ParseAny` can provide a helpful error message, perhaps listing available commands.

2.  **`Parse` Function:**
    *   **Role:** Parses the arguments for a specific `Command` instance based on its `Slots()` definition.
    *   **Inputs:** Takes a `Command` instance (which is a pointer to the concrete command struct, e.g., `*AddCommand`) and the full `args` slice (as received by `ParseAny`).
    *   **Helper Functions:** Relies on several helper functions for reflection and argument manipulation:
        *   `walkStruct(v reflect.Value, fn func(sf reflect.StructField, fv reflect.Value))`: Recursively traverses a struct's fields, applying a function to each field. Used to collect `fieldInfo`.
        *   `flagTakesValue(v reflect.Value) bool`: Determines if a flag's corresponding field type expects a value (e.g., `string`, `int`) or is a boolean flag.
        *   `setValue(v reflect.Value, val string) error`: Sets the value of a `reflect.Value` field based on a string, handling type conversions and pointers.
        *   `expandEquals(args []string) []string`: Pre-processes arguments to split `--flag=value` into `--flag` and `value` tokens.
    *   **Process:**
        *   Initializes by collecting `fieldInfo` for all fields in the `Command` struct (using `walkStruct`), and building maps (`flagByToken`, `flagsByGroup`, `argByName`) to quickly look up fields by flag name, group name, or argument name.
        *   Maintains an `argIdx` to track the current position in the `args` slice and a `stopFlagParsing` boolean, which becomes `true` after a `Literal{Value: "--"}` is encountered.
        *   It iterates sequentially through the `Command.Slots()` definition:
            *   **When processing a `Group` slot:**
                *   It identifies all flags associated with the `FlagGroup`s in the `Group.Unordered` list. These are the flags allowed within this group's scope.
                *   It then iterates through the input `args` for this segment, attempting to consume arguments.
                *   If `stopFlagParsing` is `false` and the `currentArg` starts with `-` and matches an allowed flag, it consumes the flag (and its value if required) and continues to the next `arg`. This enables flags to be interleaved.
                *   If `currentArg` is not a flag (or `stopFlagParsing` is `true`), it must match the next expected `Ordered` slot (`Subcommand`, `Literal`, `Argument`, or `Arguments`). If it's not, or if the `Ordered` slots are exhausted, it's an error.
                *   `Subcommand` and `Literal` slots are strictly matched.
                *   `Argument` slots consume the next single argument.
                *   `Arguments` slots consume all remaining arguments for that segment.
            *   **When processing a top-level `Literal` slot (e.g., `Literal{Value: "--"}`):**
                *   It strictly expects that literal value at the current position in the `args` slice. If it's not present, parsing fails.
                *   If the `Literal` is `"--"`, `stopFlagParsing` is set to `true`, ensuring no more flags are parsed for subsequent arguments.
            *   **When processing a top-level `Arguments` slot:**
                *   It consumes all remaining arguments in the `args` slice, effectively acting as a "catch-all" for trailing arguments.
        *   Finally, it checks for any unexpected trailing arguments that were not consumed by any slot.

This structured approach provides a powerful and explicit way to define complex CLI parsing rules, enhancing readability, maintainability, and composability.
