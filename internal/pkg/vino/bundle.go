package vino

import (
	"github.com/TheGrizzlyDev/vino/internal/pkg/runc"
	specs "github.com/opencontainers/runtime-spec/specs-go"
)

var (
	_ runc.BundleRewriter = &BundleRewriter{}
)

type BundleRewriter struct{}

func (b *BundleRewriter) RewriteBundle(*specs.Spec) error {
	return nil
}
