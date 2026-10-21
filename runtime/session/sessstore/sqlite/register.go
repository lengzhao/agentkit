// Package sqlite registers session/sqlite and links the SQLite driver.
// It is separate from sessstore so importing JSONL/memory stores does not
// compile modernc.org/sqlite into the binary.
package sqlite

import (
	_ "modernc.org/sqlite"

	sessstore "github.com/lengzhao/agentkit/runtime/session/sessstore"
	"github.com/lengzhao/pluginkit"
)

func init() {
	pluginkit.Register("session/sqlite", sessstore.NewSQLiteStore)
}
