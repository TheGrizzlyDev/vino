package hook

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/TheGrizzlyDev/vino/internal/pkg/vino/labels"
)

func TestApplyDevices(t *testing.T) {
	t.Run("creates symlink", func(t *testing.T) {
		prefix := t.TempDir()
		devFile := filepath.Join(prefix, "hostdev")
		if err := os.WriteFile(devFile, []byte(""), 0o644); err != nil {
			t.Fatalf("write devfile: %v", err)
		}

		vc := &VinoContainer{WinePrefix: prefix}
		dev := labels.Device{Class: "disk", Path: devFile, Label: "SDA"}
		if err := vc.ApplyDevices([]labels.Device{dev}); err != nil {
			t.Fatalf("ApplyDevices: %v", err)
		}

		link := filepath.Join(prefix, "dosdevices", "sda")
		target, err := os.Readlink(link)
		if err != nil {
			t.Fatalf("Readlink: %v", err)
		}
		if target != devFile {
			t.Fatalf("link target %q want %q", target, devFile)
		}
	})

	t.Run("missing path error", func(t *testing.T) {
		prefix := t.TempDir()
		vc := &VinoContainer{WinePrefix: prefix}
		dev := labels.Device{Class: "disk", Label: "BAD"}
		if err := vc.ApplyDevices([]labels.Device{dev}); err == nil {
			t.Fatalf("expected error, got nil")
		}
	})
}

func TestApplyMounts(t *testing.T) {
	t.Run("creates attachment", func(t *testing.T) {
		prefix := t.TempDir()
		src := t.TempDir()
		vc := &VinoContainer{WinePrefix: prefix}
		m := labels.Mount{SourcePath: src, DestinationLabel: "Z:"}
		if err := vc.ApplyMounts([]labels.Mount{m}); err != nil {
			t.Fatalf("ApplyMounts: %v", err)
		}

		dest := filepath.Join(prefix, "dosdevices", "z:")
		info, err := os.Lstat(dest)
		if err != nil {
			t.Fatalf("stat dest: %v", err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			target, err := os.Readlink(dest)
			if err != nil {
				t.Fatalf("readlink: %v", err)
			}
			if target != src {
				t.Fatalf("link target %q want %q", target, src)
			}
		} else if !info.IsDir() {
			t.Fatalf("dest is neither symlink nor dir")
		}
	})

	t.Run("missing source error", func(t *testing.T) {
		prefix := t.TempDir()
		vc := &VinoContainer{WinePrefix: prefix}
		m := labels.Mount{DestinationLabel: "Z:"}
		if err := vc.ApplyMounts([]labels.Mount{m}); err == nil {
			t.Fatalf("expected error, got nil")
		}
	})
}
