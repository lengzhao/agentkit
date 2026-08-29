package filesystem

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
)

// IgnoreMatcher applies .gitignore-style patterns relative to a workspace root.
type IgnoreMatcher struct {
	patterns []ignorePattern
}

type ignorePattern struct {
	negate   bool
	dirOnly  bool
	segments []string
	raw      string
}

// LoadIgnoreMatcher reads the workspace-root .gitignore when present and always
// ignores .git and node_modules.
func LoadIgnoreMatcher(workspaceRoot string) (*IgnoreMatcher, error) {
	m := &IgnoreMatcher{
		patterns: []ignorePattern{
			{segments: []string{".git"}},
			{segments: []string{"node_modules"}},
			{segments: []string{".env"}},
		},
	}
	path := filepath.Join(workspaceRoot, ".gitignore")
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return m, nil
		}
		return nil, err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		m.addLine(line)
	}
	return m, scanner.Err()
}

func (m *IgnoreMatcher) addLine(line string) {
	negate := false
	if strings.HasPrefix(line, "!") {
		negate = true
		line = strings.TrimPrefix(line, "!")
	}
	dirOnly := strings.HasSuffix(line, "/")
	line = strings.TrimSuffix(line, "/")
	line = filepath.ToSlash(strings.TrimSpace(line))
	if line == "" {
		return
	}
	m.patterns = append(m.patterns, ignorePattern{
		negate:   negate,
		dirOnly:  dirOnly,
		segments: strings.Split(line, "/"),
		raw:      line,
	})
}

// Ignored reports whether relPath (slash-separated, relative to workspace root)
// should be skipped during directory walks.
func (m *IgnoreMatcher) Ignored(relPath string, isDir bool) bool {
	if relPath == "" || relPath == "." {
		return false
	}
	relPath = strings.Trim(filepath.ToSlash(relPath), "/")
	if relPath == "" {
		return false
	}

	ignored := false
	for _, pattern := range m.patterns {
		if pattern.dirOnly && !isDir {
			continue
		}
		if patternMatches(pattern, relPath, isDir) {
			ignored = !pattern.negate
		}
	}
	return ignored
}

func patternMatches(pattern ignorePattern, relPath string, isDir bool) bool {
	pathSegments := strings.Split(relPath, "/")
	patternSegments := pattern.segments

	if len(patternSegments) == 1 && !strings.Contains(pattern.raw, "/") {
		if patternSegments[0] == pathSegments[0] {
			return true
		}
		name := pathSegments[len(pathSegments)-1]
		if matched, _ := filepath.Match(patternSegments[0], name); matched {
			return true
		}
		if matched, _ := filepath.Match(patternSegments[0], relPath); matched {
			return true
		}
		return false
	}

	if len(patternSegments) > len(pathSegments) {
		return false
	}
	for i, seg := range patternSegments {
		target := pathSegments[i]
		if seg == "**" {
			if i == len(patternSegments)-1 {
				return true
			}
			for start := i; start <= len(pathSegments)-len(patternSegments)+i+1; start++ {
				suffix := append([]string{}, patternSegments[i+1:]...)
				if len(suffix) == 0 {
					return true
				}
				if start+len(suffix) > len(pathSegments) {
					continue
				}
				ok := true
				for j, s := range suffix {
					if s == "**" {
						ok = true
						break
					}
					if matched, _ := filepath.Match(s, pathSegments[start+j]); !matched {
						ok = false
						break
					}
				}
				if ok {
					return true
				}
			}
			return false
		}
		if matched, _ := filepath.Match(seg, target); !matched {
			return false
		}
	}
	if len(patternSegments) == len(pathSegments) {
		return true
	}
	return isDir && len(patternSegments) < len(pathSegments)
}
