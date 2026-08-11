package main

import (
	"reflect"
	"testing"
)

// TestDispatch_SplitsSubcommandAndArgs guards the contract of
// dispatch: the first positional arg is the subcommand and every
// subsequent arg is forwarded to the handler unchanged. This is
// the pure helper cmd/lofi relies on to keep the run() switch
// short and the subcommand boundary testable in isolation.
//
// Contract:
//   - dispatch(nil)         -> ("", nil)
//   - dispatch([])          -> ("", nil)
//   - dispatch([]string{"play"})                  -> ("play", nil)
//   - dispatch([]string{"play", "track_drizzle"}) -> ("play", []string{"track_drizzle"})
//   - dispatch([]string{"list", "--json"})        -> ("list", []string{"--json"})
func TestDispatch_SplitsSubcommandAndArgs(t *testing.T) {
	cases := []struct {
		name    string
		args    []string
		wantSub string
		wantRest []string
	}{
		{"nil", nil, "", nil},
		{"empty", []string{}, "", nil},
		{"only_subcommand", []string{"play"}, "play", nil},
		{"subcommand_with_single_arg", []string{"play", "track_drizzle"}, "play", []string{"track_drizzle"}},
		{"subcommand_with_flag", []string{"list", "--json"}, "list", []string{"--json"}},
		{"subcommand_with_many_args", []string{"sync", "--force", "extra"}, "sync", []string{"--force", "extra"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gotSub, gotRest := dispatch(tc.args)
			if gotSub != tc.wantSub {
				t.Errorf("dispatch(%v) subcommand = %q, want %q", tc.args, gotSub, tc.wantSub)
			}
			if !reflect.DeepEqual(gotRest, tc.wantRest) {
				t.Errorf("dispatch(%v) rest = %#v, want %#v", tc.args, gotRest, tc.wantRest)
			}
		})
	}
}