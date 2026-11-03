package derive

import (
	"github.com/lengzhao/agentkit"
	"testing"
)

func TestHasUserInstruction(t *testing.T) {
	for _, tt := range []struct {
		name, role, typ, text string
		want                  bool
	}{
		{"empty", "user", "text", "", false},
		{"assistant is not user intent", "assistant", "text", "summarize this", false},
		{"media", "user", "image", "", false},
		{"attachment-only fallback", "user", "text", "Please analyze the attached file(s).", false},
		{"attachment-only paths", "user", "text", "Please analyze the attached file(s).\n\n(Files the user uploaded with this message. workspace paths:\nwork/a.txt)", false},
		{"legacy attachment hint", "user", "text", "[attachment: work/a.txt]", false},
		{"legacy translated attachment hint", "user", "text", "[附件 a.txt]", false},
		{"instruction with attachments", "user", "text", "summarize this\n\n(Files the user uploaded with this message. workspace paths:\nwork/a.txt)", true},
		{"instruction", "user", "text", "  summarize this  ", true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got := HasUserInstruction([]agentkit.ModelMessage{{Role: tt.role, Content: []agentkit.ContentPart{{Type: tt.typ, Text: tt.text}}}})
			if got != tt.want {
				t.Fatalf("got %v want %v", got, tt.want)
			}
		})
	}
}
