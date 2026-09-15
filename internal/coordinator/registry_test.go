package coordinator

import (
	"fmt"
	"sync"
	"testing"

	"github.com/MoustAhmed/distributed-monero/internal/protocol"
)

func TestRegistryEnrollRequiresInstallationID(t *testing.T) {
	r := NewRegistry()
	_, _, err := r.Enroll("", protocol.CPUCapabilities{})
	if err != ErrEmptyInstallationID {
		t.Fatalf("Enroll(\"\") err = %v, want %v", err, ErrEmptyInstallationID)
	}
}

func TestRegistryEnrollAssignsStableWorkerIDAndIdleStatus(t *testing.T) {
	r := NewRegistry()
	caps := protocol.CPUCapabilities{LogicalCores: 8}

	first, rejoined, err := r.Enroll("install-a", caps)
	if err != nil {
		t.Fatalf("first Enroll error: %v", err)
	}
	if rejoined {
		t.Fatalf("first Enroll reported rejoined = true, want false")
	}
	if first.WorkerID == "" {
		t.Fatalf("first Enroll returned empty WorkerID")
	}
	if first.Status != protocol.StatusIdle {
		t.Fatalf("first Enroll Status = %q, want %q", first.Status, protocol.StatusIdle)
	}

	second, rejoined, err := r.Enroll("install-a", caps)
	if err != nil {
		t.Fatalf("second Enroll error: %v", err)
	}
	if !rejoined {
		t.Fatalf("second Enroll reported rejoined = false, want true")
	}
	if second.WorkerID != first.WorkerID {
		t.Fatalf("duplicate enrollment WorkerID = %q, want stable id %q", second.WorkerID, first.WorkerID)
	}
	if second.Status != protocol.StatusIdle {
		t.Fatalf("second Enroll Status = %q, want %q", second.Status, protocol.StatusIdle)
	}
}

func TestRegistryEnrollRotatesSessionTokenOnRejoin(t *testing.T) {
	r := NewRegistry()
	caps := protocol.CPUCapabilities{LogicalCores: 4}

	first, _, err := r.Enroll("install-b", caps)
	if err != nil {
		t.Fatalf("first Enroll error: %v", err)
	}
	if first.SessionToken == "" {
		t.Fatalf("first Enroll returned empty SessionToken")
	}

	second, _, err := r.Enroll("install-b", caps)
	if err != nil {
		t.Fatalf("second Enroll error: %v", err)
	}
	if second.SessionToken == "" {
		t.Fatalf("second Enroll returned empty SessionToken")
	}
	if second.SessionToken == first.SessionToken {
		t.Fatalf("session token did not rotate on rejoin: %q", second.SessionToken)
	}
}

func TestRegistryDistinctInstallationsGetDistinctWorkerIDs(t *testing.T) {
	r := NewRegistry()
	a, _, err := r.Enroll("install-c", protocol.CPUCapabilities{})
	if err != nil {
		t.Fatalf("Enroll(install-c) error: %v", err)
	}
	b, _, err := r.Enroll("install-d", protocol.CPUCapabilities{})
	if err != nil {
		t.Fatalf("Enroll(install-d) error: %v", err)
	}
	if a.WorkerID == b.WorkerID {
		t.Fatalf("distinct installations got the same WorkerID %q", a.WorkerID)
	}
}

func TestRegistryConcurrentRegistrationsGetUniqueWorkerIDs(t *testing.T) {
	r := NewRegistry()
	const n = 50

	var wg sync.WaitGroup
	ids := make([]string, n)
	errs := make([]error, n)

	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			rec, _, err := r.Enroll(fmt.Sprintf("install-concurrent-%d", i), protocol.CPUCapabilities{LogicalCores: i})
			ids[i] = rec.WorkerID
			errs[i] = err
		}(i)
	}
	wg.Wait()

	seen := make(map[string]bool, n)
	for i, id := range ids {
		if errs[i] != nil {
			t.Fatalf("Enroll goroutine %d error: %v", i, errs[i])
		}
		if id == "" {
			t.Fatalf("Enroll goroutine %d returned empty WorkerID", i)
		}
		if seen[id] {
			t.Fatalf("WorkerID %q assigned more than once", id)
		}
		seen[id] = true
	}
	if got := r.Len(); got != n {
		t.Fatalf("registry Len() = %d, want %d", got, n)
	}
}

func TestRegistryConcurrentRejoinsOfSameInstallationStaySingleRecord(t *testing.T) {
	r := NewRegistry()
	const n = 50

	var wg sync.WaitGroup
	tokens := make([]string, n)
	workerIDs := make([]string, n)

	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			rec, _, err := r.Enroll("install-shared", protocol.CPUCapabilities{})
			if err != nil {
				t.Errorf("Enroll goroutine %d error: %v", i, err)
				return
			}
			tokens[i] = rec.SessionToken
			workerIDs[i] = rec.WorkerID
		}(i)
	}
	wg.Wait()

	if got := r.Len(); got != 1 {
		t.Fatalf("registry Len() = %d, want 1 (single installation)", got)
	}

	wantWorkerID := workerIDs[0]
	for i, id := range workerIDs {
		if id != wantWorkerID {
			t.Fatalf("goroutine %d WorkerID = %q, want stable id %q", i, id, wantWorkerID)
		}
	}

	final, ok := r.Lookup("install-shared")
	if !ok {
		t.Fatalf("Lookup(install-shared) not found after concurrent enrollment")
	}
	if final.JoinCount != n {
		t.Fatalf("final JoinCount = %d, want %d", final.JoinCount, n)
	}
	if final.Status != protocol.StatusIdle {
		t.Fatalf("final Status = %q, want %q", final.Status, protocol.StatusIdle)
	}
}
