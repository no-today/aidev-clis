package errs

import (
	"errors"
	"fmt"
	"testing"
)

func TestError_MessageAndExit(t *testing.T) {
	e := Config("BAD_ENV", "no such env")
	if e.Code != "BAD_ENV" || e.Exit != ExitConfig {
		t.Fatalf("got %+v", e)
	}
	if e.Error() != "BAD_ENV: no such env" {
		t.Fatalf("Error()=%q", e.Error())
	}
}

func TestError_AsUnwrap(t *testing.T) {
	var target *Error
	if !errors.As(Remote("X", "y"), &target) || target.Exit != ExitRemote {
		t.Fatal("errors.As failed")
	}
}

// From must see through a %w wrap so the code + exit class survive; a bare type
// assertion (the old bug) would have degraded this to UNKNOWN/ExitGeneral.
func TestFrom_UnwrapsWrappedError(t *testing.T) {
	wrapped := fmt.Errorf("connecting: %w", Timeout("DB_TIMEOUT", "deadline"))
	got := From(wrapped)
	if got.Code != "DB_TIMEOUT" || got.Exit != ExitTimeout {
		t.Fatalf("From(wrapped) = %+v, want DB_TIMEOUT/ExitTimeout", got)
	}
	if plain := From(errors.New("boom")); plain.Code != "UNKNOWN" || plain.Exit != ExitGeneral {
		t.Fatalf("From(plain) = %+v, want UNKNOWN/ExitGeneral", plain)
	}
}
