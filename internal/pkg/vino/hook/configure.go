package hook

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/TheGrizzlyDev/vino/internal/pkg/vino/labels"
	"golang.org/x/sys/unix"
)

type VinoContainer struct {
	WinePrefix string
}

func FromEnvironment() (*VinoContainer, error) {
	prefix := os.Getenv("WINEPREFIX")
	if prefix == "" {
		return nil, fmt.Errorf("WINEPREFIX not set")
	}

	return &VinoContainer{
		WinePrefix: prefix,
	}, nil
}

func (v *VinoContainer) getOrCreateDosDevices() (string, error) {
	dosDir := filepath.Join(v.WinePrefix, "dosdevices")
	if err := os.MkdirAll(dosDir, 0o755); err != nil {
		return "", fmt.Errorf("create dosdevices dir: %w", err)
	}
	return dosDir, nil
}

func (v *VinoContainer) ApplyDevices(devs []labels.Device) error {
	if len(devs) == 0 {
		return nil
	}

	dosDir, err := v.getOrCreateDosDevices()
	if err != nil {
		return err
	}

	for _, d := range devs {
		if d.Path == "" {
			if d.Optional {
				continue
			}
			return fmt.Errorf("device %q missing path", d.Label)
		}

		if _, err := os.Stat(d.Path); err != nil {
			if os.IsNotExist(err) && d.Optional {
				continue
			}
			return fmt.Errorf("stat %s: %w", d.Path, err)
		}

		linkName := filepath.Join(dosDir, strings.ToLower(d.Label))
		if err := os.Remove(linkName); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("remove existing link %s: %w", linkName, err)
		}
		if err := os.Symlink(d.Path, linkName); err != nil {
			return fmt.Errorf("symlink %s -> %s: %w", linkName, d.Path, err)
		}
	}

	return nil
}

func (v *VinoContainer) ApplyMounts(mounts []labels.Mount) error {
	if len(mounts) == 0 {
		return nil
	}

	dosDir, err := v.getOrCreateDosDevices()
	if err != nil {
		return err
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

		drive := strings.ToLower(m.DestinationLabel)
		dest := filepath.Join(dosDir, drive)

		if m.DestinationPath != "" {
			sub := strings.TrimLeft(m.DestinationPath, "\\/")
			sub = strings.ReplaceAll(sub, "\\", "/")
			dest = filepath.Join(dest, filepath.FromSlash(sub))
		}

		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			return fmt.Errorf("create parent dir for %s: %w", dest, err)
		}

		if err := os.Remove(dest); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("remove existing %s: %w", dest, err)
		}

		if err := bindOrSymlink(src, dest, m.Mode); err != nil {
			if m.Optional {
				continue
			}
			return fmt.Errorf("attach %s to %s: %w", src, dest, err)
		}
	}

	return nil
}

func bindOrSymlink(src, dest, mode string) error {
	fi, err := os.Stat(src)
	if err != nil {
		return err
	}

	if fi.IsDir() {
		if err := os.MkdirAll(dest, 0o755); err != nil {
			return err
		}
	} else {
		f, err := os.OpenFile(dest, os.O_CREATE, fi.Mode())
		if err != nil {
			return err
		}
		f.Close()
	}

	if err := unix.Mount(src, dest, "", unix.MS_BIND, ""); err != nil {
		if err := os.Remove(dest); err != nil && !os.IsNotExist(err) {
			return err
		}
		return os.Symlink(src, dest)
	}

	if mode == "ro" {
		if err := unix.Mount("", dest, "", unix.MS_REMOUNT|unix.MS_BIND|unix.MS_RDONLY, ""); err != nil {
			unix.Unmount(dest, 0)
			return err
		}
	}

	return nil
}
