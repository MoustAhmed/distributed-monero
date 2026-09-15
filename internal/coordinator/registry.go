package coordinator

import (
	"errors"
	"sync"
	"time"

	"github.com/MoustAhmed/distributed-monero/internal/protocol"
)

// ErrEmptyInstallationID is returned when a worker attempts to enroll
// without an installation ID.
var ErrEmptyInstallationID = errors.New("coordinator: installation id must not be empty")

// WorkerRecord is a worker's entry in the coordinator's registry.
type WorkerRecord struct {
	WorkerID       string
	InstallationID string
	SessionToken   string
	Capabilities   protocol.CPUCapabilities
	Status         string

	EnrolledAt time.Time
	LastJoinAt time.Time
	JoinCount  int
}

// Registry tracks enrolled workers, keyed by installation ID so that a
// machine that rejoins keeps the same worker ID. Registry holds nothing
// beyond enrollment state: no heartbeats, telemetry, scheduling, or
// persistence.
type Registry struct {
	mu        sync.Mutex
	byInstall map[string]*WorkerRecord
}

// NewRegistry returns an empty, ready-to-use Registry.
func NewRegistry() *Registry {
	return &Registry{byInstall: make(map[string]*WorkerRecord)}
}

// Enroll records a worker's enrollment or re-enrollment.
//
// The first enrollment for a given installation ID assigns a new, stable
// worker ID. Every enrollment — first or repeat — issues a fresh session
// token, so rejoining rotates the session token. The returned record always
// has Status protocol.StatusIdle: enrollment never starts a workload.
//
// Enroll is safe for concurrent use.
func (r *Registry) Enroll(installationID string, caps protocol.CPUCapabilities) (rec WorkerRecord, rejoined bool, err error) {
	if installationID == "" {
		return WorkerRecord{}, false, ErrEmptyInstallationID
	}

	sessionToken, err := newToken("session", 32)
	if err != nil {
		return WorkerRecord{}, false, err
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	now := time.Now().UTC()

	if existing, ok := r.byInstall[installationID]; ok {
		existing.SessionToken = sessionToken
		existing.Capabilities = caps
		existing.Status = protocol.StatusIdle
		existing.LastJoinAt = now
		existing.JoinCount++
		return *existing, true, nil
	}

	workerID, err := newToken("worker", 8)
	if err != nil {
		return WorkerRecord{}, false, err
	}

	record := &WorkerRecord{
		WorkerID:       workerID,
		InstallationID: installationID,
		SessionToken:   sessionToken,
		Capabilities:   caps,
		Status:         protocol.StatusIdle,
		EnrolledAt:     now,
		LastJoinAt:     now,
		JoinCount:      1,
	}
	r.byInstall[installationID] = record
	return *record, false, nil
}

// Lookup returns the current record for an installation ID, if any.
func (r *Registry) Lookup(installationID string) (WorkerRecord, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	rec, ok := r.byInstall[installationID]
	if !ok {
		return WorkerRecord{}, false
	}
	return *rec, true
}

// Len returns the number of distinct enrolled workers.
func (r *Registry) Len() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.byInstall)
}
