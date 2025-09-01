package vino

import (
	"github.com/TheGrizzlyDev/vino/internal/pkg/runc"
	specs "github.com/opencontainers/runtime-spec/specs-go"
)

var (
	_ runc.BundleRewriter = &BundleRewriter{}
)

type BundleRewriter struct {
	HookPath string
	HookArgs []string
}

func (b *BundleRewriter) RewriteBundle(bundle *specs.Spec) error {
	if bundle == nil || b.HookPath == "" {
		return nil
	}
	if bundle.Hooks == nil {
		bundle.Hooks = &specs.Hooks{}
	}
	args := make([]string, 0, 1+len(b.HookArgs))
	args = append(args, b.HookPath)
	args = append(args, b.HookArgs...)
	h := specs.Hook{
		Path: b.HookPath,
		Args: args,
	}
	bundle.Hooks.CreateContainer = append(bundle.Hooks.CreateContainer, h)
	return nil
}
