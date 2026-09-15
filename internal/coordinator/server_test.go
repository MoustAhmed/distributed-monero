package coordinator

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/MoustAhmed/distributed-monero/internal/protocol"
)

func newTestServer(t *testing.T) (*httptest.Server, *Identity, *Registry) {
	t.Helper()
	identity, err := NewIdentity()
	if err != nil {
		t.Fatalf("NewIdentity() error: %v", err)
	}
	registry := NewRegistry()
	srv := NewServer(identity, registry)

	ts := httptest.NewUnstartedServer(srv.Handler())
	ts.TLS = &tls.Config{Certificates: []tls.Certificate{identity.TLSCert}}
	ts.StartTLS()
	t.Cleanup(ts.Close)

	return ts, identity, registry
}

// doEnroll performs an enrollment HTTP request and reports failures via a
// returned error rather than t.Fatalf, so it's safe to call from goroutines
// spawned by a test (t.Fatalf may only be called from the goroutine running
// the test itself).
func doEnroll(ts *httptest.Server, token string, req protocol.EnrollRequest) (status int, out protocol.EnrollResponse, err error) {
	body, err := json.Marshal(req)
	if err != nil {
		return 0, out, fmt.Errorf("marshal request: %w", err)
	}
	httpReq, err := http.NewRequest(http.MethodPost, ts.URL+"/v1/enroll", bytes.NewReader(body))
	if err != nil {
		return 0, out, fmt.Errorf("build request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if token != "" {
		httpReq.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := ts.Client().Do(httpReq)
	if err != nil {
		return 0, out, fmt.Errorf("do request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
			return resp.StatusCode, out, fmt.Errorf("decode response: %w", err)
		}
	}
	return resp.StatusCode, out, nil
}

// enroll is the sequential-test convenience wrapper around doEnroll: it
// fails the test immediately on transport-level errors.
func enroll(t *testing.T, ts *httptest.Server, token string, req protocol.EnrollRequest) (int, protocol.EnrollResponse) {
	t.Helper()
	status, out, err := doEnroll(ts, token, req)
	if err != nil {
		t.Fatalf("enroll request failed: %v", err)
	}
	return status, out
}

func TestHandleEnrollValid(t *testing.T) {
	ts, identity, _ := newTestServer(t)

	status, out := enroll(t, ts, identity.JoinToken, protocol.EnrollRequest{
		InstallationID: "install-1",
		Capabilities:   protocol.CPUCapabilities{LogicalCores: 16},
	})
	if status != http.StatusOK {
		t.Fatalf("status = %d, want %d", status, http.StatusOK)
	}
	if out.ClusterID != identity.ClusterID {
		t.Fatalf("ClusterID = %q, want %q", out.ClusterID, identity.ClusterID)
	}
	if out.WorkerID == "" {
		t.Fatalf("WorkerID is empty")
	}
	if out.SessionToken == "" {
		t.Fatalf("SessionToken is empty")
	}
	if out.Status != protocol.StatusIdle {
		t.Fatalf("Status = %q, want %q", out.Status, protocol.StatusIdle)
	}
}

func TestHandleEnrollInvalidToken(t *testing.T) {
	ts, _, _ := newTestServer(t)

	status, _ := enroll(t, ts, "not-the-join-token", protocol.EnrollRequest{
		InstallationID: "install-2",
	})
	if status != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", status, http.StatusUnauthorized)
	}
}

func TestHandleEnrollMissingToken(t *testing.T) {
	ts, _, _ := newTestServer(t)

	status, _ := enroll(t, ts, "", protocol.EnrollRequest{
		InstallationID: "install-3",
	})
	if status != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", status, http.StatusUnauthorized)
	}
}

func TestHandleEnrollMissingInstallationID(t *testing.T) {
	ts, identity, _ := newTestServer(t)

	status, _ := enroll(t, ts, identity.JoinToken, protocol.EnrollRequest{})
	if status != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", status, http.StatusBadRequest)
	}
}

func TestHandleEnrollMalformedBody(t *testing.T) {
	ts, identity, _ := newTestServer(t)

	req, err := http.NewRequest(http.MethodPost, ts.URL+"/v1/enroll", strings.NewReader("{not json"))
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+identity.JoinToken)
	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusBadRequest)
	}
}

func TestHandleEnrollDuplicateEnrollmentKeepsWorkerIDRotatesToken(t *testing.T) {
	ts, identity, _ := newTestServer(t)
	req := protocol.EnrollRequest{InstallationID: "install-dup", Capabilities: protocol.CPUCapabilities{LogicalCores: 4}}

	status1, out1 := enroll(t, ts, identity.JoinToken, req)
	if status1 != http.StatusOK {
		t.Fatalf("first enroll status = %d, want 200", status1)
	}

	status2, out2 := enroll(t, ts, identity.JoinToken, req)
	if status2 != http.StatusOK {
		t.Fatalf("second enroll status = %d, want 200", status2)
	}

	if out2.WorkerID != out1.WorkerID {
		t.Fatalf("WorkerID changed across duplicate enrollment: %q -> %q", out1.WorkerID, out2.WorkerID)
	}
	if out2.SessionToken == out1.SessionToken {
		t.Fatalf("SessionToken did not rotate across duplicate enrollment")
	}
	if out2.Status != protocol.StatusIdle {
		t.Fatalf("Status after duplicate enrollment = %q, want %q", out2.Status, protocol.StatusIdle)
	}
}

func TestHandleEnrollConcurrentRequests(t *testing.T) {
	ts, identity, _ := newTestServer(t)
	const n = 30

	var wg sync.WaitGroup
	workerIDs := make([]string, n)
	statuses := make([]int, n)
	errs := make([]error, n)

	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			status, out, err := doEnroll(ts, identity.JoinToken, protocol.EnrollRequest{
				InstallationID: fmt.Sprintf("install-http-%d", i),
			})
			statuses[i] = status
			workerIDs[i] = out.WorkerID
			errs[i] = err
		}(i)
	}
	wg.Wait()

	seen := make(map[string]bool, n)
	for i := 0; i < n; i++ {
		if errs[i] != nil {
			t.Fatalf("goroutine %d request error: %v", i, errs[i])
		}
		if statuses[i] != http.StatusOK {
			t.Fatalf("goroutine %d status = %d, want 200", i, statuses[i])
		}
		if seen[workerIDs[i]] {
			t.Fatalf("WorkerID %q returned more than once", workerIDs[i])
		}
		seen[workerIDs[i]] = true
	}
}
