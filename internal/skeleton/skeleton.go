// Package skeleton hosts the minimal placeholder types used by PR #1
// while the real CLI/TUI surface is still under construction. Anything
// here is expected to be removed once its owning capability lands.
package skeleton

// Hello returns the canonical greeting used by the bootstrap entry
// point. It is intentionally trivial: it exists so the build pipeline
// has a runnable unit from PR #1 onward and so we can wire the strict
// TDD loop before the real backend ports appear.
func Hello() string {
	return "world"
}
