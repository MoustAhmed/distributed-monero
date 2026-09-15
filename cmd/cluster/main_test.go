package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestRunVersion(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"version"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("run(version) exit code = %d, want 0", code)
	}
	if !strings.Contains(stdout.String(), version) {
		t.Fatalf("run(version) stdout = %q, want it to contain %q", stdout.String(), version)
	}
}

func TestRunHelp(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"help"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("run(help) exit code = %d, want 0", code)
	}
	if !strings.Contains(stdout.String(), "Usage:") {
		t.Fatalf("run(help) stdout missing usage text: %q", stdout.String())
	}
}

func TestRunNoArgs(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run(nil, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("run(nil) exit code = %d, want 0", code)
	}
	if !strings.Contains(stdout.String(), "Usage:") {
		t.Fatalf("run(nil) stdout missing usage text: %q", stdout.String())
	}
}

func TestRunUnknownCommand(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"mine"}, &stdout, &stderr)
	if code == 0 {
		t.Fatalf("run(mine) exit code = 0, want non-zero for an unknown command")
	}
	if !strings.Contains(stderr.String(), "unknown command") {
		t.Fatalf("run(mine) stderr = %q, want it to mention the unknown command", stderr.String())
	}
}

func TestRunCoordinatorInit(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"coordinator", "init"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("run(coordinator init) exit code = %d, want 0; stderr: %s", code, stderr.String())
	}
	out := stdout.String()
	for _, want := range []string{"Cluster ID:", "Join Token:", "TLS Fingerprint:", "BEGIN CERTIFICATE", "BEGIN PRIVATE KEY"} {
		if !strings.Contains(out, want) {
			t.Fatalf("run(coordinator init) stdout missing %q: %q", want, out)
		}
	}
}

func TestRunCoordinatorRunRejectsPartialIdentityFlags(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"coordinator", "run", "--cluster-id", "cl_x"}, &stdout, &stderr)
	if code == 0 {
		t.Fatalf("run(coordinator run --cluster-id only) exit code = 0, want non-zero")
	}
	if !strings.Contains(stderr.String(), "must all be provided together") {
		t.Fatalf("stderr = %q, want it to mention the missing identity flags", stderr.String())
	}
}

func TestRunWorkerJoinRequiresAddress(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"worker", "join", "--token", "t", "--fingerprint", "aa", "--install-id", "m1"}, &stdout, &stderr)
	if code == 0 {
		t.Fatalf("run(worker join) with no address exit code = 0, want non-zero")
	}
	if !strings.Contains(stderr.String(), "coordinator-address") {
		t.Fatalf("stderr = %q, want it to mention the missing address", stderr.String())
	}
}

func TestRunWorkerJoinRequiresFlags(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"worker", "join", "127.0.0.1:8443"}, &stdout, &stderr)
	if code == 0 {
		t.Fatalf("run(worker join) with no flags exit code = 0, want non-zero")
	}
	if !strings.Contains(stderr.String(), "--token") {
		t.Fatalf("stderr = %q, want it to mention the required flags", stderr.String())
	}
}
