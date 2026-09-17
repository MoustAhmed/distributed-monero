// Command cluster is the unified command-line interface for
// distributed-monero, a distributed CPU computing cluster for coordinated
// Monero RandomX mining.
//
// help, version, coordinator init, coordinator run, and worker join are
// implemented. coordinator init/run and worker join cover authenticated
// worker enrollment only — heartbeats, telemetry, scheduling, persistence,
// and RandomX are not implemented. status, start, and stop shown in the
// usage text are part of the planned CLI surface and are not yet
// functional.
package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/signal"
	"runtime"
	"syscall"
	"time"

	"github.com/MoustAhmed/distributed-monero/internal/coordinator"
	"github.com/MoustAhmed/distributed-monero/internal/protocol"
	"github.com/MoustAhmed/distributed-monero/internal/worker"
)

// version is the current build version of the cluster CLI.
const version = "0.1.0-dev"

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// run executes the CLI and returns a process exit code. Output is written to
// stdout/stderr rather than directly to os.Stdout/os.Stderr so behavior can
// be tested without touching the real process streams.
func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		printUsage(stdout)
		return 0
	}

	switch args[0] {
	case "help", "-h", "--help":
		printUsage(stdout)
		return 0
	case "version", "-v", "--version":
		fmt.Fprintf(stdout, "cluster version %s\n", version)
		return 0
	case "coordinator":
		if len(args) < 2 {
			fmt.Fprint(stderr, "cluster: coordinator requires a subcommand (init, run)\n\n")
			printUsage(stderr)
			return 2
		}
		switch args[1] {
		case "init":
			return runCoordinatorInit(args[2:], stdout, stderr)
		case "run":
			return runCoordinatorRun(args[2:], stdout, stderr)
		default:
			fmt.Fprintf(stderr, "cluster: unknown coordinator subcommand %q\n\n", args[1])
			printUsage(stderr)
			return 2
		}
	case "worker":
		if len(args) < 2 || args[1] != "join" {
			fmt.Fprint(stderr, "cluster: worker requires the join subcommand\n\n")
			printUsage(stderr)
			return 2
		}
		return runWorkerJoin(args[2:], stdout, stderr)
	default:
		fmt.Fprintf(stderr, "cluster: unknown command %q\n\n", args[0])
		printUsage(stderr)
		return 2
	}
}

func printUsage(w io.Writer) {
	fmt.Fprint(w, `cluster is the unified command-line interface for distributed-monero.

Usage:

  cluster <command> [arguments]

Available commands:

  help                       show this help message
  version                    print the cluster CLI version
  coordinator init           generate a cluster ID, join token, and TLS identity
  coordinator run            run the coordinator's authenticated enrollment endpoint
  worker join <addr>         enroll this machine as a worker

Run "cluster coordinator run -h" or "cluster worker join -h" for flags.

coordinator init/run and worker join implement authenticated worker
enrollment only. Nothing is persisted to disk by this CLI; capture the
values coordinator init/run print and pass them where they're needed.

Planned commands (not yet implemented):

  status                    show cluster status
  start                     start a distributed workload
  stop                      stop a distributed workload
`)
}

func runCoordinatorInit(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("coordinator init", flag.ContinueOnError)
	fs.SetOutput(stderr)
	if err := fs.Parse(args); err != nil {
		return 2
	}

	identity, err := coordinator.NewIdentity()
	if err != nil {
		fmt.Fprintf(stderr, "cluster: failed to generate cluster identity: %v\n", err)
		return 1
	}

	printIdentity(stdout, identity)
	fmt.Fprint(stdout, "\nThese values are not persisted anywhere. Save them yourself and pass\n")
	fmt.Fprint(stdout, "them to \"cluster coordinator run\" and \"cluster worker join\".\n")
	return 0
}

func runCoordinatorRun(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("coordinator run", flag.ContinueOnError)
	fs.SetOutput(stderr)
	addr := fs.String("addr", "127.0.0.1:8443", "address to listen on")
	clusterID := fs.String("cluster-id", "", "existing cluster ID (omit with --join-token/--cert/--key to generate a new identity)")
	joinToken := fs.String("join-token", "", "existing join token")
	certFile := fs.String("cert", "", "PEM certificate file")
	keyFile := fs.String("key", "", "PEM private key file")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	identity, generated, err := loadOrGenerateIdentity(*clusterID, *joinToken, *certFile, *keyFile)
	if err != nil {
		fmt.Fprintf(stderr, "cluster: %v\n", err)
		return 2
	}
	if generated {
		printIdentity(stdout, identity)
		fmt.Fprintln(stdout)
	}

	registry := coordinator.NewRegistry()
	srv := coordinator.NewServer(identity, registry)

	ln, err := net.Listen("tcp", *addr)
	if err != nil {
		fmt.Fprintf(stderr, "cluster: failed to listen on %s: %v\n", *addr, err)
		return 1
	}
	fmt.Fprintf(stdout, "coordinator listening on %s (cluster %s)\n", ln.Addr().String(), identity.ClusterID)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	errCh := make(chan error, 1)
	go func() { errCh <- srv.Serve(ln) }()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			fmt.Fprintf(stderr, "cluster: shutdown error: %v\n", err)
			return 1
		}
		return 0
	case err := <-errCh:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			fmt.Fprintf(stderr, "cluster: server error: %v\n", err)
			return 1
		}
		return 0
	}
}

// loadOrGenerateIdentity generates a fresh cluster identity when none of
// the identity flags are set, or loads one from the given cluster ID, join
// token, and PEM cert/key files when all four are set. It reports whether
// it generated a new identity so the caller knows whether to print it.
func loadOrGenerateIdentity(clusterID, joinToken, certFile, keyFile string) (*coordinator.Identity, bool, error) {
	if clusterID == "" && joinToken == "" && certFile == "" && keyFile == "" {
		identity, err := coordinator.NewIdentity()
		return identity, true, err
	}
	if clusterID == "" || joinToken == "" || certFile == "" || keyFile == "" {
		return nil, false, errors.New("--cluster-id, --join-token, --cert, and --key must all be provided together")
	}
	cert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return nil, false, fmt.Errorf("failed to load TLS identity: %w", err)
	}
	identity, err := coordinator.NewIdentityFromCert(clusterID, joinToken, cert)
	return identity, false, err
}

func printIdentity(w io.Writer, identity *coordinator.Identity) {
	fmt.Fprintf(w, "Cluster ID:      %s\n", identity.ClusterID)
	fmt.Fprintf(w, "Join Token:      %s\n", identity.JoinToken)
	fmt.Fprintf(w, "TLS Fingerprint: %s\n", identity.Fingerprint)

	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: identity.TLSCert.Certificate[0]})
	w.Write(certPEM)

	if keyDER, err := x509.MarshalPKCS8PrivateKey(identity.TLSCert.PrivateKey); err == nil {
		keyPEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})
		w.Write(keyPEM)
	}
}

func runWorkerJoin(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("worker join", flag.ContinueOnError)
	fs.SetOutput(stderr)
	token := fs.String("token", "", "join token issued by the coordinator (required)")
	fingerprint := fs.String("fingerprint", "", "expected coordinator TLS fingerprint, hex sha256 (required)")
	installID := fs.String("install-id", "", "stable installation ID for this machine (required)")
	timeout := fs.Duration("timeout", 10*time.Second, "enrollment request timeout")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	remaining := fs.Args()
	if len(remaining) != 1 {
		fmt.Fprintln(stderr, "cluster: worker join requires exactly one <coordinator-address> argument")
		return 2
	}
	if *token == "" || *fingerprint == "" || *installID == "" {
		fmt.Fprintln(stderr, "cluster: --token, --fingerprint, and --install-id are required")
		return 2
	}

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()

	result, err := worker.Join(ctx, worker.JoinOptions{
		Address:           remaining[0],
		JoinToken:         *token,
		ServerFingerprint: *fingerprint,
		InstallationID:    *installID,
		Capabilities:      localCapabilities(),
	})
	if err != nil {
		fmt.Fprintf(stderr, "cluster: enrollment failed: %v\n", err)
		return 1
	}

	fmt.Fprintf(stdout, "Cluster ID:     %s\n", result.ClusterID)
	fmt.Fprintf(stdout, "Worker ID:      %s\n", result.WorkerID)
	fmt.Fprintf(stdout, "Session Token:  %s\n", result.SessionToken)
	fmt.Fprintf(stdout, "Status:         %s\n", result.Status)
	return 0
}

// localCapabilities reports what this package can determine about the
// local CPU without platform-specific detection, which is out of scope
// here. Fields left at zero/false mean "not detected", not "absent".
func localCapabilities() protocol.CPUCapabilities {
	return protocol.CPUCapabilities{
		LogicalCores: runtime.NumCPU(),
	}
}
