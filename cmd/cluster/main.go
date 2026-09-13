// Command cluster is the unified command-line interface for
// distributed-monero, a distributed CPU computing cluster for coordinated
// Monero RandomX mining.
//
// Only the help and version commands are implemented so far. All other
// commands shown in the usage text are part of the planned CLI surface and
// are not yet functional.
package main

import (
	"fmt"
	"io"
	"os"
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

  help       show this help message
  version    print the cluster CLI version

Planned commands (not yet implemented):

  coordinator init          initialize a new cluster coordinator
  coordinator run           run the cluster coordinator
  worker join <addr>        join an existing cluster as a worker
  status                    show cluster status
  start                     start a distributed workload
  stop                      stop a distributed workload
`)
}
