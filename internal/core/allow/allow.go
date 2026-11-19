// Package allow is the secondary defense layer for pass-through adapters: a
// per-adapter verb allow-list plus a dangerous-flag deny-list. The PRIMARY
// boundary is the scoped credential (see docs/SECURITY-MODEL.md); this is
// defense-in-depth.
package allow

import (
	"fmt"
	"strings"

	"github.com/no-today/aidev-clis/internal/core/errs"
)

// Policy is declared by a pass-through adapter. Verbs is an allow-list (only
// these first-args pass). DenyFlags is a deny-list — every other native flag
// passes through (that's the point of pass-through); only these are refused.
type Policy struct {
	Verbs     []string
	DenyFlags []string
}

// Check enforces the policy on the user's args (args[0] is the verb).
func (p Policy) Check(args []string) error {
	if len(args) == 0 {
		return errs.Config("NO_VERB", fmt.Sprintf("expected one of %v", p.Verbs))
	}
	if !contains(p.Verbs, args[0]) {
		return errs.Config("VERB_NOT_ALLOWED",
			fmt.Sprintf("verb %q not allowed; allowed: %v", args[0], p.Verbs))
	}
	// Tokens are checked RAW (not flag-value-paired): a denied flag is rejected
	// wherever it appears, even as another flag's value. Conservative by design
	// — don't "fix" this by adding flag-parsing awareness.
	for _, a := range args[1:] {
		for _, d := range p.DenyFlags {
			if a == d || strings.HasPrefix(a, d+"=") {
				return errs.Config("FLAG_NOT_ALLOWED",
					fmt.Sprintf("flag %q is not permitted (it could override the injected credential/target)", d))
			}
		}
	}
	return nil
}

func contains(xs []string, x string) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}
