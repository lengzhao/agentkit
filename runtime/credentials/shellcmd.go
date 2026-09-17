package credentials

import (
	"path/filepath"
	"strings"
)

// FirstShellCommandToken returns the first token of a bash -lc command string (minimal parse).
func FirstShellCommandToken(command string) string {
	command = strings.TrimSpace(command)
	if command == "" {
		return ""
	}
	if command[0] == '\'' || command[0] == '"' {
		quote := command[0]
		if end := strings.IndexByte(command[1:], quote); end >= 0 {
			return command[1 : 1+end]
		}
	}
	if i := strings.IndexAny(command, " \t\n"); i >= 0 {
		return command[:i]
	}
	return command
}

// ShellBashScopeForCommand derives command basename and scope shell-bash.<cmd>.
func ShellBashScopeForCommand(command string) (cmd string, scope string) {
	cmd = strings.TrimSpace(FirstShellCommandToken(command))
	if cmd == "" {
		return "", ""
	}
	cmd = filepath.Base(cmd)
	if cmd == "" || cmd == "." || cmd == ".." {
		return "", ""
	}
	return cmd, ShellBashCredentialScope(cmd)
}
