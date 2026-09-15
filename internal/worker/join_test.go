package worker_test

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/MoustAhmed/distributed-monero/internal/coordinator"
	"github.com/MoustAhmed/distributed-monero/internal/protocol"
	"github.com/MoustAhmed/distributed-monero/internal/worker"
)

// testCoordinator starts a real coordinator.Server on a loopback TCP
// listener so worker.Join exercises an actual TLS handshake, not just the
// in-process HTTP handler.
func testCoordinator(t *testing.T) (addr string, identity *coordinator.Identity, registry *coordinator.Registry) {
	t.Helper()

	identity, err := coordinator.NewIdentity()
	if err != nil {
		t.Fatalf("NewIdentity() error: %v", err)
	}
	registry = coordinator.NewRegistry()
	srv := coordinator.NewServer(identity, registry)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}

	go srv.Serve(ln)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
	})

	return ln.Addr().String(), identity, registry
}

func TestJoinValidEnrollment(t *testing.T) {
	addr, identity, registry := testCoordinator(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	result, err := worker.Join(ctx, worker.JoinOptions{
		Address:           addr,
		JoinToken:         identity.JoinToken,
		ServerFingerprint: identity.Fingerprint,
		InstallationID:    "machine-a",
		Capabilities:      protocol.CPUCapabilities{LogicalCores: 8, AESNI: true},
	})
	if err != nil {
		t.Fatalf("Join() error: %v", err)
	}
	if result.ClusterID != identity.ClusterID {
		t.Fatalf("ClusterID = %q, want %q", result.ClusterID, identity.ClusterID)
	}
	if result.WorkerID == "" {
		t.Fatalf("WorkerID is empty")
	}
	if result.SessionToken == "" {
		t.Fatalf("SessionToken is empty")
	}
	if result.Status != protocol.StatusIdle {
		t.Fatalf("Status = %q, want %q", result.Status, protocol.StatusIdle)
	}

	rec, ok := registry.Lookup("machine-a")
	if !ok {
		t.Fatalf("registry has no record for machine-a")
	}
	if rec.Capabilities.LogicalCores != 8 || !rec.Capabilities.AESNI {
		t.Fatalf("registry Capabilities = %+v, want LogicalCores=8 AESNI=true", rec.Capabilities)
	}
}

func TestJoinInvalidToken(t *testing.T) {
	addr, identity, _ := testCoordinator(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := worker.Join(ctx, worker.JoinOptions{
		Address:           addr,
		JoinToken:         "wrong-token",
		ServerFingerprint: identity.Fingerprint,
		InstallationID:    "machine-b",
	})
	if !errors.Is(err, worker.ErrInvalidToken) {
		t.Fatalf("Join() error = %v, want %v", err, worker.ErrInvalidToken)
	}
}

func TestJoinCertificateMismatch(t *testing.T) {
	addr, identity, _ := testCoordinator(t)

	// A fingerprint from an unrelated identity must not be accepted.
	other, err := coordinator.NewIdentity()
	if err != nil {
		t.Fatalf("NewIdentity() error: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err = worker.Join(ctx, worker.JoinOptions{
		Address:           addr,
		JoinToken:         identity.JoinToken,
		ServerFingerprint: other.Fingerprint,
		InstallationID:    "machine-c",
	})
	if !errors.Is(err, worker.ErrCertificateMismatch) {
		t.Fatalf("Join() error = %v, want %v", err, worker.ErrCertificateMismatch)
	}
}

func TestJoinMissingInstallationID(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	_, err := worker.Join(ctx, worker.JoinOptions{
		Address:           "127.0.0.1:1",
		JoinToken:         "token",
		ServerFingerprint: "aa",
	})
	if !errors.Is(err, worker.ErrInstallationIDRequired) {
		t.Fatalf("Join() error = %v, want %v", err, worker.ErrInstallationIDRequired)
	}
}

func TestJoinDuplicateEnrollmentKeepsWorkerIDAndRotatesSession(t *testing.T) {
	addr, identity, _ := testCoordinator(t)
	opts := worker.JoinOptions{
		Address:           addr,
		JoinToken:         identity.JoinToken,
		ServerFingerprint: identity.Fingerprint,
		InstallationID:    "machine-d",
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	first, err := worker.Join(ctx, opts)
	if err != nil {
		t.Fatalf("first Join() error: %v", err)
	}

	second, err := worker.Join(ctx, opts)
	if err != nil {
		t.Fatalf("second Join() error: %v", err)
	}

	if second.WorkerID != first.WorkerID {
		t.Fatalf("WorkerID changed on rejoin: %q -> %q", first.WorkerID, second.WorkerID)
	}
	if second.SessionToken == first.SessionToken {
		t.Fatalf("SessionToken did not rotate on rejoin")
	}
	if second.Status != protocol.StatusIdle {
		t.Fatalf("Status after rejoin = %q, want %q", second.Status, protocol.StatusIdle)
	}
}

func TestJoinConcurrentRegistrations(t *testing.T) {
	addr, identity, _ := testCoordinator(t)
	const n = 25

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	var wg sync.WaitGroup
	results := make([]*worker.EnrollmentResult, n)
	errs := make([]error, n)

	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			res, err := worker.Join(ctx, worker.JoinOptions{
				Address:           addr,
				JoinToken:         identity.JoinToken,
				ServerFingerprint: identity.Fingerprint,
				InstallationID:    fmt.Sprintf("machine-concurrent-%d", i),
			})
			results[i] = res
			errs[i] = err
		}(i)
	}
	wg.Wait()

	seen := make(map[string]bool, n)
	for i := 0; i < n; i++ {
		if errs[i] != nil {
			t.Fatalf("goroutine %d Join() error: %v", i, errs[i])
		}
		if results[i].Status != protocol.StatusIdle {
			t.Fatalf("goroutine %d Status = %q, want %q", i, results[i].Status, protocol.StatusIdle)
		}
		id := results[i].WorkerID
		if id == "" {
			t.Fatalf("goroutine %d WorkerID is empty", i)
		}
		if seen[id] {
			t.Fatalf("WorkerID %q assigned to more than one concurrent registration", id)
		}
		seen[id] = true
	}
}

func TestJoinInvalidFingerprintEncoding(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	_, err := worker.Join(ctx, worker.JoinOptions{
		Address:           "127.0.0.1:1",
		JoinToken:         "token",
		ServerFingerprint: "not-hex",
		InstallationID:    "machine-e",
	})
	if err == nil || !strings.Contains(err.Error(), "invalid server fingerprint") {
		t.Fatalf("Join() error = %v, want it to mention an invalid server fingerprint", err)
	}
}
