package vino

import (
	"github.com/TheGrizzlyDev/vino/internal/pkg/runc"
	specs "github.com/opencontainers/runtime-spec/specs-go"
)

var (
	_ runc.ProcessRewriter = &ProcessRewriter{}
)

type ProcessRewriter struct{}

func (p *ProcessRewriter) RewriteProcess(*specs.Process) error {
	return nil
}
