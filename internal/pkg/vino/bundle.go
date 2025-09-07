package vino

import (
	"fmt"
	"os"

	"github.com/TheGrizzlyDev/vino/internal/pkg/runc"
	"github.com/TheGrizzlyDev/vino/internal/pkg/vino/labels"
	specs "github.com/opencontainers/runtime-spec/specs-go"
	"golang.org/x/sys/unix"
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
	devices, mounts, err := labels.Parse(bundle.Annotations)
	if err != nil {
		return fmt.Errorf("parse annotations: %w", err)
	}
	if bundle.Linux == nil {
		bundle.Linux = &specs.Linux{}
	}
	if bundle.Linux.Resources == nil {
		bundle.Linux.Resources = &specs.LinuxResources{}
	}

	for _, d := range devices {
		var st unix.Stat_t
		if err := unix.Stat(d.Path, &st); err != nil {
			if os.IsNotExist(err) && d.Optional {
				continue
			}
			return fmt.Errorf("stat %s: %w", d.Path, err)
		}

		devType := "c"
		if st.Mode&unix.S_IFMT == unix.S_IFBLK {
			devType = "b"
		}
		major := int64(unix.Major(uint64(st.Rdev)))
		minor := int64(unix.Minor(uint64(st.Rdev)))

		exists := false
		for _, existing := range bundle.Linux.Devices {
			if existing.Path == d.Path {
				exists = true
				break
			}
		}
		if !exists {
			bundle.Linux.Devices = append(bundle.Linux.Devices, specs.LinuxDevice{
				Path:  d.Path,
				Type:  devType,
				Major: major,
				Minor: minor,
			})
		}

		cgExists := false
		for _, cg := range bundle.Linux.Resources.Devices {
			if cg.Type == devType && cg.Major != nil && cg.Minor != nil && *cg.Major == major && *cg.Minor == minor {
				cgExists = true
				break
			}
		}
		if !cgExists {
			access := "r"
			if d.Mode == "rw" {
				access = "rw"
			}
			bundle.Linux.Resources.Devices = append(bundle.Linux.Resources.Devices, specs.LinuxDeviceCgroup{
				Allow:  true,
				Type:   devType,
				Major:  &major,
				Minor:  &minor,
				Access: access,
			})
		}

		mAccess := "ro"
		if d.Mode == "rw" {
			mAccess = "rw"
		}
		mountExists := false
		for _, m := range bundle.Mounts {
			if m.Destination == d.Path && m.Source == d.Path {
				mountExists = true
				break
			}
		}
		if !mountExists {
			bundle.Mounts = append(bundle.Mounts, specs.Mount{
				Destination: d.Path,
				Type:        "bind",
				Source:      d.Path,
				Options:     []string{"rbind", mAccess},
			})
		}
	}

	for _, m := range mounts {
		src := m.SourcePath
		if src == "" {
			src = m.Volume
		}
		if src == "" {
			if m.Optional {
				continue
			}
			return fmt.Errorf("mount %q missing source path and volume", m.DestinationLabel)
		}
		if _, err := os.Stat(src); err != nil {
			if os.IsNotExist(err) && m.Optional {
				continue
			}
			return fmt.Errorf("stat %s: %w", src, err)
		}
		access := "ro"
		if m.Mode == "rw" {
			access = "rw"
		}
		exists := false
		for _, existing := range bundle.Mounts {
			if existing.Destination == src && existing.Source == src {
				exists = true
				break
			}
		}
		if !exists {
			bundle.Mounts = append(bundle.Mounts, specs.Mount{
				Destination: src,
				Type:        "bind",
				Source:      src,
				Options:     []string{"rbind", access},
			})
		}
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
