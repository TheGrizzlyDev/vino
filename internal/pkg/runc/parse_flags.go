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

	v := reflect.ValueOf(cmdUnion).Elem()

	for i := range v.NumField() {
		field := v.Field(i)
		cmdReflect := reflect.New(field.Type().Elem())
		subcommandCall := cmdReflect.MethodByName("Subcommand").Call([]reflect.Value{})
		if args[0] != subcommandCall[0].String() {
			continue
		}
		cmd, ok := cmdReflect.Interface().(Command)
		if !ok {
			return fmt.Errorf("field type '%s' does not implement Command", field.Type().Name())
		}
		if err := Parse(&cmd, args[1:]); err != nil {
			return err
		}

		if !field.CanSet() {
			return fmt.Errorf("field %d not settable", i)
		}

		field.Set(cmdReflect)
		return nil
	}

	return fmt.Errorf("Parse: no valid subcommand found")
}

// Parse reads args into cmd according to struct tags.
// Flags from groups within the same contiguous segment may appear in any order.
// The ordering is only enforced between argument groups, literal "--" markers,
// and contiguous flag-group segments defined by cmd.Groups().
func Parse[T Command](cmd *T, args []string) error {
	if cmd == nil {
		return fmt.Errorf("Parse: nil cmd")
	}
	// validate tags first
	if err := validateCommandTags(*cmd); err != nil {
		return err
	}

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

	// Build maps for flags and argument groups
	flagByToken := map[string]*fieldInfo{}
	flagsByGroup := map[string][]*fieldInfo{}
	argsByGroup := map[string][]*fieldInfo{}
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
			argsByGroup[f.argG] = append(argsByGroup[f.argG], f)
		}
	}

	type segment interface{}
	type flagSegment struct{ groups []string }
	type argSegment struct{ group string }
	type literalSegment struct{}

	var segs []segment
	var curFlagSeg []string
	groups := (*cmd).Groups()
	for _, g := range groups {
		if g == "--" {
			if len(curFlagSeg) > 0 {
				segs = append(segs, flagSegment{groups: curFlagSeg})
				curFlagSeg = nil
			}
			segs = append(segs, literalSegment{})
			continue
		}
		if _, ok := argsByGroup[g]; ok {
			if len(curFlagSeg) > 0 {
				segs = append(segs, flagSegment{groups: curFlagSeg})
				curFlagSeg = nil
			}
			segs = append(segs, argSegment{group: g})
			continue
		}
		curFlagSeg = append(curFlagSeg, g)
	}
	if len(curFlagSeg) > 0 {
		segs = append(segs, flagSegment{groups: curFlagSeg})
	}

	idx := 0
	for si, sg := range segs {
		switch s := sg.(type) {
		case flagSegment:
			// build allowed flags map for this segment
			allowed := map[string]*fieldInfo{}
			for _, g := range s.groups {
				for _, f := range flagsByGroup[g] {
					allowed[f.flag] = f
					for _, a := range f.alts {
						allowed[a] = f
					}
				}
			}
			for idx < len(args) {
				tok := args[idx]
				if tok == "--" {
					break
				}
				fi, ok := allowed[tok]
				if !ok {
					if strings.HasPrefix(tok, "-") {
						return fmt.Errorf("unexpected flag %q", tok)
					}
					break // start of next segment
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
		case literalSegment:
			if idx >= len(args) || args[idx] != "--" {
				return fmt.Errorf("expected literal --")
			}
			idx++
		case argSegment:
			fs := argsByGroup[s.group]
			for _, fi := range fs {
				if fi.val.Kind() == reflect.Slice {
					if si != len(segs)-1 {
						return fmt.Errorf("slice argument %s must be last", s.group)
					}
					for idx < len(args) {
						val := args[idx]
						idx++
						if err := setValue(fi.val, val); err != nil {
							return fmt.Errorf("%s: %w", fi.sf.Name, err)
						}
					}
				} else {
					if idx >= len(args) {
						return fmt.Errorf("missing value for %s", s.group)
					}
					val := args[idx]
					idx++
					if err := setValue(fi.val, val); err != nil {
						return fmt.Errorf("%s: %w", fi.sf.Name, err)
					}
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
		v.SetInt(n)
		return nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		n, err := strconv.ParseUint(val, 10, 64)
		if err != nil {
			return err
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
