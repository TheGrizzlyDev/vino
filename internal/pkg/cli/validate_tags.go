package cli

import (
	"errors"
	"fmt"
	"reflect"
	"regexp"
	"strings"
)

func ValidateCommandTags(cmd Command) error {
	if cmd == nil {
		return errors.New("ValidateCommandTags: nil cmd")
	}
	typ := reflect.TypeOf(cmd)

	// Validate Slots(): collect allowed flag-group names and argument names.
	// Subcommands are optional.
	allowedGroups := map[string]struct{}{}
	allowedArgs := map[string]struct{}{}
	// (no special casing of any literal values)
	var walk func(Slot)
	walk = func(s Slot) {
		switch v := s.(type) {
		case Group:
			// accumulate unordered flag groups and arguments
			for _, u := range v.Unordered {
				switch uu := u.(type) {
				case FlagGroup:
					if name := strings.TrimSpace(uu.Name); name != "" {
						allowedGroups[name] = struct{}{}
					}
				case Argument:
					if n := strings.TrimSpace(uu.Name); n != "" {
						allowedArgs[n] = struct{}{}
					}
				case Arguments:
					if n := strings.TrimSpace(uu.Name); n != "" {
						allowedArgs[n] = struct{}{}
					}
				}
			}
			// process ordered items and recurse
			for _, o := range v.Ordered {
				switch ov := o.(type) {
				case FlagGroup:
					if name := strings.TrimSpace(ov.Name); name != "" {
						allowedGroups[name] = struct{}{}
					}
				case Argument:
					if n := strings.TrimSpace(ov.Name); n != "" {
						allowedArgs[n] = struct{}{}
					}
				case Arguments:
					if n := strings.TrimSpace(ov.Name); n != "" {
						allowedArgs[n] = struct{}{}
					}
				case Literal:
					// literals don't affect tag validation
				}
				walk(o)
			}
		}
	}
	walk(cmd.Slots())
	// no literal-specific validation

	var errs []string
	v := reflect.ValueOf(cmd)
	walkStruct(v, func(sf reflect.StructField, fv reflect.Value) {
		flag, hasFlag := sf.Tag.Lookup("cli_flag")
		altSpec, hasAlt := sf.Tag.Lookup("cli_flag_alternatives")
		argGroup, hasArg := sf.Tag.Lookup("cli_argument")
		group, hasGroup := sf.Tag.Lookup("cli_group")
		enum, hasEnum := sf.Tag.Lookup("cli_enum")

		// skip untagged fields
		if !hasFlag && !hasArg && !hasAlt {
			return
		}

		if hasAlt && !hasFlag {
			errs = append(errs, fmt.Sprintf("%s: field %q has cli_flag_alternatives but no cli_flag", typ, sf.Name))
		}

		// mutually exclusive tags
		if hasFlag && hasArg {
			errs = append(errs, fmt.Sprintf("%s: field %q cannot have both cli_flag and cli_argument", typ, sf.Name))
			return
		}

		// --- cli_flag rules (tags only) ---
		if hasFlag {
			if strings.TrimSpace(flag) == "" {
				errs = append(errs, fmt.Sprintf("%s: field %q has empty cli_flag", typ, sf.Name))
			} else if !looksLikeFlag(flag) {
				errs = append(errs, fmt.Sprintf("%s: field %q cli_flag=%q must start with '-' or '--'", typ, sf.Name, flag))
			}
			if !hasGroup || strings.TrimSpace(group) == "" {
				errs = append(errs, fmt.Sprintf("%s: field %q (flag %q) missing required cli_group", typ, sf.Name, flag))
			} else if _, ok := allowedGroups[group]; !ok {
				errs = append(errs, fmt.Sprintf(`%s: field %q (flag %q) references group %q not present in Slots()`, typ, sf.Name, flag, group))
			}
			// alternatives
			if hasAlt {
				altsRaw := strings.Split(altSpec, "|")
				var alts []string
				for _, a := range altsRaw {
					a = strings.TrimSpace(a)
					if a == "" {
						errs = append(errs, fmt.Sprintf("%s: field %q has empty cli_flag_alternative", typ, sf.Name))
						continue
					}
					if !looksLikeFlag(a) {
						errs = append(errs, fmt.Sprintf("%s: field %q cli_flag_alternative %q must start with '-' or '--'", typ, sf.Name, a))
					}
					if a == flag {
						errs = append(errs, fmt.Sprintf("%s: field %q cli_flag_alternative %q duplicates cli_flag", typ, sf.Name, a))
					}
					alts = append(alts, a)
				}
				if dup := firstDuplicate(alts); dup != "" {
					errs = append(errs, fmt.Sprintf("%s: field %q has duplicate cli_flag_alternative %q", typ, sf.Name, dup))
				}
			}

			// enum (static) rules: must be applied only to string/*string and non-empty spec
			if hasEnum {
				if strings.TrimSpace(enum) == "" || !strings.Contains(enum, "|") {
					errs = append(errs, fmt.Sprintf(`%s: field %q has invalid cli_enum %q (must be pipe-delimited like "a|b|c")`, typ, sf.Name, enum))
				}
				if !isStringish(sf.Type) {
					errs = append(errs, fmt.Sprintf(`%s: field %q has cli_enum but is not string or *string`, typ, sf.Name))
				}
			}
			return
		}

		// --- cli_argument rules (tags only) ---
		if hasArg {
			if hasGroup {
				errs = append(errs, fmt.Sprintf("%s: field %q (argument %q) must NOT set cli_group", typ, sf.Name, argGroup))
			}
			argGroup = strings.TrimSpace(argGroup)
			if argGroup == "" {
				errs = append(errs, fmt.Sprintf("%s: field %q has empty cli_argument", typ, sf.Name))
			} else if _, ok := allowedArgs[argGroup]; !ok {
				errs = append(errs, fmt.Sprintf(`%s: field %q (argument %q) not present in Slots()`, typ, sf.Name, argGroup))
			}
			// cli_enum on arguments is meaningless in this model; reject if present
			if hasEnum {
				errs = append(errs, fmt.Sprintf(`%s: field %q (argument %q) must not have cli_enum`, typ, sf.Name, argGroup))
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

// (optional) quick sanity check for enum spec format (kept private; used above)
var enumSpecRe = regexp.MustCompile(`^[^|]+(\|[^|]+)+$`)
