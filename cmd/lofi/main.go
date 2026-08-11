// Command lofi is the entry point for the lo-fi-player terminal music
// player. PR #1 ships only the dispatcher: it routes the documented
// subcommands (play, list, credits, sync) and refuses anything else
// with a usage banner and exit code 2. The concrete behaviour of each
// subcommand lands in subsequent chained PRs.
package main

import (
	"fmt"
	"os"
)

const usage = `lofi — terminal lo-fi player

Usage:
  lofi play [track|procedural:station]
  lofi list
  lofi credits [--json]
  lofi sync

Run a subcommand. Without arguments, or with an unknown subcommand,
lofi prints this message and exits with code 2.
`

func main() {
	os.Exit(run(os.Args[1:]))
}

// run is the testable dispatcher. It returns the process exit code so
// tests can assert behaviour without spawning a subprocess.
func run(args []string) int {
	if len(args) == 0 {
		fmt.Fprint(os.Stderr, usage)
		return 2
	}
	switch args[0] {
	case "credits":
		return codeFor(runCredits(args[1:]))
	case "play", "list", "sync":
		fmt.Fprintf(os.Stderr, "lofi %s: not yet implemented\n", args[0])
		return 0
	default:
		fmt.Fprintf(os.Stderr, "lofi: unknown subcommand %q\n\n%s", args[0], usage)
		return 2
	}
}
