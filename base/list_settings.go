package base

// ListSettings are the global settings for list presentation behavior
// (U:list_settings.rs). serde becomes JSON tags; schemars is dropped.
type ListSettings struct {
	// ActiveHighlight reports whether list items use the active highlight
	// style by default.
	ActiveHighlight bool `json:"active_highlight"`
}

// DefaultListSettings returns the defaults: the active highlight is on
// (U:list_settings.rs:11-17).
func DefaultListSettings() ListSettings { return ListSettings{ActiveHighlight: true} }
