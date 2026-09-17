// Package protocol defines the wire types shared by the coordinator and
// worker for the control-plane enrollment handshake. It intentionally
// covers enrollment only — scheduling, telemetry, and workload messages are
// out of scope until those subsystems exist.
package protocol

// StatusIdle is the status every worker record has immediately after
// enrollment. Enrollment never transitions a worker into any other state.
const StatusIdle = "IDLE"

// CPUCapabilities describes the CPU resources a worker makes available to
// the cluster. Fields left at their zero value mean "not detected" rather
// than "absent".
type CPUCapabilities struct {
	LogicalCores  int    `json:"logical_cores"`
	PhysicalCores int    `json:"physical_cores,omitempty"`
	ModelName     string `json:"model_name,omitempty"`
	AESNI         bool   `json:"aes_ni,omitempty"`
	LargePages    bool   `json:"large_pages,omitempty"`
}

// EnrollRequest is the body of a worker's enrollment request. The join
// token travels in the Authorization header, not in this body.
type EnrollRequest struct {
	InstallationID string          `json:"installation_id"`
	Capabilities   CPUCapabilities `json:"capabilities"`
}

// EnrollResponse is the body returned by a successful enrollment.
type EnrollResponse struct {
	ClusterID    string `json:"cluster_id"`
	WorkerID     string `json:"worker_id"`
	SessionToken string `json:"session_token"`
	Status       string `json:"status"`
}

// ErrorResponse is the body returned when enrollment fails.
type ErrorResponse struct {
	Error string `json:"error"`
}
