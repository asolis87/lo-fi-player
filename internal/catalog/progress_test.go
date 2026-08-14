// Unit tests for the ProgressReporter primitive (PR-6B 6B.1).
// The table-driven cases cover the three observable states
// (start, success, failure) for the stderr reporter; the null
// reporter and the nil-Writer fallback get explicit guards so
// the "always safe" contract is locked.
package catalog

import (
	"bytes"
	"errors"
	"testing"
)

func TestProgressReporter_NullIsNoop(t *testing.T) {
	var r ProgressReporter = NullProgressReporter{}
	r.Report(ProgressEvent{Phase: ProgressFetch})             // start
	r.Report(ProgressEvent{Phase: ProgressFetch, Done: true}) // success
	r.Report(ProgressEvent{Phase: ProgressApply, Done: true, Err: errors.New("boom")})
}

func TestProgressReporter_StderrFormats(t *testing.T) {
	cases := []struct {
		name string
		ev   ProgressEvent
		want string
	}{
		{
			name: "fetch start",
			ev:   ProgressEvent{Phase: ProgressFetch},
			want: "lofi sync: fetch...\n",
		},
		{
			name: "fetch done",
			ev:   ProgressEvent{Phase: ProgressFetch, Done: true},
			want: "lofi sync: fetch done\n",
		},
		{
			name: "apply start",
			ev:   ProgressEvent{Phase: ProgressApply},
			want: "lofi sync: apply...\n",
		},
		{
			name: "apply done",
			ev:   ProgressEvent{Phase: ProgressApply, Done: true},
			want: "lofi sync: apply done\n",
		},
		{
			name: "fetch failure",
			ev:   ProgressEvent{Phase: ProgressFetch, Done: true, Err: errors.New("network unreachable")},
			want: "lofi sync: fetch: network unreachable\n",
		},
		{
			name: "apply failure",
			ev:   ProgressEvent{Phase: ProgressApply, Done: true, Err: errors.New("disk full")},
			want: "lofi sync: apply: disk full\n",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			r := StderrReporter{Writer: &buf}
			r.Report(tc.ev)
			if got := buf.String(); got != tc.want {
				t.Fatalf("StderrReporter.Report output = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestProgressReporter_StderrNilWriterSafe(t *testing.T) {
	// Constructor literal with explicit nil Writer MUST NOT
	// panic; Report is a graceful no-op so Syncer.Progress can
	// be set to &StderrReporter{} without side effects.
	r := StderrReporter{}
	r.Report(ProgressEvent{Phase: ProgressFetch, Done: true})
}

func TestProgressReporter_PhasesAreCanonical(t *testing.T) {
	// Spec rule: "do not fake absent asset orchestration; emit
	// only Fetch start/end and Apply start/end events for
	// operations that actually exist". Lock the public phase
	// vocabulary to the two strings the Syncer wires up.
	if ProgressFetch != "fetch" {
		t.Errorf("ProgressFetch = %q, want %q", ProgressFetch, "fetch")
	}
	if ProgressApply != "apply" {
		t.Errorf("ProgressApply = %q, want %q", ProgressApply, "apply")
	}
}
