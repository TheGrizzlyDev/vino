package hook

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/TheGrizzlyDev/vino/internal/pkg/vino/labels"
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

func (v *VinoContainer) ApplyDevices(devs []labels.Device) error {
	if len(devs) == 0 {
		return nil
	}

	dosDir := filepath.Join(v.WinePrefix, "dosdevices")
	if err := os.MkdirAll(dosDir, 0o755); err != nil {
		return fmt.Errorf("create dosdevices dir: %w", err)
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
