package cli

// Command represents a command that can be parsed or rendered by the CLI package.
type Command interface {
	Slots() Slot
}

// SubcommandOf walks a command's Slots() and returns the Subcommand value.
// Returns an empty string if none found (invalid).
func SubcommandOf(cmd Command) string {
	var find func(Slot) (string, bool)
	find = func(s Slot) (string, bool) {
		switch v := s.(type) {
		case Subcommand:
			return v.Value, true
		case Group:
			for _, o := range v.Ordered {
				if name, ok := find(o); ok {
					return name, true
				}
			}
		}
		return "", false
	}
	if cmd == nil {
		return ""
	}
	if name, ok := find(cmd.Slots()); ok {
		return name
	}
	return ""
}
