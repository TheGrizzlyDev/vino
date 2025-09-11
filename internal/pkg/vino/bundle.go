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
	HookPathBeforePivot     string
	HookPathAfterPivot      string
	CreateContainerHookArgs []string
	StartContainerHookArgs  []string
	RebindPaths             map[string]string
}

func (b *BundleRewriter) RewriteBundle(bundle *specs.Spec) error {
	if bundle == nil {
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
		if (st.Mode & unix.S_IFMT) == unix.S_IFBLK {
			devType = "b"
		}
		major := int64(unix.Major(uint64(st.Rdev)))
		minor := int64(unix.Minor(uint64(st.Rdev)))

		bundle.Linux.Devices = append(bundle.Linux.Devices, specs.LinuxDevice{
			Path:  d.Path,
			Type:  devType,
			Major: major,
			Minor: minor,
		})

		access := "r"
		if d.Mode != "r" {
			access = d.Mode
		}
		bundle.Linux.Resources.Devices = append(bundle.Linux.Resources.Devices, specs.LinuxDeviceCgroup{
			Allow:  true,
			Type:   devType,
			Major:  &major,
			Minor:  &minor,
			Access: access,
		})
		bundle.Mounts = append(bundle.Mounts, specs.Mount{
			Destination: d.Path,
			Type:        "bind",
			Source:      d.Path,
			Options:     []string{"rbind", access},
		})
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
		if m.Mode != "ro" {
			access = m.Mode
		}
		bundle.Mounts = append(bundle.Mounts, specs.Mount{
			Destination: src,
			Type:        "bind",
			Source:      src,
			Options:     []string{"rbind", access},
		})
	}
	if bundle.Hooks == nil {
		bundle.Hooks = &specs.Hooks{}
	}

	for rebindPathSrc, rebindPathDest := range b.RebindPaths {
		bundle.Mounts = append(bundle.Mounts, specs.Mount{
			Destination: rebindPathDest,
			Type:        "bind",
			Source:      rebindPathSrc,
			Options:     []string{"rbind", "ro", "nosuid", "nodev"},
		})
	}

	bundle.Hooks.CreateContainer = append(bundle.Hooks.CreateContainer, specs.Hook{
		Path: b.HookPathBeforePivot,
		Args: b.CreateContainerHookArgs,
	})

	// TODO: for some reason this doesn't work despite the bind to VINO_HOOK_PATH_IN_CONTAINER being present
	// bundle.Hooks.StartContainer = append(bundle.Hooks.StartContainer, specs.Hook{
	// 	Path: b.HookPathAfterPivot,
	// 	Args: b.StartContainerHookArgs,
	// })

	return nil
}
