// Package errs defines exit codes and a typed error carrying one.
//
// Codes mirror the Python implementation:
//   - 0 OK
//   - 1 generic error
//   - 2 auth expired / unauthenticated
//   - 3 rate limited
//   - 4 timed out waiting (mm-specific, no Python counterpart)
package errs

import "fmt"

const (
	CodeOK          = 0
	CodeGeneric     = 1
	CodeAuthExpired = 2
	CodeRateLimited = 3
	// CodeTimeout means the command gave up waiting rather than failing. A
	// supervisor can restart on it instead of treating the silence as success.
	CodeTimeout = 4
)

// ExitError carries a process exit code along with a human-readable message.
// When main sees one, it prints Msg to stderr (if non-empty) and exits Code.
type ExitError struct {
	Code int
	Msg  string
}

func (e ExitError) Error() string { return e.Msg }

// Errorf wraps fmt.Errorf with an exit code.
func Errorf(code int, format string, args ...any) ExitError {
	return ExitError{Code: code, Msg: fmt.Sprintf(format, args...)}
}
