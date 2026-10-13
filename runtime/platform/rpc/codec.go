package rpcplatform

// Pi RPC wire types consumed by Centaur PiHarness.
// One JSON object per line. Bool fields stay present when false.

type inboundCommand struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	ModelID  string `json:"modelId,omitempty"`
	Provider string `json:"provider,omitempty"`
	Level    string `json:"level,omitempty"`
	Message  string `json:"message,omitempty"`
}

type wireResponse struct {
	Type    string `json:"type"`
	ID      string `json:"id"`
	Command string `json:"command"`
	Success bool   `json:"success"`
	Data    any    `json:"data,omitempty"`
}

type stateData struct {
	SessionFile         string     `json:"sessionFile"`
	IsStreaming         bool       `json:"isStreaming"`
	PendingMessageCount int        `json:"pendingMessageCount"`
	SessionID           string     `json:"sessionId"`
	Model               stateModel `json:"model"`
}

type stateModel struct {
	ID       string `json:"id"`
	Provider string `json:"provider"`
	API      string `json:"api"`
	BaseURL  string `json:"baseUrl"`
}

type wireUpdate struct {
	Type                  string  `json:"type"`
	Message               wireID  `json:"message"`
	AssistantMessageEvent wireAME `json:"assistantMessageEvent"`
}

type wireID struct {
	Role      string `json:"role"`
	Timestamp int    `json:"timestamp"`
}

type wireAME struct {
	Type         string `json:"type"`
	ContentIndex int    `json:"contentIndex"`
	Delta        string `json:"delta,omitempty"`
}

type wireToolStart struct {
	Type       string         `json:"type"`
	ToolCallID string         `json:"toolCallId"`
	ToolName   string         `json:"toolName"`
	Args       map[string]any `json:"args"`
}

type wireToolEnd struct {
	Type       string     `json:"type"`
	ToolCallID string     `json:"toolCallId"`
	IsError    bool       `json:"isError"`
	Result     wireResult `json:"result"`
}

type wireResult struct {
	Content []wireText `json:"content"`
}

type wireText struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type wireMessageEnd struct {
	Type    string        `json:"type"`
	Message wireAssistant `json:"message"`
}

type wireAgentEnd struct {
	Type     string          `json:"type"`
	Messages []wireAssistant `json:"messages"`
}

type wireAssistant struct {
	Role       string     `json:"role"`
	Timestamp  int        `json:"timestamp"`
	Model      string     `json:"model"`
	StopReason string     `json:"stopReason"`
	Content    []any      `json:"content"`
	Usage      *wireUsage `json:"usage,omitempty"`
}

type wireUsage struct {
	Input       int `json:"input"`
	Output      int `json:"output"`
	CacheRead   int `json:"cacheRead"`
	CacheWrite  int `json:"cacheWrite"`
	TotalTokens int `json:"totalTokens"`
}

type textPart struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type thinkingPart struct {
	Type     string `json:"type"`
	Thinking string `json:"thinking"`
}

type toolCallPart struct {
	Type      string `json:"type"`
	ID        string `json:"id"`
	Name      string `json:"name"`
	Arguments any    `json:"arguments"`
}

type wireCentaurError struct {
	Type string `json:"type"`
	ID   string `json:"id"`
	Code string `json:"code"`
}

type wireAgentStart struct {
	Type string `json:"type"`
}
