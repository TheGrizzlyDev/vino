package labels

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Parse validates and parses annotations into Device and Mount slices.
func Parse(annotations map[string]string) ([]Device, []Mount, error) {
	if err := Validate(annotations); err != nil {
		return nil, nil, err
	}

	data := map[string]interface{}{}
	for k, v := range annotations {
		parts := strings.Split(k, ".")
		m := data
		for i, p := range parts {
			if i == len(parts)-1 {
				var val interface{}
				if err := json.Unmarshal([]byte(v), &val); err != nil {
					val = v
				}
				m[p] = val
				break
			}
			next, ok := m[p].(map[string]interface{})
			if !ok {
				next = map[string]interface{}{}
				m[p] = next
			}
			m = next
		}
	}

	var root struct {
		Dev struct {
			Vinoc struct {
				Devices map[string]Device `json:"devices"`
				Mounts  map[string]Mount  `json:"mounts"`
			} `json:"vinoc"`
		} `json:"dev"`
	}

	b, err := json.Marshal(data)
	if err != nil {
		return nil, nil, fmt.Errorf("marshal annotations: %w", err)
	}
	if err := json.Unmarshal(b, &root); err != nil {
		return nil, nil, fmt.Errorf("unmarshal annotations: %w", err)
	}

	devices := make([]Device, 0, len(root.Dev.Vinoc.Devices))
	for _, d := range root.Dev.Vinoc.Devices {
		devices = append(devices, d)
	}
	mounts := make([]Mount, 0, len(root.Dev.Vinoc.Mounts))
	for _, m := range root.Dev.Vinoc.Mounts {
		mounts = append(mounts, m)
	}

	return devices, mounts, nil
}
