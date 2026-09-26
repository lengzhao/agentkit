package sessindex

import (
	capsessionindex "github.com/lengzhao/agentkit/cap/sessionindex"
	"github.com/lengzhao/pluginkit"
)

func init() {
	pluginkit.Register("session/sqlite-index", NewSQLiteIndex)
	pluginkit.Register("session/sql-index", NewSQLIndex)
}

var (
	_ capsessionindex.Service = (*SQLiteIndex)(nil)
	_ capsessionindex.Service = (*SQLIndex)(nil)
)
