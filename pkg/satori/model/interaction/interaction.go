package interaction

// Argv represents a command-style interaction payload.
type Argv struct {
	Name      string         `json:"name"`
	Arguments []any          `json:"arguments"`
	Options   map[string]any `json:"options"`
}

// Button represents a button interaction payload.
type Button struct {
	Id   string `json:"id"`
	Data string `json:"data,omitempty"`
}
