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

3.  **`Arguments`**: Represents a variadic list of positional arguments. It consumes all remaining tokens until a `Literal{Value: "--"}` or the end of the command line. When a `Literal` follows an `Arguments` slot, any token matching that literal's value belongs to the `Literal` and is not collected by `Arguments`. This is useful for collecting arguments to be passed to an external process.

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

5.  **`Subcommand`**: Represents the literal string that identifies this command. It must be the first `Ordered` slot in the command's `Slots()` definition so that `Parse` can quickly determine whether the input matches the command.

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
    *   **Role:** Tries to parse the input against each known command and selects the first successful match.
    *   **Process:**
        *   `ParseAny` iterates through a registry of `Command` prototypes.
        *   For each prototype it instantiates a concrete struct and calls `Parse` with the full `args` slice.
        *   `Parse` returns `(matched bool, err error)`. If `matched` is `false`, `ParseAny` moves to the next prototype.
        *   If `matched` is `true` and `err` is `nil`, `ParseAny` sets the populated command into the `cmdUnion` and returns successfully.
        *   If `matched` is `true` and `err` is non-nil`, the input belongs to that command but was malformed; `ParseAny` returns the error immediately.
        *   If no command reports a match, `ParseAny` returns an error indicating that the subcommand was not recognized.

2.  **`Parse` Function:**
    *   **Role:** Attempts to parse the input according to a command's `Slots()` definition and reports whether the input matches.
    *   **Inputs:** Takes a `Command` instance (which is a pointer to the concrete command struct, e.g., `*AddCommand`) and the full `args` slice (as received by `ParseAny`). It returns `(bool, error)`, where the boolean indicates whether the arguments matched the command's shape.
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
                *   `Arguments` slots consume all remaining arguments for that segment until a following `Literal` slot's value is encountered.
            *   **When processing a top-level `Literal` slot (e.g., `Literal{Value: "--"}`):**
                *   It strictly expects that literal value at the current position in the `args` slice. If it's not present, parsing fails.
                *   If the `Literal` is `"--"`, `stopFlagParsing` is set to `true`, ensuring no more flags are parsed for subsequent arguments.
            *   **When processing a top-level `Arguments` slot:**
                *   It consumes all remaining arguments in the `args` slice, stopping early if the next slot is a `Literal` whose value is encountered. This slot acts as a "catch-all" for trailing arguments.
        *   Finally, it checks for any unexpected trailing arguments that were not consumed by any slot.
        *   If all slots are satisfied and no extraneous arguments remain, `Parse` returns `(true, nil)`. If a slot fails to match after a preliminary match (e.g., wrong flag value), it returns `(true, err)`. If the overall shape does not match the command (e.g., different subcommand name), it returns `(false, nil)`.

This structured approach provides a powerful and explicit way to define complex CLI parsing rules, enhancing readability, maintainability, and composability.

---

## Revised Semantics (Current Implementation)

The implementation has evolved; this section clarifies the authoritative behavior and supersedes conflicting notes above where applicable.

1) Root shape
- `Command.Slots()` returns a single root `Slot` (always a `Group`).

2) Unordered vs Ordered
- Unordered (`Group.Unordered`): lists `FlagGroup`s whose flags are valid anywhere within that Group’s span (before and after ordered items, and inside nested Groups). Ancestor Unordered sets are inherited by descendants.
- Ordered (`Group.Ordered`): items are strict relative to each other. An ordered `FlagGroup` opens a position‑specific window where only its flags are valid.

3) Literals
- `Literal` tokens (including `--`) are matched as‑is and have no implicit side effects. Any “post‑separator” behavior should be modeled using Slots (e.g., `Literal{"--"}` followed by `Arguments{"args"}`).

4) Parsing strategy (informative)
- Recursive descent over `Slots()`, maintaining the union of ancestor + current Unordered sets as the active Unordered flags.
- Before and after each ordered item, greedily consume any number of flags from the active Unordered set.
- For an ordered `FlagGroup`, consume only that group’s flags at that position.
- Unknown flag at a position ends the current flag window so later items can claim it; if no later item accepts it, it’s an error.
- `Arguments` must be last within its parsing segment.
- `--flag=value` is expanded into `--flag value` before parsing.

5) Generation strategy (informative)
- Emit `Subcommand` first, then place Unordered flags and Ordered items consistent with the same visibility rules. If Unordered flags should appear after a specific `Argument` (e.g., `update <id> [flags]`), structure Slots accordingly so the parser/generator follow the same rules mechanically.

6) Embedding
- `runc_embed:""` treats a named field as if anonymously embedded for tag collection; nested `Group`s naturally model wrappers that add Unordered/Ordered items around an inner command.

7) Validation
- Tags are validated against the Slot tree: `runc_flag` must reference an existing `FlagGroup` name; `runc_argument` names must exist as ordered `Argument`/`Arguments` in the Slot tree; `runc_flag_alternatives` expand lookups; `runc_enum` constrains stringish fields.
