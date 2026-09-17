// Package worker implements authenticated cluster enrollment for a worker.
// It covers joining a cluster only: no heartbeats, telemetry, scheduling,
// or workload execution live here.
package worker

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/MoustAhmed/distributed-monero/internal/protocol"
)

// Errors returned by Join. Use errors.Is to check for these.
var (
	// ErrCertificateMismatch means the coordinator's TLS certificate did
	// not match the expected fingerprint.
	ErrCertificateMismatch = errors.New("worker: coordinator TLS fingerprint does not match expected fingerprint")
	// ErrInvalidToken means the coordinator rejected the join token.
	ErrInvalidToken = errors.New("worker: coordinator rejected join token")
	// ErrInstallationIDRequired means no installation ID was supplied.
	ErrInstallationIDRequired = errors.New("worker: installation id is required")
)

const defaultTimeout = 10 * time.Second

// JoinOptions configures a call to Join.
type JoinOptions struct {
	// Address is the coordinator's host:port.
	Address string
	// JoinToken authorizes enrollment.
	JoinToken string
	// ServerFingerprint is the expected hex-encoded SHA-256 digest of the
	// coordinator's leaf TLS certificate. Join pins its TLS trust to this
	// value instead of validating a certificate chain.
	ServerFingerprint string
	// InstallationID identifies this machine. Reusing the same
	// InstallationID across joins keeps the same worker ID and rotates the
	// session token; a new InstallationID enrolls as a new worker.
	InstallationID string
	// Capabilities describes this machine's CPU resources.
	Capabilities protocol.CPUCapabilities
	// Timeout bounds the enrollment request. Defaults to 10s.
	Timeout time.Duration
}

// EnrollmentResult is what a worker learns from a successful enrollment.
type EnrollmentResult struct {
	ClusterID    string
	WorkerID     string
	SessionToken string
	Status       string
}

// Join enrolls this worker with the coordinator at opts.Address. It
// verifies the coordinator's TLS fingerprint before authenticating with
// the join token, and reports opts.InstallationID and opts.Capabilities.
// It never causes any workload to start.
func Join(ctx context.Context, opts JoinOptions) (*EnrollmentResult, error) {
	if opts.InstallationID == "" {
		return nil, ErrInstallationIDRequired
	}

	expectedFingerprint, err := hex.DecodeString(opts.ServerFingerprint)
	if err != nil {
		return nil, fmt.Errorf("worker: invalid server fingerprint: %w", err)
	}

	var certErr error
	tlsConfig := &tls.Config{
		// The chain isn't validated against a CA; the fingerprint check in
		// VerifyPeerCertificate below is the actual trust decision.
		InsecureSkipVerify: true,
		VerifyPeerCertificate: func(rawCerts [][]byte, _ [][]*x509.Certificate) error {
			if len(rawCerts) == 0 {
				certErr = ErrCertificateMismatch
				return certErr
			}
			sum := sha256.Sum256(rawCerts[0])
			if subtle.ConstantTimeCompare(sum[:], expectedFingerprint) != 1 {
				certErr = ErrCertificateMismatch
				return certErr
			}
			return nil
		},
	}

	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	client := &http.Client{
		Transport: &http.Transport{TLSClientConfig: tlsConfig},
		Timeout:   timeout,
	}

	body, err := json.Marshal(protocol.EnrollRequest{
		InstallationID: opts.InstallationID,
		Capabilities:   opts.Capabilities,
	})
	if err != nil {
		return nil, fmt.Errorf("worker: encode enrollment request: %w", err)
	}

	url := "https://" + opts.Address + "/v1/enroll"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("worker: build enrollment request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+opts.JoinToken)

	resp, err := client.Do(req)
	if err != nil {
		if certErr != nil {
			return nil, certErr
		}
		return nil, fmt.Errorf("worker: enrollment request failed: %w", err)
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK:
		var out protocol.EnrollResponse
		if err := json.NewDecoder(io.LimitReader(resp.Body, 64*1024)).Decode(&out); err != nil {
			return nil, fmt.Errorf("worker: decode enrollment response: %w", err)
		}
		return &EnrollmentResult{
			ClusterID:    out.ClusterID,
			WorkerID:     out.WorkerID,
			SessionToken: out.SessionToken,
			Status:       out.Status,
		}, nil
	case http.StatusUnauthorized:
		return nil, ErrInvalidToken
	default:
		return nil, fmt.Errorf("worker: coordinator returned status %d", resp.StatusCode)
	}
}
