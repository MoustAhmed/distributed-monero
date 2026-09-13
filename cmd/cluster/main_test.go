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
