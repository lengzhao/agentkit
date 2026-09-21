package dreaming

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/lengzhao/agentkit/cap/filesystem"
)

// Diary appends human-readable phase blocks to DREAMS.md.
// Path is a filesystem.Service-relative path.
type Diary struct {
	FS   filesystem.Service
	Path string
}

func (d *Diary) AppendPhase(ctx context.Context, phase string, now time.Time, lines []string) error {
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
	// Prepend the document header when the diary is missing or empty.
	header := ""
	if st, err := d.FS.Stat(ctx, d.Path); err != nil || st.Size == 0 {
		header = "# DREAMS.md\n\n"
	}
	return d.FS.Append(ctx, d.Path, []byte(header+b.String()))
}
