package session

type IngressPayload struct {
	Command     string                `json:"command,omitempty"`
	Controllers map[string]Controller `json:"controllers,omitempty"`
}

type Controller struct {
	Body  map[string]string `json:"body"`
	UUIDs []string          `json:"uuids"`
}
