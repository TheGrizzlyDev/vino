package runc

import (
	"fmt"
	"reflect"
	"strconv"
)

// convertToCmdline validates command values and renders: <subcommand> [flags/args…] according to Groups() order.
func convertToCmdline(cmd Command) ([]string, error) {
	if err := validateCommandTags(cmd); err != nil {
		return nil, err
	}

	argv := []string{cmd.Subcommand()}

	type fieldInfo struct {
		sf   reflect.StructField
		val  reflect.Value
		flag string // runc_flag value, if any
		argG string // runc_argument value, if any (used as group)
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

	for _, g := range cmd.Groups() {
		if g == "--" {
			argv = append(argv, "--")
			continue
		}
		for _, f := range fields {
			if f.flag == "" || f.grp != g {
				continue
			}
			added, err := emitFlag(&argv, f.flag, f.val)
			if err != nil {
				return nil, fmt.Errorf("%T.%s: %w", cmd, f.sf.Name, err)
			}
			_ = added
		}
		for _, f := range fields {
			if f.argG != g {
				continue
			}
			if err := emitArg(&argv, f.val); err != nil {
				return nil, fmt.Errorf("%T.%s: %w", cmd, f.sf.Name, err)
			}
		}
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