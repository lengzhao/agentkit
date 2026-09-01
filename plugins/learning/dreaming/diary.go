package dreaming

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Diary appends human-readable phase blocks to DREAMS.md.
type Diary struct {
	Path string
}

func (d *Diary) AppendPhase(phase string, now time.Time, lines []string) error {
	if d.Path == "" {
		return fmt.Errorf("dream diary path is required")
	}
	var b strings.Builder
	b.WriteString(fmt.Sprintf("## %s — %s\n\n", phase, now.UTC().Format(time.RFC3339)))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		b.WriteString("- ")
		b.WriteString(line)
		b.WriteByte('\n')
	}
	b.WriteByte('\n')
	if err := os.MkdirAll(filepath.Dir(d.Path), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(d.Path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	if st, err := f.Stat(); err == nil && st.Size() == 0 {
		if _, err := f.WriteString("# DREAMS.md\n\n"); err != nil {
			return err
		}
	}
	_, err = f.WriteString(b.String())
	return err
}
