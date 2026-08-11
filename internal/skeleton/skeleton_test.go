package skeleton

import "testing"

// TestHello_Greeting locks the public contract of the skeleton package.
// The function is the canary used by the bootstrap dispatcher until the
// real CLI surface lands in later PRs. Changing the greeting is a
// breaking change for downstream consumers.
func TestHello_Greeting(t *testing.T) {
	got := Hello()
	if got != "world" {
		t.Fatalf("Hello() = %q, want %q", got, "world")
	}
}
