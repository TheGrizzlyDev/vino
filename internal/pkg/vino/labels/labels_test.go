package labels

import (
	"reflect"
	"testing"
)

func TestParse(t *testing.T) {
	tests := []struct {
		name        string
		annotations map[string]string
		wantDevs    []Device
		wantMounts  []Mount
		wantErr     bool
	}{
		{
			name: "valid",
			annotations: map[string]string{
				"dev.vinoc.devices.gpu0.class":            "gpu",
				"dev.vinoc.devices.gpu0.path":             "/dev/dri/renderD128",
				"dev.vinoc.devices.gpu0.label":            "GPU0",
				"dev.vinoc.mounts.data.source_path":       "/data",
				"dev.vinoc.mounts.data.destination_label": "D:",
			},
			wantDevs:   []Device{{Class: "gpu", Path: "/dev/dri/renderD128", Label: "GPU0"}},
			wantMounts: []Mount{{SourcePath: "/data", DestinationLabel: "D:"}},
		},
		{
			name: "invalid device class",
			annotations: map[string]string{
				"dev.vinoc.devices.bad.class":             "bad",
				"dev.vinoc.devices.bad.path":              "/dev/null",
				"dev.vinoc.devices.bad.label":             "BAD",
				"dev.vinoc.mounts.data.source_path":       "/data",
				"dev.vinoc.mounts.data.destination_label": "D:",
			},
			wantErr: true,
		},
		{
			name: "invalid mount missing source",
			annotations: map[string]string{
				"dev.vinoc.devices.gpu0.class":            "gpu",
				"dev.vinoc.devices.gpu0.path":             "/dev/dri/renderD128",
				"dev.vinoc.devices.gpu0.label":            "GPU0",
				"dev.vinoc.mounts.data.destination_label": "D:",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			devs, mounts, err := Parse(tt.annotations)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !reflect.DeepEqual(devs, tt.wantDevs) {
				t.Fatalf("devices = %#v, want %#v", devs, tt.wantDevs)
			}
			if !reflect.DeepEqual(mounts, tt.wantMounts) {
				t.Fatalf("mounts = %#v, want %#v", mounts, tt.wantMounts)
			}
		})
	}
}
