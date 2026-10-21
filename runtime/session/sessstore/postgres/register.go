// Package postgres registers session/postgres and links the Postgres driver.
// It is separate from sessstore so importing JSONL/memory stores does not
// compile pgx into the binary.
package postgres

import (
	_ "github.com/jackc/pgx/v5/stdlib"

	sessstore "github.com/lengzhao/agentkit/runtime/session/sessstore"
	"github.com/lengzhao/pluginkit"
)

func init() {
	pluginkit.Register("session/postgres", sessstore.NewPostgresStore)
}
