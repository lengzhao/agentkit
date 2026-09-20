package configfile

import (
	"os"

	capconfigfile "github.com/lengzhao/agentkit/cap/configfile"
	"github.com/lengzhao/pluginkit"
)

// writer is the standard capconfigfile.Writer over the local filesystem.
type writer struct{}

// New is the configfile/writer kind constructor: atomic config-file writes
// for plugin /add commands. Stateless; safe to share across instances.
func New(_ struct{}, _ struct{}) (capconfigfile.Writer, error) {
	return writer{}, nil
}

func init() {
	pluginkit.Register("configfile/writer", New)
}

func (writer) WriteTarget(files []string) (string, error) {
	return WriteTarget(files)
}

func (writer) WriteTargetForAdd(files []string, global bool) (string, error) {
	return WriteTargetForAdd(files, global)
}

func (writer) WriteAtomic(path string, data []byte, perm os.FileMode) error {
	return WriteAtomic(path, data, perm)
}

func (writer) Restore(path string, prev []byte, perm os.FileMode) error {
	return Restore(path, prev, perm)
}
