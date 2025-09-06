package vino

import (
	"fmt"

	"github.com/TheGrizzlyDev/vino/internal/pkg/runc"
	specs "github.com/opencontainers/runtime-spec/specs-go"
)

var (
	_ runc.ProcessRewriter = &ProcessRewriter{}
)

type ProcessRewriter struct {
	WineLauncherPath string
	WineLauncherArgs []string
}

func (p *ProcessRewriter) RewriteProcess(proc *specs.Process) error {
	if proc == nil {
		return fmt.Errorf("vinoc: nil process")
	}
	if len(proc.Args) == 0 {
		return fmt.Errorf("vinoc: empty process args")
	}

	args := append([]string{p.WineLauncherPath}, p.WineLauncherArgs...)
	proc.Args = append(args, proc.Args...)
	return nil
}
