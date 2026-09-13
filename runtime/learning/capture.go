package learning

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	caplearning "github.com/lengzhao/agentkit/cap/learning"
	capmemory "github.com/lengzhao/agentkit/cap/memory"
)

type CaptureInput = caplearning.CaptureInput
type CaptureOutput = caplearning.CaptureOutput

// ApplyCapture applies one learn_capture action via memory capture + skill proposer.
func ApplyCapture(ctx context.Context, mem capmemory.Capture, skills caplearning.SkillProposer, sessionID string, in CaptureInput) (CaptureOutput, error) {
	action := strings.ToLower(strings.TrimSpace(in.Action))
	switch action {
	case "memory_add":
		if mem == nil {
			return CaptureOutput{}, fmt.Errorf("memory capture is required for memory_add")
		}
		text := strings.TrimSpace(in.Content)
		if text == "" {
			return CaptureOutput{}, fmt.Errorf("content is required for memory_add")
		}
		msg, err := mem.CaptureMemoryAdd(ctx, text, "background-review")
		if err != nil {
			return CaptureOutput{OK: false, Message: err.Error()}, nil
		}
		return CaptureOutput{OK: true, Message: msg}, nil
	case "memory_replace":
		if mem == nil {
			return CaptureOutput{}, fmt.Errorf("memory capture is required for memory_replace")
		}
		old := strings.TrimSpace(in.OldText)
		content := strings.TrimSpace(in.Content)
		if old == "" {
			return CaptureOutput{}, fmt.Errorf("old_text is required for memory_replace")
		}
		if content == "" {
			return CaptureOutput{}, fmt.Errorf("content is required for memory_replace")
		}
		msg, err := mem.CaptureMemoryReplace(ctx, old, content, "background-review")
		if err != nil {
			return CaptureOutput{OK: false, Message: err.Error()}, nil
		}
		return CaptureOutput{OK: true, Message: msg}, nil
	case "memory_remove":
		if mem == nil {
			return CaptureOutput{}, fmt.Errorf("memory capture is required for memory_remove")
		}
		old := strings.TrimSpace(in.OldText)
		if old == "" {
			return CaptureOutput{}, fmt.Errorf("old_text is required for memory_remove")
		}
		msg, err := mem.CaptureMemoryRemove(ctx, old)
		if err != nil {
			return CaptureOutput{OK: false, Message: err.Error()}, nil
		}
		return CaptureOutput{OK: true, Message: msg}, nil
	case "skill_propose":
		if skills == nil {
			return CaptureOutput{}, fmt.Errorf("skill proposer is required for skill_propose")
		}
		body := strings.TrimSpace(in.Content)
		if body == "" {
			return CaptureOutput{}, fmt.Errorf("content is required for skill_propose")
		}
		name := strings.TrimSpace(in.Name)
		focus := strings.TrimSpace(in.Focus)
		msg, err := skills.CaptureSkillPropose(ctx, name, body, sessionID, focus, "background-review")
		if err != nil {
			return CaptureOutput{OK: false, Message: err.Error()}, nil
		}
		return CaptureOutput{OK: true, Message: msg}, nil
	default:
		return CaptureOutput{}, fmt.Errorf("unknown action %q", in.Action)
	}
}

// FormatCaptureResult JSON-encodes CaptureOutput for Tool.Call.
func FormatCaptureResult(out CaptureOutput) (string, error) {
	data, err := json.Marshal(out)
	if err != nil {
		return "", err
	}
	return string(data), nil
}
