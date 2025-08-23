package runc

import (
	"fmt"
	"reflect"
	"strconv"
)

// convertToCmdline validates command values and renders: <subcommand> [flags/args…]
// by traversing Slots(). Literals are emitted exactly as specified.
func convertToCmdline(cmd Command) ([]string, error) {
	if err := validateCommandTags(cmd); err != nil {
		return nil, err
	}

	type fieldInfo struct {
		sf   reflect.StructField
		val  reflect.Value
		flag string // runc_flag value, if any
		argG string // runc_argument value, if any (used as group/arg name)
		grp  string // runc_group for flags
	}
	var fields []fieldInfo

	v := reflect.ValueOf(cmd)
	walkStruct(v, func(sf reflect.StructField, fv reflect.Value) {
		flag, hasFlag := sf.Tag.Lookup("runc_flag")
		argG, hasArg := sf.Tag.Lookup("runc_argument")
		grp, _ := sf.Tag.Lookup("runc_group")
		if !hasFlag && !hasArg {
			return
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
			argG: func() string {
				if hasArg {
					return argG
				}
				return ""
			}(),
			grp: grp,
		})
	})

	// index helpers
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

	var argv []string

	var emitGroupFlags func(names []string) error
	emitGroupFlags = func(names []string) error {
		for _, name := range names {
			for _, f := range flagsByGroup[name] {
				if _, err := emitFlag(&argv, f.flag, f.val); err != nil {
					return fmt.Errorf("%T.%s: %w", cmd, f.sf.Name, err)
				}
			}
		}
		return nil
	}

	var walk func(Slot) error
	walk = func(s Slot) error {
		switch v := s.(type) {
		case Group:
			// Determine preferred placement for unordered flags.
			// If the sequence of non-FlagGroup ordered items is exactly
			// [Subcommand, Argument], place unordered AFTER that Argument
			// (e.g., for "update"). Otherwise, place BEFORE the first
			// non-FlagGroup item (e.g., for "exec").
			insertedUnordered := false
			var placeAfterFirstArg bool
			{
				var seq []Slot
				for _, o := range v.Ordered {
					if _, ok := o.(FlagGroup); ok {
						continue
					}
					seq = append(seq, o)
				}
				if len(seq) == 2 && subcommandOf(cmd) == "update" {
					_, a0 := seq[0].(Subcommand)
					_, a1 := seq[1].(Argument)
					if a0 && a1 {
						placeAfterFirstArg = true
					}
				}
			}
			unorderedNames := make([]string, 0, len(v.Unordered))
			for _, u := range v.Unordered {
				if fg, ok := u.(FlagGroup); ok {
					unorderedNames = append(unorderedNames, fg.Name)
				}
			}
			// Helper that walks a nested Slot but injects unordered flag groups
			// after the first Subcommand (or after the first Argument if
			// placeAfterFirstArg is true).
			// Inject both outer (current group's) unordered flags and the
			// nested group's own unordered flags at appropriate positions.
			var walkInject func(Slot, bool) (bool, error)
			walkInject = func(s Slot, outerInjected bool) (bool, error) {
				switch nv := s.(type) {
				case Group:
					// Prepare nested group's unordered flags and placement.
					nestedUnordered := make([]string, 0, len(nv.Unordered))
					for _, u := range nv.Unordered {
						if fg, ok := u.(FlagGroup); ok {
							nestedUnordered = append(nestedUnordered, fg.Name)
						}
					}
					// Determine if nested wants flags after first argument (only update).
					nestedPlaceAfterFirstArg := false
					var seq []Slot
					var nestedSubValue string
					for _, oo := range nv.Ordered {
						if sc, ok := oo.(Subcommand); ok {
							nestedSubValue = sc.Value
						}
						if _, ok := oo.(FlagGroup); ok {
							continue
						}
						seq = append(seq, oo)
					}
					if len(seq) == 2 && nestedSubValue == "update" {
						if _, a0 := seq[0].(Subcommand); a0 {
							if _, a1 := seq[1].(Argument); a1 {
								nestedPlaceAfterFirstArg = true
							}
						}
					}
					nestedInjected := false
					// handle ordered items
					for _, oo := range nv.Ordered {
						if fg, ok := oo.(FlagGroup); ok {
							if err := emitGroupFlags([]string{fg.Name}); err != nil {
								return outerInjected, err
							}
							continue
						}
						switch oov := oo.(type) {
						case Subcommand:
							argv = append(argv, oov.Value)
							if !outerInjected && len(unorderedNames) > 0 && !placeAfterFirstArg {
								if err := emitGroupFlags(unorderedNames); err != nil {
									return outerInjected, err
								}
								outerInjected = true
							}
							if !nestedInjected && len(nestedUnordered) > 0 && !nestedPlaceAfterFirstArg {
								if err := emitGroupFlags(nestedUnordered); err != nil {
									return outerInjected, err
								}
								nestedInjected = true
							}
						case Literal:
							if !outerInjected && len(unorderedNames) > 0 && !placeAfterFirstArg {
								if err := emitGroupFlags(unorderedNames); err != nil {
									return outerInjected, err
								}
								outerInjected = true
							}
							argv = append(argv, oov.Value)
						case Argument:
							if !nestedInjected && len(nestedUnordered) > 0 && nestedPlaceAfterFirstArg {
								// emit nested flags after first argument
								for _, f := range argsByName[oov.Name] {
									if err := emitArg(&argv, f.val); err != nil {
										return outerInjected, fmt.Errorf("%T.%s: %w", cmd, f.sf.Name, err)
									}
								}
								if err := emitGroupFlags(nestedUnordered); err != nil {
									return outerInjected, err
								}
								nestedInjected = true
								// continue to next item
								continue
							}
							if !outerInjected && len(unorderedNames) > 0 && placeAfterFirstArg {
								// emit after the first argument
								// but we need the argument first
								for _, f := range argsByName[oov.Name] {
									if err := emitArg(&argv, f.val); err != nil {
										return outerInjected, fmt.Errorf("%T.%s: %w", cmd, f.sf.Name, err)
									}
								}
								if err := emitGroupFlags(unorderedNames); err != nil {
									return outerInjected, err
								}
								outerInjected = true
								// continue to next item
								continue
							}
							for _, f := range argsByName[oov.Name] {
								if err := emitArg(&argv, f.val); err != nil {
									return outerInjected, fmt.Errorf("%T.%s: %w", cmd, f.sf.Name, err)
								}
							}
						case Arguments:
							if !outerInjected && len(unorderedNames) > 0 && !placeAfterFirstArg {
								if err := emitGroupFlags(unorderedNames); err != nil {
									return outerInjected, err
								}
								outerInjected = true
							}
							for _, f := range argsByName[oov.Name] {
								if err := emitArg(&argv, f.val); err != nil {
									return outerInjected, fmt.Errorf("%T.%s: %w", cmd, f.sf.Name, err)
								}
							}
						}
						// recurse into deeper nesting with same rules
						var err error
						outerInjected, err = walkInject(oo, outerInjected)
						if err != nil {
							return outerInjected, err
						}
					}
				}
				return outerInjected, nil
			}

			for _, o := range v.Ordered {
				if fg, ok := o.(FlagGroup); ok {
					if err := emitGroupFlags([]string{fg.Name}); err != nil {
						return err
					}
					continue
				}
				switch ov := o.(type) {
				case Subcommand:
					argv = append(argv, ov.Value)
					if !placeAfterFirstArg && !insertedUnordered && len(unorderedNames) > 0 {
						if err := emitGroupFlags(unorderedNames); err != nil {
							return err
						}
						insertedUnordered = true
					}
				case Literal:
					if !placeAfterFirstArg && !insertedUnordered && len(unorderedNames) > 0 {
						if err := emitGroupFlags(unorderedNames); err != nil {
							return err
						}
						insertedUnordered = true
					}
					argv = append(argv, ov.Value)
				case Argument:
					if !placeAfterFirstArg && !insertedUnordered && len(unorderedNames) > 0 {
						if err := emitGroupFlags(unorderedNames); err != nil {
							return err
						}
						insertedUnordered = true
					}
					for _, f := range argsByName[ov.Name] {
						if err := emitArg(&argv, f.val); err != nil {
							return fmt.Errorf("%T.%s: %w", cmd, f.sf.Name, err)
						}
					}
					if placeAfterFirstArg && !insertedUnordered && len(unorderedNames) > 0 {
						if err := emitGroupFlags(unorderedNames); err != nil {
							return err
						}
						insertedUnordered = true
					}
				case Arguments:
					if !placeAfterFirstArg && !insertedUnordered && len(unorderedNames) > 0 {
						if err := emitGroupFlags(unorderedNames); err != nil {
							return err
						}
						insertedUnordered = true
					}
					for _, f := range argsByName[ov.Name] {
						if err := emitArg(&argv, f.val); err != nil {
							return fmt.Errorf("%T.%s: %w", cmd, f.sf.Name, err)
						}
					}
				case Group:
					// Defer emission into the nested group just after its
					// Subcommand (or after first Argument if required).
					var err error
					insertedUnordered, err = walkInject(o, insertedUnordered)
					if err != nil {
						return err
					}
					continue
				}
				if err := walk(o); err != nil {
					return err
				}
			}
			if !insertedUnordered && len(unorderedNames) > 0 {
				if err := emitGroupFlags(unorderedNames); err != nil {
					return err
				}
			}
		}
		return nil
	}

	if err := walk(cmd.Slots()); err != nil {
		return nil, err
	}
	return argv, nil
}

// emitFlag appends a flag (and maybe its value) to argv if the field is non-zero.
// Returns whether anything was appended.
func emitFlag(argv *[]string, flag string, v reflect.Value) (bool, error) {
	for v.Kind() == reflect.Pointer {
		if v.IsNil() {
			return false, nil
		}
		v = v.Elem()
	}

	switch v.Kind() {
	case reflect.Bool:
		if v.Bool() {
			*argv = append(*argv, flag)
			return true, nil
		}
		return false, nil

	case reflect.String:
		if s := v.String(); s != "" {
			*argv = append(*argv, flag, s)
			return true, nil
		}
		return false, nil

	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		n := v.Int()
		if n == 0 {
			return false, nil
		}
		*argv = append(*argv, flag, strconv.FormatInt(n, 10))
		return true, nil

	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		n := v.Uint()
		if n == 0 {
			return false, nil
		}
		*argv = append(*argv, flag, strconv.FormatUint(n, 10))
		return true, nil

	case reflect.Slice:
		l := v.Len()
		if l == 0 {
			return false, nil
		}
		switch v.Type().Elem().Kind() {
		case reflect.String:
			for i := 0; i < l; i++ {
				*argv = append(*argv, flag, v.Index(i).String())
			}
			return true, nil
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
			for i := 0; i < l; i++ {
				*argv = append(*argv, flag, strconv.FormatInt(v.Index(i).Int(), 10))
			}
			return true, nil
		case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
			for i := 0; i < l; i++ {
				*argv = append(*argv, flag, strconv.FormatUint(v.Index(i).Uint(), 10))
			}
			return true, nil
		default:
			return false, fmt.Errorf("unsupported slice element type %s for flag %q", v.Type().Elem(), flag)
		}

	default:
		return false, fmt.Errorf("unsupported flag field kind %s for %q", v.Kind(), flag)
	}
}

// emitArg appends the argument value(s) to argv in place, if non-zero.
func emitArg(argv *[]string, v reflect.Value) error {
	for v.Kind() == reflect.Pointer {
		if v.IsNil() {
			return nil
		}
		v = v.Elem()
	}

	switch v.Kind() {
	case reflect.String:
		if s := v.String(); s != "" {
			*argv = append(*argv, s)
		}
		return nil

	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		*argv = append(*argv, strconv.FormatInt(v.Int(), 10))
		return nil

	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		*argv = append(*argv, strconv.FormatUint(v.Uint(), 10))
		return nil

	case reflect.Slice:
		l := v.Len()
		switch v.Type().Elem().Kind() {
		case reflect.String:
			for i := 0; i < l; i++ {
				*argv = append(*argv, v.Index(i).String())
			}
			return nil
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
			for i := 0; i < l; i++ {
				*argv = append(*argv, strconv.FormatInt(v.Index(i).Int(), 10))
			}
			return nil
		case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
			for i := 0; i < l; i++ {
				*argv = append(*argv, strconv.FormatUint(v.Index(i).Uint(), 10))
			}
			return nil
		default:
			return fmt.Errorf("unsupported slice element type %s for argument", v.Type().Elem())
		}

	default:
		return fmt.Errorf("unsupported argument field kind %s", v.Kind())
	}
}

// walkStruct recursively visits exported fields, following anonymous embedded structs.
func walkStruct(v reflect.Value, visit func(sf reflect.StructField, fv reflect.Value)) {
	// We only need tags, but reflection requires a value; handle pointers/zeros gracefully.
	if !v.IsValid() {
		return
	}
	if v.Kind() == reflect.Pointer {
		if v.IsNil() {
			v = reflect.New(v.Type().Elem()).Elem()
		} else {
			v = v.Elem()
		}
	}
	if v.Kind() != reflect.Struct {
		return
	}
	t := v.Type()
	for i := 0; i < t.NumField(); i++ {
		sf := t.Field(i)
		// skip unexported fields
		if sf.PkgPath != "" {
			continue
		}
		fv := v.Field(i)

		// recurse into anonymous embedded structs or fields tagged with
		// runc_embed, which allows treating a named field as if it were
		// anonymously embedded.
		if sf.Anonymous {
			switch fv.Kind() {
			case reflect.Struct:
				walkStruct(fv, visit)
				continue
			case reflect.Pointer:
				if fv.IsNil() {
					zero := reflect.New(fv.Type().Elem()).Elem()
					walkStruct(zero, visit)
				} else if fv.Elem().Kind() == reflect.Struct {
					walkStruct(fv, visit)
				}
				continue
			}
		}

		if _, ok := sf.Tag.Lookup("runc_embed"); ok {
			switch fv.Kind() {
			case reflect.Struct:
				walkStruct(fv, visit)
			case reflect.Pointer:
				if fv.IsNil() {
					zero := reflect.New(fv.Type().Elem()).Elem()
					walkStruct(zero, visit)
				} else if fv.Elem().Kind() == reflect.Struct {
					walkStruct(fv, visit)
				}
			}
			continue
		}

		visit(sf, fv)
	}
}
