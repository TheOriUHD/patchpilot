package ipc

import "time"

// HealthResponse is returned by the daemon health endpoint.
type HealthResponse struct {
	Status    string    `json:"status"`
	Version   string    `json:"version"`
	Commit    string    `json:"commit"`
	BuildDate string    `json:"build_date"`
	StartedAt time.Time `json:"started_at"`
}
