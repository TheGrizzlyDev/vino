package labels

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/santhosh-tekuri/jsonschema/v5"
)

//go:embed labels.schema.json
var schemaData []byte

var compiled *jsonschema.Schema

func init() {
	var err error
	compiled, err = jsonschema.CompileString("labels.schema.json", string(schemaData))
	if err != nil {
		panic(fmt.Errorf("compile labels schema: %w", err))
	}
}

// Validate checks annotations against the labels schema.
func Validate(annotations map[string]string) error {
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

	if err := compiled.Validate(data); err != nil {
		return fmt.Errorf("validate annotations: %w", err)
	}
	return nil
}
