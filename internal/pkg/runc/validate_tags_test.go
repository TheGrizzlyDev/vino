package runc

import (
	"testing"
)

func TestValidateCommandTags(t *testing.T) {
	t.Parallel()

	commands := []Command{
		Checkpoint{},
		Restore{},
		Create{},
		Run{},
		Start{},
		Delete{},
		Pause{},
		Resume{},
		Kill{},
		List{},
		Ps{},
		State{},
		Events{},
		Exec{},
		Spec{},
		Update{},
	}

    for _, c := range commands {
        c := c
        t.Run(subcommandOf(c), func(t *testing.T) {
            t.Parallel()
            if err := validateCommandTags(c); err != nil {
                t.Fatalf("%T: %v", c, err)
            }
        })
    }
}
