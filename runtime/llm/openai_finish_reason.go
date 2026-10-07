package llm

import (
	"github.com/lengzhao/agentkit"
	openai "github.com/sashabaranov/go-openai"
)

func assistantStopReasonFromOpenAI(reason openai.FinishReason) string {
	switch reason {
	case openai.FinishReasonLength:
		return agentkit.AssistantStopReasonLength
	default:
		return string(reason)
	}
}
