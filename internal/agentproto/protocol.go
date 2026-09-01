package agentproto

type Message struct {
	Type    string `json:"type"`
	ID      string `json:"id,omitempty"`
	Path    string `json:"path,omitempty"`
	Offset  int64  `json:"offset,omitempty"`
	Length  int    `json:"length,omitempty"`
	Data    []byte `json:"data,omitempty"`
	Size    int64  `json:"size,omitempty"`
	EOF     bool   `json:"eof,omitempty"`
	Error   string `json:"error,omitempty"`
	Version string `json:"version,omitempty"`
}
