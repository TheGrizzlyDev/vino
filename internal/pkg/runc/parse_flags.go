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
    // validate tags first
    if err := validateCommandTags(cmd); err != nil {
        return err
    }

	// Support flags in the form --flag=value by splitting them into
	// separate tokens before processing.
	args = expandEquals(args)

	type fieldInfo struct {
		sf   reflect.StructField
		val  reflect.Value
		flag string   // runc_flag
		alts []string // runc_flag_alternatives
		argG string   // runc_argument (group)
		grp  string   // runc_group for flags
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
		fields = append(fields, fieldInfo{
			sf:  sf,
			val: fv,
			flag: func() string {
				if hasFlag {
					return flag
				}
				return ""
			}(),
			alts: alts,
			argG: func() string {
				if hasArg {
					return argG
				}
				return ""
			}(),
			grp: grp,
		})
	})

    // Build maps for flags and argument names
    flagByToken := map[string]*fieldInfo{}
    flagsByGroup := map[string][]*fieldInfo{}
    argsByName := map[string][]*fieldInfo{}
    for i := range fields {
        f := &fields[i]
        if f.flag != "" {
            flagByToken[f.flag] = f
            for _, a := range f.alts {
                flagByToken[a] = f
            }
            flagsByGroup[f.grp] = append(flagsByGroup[f.grp], f)
        }
        if f.argG != "" {
            argsByName[f.argG] = append(argsByName[f.argG], f)
        }
    }

    // Build a sequence of parsing actions by walking Slots().
    type segment interface{}
    type flagSegment struct{ groups []string }
    type literalSegment struct{ value string }
    type argSegment struct{ name string; variadic bool }

    var segs []segment
    var walk func(Slot)
    walk = func(s Slot) {
        switch v := s.(type) {
        case Group:
            insertedUnordered := false
            // Prefer placing unordered flags after [Subcommand, Argument]
            // if that exact pattern occurs; otherwise place before the
            // first non-FlagGroup token.
            placeAfterFirstArg := false
            {
                var seq []Slot
                for _, o := range v.Ordered {
                    if _, ok := o.(FlagGroup); ok { continue }
                    seq = append(seq, o)
                }
                if len(seq) == 2 && subcommandOf(cmd) == "update" {
                    _, a0 := seq[0].(Subcommand)
                    _, a1 := seq[1].(Argument)
                    if a0 && a1 { placeAfterFirstArg = true }
                }
            }
            // helper to append unordered flags once
            appendUnordered := func() {
                if insertedUnordered || len(v.Unordered) == 0 { return }
                var names []string
                for _, u := range v.Unordered {
                    if fg, ok := u.(FlagGroup); ok { names = append(names, fg.Name) }
                }
                if len(names) > 0 {
                    segs = append(segs, flagSegment{groups: names})
                }
                insertedUnordered = true
            }
            for _, o := range v.Ordered {
                if fg, ok := o.(FlagGroup); ok {
                    // Ignore the special "global" group in per-command parsing.
                    if fg.Name != "global" {
                        segs = append(segs, flagSegment{groups: []string{fg.Name}})
                    }
                } else {
                    switch ov := o.(type) {
                    case Subcommand:
                        // Subcommand token consumed by ParseAny; insert unordered
                        // here only if we are NOT delaying until after first arg.
                        if !placeAfterFirstArg {
                            appendUnordered()
                        }
                    case Literal:
                        appendUnordered()
                        segs = append(segs, literalSegment{value: ov.Value})
                    case Argument:
                        if placeAfterFirstArg {
                            // First capture the argument, then place unordered flags
                            segs = append(segs, argSegment{name: ov.Name})
                            appendUnordered()
                        } else {
                            appendUnordered()
                            segs = append(segs, argSegment{name: ov.Name})
                        }
                    case Arguments:
                        appendUnordered()
                        segs = append(segs, argSegment{name: ov.Name, variadic: true})
                    }
                }
                // If next is a nested group and we didn't insert yet (and
                // we're not delaying), insert before recursing so flags can
                // appear before nested tokens like Subcommand.
                if _, isGroup := o.(Group); isGroup && !insertedUnordered && !placeAfterFirstArg {
                    appendUnordered()
                }
                walk(o)
            }
            if !insertedUnordered {
                // no non-flag ordered items; place unordered at end
                var names []string
                for _, u := range v.Unordered { if fg, ok := u.(FlagGroup); ok { names = append(names, fg.Name) } }
                if len(names) > 0 { segs = append(segs, flagSegment{groups: names}) }
            }
        }
    }
    walk(cmd.Slots())

    idx := 0
    for si, sg := range segs {
        switch s := sg.(type) {
        case flagSegment:
            // collect allowed flags
            allowed := map[string]*fieldInfo{}
            for _, g := range s.groups {
                for _, f := range flagsByGroup[g] {
                    allowed[f.flag] = f
                    for _, a := range f.alts { allowed[a] = f }
                }
            }
            for idx < len(args) {
                tok := args[idx]
                fi, ok := allowed[tok]
                if !ok {
                    // not a flag for this segment; move to next segment
                    break
                }
                idx++
                if flagTakesValue(fi.val) {
                    if idx >= len(args) { return fmt.Errorf("flag %s requires value", tok) }
                    val := args[idx]; idx++
                    if err := setValue(fi.val, val); err != nil { return fmt.Errorf("%s: %w", fi.sf.Name, err) }
                } else {
                    if err := setValue(fi.val, ""); err != nil { return fmt.Errorf("%s: %w", fi.sf.Name, err) }
                }
            }
        case literalSegment:
            if idx >= len(args) || args[idx] != s.value {
                return fmt.Errorf("expected literal %q", s.value)
            }
            idx++
        case argSegment:
            fs := argsByName[s.name]
            for _, fi := range fs {
                if fi.val.Kind() == reflect.Slice || (fi.val.Kind() == reflect.Pointer && fi.val.Elem().Kind() == reflect.Slice) || s.variadic {
                    if si != len(segs)-1 {
                        return fmt.Errorf("slice argument %s must be last", s.name)
                    }
                    for idx < len(args) {
                        val := args[idx]; idx++
                        if err := setValue(fi.val, val); err != nil { return fmt.Errorf("%s: %w", fi.sf.Name, err) }
                    }
                } else {
                    if idx >= len(args) { return fmt.Errorf("missing value for %s", s.name) }
                    val := args[idx]; idx++
                    if err := setValue(fi.val, val); err != nil { return fmt.Errorf("%s: %w", fi.sf.Name, err) }
                }
            }
        }
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
