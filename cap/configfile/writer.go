// Package configfile defines the config-file mutation boundary for plugin
// slash commands (/env add, /mcp add, /openapi add): pick the write target
// among configured files, write atomically, roll back on failure.
package configfile

import "os"

// Writer abstracts config-file target selection and atomic mutation.
// The standard implementation lives in runtime/configfile and is injected
// via deps, so tests and alternate hosts can substitute their own storage.
type Writer interface {
	// WriteTarget picks the local-scoped config path used for /add writes.
	WriteTarget(files []string) (string, error)
	// WriteTargetForAdd picks the config file path for /add writes:
	// global=true prefers the first global: entry, otherwise local: then bare paths.
	WriteTargetForAdd(files []string, global bool) (string, error)
	// WriteAtomic writes data to path via a temp file in the same directory.
	WriteAtomic(path string, data []byte, perm os.FileMode) error
	// Restore replaces path with prev after a failed mutation; nil prev removes the file.
	Restore(path string, prev []byte, perm os.FileMode) error
}

// PeelGlobalFlag removes -g/--global from args: the shared flag vocabulary
// of the /add slash commands.
func PeelGlobalFlag(args []string) (global bool, rest []string) {
	for _, arg := range args {
		switch arg {
		case "-g", "--global":
			global = true
		default:
			rest = append(rest, arg)
		}
	}
	return global, rest
}
