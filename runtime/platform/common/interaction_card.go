package common

import (
	"fmt"

	"github.com/lengzhao/agentkit/cap/permission"
)

// PermissionCardFromPayload builds a card from Loop permission/request payload.
func PermissionCardFromPayload(payload permission.RequestPayload) *Card {
	switch payload.Kind {
	case permission.KindAllowDeny:
		return allowDenyCard(payload)
	case permission.KindQuestion:
		return questionCard(payload)
	default:
		return NewCard().Title("需要处理", "blue").Markdown(payload.Reason).Build()
	}
}

func questionCard(payload permission.RequestPayload) *Card {
	if payload.Question == nil {
		return NewCard().Title("问题", "blue").Note("缺少问题内容").Build()
	}
	q := payload.Question
	title := "问题"
	if q.Header != "" {
		title = q.Header
	}
	b := NewCard().Title(title, "blue").Markdown(q.Prompt)
	if len(q.Options) == 0 {
		return b.Note("可直接回复文字").Build()
	}
	for i, opt := range q.Options {
		label := opt.Label
		if label == "" {
			label = fmt.Sprintf("选项 %d", i+1)
		}
		desc := label
		if opt.Description != "" {
			desc += "\n" + opt.Description
		}
		extra := map[string]string{
			"request_id":  payload.ID,
			"answer_text": label,
			"selected":    fmt.Sprintf("%d", i),
			"askq_label":  label,
			"askq_question": q.Prompt,
		}
		b.ListItemBtnExtra(desc, label, "default", PermissionActionValue(i), extra)
	}
	return b.Build()
}

func allowDenyCard(payload permission.RequestPayload) *Card {
	title := "需要确认"
	reason := payload.Reason
	if reason == "" {
		reason = "是否允许执行该操作？"
	}
	if payload.ToolCall != nil && payload.ToolCall.Name != "" {
		title = "确认工具调用：" + payload.ToolCall.Name
	}
	b := NewCard().Title(title, "orange").Markdown(reason)
	permExtra := func(label, color, decision string) map[string]string {
		return map[string]string{
			"request_id": payload.ID,
			"decision":   decision,
			"perm_label": label,
			"perm_color": color,
			"perm_body":  reason,
		}
	}
	b.ListItemBtnExtra("允许", "允许", "primary", PermissionDecisionValue("allow"), permExtra("✅ 允许", "green", "allow"))
	b.ListItemBtnExtra("拒绝", "拒绝", "danger", PermissionDecisionValue("deny"), permExtra("❌ 拒绝", "red", "deny"))
	return b.Build()
}

