package labels

// Device describes a host device exposed to the guest.
type Device struct {
	Class    string `json:"class"`
	Path     string `json:"path"`
	Label    string `json:"label"`
	Mode     string `json:"mode,omitempty"`
	Optional bool   `json:"optional,omitempty"`
	Backend  string `json:"backend,omitempty"`
}

// Mount describes a host mount exposed to the guest.
type Mount struct {
	SourcePath       string `json:"source_path,omitempty"`
	Volume           string `json:"volume,omitempty"`
	DestinationLabel string `json:"destination_label"`
	DestinationPath  string `json:"destination_path,omitempty"`
	Mode             string `json:"mode,omitempty"`
	Optional         bool   `json:"optional,omitempty"`
}
