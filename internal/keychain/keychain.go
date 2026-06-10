// Package keychain stores and retrieves the iCloud app-specific password in the
// macOS Keychain by shelling out to the built-in `security` tool. No secret is
// ever written to config or the SQLite store.
package keychain

import (
	"bytes"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

// service is the Keychain generic-password service name under which CalMirror
// stores credentials. The account is the iCloud username.
const service = "calmirror"

// ErrNotFound is returned by Get when no password is stored for the account.
var ErrNotFound = errors.New("keychain: password not found")

// Set stores (or replaces) the password for account in the Keychain.
func Set(account, password string) error {
	if account == "" {
		return errors.New("keychain: account is required")
	}
	// -U updates the item if it already exists instead of erroring.
	cmd := exec.Command("security", "add-generic-password",
		"-a", account, "-s", service, "-w", password, "-U")
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("keychain: store password: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// Get returns the password stored for account, or ErrNotFound if none exists.
func Get(account string) (string, error) {
	if account == "" {
		return "", errors.New("keychain: account is required")
	}
	cmd := exec.Command("security", "find-generic-password",
		"-a", account, "-s", service, "-w")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		// `security` exits 44 when the item is not found.
		var exit *exec.ExitError
		if errors.As(err, &exit) && exit.ExitCode() == 44 {
			return "", ErrNotFound
		}
		return "", fmt.Errorf("keychain: read password: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	// `-w` prints the password followed by a newline.
	return strings.TrimRight(stdout.String(), "\n"), nil
}

// Delete removes the stored password for account. Deleting a missing item
// returns ErrNotFound.
func Delete(account string) error {
	if account == "" {
		return errors.New("keychain: account is required")
	}
	cmd := exec.Command("security", "delete-generic-password",
		"-a", account, "-s", service)
	if out, err := cmd.CombinedOutput(); err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) && exit.ExitCode() == 44 {
			return ErrNotFound
		}
		return fmt.Errorf("keychain: delete password: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}
