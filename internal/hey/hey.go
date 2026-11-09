// Package hey reads HEY calendars through the hey CLI (github.com/basecamp/hey-cli)
// instead of an expiring ICS feed URL. The CLI owns sign-in and token refresh;
// CalMirror only runs it and reads its JSON output, so no HEY credential is ever
// stored here.
package hey

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Exit statuses the hey CLI documents (`hey help exit-codes`) that CalMirror
// acts on.
const (
	ExitAuth        = 3 // authentication is required or failed
	ExitRateLimited = 5 // HEY rate-limited the request
	ExitNetwork     = 6 // a network connection failed
)

// Find locates the hey CLI. launchd jobs run with a minimal PATH, so it falls
// back to the usual install locations.
func Find() (string, error) {
	if p, err := exec.LookPath("hey"); err == nil {
		return p, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("locate home directory: %w", err)
	}
	for _, dir := range []string{
		filepath.Join(home, ".local", "bin"),
		filepath.Join(home, "go", "bin"),
		"/opt/homebrew/bin",
		"/usr/local/bin",
	} {
		p := filepath.Join(dir, "hey")
		if info, err := os.Stat(p); err == nil && !info.IsDir() && info.Mode()&0o111 != 0 {
			return p, nil
		}
	}
	return "", errors.New("hey CLI not found; install it from github.com/basecamp/hey-cli and run `hey auth login`")
}

// Error is a failed hey CLI run: its exit status plus the error, code and hint
// from its JSON envelope when it printed one.
type Error struct {
	Args     []string
	ExitCode int
	Code     string // machine-readable code from the envelope, e.g. "api"
	Message  string
	Hint     string
}

func (e *Error) Error() string {
	msg := e.Message
	if msg == "" {
		msg = fmt.Sprintf("exit status %d", e.ExitCode)
	}
	s := fmt.Sprintf("hey %s: %s", strings.Join(e.Args, " "), msg)
	if e.Hint != "" {
		s += " (" + e.Hint + ")"
	}
	return s
}

// Auth reports whether the run failed on sign-in: HEY needs `hey auth login`,
// or refused to refresh the token.
func (e *Error) Auth() bool {
	return e.ExitCode == ExitAuth || strings.Contains(e.Message, "token refresh failed")
}

// RateLimited reports whether HEY rate-limited the run.
func (e *Error) RateLimited() bool {
	return e.ExitCode == ExitRateLimited || strings.Contains(e.Message, "rate_limit")
}

// Runner runs the hey CLI with args and returns its stdout and stderr. A
// non-zero exit is returned as an *exec.ExitError (or any error); Client turns
// that into an *Error.
type Runner func(ctx context.Context, args ...string) (stdout, stderr []byte, err error)

// Client runs hey CLI commands and decodes their JSON envelopes.
type Client struct {
	// Run executes hey. If nil, the binary from Find is run.
	Run Runner
}

func (c *Client) run(ctx context.Context, args ...string) ([]byte, []byte, error) {
	if c.Run != nil {
		return c.Run(ctx, args...)
	}
	bin, err := Find()
	if err != nil {
		return nil, nil, err
	}
	var stdout, stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err = cmd.Run()
	return stdout.Bytes(), stderr.Bytes(), err
}

// envelope is the hey CLI's --json response shape.
type envelope struct {
	OK    bool            `json:"ok"`
	Data  json.RawMessage `json:"data"`
	Error string          `json:"error"`
	Code  string          `json:"code"`
	Hint  string          `json:"hint"`
}

// JSON runs hey with args plus --json and decodes the envelope's data into out.
func (c *Client) JSON(ctx context.Context, out any, args ...string) error {
	args = append(append([]string(nil), args...), "--json")
	stdout, stderr, runErr := c.run(ctx, args...)

	var env envelope
	decodeErr := decodeEnvelope(stdout, stderr, &env)

	if runErr == nil && decodeErr == nil && env.OK {
		if err := json.Unmarshal(env.Data, out); err != nil {
			return fmt.Errorf("hey %s: decode data: %w", strings.Join(args, " "), err)
		}
		return nil
	}
	var exitErr *exec.ExitError
	if runErr != nil && !errors.As(runErr, &exitErr) {
		return fmt.Errorf("hey %s: %w", strings.Join(args, " "), runErr) // it never ran
	}
	if runErr == nil && decodeErr != nil {
		return fmt.Errorf("hey %s: %w", strings.Join(args, " "), decodeErr)
	}
	e := &Error{Args: args, Code: env.Code, Message: env.Error, Hint: env.Hint}
	if exitErr != nil {
		e.ExitCode = exitErr.ExitCode()
	}
	if e.Message == "" {
		e.Message = strings.TrimSpace(string(stderr))
	}
	return e
}

// decodeEnvelope reads the JSON envelope from stdout, or from stderr when the
// CLI wrote its failure there.
func decodeEnvelope(stdout, stderr []byte, env *envelope) error {
	for _, b := range [][]byte{stdout, stderr} {
		if b = bytes.TrimSpace(b); len(b) > 0 && b[0] == '{' {
			return json.Unmarshal(b, env)
		}
	}
	return errors.New("no JSON response")
}

// Calendar is one HEY calendar, as `hey calendar list` reports it.
type Calendar struct {
	ID       int64  `json:"id"`
	Name     string `json:"name"`
	Kind     string `json:"kind"`
	Owned    bool   `json:"owned"`
	Personal bool   `json:"personal"`
	External bool   `json:"external"`
}

// Calendars lists the signed-in account's HEY calendars.
func (c *Client) Calendars(ctx context.Context) ([]Calendar, error) {
	var cals []Calendar
	if err := c.JSON(ctx, &cals, "calendar", "list"); err != nil {
		return nil, err
	}
	return cals, nil
}
