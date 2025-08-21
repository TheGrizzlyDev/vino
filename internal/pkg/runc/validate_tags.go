package runc

import (
	"errors"
	"fmt"
	"reflect"
	"regexp"
	"strings"
)

func validateCommandTags(cmd Command) error {
	if cmd == nil {
		return errors.New("ValidateCommandTags: nil cmd")
	}
	typ := reflect.TypeOf(cmd)

	if strings.TrimSpace(cmd.Subcommand()) == "" {
		return fmt.Errorf("ValidateCommandTags: %s Subcommand() returned empty", typ)
	}
	groups := cmd.Groups()
	if len(groups) == 0 {
		return fmt.Errorf("ValidateCommandTags: %s Groups() returned empty", typ)
	}
	if dup := firstDuplicate(groups); dup != "" {
		return fmt.Errorf(`ValidateCommandTags: %s Groups() contains duplicate group %q`, typ, dup)
	}
	if count(groups, "--") > 1 {
		return fmt.Errorf(`ValidateCommandTags: %s Groups() contains "--" more than once`, typ)
	}
	declared := make(map[string]struct{}, len(groups))
	for _, g := range groups {
		declared[g] = struct{}{}
	}

	var errs []string
	v := reflect.ValueOf(cmd)
	walkStruct(v, func(sf reflect.StructField, fv reflect.Value) {
		flag, hasFlag := sf.Tag.Lookup("runc_flag")
		altSpec, hasAlt := sf.Tag.Lookup("runc_flag_alternatives")
		argGroup, hasArg := sf.Tag.Lookup("runc_argument")
		group, hasGroup := sf.Tag.Lookup("runc_group")
		enum, hasEnum := sf.Tag.Lookup("runc_enum")

		// skip untagged fields
		if !hasFlag && !hasArg && !hasAlt {
			return
		}

		if hasAlt && !hasFlag {
			errs = append(errs, fmt.Sprintf("%s: field %q has runc_flag_alternatives but no runc_flag", typ, sf.Name))
		}

		// mutually exclusive tags
		if hasFlag && hasArg {
			errs = append(errs, fmt.Sprintf("%s: field %q cannot have both runc_flag and runc_argument", typ, sf.Name))
			return
		}

		// --- runc_flag rules (tags only) ---
		if hasFlag {
			if strings.TrimSpace(flag) == "" {
				errs = append(errs, fmt.Sprintf("%s: field %q has empty runc_flag", typ, sf.Name))
			} else if !looksLikeFlag(flag) {
				errs = append(errs, fmt.Sprintf("%s: field %q runc_flag=%q must start with '-' or '--'", typ, sf.Name, flag))
			}
			if !hasGroup || strings.TrimSpace(group) == "" {
				errs = append(errs, fmt.Sprintf("%s: field %q (flag %q) missing required runc_group", typ, sf.Name, flag))
			} else {
				if group == "--" {
					errs = append(errs, fmt.Sprintf(`%s: field %q (flag %q) uses forbidden group "--" (use "--" only in Groups())`, typ, sf.Name, flag))
				} else if _, ok := declared[group]; !ok {
					errs = append(errs, fmt.Sprintf(`%s: field %q (flag %q) references group %q not present in Groups()`, typ, sf.Name, flag, group))
				}
			}
			// alternatives
			if hasAlt {
				altsRaw := strings.Split(altSpec, "|")
				var alts []string
				for _, a := range altsRaw {
					a = strings.TrimSpace(a)
					if a == "" {
						errs = append(errs, fmt.Sprintf("%s: field %q has empty runc_flag_alternative", typ, sf.Name))
						continue
					}
					if !looksLikeFlag(a) {
						errs = append(errs, fmt.Sprintf("%s: field %q runc_flag_alternative %q must start with '-' or '--'", typ, sf.Name, a))
					}
					if a == flag {
						errs = append(errs, fmt.Sprintf("%s: field %q runc_flag_alternative %q duplicates runc_flag", typ, sf.Name, a))
					}
					alts = append(alts, a)
				}
				if dup := firstDuplicate(alts); dup != "" {
					errs = append(errs, fmt.Sprintf("%s: field %q has duplicate runc_flag_alternative %q", typ, sf.Name, dup))
				}
			}

			// enum (static) rules: must be applied only to string/*string and non-empty spec
			if hasEnum {
				if strings.TrimSpace(enum) == "" || !strings.Contains(enum, "|") {
					errs = append(errs, fmt.Sprintf(`%s: field %q has invalid runc_enum %q (must be pipe-delimited like "a|b|c")`, typ, sf.Name, enum))
				}
				if !isStringish(sf.Type) {
					errs = append(errs, fmt.Sprintf(`%s: field %q has runc_enum but is not string or *string`, typ, sf.Name))
				}
			}
			return
		}

		// --- runc_argument rules (tags only) ---
		if hasArg {
			if hasGroup {
				errs = append(errs, fmt.Sprintf("%s: field %q (argument %q) must NOT set runc_group", typ, sf.Name, argGroup))
			}
			argGroup = strings.TrimSpace(argGroup)
			if argGroup == "" {
				errs = append(errs, fmt.Sprintf("%s: field %q has empty runc_argument", typ, sf.Name))
			} else {
				if argGroup == "--" {
					errs = append(errs, fmt.Sprintf(`%s: field %q uses forbidden runc_argument "--" (use "--" only in Groups())`, typ, sf.Name))
				} else if _, ok := declared[argGroup]; !ok {
					errs = append(errs, fmt.Sprintf(`%s: field %q (argument %q) references group %q not present in Groups()`, typ, sf.Name, argGroup, argGroup))
				}
			}
			// runc_enum on arguments is meaningless in this model; reject if present
			if hasEnum {
				errs = append(errs, fmt.Sprintf(`%s: field %q (argument %q) must not have runc_enum`, typ, sf.Name, argGroup))
			}
			return
		}
	})

	if len(errs) > 0 {
		return errors.New("ValidateCommandTags:\n  - " + strings.Join(errs, "\n  - "))
	}
	return nil
}

// ----------------- helpers (tags-only) -----------------

func looksLikeFlag(s string) bool {
	return strings.HasPrefix(s, "-")
}

func isStringish(t reflect.Type) bool {
	if t.Kind() == reflect.String {
		return true
	}
	return t.Kind() == reflect.Pointer && t.Elem().Kind() == reflect.String
}

func firstDuplicate(ss []string) string {
	seen := map[string]struct{}{}
	for _, s := range ss {
		if _, ok := seen[s]; ok {
			return s
		}
		seen[s] = struct{}{}
	}
	return ""
}

func count(ss []string, needle string) int {
	n := 0
	for _, s := range ss {
		if s == needle {
			n++
		}
	}
	return n
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

// (optional) quick sanity check for enum spec format (kept private; used above)
var enumSpecRe = regexp.MustCompile(`^[^|]+(\|[^|]+)+$`)
