// Command lofi is the entry point for the lo-fi-player terminal music
// player. PR #1 ships only the dispatcher: it routes the documented
// subcommands (play, list, credits, sync) and refuses anything else
// with a usage banner and exit code 2. PR #9 wires runList / runSync
// / runPlay through dispatch() so the placeholder banner is gone and
// each subcommand does its real work.
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
	subcommand, rest := dispatch(args)
	if subcommand == "" {
		fmt.Fprint(os.Stderr, usage)
		return 2
	}
	switch subcommand {
	case "credits":
		return codeFor(runCredits(rest))
	case "play":
		return codeFor(runPlay(rest))
	case "list":
		return codeFor(runList(rest))
	case "sync":
		return codeFor(runSync(rest))
	default:
		fmt.Fprintf(os.Stderr, "lofi: unknown subcommand %q\n\n%s", subcommand, usage)
		return 2
	}
}