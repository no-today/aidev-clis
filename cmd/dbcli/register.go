package main

import (
	"github.com/no-today/aidev-clis/adapters/db/dataease"
	"github.com/no-today/aidev-clis/adapters/db/kingbase"
	"github.com/no-today/aidev-clis/adapters/db/mongo"
	"github.com/no-today/aidev-clis/adapters/db/mysql"
	"github.com/no-today/aidev-clis/adapters/db/pg"
	"github.com/no-today/aidev-clis/adapters/db/redis"
	"github.com/no-today/aidev-clis/adapters/db/sqlite"
	"github.com/no-today/aidev-clis/internal/dbcli"
)

// builtins is the ONLY place drivers are wired in. Retire one = delete its line
// here + delete its package dir.
func builtins() []dbcli.Driver {
	return []dbcli.Driver{
		mysql.New(),
		pg.New(),
		kingbase.New(),
		redis.New(),
		sqlite.New(),
		mongo.New(),
		dataease.New(), // last-resort HTTP bypass; see adapters/db/dataease/README.md
	}
}
