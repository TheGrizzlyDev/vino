package runc

import "fmt"

// Parse reads argv and returns the corresponding Command.
// The first argument must be the runc subcommand.
func Parse(argv []string) (Command, error) {
	if len(argv) == 0 {
		return nil, fmt.Errorf("Parse: missing subcommand")
	}
	sub := argv[0]
	args := argv[1:]
	switch sub {
	case "checkpoint":
		var cmd Checkpoint
		if err := parseFlags(&cmd, args); err != nil {
			return nil, err
		}
		return cmd, nil
	case "restore":
		var cmd Restore
		if err := parseFlags(&cmd, args); err != nil {
			return nil, err
		}
		return cmd, nil
	case "create":
		var cmd Create
		if err := parseFlags(&cmd, args); err != nil {
			return nil, err
		}
		return cmd, nil
	case "run":
		var cmd Run
		if err := parseFlags(&cmd, args); err != nil {
			return nil, err
		}
		return cmd, nil
	case "start":
		var cmd Start
		if err := parseFlags(&cmd, args); err != nil {
			return nil, err
		}
		return cmd, nil
	case "delete":
		var cmd Delete
		if err := parseFlags(&cmd, args); err != nil {
			return nil, err
		}
		return cmd, nil
	case "pause":
		var cmd Pause
		if err := parseFlags(&cmd, args); err != nil {
			return nil, err
		}
		return cmd, nil
	case "resume":
		var cmd Resume
		if err := parseFlags(&cmd, args); err != nil {
			return nil, err
		}
		return cmd, nil
	case "kill":
		var cmd Kill
		if err := parseFlags(&cmd, args); err != nil {
			return nil, err
		}
		return cmd, nil
	case "list":
		var cmd List
		if err := parseFlags(&cmd, args); err != nil {
			return nil, err
		}
		return cmd, nil
	case "ps":
		var cmd Ps
		if err := parseFlags(&cmd, args); err != nil {
			return nil, err
		}
		return cmd, nil
	case "state":
		var cmd State
		if err := parseFlags(&cmd, args); err != nil {
			return nil, err
		}
		return cmd, nil
	case "events":
		var cmd Events
		if err := parseFlags(&cmd, args); err != nil {
			return nil, err
		}
		return cmd, nil
	case "exec":
		var cmd Exec
		if err := parseFlags(&cmd, args); err != nil {
			return nil, err
		}
		return cmd, nil
	case "spec":
		var cmd Spec
		if err := parseFlags(&cmd, args); err != nil {
			return nil, err
		}
		return cmd, nil
	case "update":
		var cmd Update
		if err := parseFlags(&cmd, args); err != nil {
			return nil, err
		}
		return cmd, nil
	default:
		return nil, fmt.Errorf("Parse: unknown subcommand %q", sub)
	}
}
