package main

// dispatch splits args into the subcommand (args[0]) and the
// remaining arguments forwarded verbatim to the matching handler.
//
// Contract:
//   - dispatch(nil)                            -> ("", nil)
//   - dispatch([]string{})                     -> ("", nil)
//   - dispatch([]string{"play"})              -> ("play", nil)
//   - dispatch([]string{"play", "track-1"})   -> ("play", []string{"track-1"})
//
// Keeping this as a pure helper means callers can drive subcommand
// routing in tests without re-implementing the split. The returned
// rest slice is the same backing array as args[1:] so callers MUST
// NOT mutate it; passing it straight into a handler preserves that
// invariant.
func dispatch(args []string) (subcommand string, rest []string) {
	if len(args) == 0 {
		return "", nil
	}
	rest = args[1:]
	if len(rest) == 0 {
		rest = nil
	}
	return args[0], rest
}