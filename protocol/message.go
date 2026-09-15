package protocol

// ClientMessage is the JSON structure sent from client (browser) to server.
type ClientMessage struct {
	Type     string `json:"type"`                // "run" | "input" | "resize" | "write" | "read_files" | "ping"
	Language string `json:"language,omitempty"`   // "python" | "javascript"
	Code     string `json:"code,omitempty"`       // source code to execute
	Stdin    string `json:"stdin,omitempty"`      // pre-supplied stdin (REST mode compat)
	Files    []File `json:"files,omitempty"`      // multi-file support
	Data     string `json:"data,omitempty"`       // stdin input (interactive mode)
	Path     string `json:"path,omitempty"`       // file path for write/read_files messages
	Cols     uint16 `json:"cols,omitempty"`       // terminal columns (resize)
	Rows     uint16 `json:"rows,omitempty"`       // terminal rows (resize)
	Lab      *LabStart `json:"lab,omitempty"`      // lab mode config (for lab_start messages)
}

// File represents a single source file.
type File struct {
	Name    string `json:"name"`
	Content string `json:"content"`
}

// ServerMessage is the JSON structure sent from server to client.
type ServerMessage struct {
	Type    string `json:"type"`                       // "output" | "exit" | "error" | "started" | "files"
	Data    string `json:"data,omitempty"`              // terminal output data
	Code    *int   `json:"code,omitempty"`              // exit code
	Reason  string `json:"reason,omitempty"`            // "normal" | "error" | "oom" | "timeout" | "crash"
	Message string `json:"message,omitempty"`           // error/status message
	Files   []File   `json:"files,omitempty"`             // files from container (for "files" message)
	Folders []string `json:"folders,omitempty"`           // directories from container (for "files" message)

	Lab      *LabStart `json:"lab,omitempty"`      // lab config (for lab_start messages)
	LabInfo  *LabInfo  `json:"labInfo,omitempty"`   // lab info (for lab_started messages)

	// REST-only fields (not sent over WebSocket)
	Stdout         string `json:"stdout,omitempty"`
	Stderr         string `json:"stderr,omitempty"`
	ExecutionTime  int64  `json:"executionTime,omitempty"`
	Status         string `json:"status,omitempty"`
}

// RESTRequest is the JSON body for POST /api/v1/run.
type RESTRequest struct {
	Language string `json:"language"`
	Code     string `json:"code,omitempty"`     // shortcut: single-file code string
	Stdin    string `json:"stdin,omitempty"`
	Files    []File `json:"files,omitempty"`
}

// RESTResponse is the JSON response for POST /api/v1/run.
type RESTResponse struct {
	Status        string `json:"status"`
	Stdout        string `json:"stdout,omitempty"`
	Stderr        string `json:"stderr,omitempty"`
	ExitCode      int    `json:"exitCode"`
	ExecutionTime int64  `json:"executionTime"`
}

// LanguageInfo describes a supported programming language.
type LanguageInfo struct {
	ID         string   `json:"id"`
	Name       string   `json:"name"`
	Version    string   `json:"version"`
	Extensions []string `json:"extensions"`
}

// LanguagesResponse is the JSON response for GET /api/v1/languages.
type LanguagesResponse struct {
	Languages []LanguageInfo `json:"languages"`
}

// LabStart is the payload for a "lab_start" client message.
// When the client sends this, the server creates an isolated Docker network,
// spins up a target container, and connects the student's code container to it.
type LabStart struct {
	// Type is the target container type: "vulnerable-web", "vulnerable-api", "tcp-server"
	Type string `json:"type"`
	// Image overrides the default image for this target type.
	Image string `json:"image,omitempty"`
	// Port is the port the target listens on (defaults to type-specific port).
	Port int `json:"port,omitempty"`
	// Timeout is the max lab duration (defaults to 5 minutes).
	Timeout string `json:"timeout,omitempty"`
	// Env is additional environment variables for the target container.
	Env []string `json:"env,omitempty"`
}

// PortInfo is sent when a listening port is detected inside a container.
type PortInfo struct {
	Port int    `json:"port"`
	URL  string `json:"url"`
}

// LabInfo is sent to the client after a lab is started.
type LabInfo struct {
	// TargetURL is the URL the student can reach the target at (e.g. "http://target:3000").
	TargetURL string `json:"targetUrl"`
	// TargetType is the type of target container running.
	TargetType string `json:"targetType"`
	// ExpiresAt is when the lab session will be automatically destroyed.
	ExpiresAt string `json:"expiresAt"`
}
