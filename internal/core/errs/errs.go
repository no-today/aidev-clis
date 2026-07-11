// Package errs defines the typed error that carries a stable code and the
// process exit class. CLIs map *Error.Exit to os.Exit.
package errs

import (
	"errors"
	"fmt"
)

const (
	ExitOK      = 0
	ExitGeneral = 1
	ExitConfig  = 2
	ExitAuth    = 3
	ExitTimeout = 4
	ExitRemote  = 5
)

// Error is the one error type the core and adapters return.
type Error struct {
	Code         string
	Message      string
	Exit         int
	RemoteStatus int
	RemoteCode   string
}

func (e *Error) Error() string { return fmt.Sprintf("%s: %s", e.Code, e.Message) }

func New(code, msg string, exit int) *Error { return &Error{Code: code, Message: msg, Exit: exit} }

// From maps any error to an *Error for the output envelope: an *Error (even one
// wrapped with %w) passes through with its code and exit class intact; anything
// else becomes a generic ExitGeneral "UNKNOWN". Use at CLI boundaries.
//
// errors.As (not a bare type assertion) is deliberate: adapters wrap with
// fmt.Errorf("...: %w", errs.Timeout(...)) and a bare assertion would silently
// degrade that to UNKNOWN/exit 1, losing the timeout/auth exit class the CLIs
// key on.
func From(err error) *Error {
	var e *Error
	if errors.As(err, &e) {
		return e
	}
	return General("UNKNOWN", err.Error())
}

func General(code, msg string) *Error { return New(code, msg, ExitGeneral) }
func Config(code, msg string) *Error  { return New(code, msg, ExitConfig) }
func Auth(code, msg string) *Error    { return New(code, msg, ExitAuth) }
func Timeout(code, msg string) *Error { return New(code, msg, ExitTimeout) }
func Remote(code, msg string) *Error  { return New(code, msg, ExitRemote) }
