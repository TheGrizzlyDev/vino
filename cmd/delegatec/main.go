package main

import (
	"fmt"
	"os"

	"github.com/TheGrizzlyDev/vino/internal/pkg/runc"
)

type DelegatecCmd[T runc.Command] struct {
	Command T

	DelegatePath string `runc_flag:"--delegate_path" runc_group:"delegate"`
}

func (d *DelegatecCmd[T]) Subcommand() string {
	return d.Command.Subcommand()
}

func (d *DelegatecCmd[T]) Groups() []string {
	return append([]string{"delegate"}, d.Command.Groups()...)
}

type Commands struct {
	Run   *DelegatecCmd[runc.Run]
	Start *DelegatecCmd[runc.Start]
}

func main() {
	cmds := Commands{}
	if err := runc.ParseAny(&cmds, os.Args[1:]); err != nil {
		panic(err)
	}

	fmt.Println(cmds)
}
