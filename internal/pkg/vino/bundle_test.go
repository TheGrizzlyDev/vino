package vino

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	specs "github.com/opencontainers/runtime-spec/specs-go"
	"golang.org/x/sys/unix"
)

func contains(opts []string, target string) bool {
	for _, o := range opts {
		if o == target {
			return true
		}
	}
	return false
}

func TestBundleRewriterAddsDevicesAndMounts(t *testing.T) {
	hook := filepath.Join(t.TempDir(), "hook")
	if err := os.WriteFile(hook, []byte{}, 0o755); err != nil {
		t.Fatalf("create hook: %v", err)
	}

	annotations := map[string]string{
		"dev.vinoc.devices.dev0": `{"class":"com","path":"/dev/null","label":"COM1","mode":"rw"}`,
		"dev.vinoc.mounts.data":  `{"source_path":"/etc/hosts","destination_label":"data","mode":"ro"}`,
	}

	spec := &specs.Spec{Annotations: annotations}
	br := &BundleRewriter{HookPathBeforePivot: hook}
	if err := br.RewriteBundle(spec); err != nil {
		t.Fatalf("rewrite bundle: %v", err)
	}

	var st unix.Stat_t
	if err := unix.Stat("/dev/null", &st); err != nil {
		t.Fatalf("stat /dev/null: %v", err)
	}
	major := int64(unix.Major(uint64(st.Rdev)))
	minor := int64(unix.Minor(uint64(st.Rdev)))

	foundDev := false
	for _, d := range spec.Linux.Devices {
		if d.Path == "/dev/null" && d.Type == "c" && d.Major == major && d.Minor == minor {
			foundDev = true
		}
	}
	if !foundDev {
		t.Fatalf("device not added to spec")
	}

	foundCg := false
	for _, cg := range spec.Linux.Resources.Devices {
		if cg.Type == "c" && cg.Major != nil && cg.Minor != nil && *cg.Major == major && *cg.Minor == minor && cg.Access == "rw" {
			foundCg = true
		}
	}
	if !foundCg {
		t.Fatalf("device cgroup not added")
	}

	foundDevMount := false
	foundMount := false
	for _, m := range spec.Mounts {
		if m.Destination == "/dev/null" && m.Source == "/dev/null" {
			foundDevMount = true
		}
		if m.Destination == "/etc/hosts" && m.Source == "/etc/hosts" && contains(m.Options, "ro") {
			foundMount = true
		}
	}
	if !foundDevMount {
		t.Fatalf("device not bind-mounted")
	}
	if !foundMount {
		t.Fatalf("mount not added")
	}

	if err := br.RewriteBundle(spec); err != nil {
		t.Fatalf("second rewrite: %v", err)
	}

	countDev := 0
	fmt.Println(spec.Linux.Devices)
	for _, d := range spec.Linux.Devices {
		if d.Path == "/dev/null" {
			countDev++
		}
	}
	if countDev != 1 {
		t.Fatalf("device duplicated: %d", countDev)
	}

	countMount := 0
	for _, m := range spec.Mounts {
		if m.Destination == "/etc/hosts" && m.Source == "/etc/hosts" {
			countMount++
		}
	}
	if countMount != 1 {
		t.Fatalf("mount duplicated: %d", countMount)
	}
}
