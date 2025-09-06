package vino

import (
	"github.com/TheGrizzlyDev/vino/internal/pkg/runc"
	specs "github.com/opencontainers/runtime-spec/specs-go"
)

var (
	_ runc.BundleRewriter = &BundleRewriter{}
)

const (
	VINO_HOOK_PATH_IN_CONTAINER = "/run/vino-hook"
)

type BundleRewriter struct {
	HookPath                string
	CreateContainerHookArgs []string
	StartContainerHookArgs  []string
	RebindPaths             map[string]string
}

func (b *BundleRewriter) RewriteBundle(bundle *specs.Spec) error {
	if bundle == nil || b.HookPath == "" {
		return nil
	}
	if bundle.Hooks == nil {
		bundle.Hooks = &specs.Hooks{}
	}

	bundle.Mounts = append(bundle.Mounts, specs.Mount{
		Destination: VINO_HOOK_PATH_IN_CONTAINER,
		Type:        "bind",
		Source:      b.HookPath,
		Options:     []string{"rbind", "ro", "nosuid", "nodev"},
	})

	for rebindPathSrc, rebindPathDest := range b.RebindPaths {
		bundle.Mounts = append(bundle.Mounts, specs.Mount{
			Destination: rebindPathDest,
			Type:        "bind",
			Source:      rebindPathSrc,
			Options:     []string{"rbind", "ro", "nosuid", "nodev"},
		})
	}

	bundle.Hooks.CreateContainer = append(bundle.Hooks.CreateContainer, b.hookFor(false, b.CreateContainerHookArgs))

	// TODO: for some reason this doesn't work despite the bind to VINO_HOOK_PATH_IN_CONTAINER being present
	// bundle.Hooks.StartContainer = append(bundle.Hooks.StartContainer, b.hookFor(true, b.StartContainerHookArgs))
	return nil
}

func (b BundleRewriter) hookFor(pivot bool, hookArgs []string) specs.Hook {
	args := make([]string, 0, 1+len(hookArgs))
	if pivot {
		args = append(args, VINO_HOOK_PATH_IN_CONTAINER)
	} else {
		args = append(args, b.HookPath)
	}
	args = append(args, hookArgs...)
	return specs.Hook{
		Path: b.HookPath,
		Args: args,
	}
}
