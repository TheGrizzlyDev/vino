package runc

import (
	"fmt"
	"reflect"
	"strconv"
	"strings"
)

func ParseAny[T any](cmdUnion *T, args []string) error {
	if cmdUnion == nil {
		return fmt.Errorf("Parse: nil cmdUnion")
	}
	if len(args) == 0 {
		return fmt.Errorf("Parse: missing subcommand")
	}

	// Expand equals for flags first.
	args = expandEquals(args)

	// Discover subcommand tokens for each union field and find a match in args.
	v := reflect.ValueOf(cmdUnion).Elem()
	matchIdx := -1
	fieldIdx := -1
	for i := 0; i < v.NumField(); i++ {
		ft := v.Field(i).Type()
		inst := reflect.New(ft.Elem()).Interface()
		cmd, ok := inst.(Command)
		if !ok {
			return fmt.Errorf("field type '%s' does not implement Command", ft.Name())
		}
		sub := subcommandOf(cmd)
		for j, tok := range args {
			if tok == sub {
				// prefer earliest occurrence among candidates
				if matchIdx == -1 || j < matchIdx {
					matchIdx = j
					fieldIdx = i
				}
				break
			}
		}
	}
	if matchIdx == -1 {
		return fmt.Errorf("Parse: no valid subcommand found")
	}

	// Instantiate the chosen command and parse with subcommand removed.
	field := v.Field(fieldIdx)
	cmdVal := reflect.New(field.Type().Elem())
	cmd := cmdVal.Interface().(Command)

	rest := append(append([]string{}, args[:matchIdx]...), args[matchIdx+1:]...)
	if err := Parse(cmd, rest); err != nil {
		return err
	}
	if !field.CanSet() {
		return fmt.Errorf("field %d not settable", fieldIdx)
	}
	field.Set(cmdVal)
	return nil
}

// Parse reads args into cmd according to struct tags.
// Flags from groups within the same contiguous segment may appear in any order.
// The ordering is enforced by the Slots() structure. Literals (including "--")
// are matched exactly and do not set any values.
func Parse(cmd Command, args []string) error {
	if cmd == nil {
		return fmt.Errorf("Parse: nil cmd")
	}
	if err := validateCommandTags(cmd); err != nil {
		return err
	}

	// Expand --flag=value
	args = expandEquals(args)

	type fieldInfo struct {
		sf   reflect.StructField
		val  reflect.Value
		flag string
		alts []string
		argG string
		grp  string
	}

	v := reflect.ValueOf(cmd).Elem()
	var fields []fieldInfo
	walkStruct(v, func(sf reflect.StructField, fv reflect.Value) {
		flag, hasFlag := sf.Tag.Lookup("runc_flag")
		altSpec, hasAlt := sf.Tag.Lookup("runc_flag_alternatives")
		argG, hasArg := sf.Tag.Lookup("runc_argument")
		grp, _ := sf.Tag.Lookup("runc_group")
		if !hasFlag && !hasArg {
			return
		}
		var alts []string
		if hasAlt {
			for _, a := range strings.Split(altSpec, "|") {
				a = strings.TrimSpace(a)
				if a != "" {
					alts = append(alts, a)
				}
			}
		}
		fields = append(fields, fieldInfo{sf: sf, val: fv, flag: func() string {
			if hasFlag {
				return flag
			}
			return ""
		}(), alts: alts, argG: func() string {
			if hasArg {
				return argG
			}
			return ""
		}(), grp: grp})
	})

	// Indexes
	flagsByGroup := map[string][]*fieldInfo{}
	argsByName := map[string][]*fieldInfo{}
	for i := range fields {
		f := &fields[i]
		if f.flag != "" {
			flagsByGroup[f.grp] = append(flagsByGroup[f.grp], f)
		}
		if f.argG != "" {
			argsByName[f.argG] = append(argsByName[f.argG], f)
		}
	}

	// Build token maps for groups
	tokensForGroups := func(groups []string) map[string]*fieldInfo {
		m := map[string]*fieldInfo{}
		for _, g := range groups {
			for _, f := range flagsByGroup[g] {
				m[f.flag] = f
				for _, a := range f.alts {
					m[a] = f
				}
			}
		}
		return m
	}

	// Recursively parse slots with inherited unordered groups
	idx := 0

	var collectUnordered func(g Group) []string
	collectUnordered = func(g Group) []string {
		var names []string
		for _, u := range g.Unordered {
			if fg, ok := u.(FlagGroup); ok {
				names = append(names, fg.Name)
			}
		}
		return names
	}

	// Consume flags from allowed set greedily; unknown flag ends window (no error here)
	consumeFlags := func(allowed map[string]*fieldInfo) error {
		for idx < len(args) {
			tok := args[idx]
			fi, ok := allowed[tok]
			if !ok {
				// not allowed here; leave for later items
				break
			}
			idx++
			if flagTakesValue(fi.val) {
				if idx >= len(args) {
					return fmt.Errorf("flag %s requires value", tok)
				}
				val := args[idx]
				idx++
				if err := setValue(fi.val, val); err != nil {
					return fmt.Errorf("%s: %w", fi.sf.Name, err)
				}
			} else {
				if err := setValue(fi.val, ""); err != nil {
					return fmt.Errorf("%s: %w", fi.sf.Name, err)
				}
			}
		}
		return nil
	}

	var parse func(s Slot, inheritedUnordered []string) error
	parse = func(s Slot, inheritedUnordered []string) error {
		switch v := s.(type) {
		case Group:
			if idx >= len(args) {
				onlyOptional := true
				for _, o := range v.Ordered {
					switch o.(type) {
					case FlagGroup, Subcommand:
					case Literal:
						// handled below
						onlyOptional = false
						break
					default:
						onlyOptional = false
					}
					if !onlyOptional {
						break
					}
				}
				if onlyOptional {
					return nil
				}
				if len(v.Ordered) > 0 {
					if _, ok := v.Ordered[0].(Literal); ok {
						return nil
					}
				}
				return fmt.Errorf("missing required arguments")
			}
			// Active unordered groups = inherited + this group's unordered
			localUnordered := append([]string{}, inheritedUnordered...)
			localUnordered = append(localUnordered, collectUnordered(v)...)
			unorderedTokens := tokensForGroups(localUnordered)

			// Walk ordered items in sequence
			blockUnordered := false
			for i := 0; i < len(v.Ordered); i++ {
				// Determine whether the next ordered item is a Literal
				nextIsLiteral := false
				if i+1 < len(v.Ordered) {
					_, nextIsLiteral = v.Ordered[i+1].(Literal)
				}

				// Greedily consume unordered before this item (unless blocked or this item is a Literal)
				if _, isLit := v.Ordered[i].(Literal); !blockUnordered && !isLit {
					if err := consumeFlags(unorderedTokens); err != nil {
						return err
					}
				}

				switch ov := v.Ordered[i].(type) {
				case FlagGroup:
					// Position-specific flag window
					allowed := tokensForGroups([]string{ov.Name})
					if err := consumeFlags(allowed); err != nil {
						return err
					}
				case Subcommand:
					// Subcommand token is removed by ParseAny; act as anchor only
				case Literal:
					if idx >= len(args) || args[idx] != ov.Value {
						return fmt.Errorf("expected literal %q", ov.Value)
					}
					idx++
					// After a Literal, disable unordered flags for the rest of this Group
					blockUnordered = true
				case Argument:
					fs := argsByName[ov.Name]
					for _, fi := range fs {
						if idx >= len(args) {
							return fmt.Errorf("missing value for %s", ov.Name)
						}
						val := args[idx]
						idx++
						if err := setValue(fi.val, val); err != nil {
							return fmt.Errorf("%s: %w", fi.sf.Name, err)
						}
					}
				case Arguments:
					// must be last within this group's ordered sequence
					if i != len(v.Ordered)-1 {
						return fmt.Errorf("variadic arguments %s must be last", ov.Name)
					}
					fs := argsByName[ov.Name]
					for idx < len(args) {
						val := args[idx]
						// No special literal handling; literals must be next ordered
						idx++
						for _, fi := range fs {
							if err := setValue(fi.val, val); err != nil {
								return fmt.Errorf("%s: %w", fi.sf.Name, err)
							}
						}
					}
				default:
					// Nested slot (e.g., nested Group)
					if err := parse(ov, localUnordered); err != nil {
						return err
					}
				}
				// Greedily consume unordered after this item unless blocked or next ordered is a Literal
				if !blockUnordered && !nextIsLiteral {
					if err := consumeFlags(unorderedTokens); err != nil {
						return err
					}
				}
			}
			return nil
		default:
			// nothing to do for other root-level non-group (not expected)
			return nil
		}
	}

	if err := parse(cmd.Slots(), nil); err != nil {
		return err
	}
	if idx != len(args) {
		return fmt.Errorf("unexpected trailing args: %v", args[idx:])
	}
	return nil
}

func flagTakesValue(v reflect.Value) bool {
	t := v.Type()
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t.Kind() == reflect.Bool {
		return false
	}
	return true
}

func setValue(v reflect.Value, val string) error {
	for v.Kind() == reflect.Pointer {
		if v.IsNil() {
			v.Set(reflect.New(v.Type().Elem()))
		}
		v = v.Elem()
	}
	switch v.Kind() {
	case reflect.Bool:
		v.SetBool(true)
		return nil
	case reflect.String:
		v.SetString(val)
		return nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		n, err := strconv.ParseInt(val, 10, 64)
		if err != nil {
			return err
		}
		if v.OverflowInt(n) {
			return fmt.Errorf("value %q overflows field of type %s", val, v.Type())
		}
		v.SetInt(n)
		return nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		n, err := strconv.ParseUint(val, 10, 64)
		if err != nil {
			return err
		}
		if v.OverflowUint(n) {
			return fmt.Errorf("value %q overflows field of type %s", val, v.Type())
		}
		v.SetUint(n)
		return nil
	case reflect.Slice:
		elem := v.Type().Elem()
		switch elem.Kind() {
		case reflect.String:
			v.Set(reflect.Append(v, reflect.ValueOf(val)))
			return nil
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
			n, err := strconv.ParseInt(val, 10, 64)
			if err != nil {
				return err
			}
			v.Set(reflect.Append(v, reflect.ValueOf(n).Convert(elem)))
			return nil
		case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
			n, err := strconv.ParseUint(val, 10, 64)
			if err != nil {
				return err
			}
			v.Set(reflect.Append(v, reflect.ValueOf(n).Convert(elem)))
			return nil
		}
	}
	return fmt.Errorf("unsupported field kind %s", v.Kind())
}

// expandEquals splits tokens of the form "--flag=value" or "-f=value" into
// separate flag and value tokens so that standard flag processing can occur.
func expandEquals(args []string) []string {
	var out []string
	for _, a := range args {
		if strings.HasPrefix(a, "-") {
			if eq := strings.Index(a, "="); eq != -1 {
				out = append(out, a[:eq], a[eq+1:])
				continue
			}
		}
		out = append(out, a)
	}
	return out
}
