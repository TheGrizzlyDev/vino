package runc

import (
	cli "github.com/TheGrizzlyDev/vino/internal/pkg/cli"
	"testing"
)

func TestValidateCommandTags(t *testing.T) {
	t.Parallel()

	commands := []cli.Command{
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
		t.Run(cli.SubcommandOf(c), func(t *testing.T) {
			t.Parallel()
			if err := cli.ValidateCommandTags(c); err != nil {
				t.Fatalf("%T: %v", c, err)
			}
		})
	}
}
