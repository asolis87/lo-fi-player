// ProgressReporter is the observer seam the Syncer exposes so the
// CLI can render human-friendly progress lines on stderr without the
// catalog package taking a hard dependency on os.Stderr or any other
// concrete output stream. Implementations MUST be safe to call from
// the goroutine that invokes Syncer.Sync; the contract does not
// require concurrent Report calls (PR-6B).
//
// The phase vocabulary is intentionally narrow: only the two
// operations the Syncer actually performs today (fetch the manifest
// over HTTPS, apply it atomically to the cache). PR-6B is scoped to
// "what the Syncer does", not to per-track asset orchestration the
// current Syncer does not implement (spec cli-sync "Progreso,
// errores y exit semantics sólo por CLI" + design decision "no fake
// progress claims").
package catalog

import (
	"fmt"
	"io"
)

// ProgressPhase names the two observable operations the Syncer
// exposes today. The set is locked by the spec rule "do not fake
// absent asset orchestration": any new phase requires a real new
// operation in the Syncer.
type ProgressPhase string

const (
	// ProgressFetch wraps the manifest download Retry may invoke
	// up to three times before Syncer.Sync returns.
	ProgressFetch ProgressPhase = "fetch"
	// ProgressApply wraps the atomic write of the parsed manifest
	// to the cache directory.
	ProgressApply ProgressPhase = "apply"
)

// ProgressEvent is the single observation a ProgressReporter
// receives. Done=true marks the end of the operation. Err is
// non-nil only on Done=true when the operation failed; a nil Err
// paired with Done=true means success. The "in progress" message
// corresponds to Done=false.
type ProgressEvent struct {
	Phase ProgressPhase
	Done  bool
	Err   error
}

// ProgressReporter receives every progress event the Syncer emits.
// Implementations are free to be no-ops (NullProgressReporter) or
// to format a human-friendly line on stderr (StderrReporter).
type ProgressReporter interface {
	Report(ProgressEvent)
}

// NullProgressReporter satisfies ProgressReporter with no
// observable side effect. Syncer.Progress defaults to this when the
// field is nil so the catalog package never depends on a concrete
// output stream.
type NullProgressReporter struct{}

// Report on NullProgressReporter does nothing.
func (NullProgressReporter) Report(ProgressEvent) {}

// StderrReporter writes a one-line, human-friendly progress message
// to its Writer for every event. The format is stable enough for
// shell pipelines to grep:
//
//   - start:    "lofi sync: <phase>..."
//   - success:  "lofi sync: <phase> done"
//   - failure:  "lofi sync: <phase>: <err>"
//
// The Writer field is exported so tests can inject a bytes.Buffer to
// capture and assert on the output without touching os.Stderr. A
// nil Writer is treated as io.Discard so the reporter is always
// safe to use.
type StderrReporter struct {
	Writer io.Writer
}

// Report writes the formatted progress line to StderrReporter.Writer.
func (s StderrReporter) Report(ev ProgressEvent) {
	w := s.Writer
	if w == nil {
		return
	}
	var line string
	switch {
	case ev.Err != nil:
		line = fmt.Sprintf("lofi sync: %s: %v\n", ev.Phase, ev.Err)
	case ev.Done:
		line = fmt.Sprintf("lofi sync: %s done\n", ev.Phase)
	default:
		line = fmt.Sprintf("lofi sync: %s...\n", ev.Phase)
	}
	_, _ = io.WriteString(w, line)
}
