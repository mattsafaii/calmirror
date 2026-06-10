package keychain

import (
	"errors"
	"os/exec"
	"testing"
)

// testAccount is a throwaway account under the calmirror service; it never
// collides with a real iCloud username and is deleted on cleanup.
const testAccount = "calmirror-keychain-selftest@example.invalid"

func TestRoundTrip(t *testing.T) {
	if _, err := exec.LookPath("security"); err != nil {
		t.Skip("security tool not available")
	}
	t.Cleanup(func() { _ = Delete(testAccount) })

	// Not found before set.
	if _, err := Get(testAccount); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get before Set: got %v, want ErrNotFound", err)
	}

	if err := Set(testAccount, "hunter2"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	got, err := Get(testAccount)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != "hunter2" {
		t.Fatalf("Get = %q, want %q", got, "hunter2")
	}

	// Replace and read back.
	if err := Set(testAccount, "correct horse"); err != nil {
		t.Fatalf("Set replace: %v", err)
	}
	if got, _ := Get(testAccount); got != "correct horse" {
		t.Fatalf("Get after replace = %q", got)
	}

	if err := Delete(testAccount); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if err := Delete(testAccount); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Delete missing: got %v, want ErrNotFound", err)
	}
}
