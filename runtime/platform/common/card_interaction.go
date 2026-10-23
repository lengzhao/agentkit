package common

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/lengzhao/agentkit"
	"log/slog"
	"sort"
	"strings"
	"time"

	"github.com/lengzhao/agentkit/cap/interaction"
)

// InteractionEventKey excludes callback delivery metadata but covers all actual
// operation data, including actor, control, selections, input and form state.
func InteractionEventKey(e interaction.Event) string {
	var action any
	decoder := json.NewDecoder(bytes.NewReader(e.RawAction))
	decoder.UseNumber()
	if decoder.Decode(&action) != nil {
		action = string(e.RawAction)
	}
	raw, _ := json.Marshal([]any{e.Platform, e.Type, e.EventID, e.ChatID, e.MessageID, e.InteractionID, e.UserID, action})
	sum := sha256.Sum256(raw)
	return "interaction:" + hex.EncodeToString(sum[:])
}

// InteractionProcessing runs in the platform's background worker. Builtin must
// report actual handling; Enrich may fetch original card and restore routing.
type InteractionProcessing struct {
	Submissions   *CardSubmissions
	SubmissionTTL time.Duration
	CloseCard     func(context.Context, interaction.Event, string) error
	NotifyUser    func(context.Context, interaction.Event, string) error
	Allowed       func(context.Context, interaction.Event) bool
	Mode          string
	Handlers      []interaction.Handler
	Builtin       func(context.Context, interaction.Event) bool
	Enrich        func(context.Context, *interaction.Event)
	Forward       func(context.Context, interaction.Event) error
}

func ProcessInteraction(ctx context.Context, e interaction.Event, p InteractionProcessing) error {
	if p.Enrich != nil {
		p.Enrich(ctx, &e)
	}
	if p.Allowed != nil && !p.Allowed(ctx, e) {
		slog.Info("interaction: permission filtered", "platform", e.Platform, "chat_id", e.ChatID)
		return nil
	}
	if p.Builtin != nil && p.Builtin(ctx, e) {
		slog.Info("interaction: builtin handled", "platform", e.Platform, "message_id", e.MessageID)
		return nil
	}
	if len(p.Handlers) == 0 && NormalizeUnknownInteraction(p.Mode) == UnknownInteractionIgnore {
		slog.Info("interaction: configuration ignored", "platform", e.Platform, "message_id", e.MessageID)
		return nil
	}
	EnrichInteractionCard(&e)
	for _, h := range p.Handlers {
		if h == nil {
			continue
		}
		handlerCtx := ctx
		if p.CloseCard != nil {
			handlerCtx = interaction.WithCardResponder(ctx, interactionCardResponder{close: p.CloseCard, event: e})
		}
		handled, err := h.HandleInteraction(handlerCtx, e)
		if err != nil {
			slog.Error("interaction: handler failed", "platform", e.Platform, "message_id", e.MessageID, "error", err)
			continue
		}
		if handled {
			slog.Info("interaction: plugin handled", "platform", e.Platform, "message_id", e.MessageID)
			return nil
		}
	}
	if NormalizeUnknownInteraction(p.Mode) == UnknownInteractionIgnore {
		slog.Info("interaction: configuration ignored", "platform", e.Platform, "message_id", e.MessageID)
		return nil
	}
	if e.Purpose == interaction.PurposeChange {
		slog.Debug("interaction: form change acknowledged", "platform", e.Platform, "message_id", e.MessageID)
		return nil
	}
	if p.Forward != nil {
		complete := func(error) {}
		once := e.Purpose == interaction.PurposeSubmit && e.SubmissionMode != interaction.SubmissionRepeat && p.Submissions != nil && cardSubmissionKey(e) != ""
		if once {
			var duplicate bool
			complete, duplicate = p.Submissions.Begin(e, p.SubmissionTTL)
			if duplicate {
				slog.Info("interaction: card already submitted", "platform", e.Platform, "message_id", e.MessageID, "user", e.UserID)
				if p.NotifyUser != nil {
					notifyCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
					defer cancel()
					if err := p.NotifyUser(notifyCtx, e, "已提交"); err != nil {
						slog.Warn("interaction: submission notice failed", "error", err)
					}
				}
				return nil
			}
		}
		// Enrichment/plugins may exhaust the worker budget. Queue delivery receives
		// a separate, bounded budget, and failures must reach the receipt owner.
		deliveryCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
		defer cancel()
		if err := p.Forward(deliveryCtx, e); err != nil {
			complete(err)
			slog.Error("interaction: delivery failed", "platform", e.Platform, "message_id", e.MessageID, "event_id", e.EventID, "error", err)
			return err
		}
		complete(nil)
		if once {
			updateCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
			text := SubmittedInteractionText(e)
			var err error
			if e.SubmissionMode == interaction.SubmissionPerUser {
				if p.NotifyUser != nil {
					err = p.NotifyUser(updateCtx, e, text)
				}
			} else if p.CloseCard != nil {
				err = p.CloseCard(updateCtx, e, InteractionSubmissionSummary(e))
			}
			cancel()
			if err != nil {
				slog.Warn("interaction: submitted card update failed", "platform", e.Platform, "message_id", e.MessageID, "error", err)
			}
		}
		slog.Info("interaction: fallback forwarded", "platform", e.Platform, "message_id", e.MessageID, "session_id", e.SessionID)
	}
	return nil
}

// FormatInteraction includes the complete structured snapshot as well as a
// readable summary. Raw JSON is data supplied by the card author/user.
func FormatInteraction(e interaction.Event) string {
	raw, err := json.MarshalIndent(e, "", "  ")
	if err != nil {
		raw = []byte(fmt.Sprintf("serialization failed: %v", err))
	}
	return fmt.Sprintf("[card_action]\n卡片获取状态: %s\n卡片内容:\n%s\n用户操作: %s\n操作人: %s\n消息 ID: %s\n完整交互数据（含原始卡片、回调和答案）:\n%s\n[/card_action]", e.CardStatus, e.CardText, e.Action, e.UserID, e.MessageID, raw)
}

func interactionValue(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	b, _ := json.Marshal(v)
	return string(b)
}
func cardLabel(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case map[string]any:
		for _, k := range []string{"text", "content", "title", "label"} {
			if s := cardLabel(x[k]); s != "" {
				return s
			}
		}
	}
	return ""
}

// EnrichInteractionCard supports nested Feishu card JSON and Slack Block Kit.
// Option values are scoped to their control; ambiguous mappings stay raw.
func EnrichInteractionCard(e *interaction.Event) {
	var root any
	decoder := json.NewDecoder(bytes.NewReader(e.RawCard))
	decoder.UseNumber()
	if decoder.Decode(&root) != nil {
		return
	}
	labels := map[string]map[string][]string{}
	var texts []string
	seen := map[string]bool{}
	add := func(control string, value any, label string) {
		if label == "" {
			return
		}
		if labels[control] == nil {
			labels[control] = map[string][]string{}
		}
		k := interactionValue(value)
		labels[control][k] = append(labels[control][k], label)
	}
	var walk func(any, string)
	walk = func(v any, control string) {
		switch x := v.(type) {
		case []any:
			for _, v := range x {
				walk(v, control)
			}
		case map[string]any:
			for _, k := range []string{"name", "action_id"} {
				if s, ok := x[k].(string); ok && s != "" {
					control = s
				}
			}
			if value, ok := x["value"]; ok {
				label := cardLabel(x["text"])
				if label == "" {
					label = cardLabel(x["label"])
				}
				add(control, value, label)
				// Feishu callback normalization unwraps a button's nested value.
				if button, ok := value.(map[string]any); ok {
					if nested, exists := button["value"]; exists {
						add(control, nested, label)
					}
				}
			}
			keys := make([]string, 0, len(x))
			for k := range x {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, k := range keys {
				v := x[k]
				switch k {
				case "text", "content", "title", "description", "placeholder", "label":
					if s := cardLabel(v); s != "" && !seen[s] {
						texts = append(texts, s)
						seen[s] = true
					}
				}
				walk(v, control)
			}
		}
	}
	walk(root, "")
	if len(texts) > 0 {
		e.CardText = strings.Join(texts, "\n")
	}
	for i := range e.Answers {
		a := &e.Answers[i]
		if a.Label != "" {
			continue
		}
		matches := labels[a.Control][interactionValue(a.Value)]
		if len(matches) == 0 {
			matches = labels[""][interactionValue(a.Value)]
		}
		if len(matches) > 0 {
			same := true
			for _, s := range matches {
				if s != matches[0] {
					same = false
				}
			}
			if same {
				a.Label = matches[0]
			}
		}
	}
}

// CardActionCommand returns only explicit platform command/navigation tokens.
func CardActionCommand(action string) (string, bool) {
	for _, prefix := range []string{"cmd:", "act:", "nav:"} {
		if strings.HasPrefix(action, prefix) {
			text := strings.TrimSpace(strings.TrimPrefix(action, prefix))
			return text, IsSlashCommand(text)
		}
	}
	return "", false
}

// InteractionMetadata bounds optional profile enrichment, preserving time for
// the actual queue operation. Platform loaders must honor the supplied context.
func InteractionMetadata(ctx context.Context, load func(context.Context) map[string]any) map[string]any {
	if load == nil {
		return nil
	}
	metadataCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	return load(metadataCtx)
}

// InteractionCommand shares registered-command semantics with normal messages.
// Unknown commands leave the original interaction intact for plugins/fallback.
type InteractionCommand struct {
	Routing  AgentRoutingConfig
	Commands agentkit.Commands
	Slash    SlashContext
	Metadata func(context.Context, string) map[string]any
	Reply    func(context.Context, string) error
}

func ProcessInteractionCommand(ctx context.Context, e interaction.Event, c InteractionCommand) bool {
	text, ok := CardActionCommand(e.Action)
	if !ok {
		return false
	}
	if c.Metadata != nil {
		c.Slash.Metadata = InteractionMetadata(ctx, func(ctx context.Context) map[string]any { return c.Metadata(ctx, text) })
	}
	out, err := ProcessPlatformSlash(ctx, c.Routing, c.Commands, c.Slash, text)
	if err == nil && out.Kind != SlashHandled {
		return false
	}
	if err != nil {
		out.Reply = fmt.Sprintf("命令执行失败: %v", err)
	}
	if out.Reply != "" && c.Reply != nil {
		if err := c.Reply(ctx, out.Reply); err != nil {
			slog.Warn("interaction: command reply failed", "platform", e.Platform, "message_id", e.MessageID, "error", err)
		}
	}
	return true
}
