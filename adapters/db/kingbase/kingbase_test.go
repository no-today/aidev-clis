package kingbase

import (
	"testing"

	"github.com/no-today/aidev-clis/internal/dbcli/pgwire"
)

func TestKingbase_ExtraSystemSchemas(t *testing.T) {
	ns := pgwire.Dialect{ExtraSystemSchemas: systemSchemas}.SystemNamespaces()
	for _, want := range []string{"pg_catalog", "sys", "sysaudit", "oracle"} {
		found := false
		for _, n := range ns {
			if n == want {
				found = true
			}
		}
		if !found {
			t.Errorf("kingbase discovery must exclude %q; got %v", want, ns)
		}
	}
}

func TestKingbase_Name(t *testing.T) {
	if New().Name() != "kingbase" {
		t.Fatal("name")
	}
}
