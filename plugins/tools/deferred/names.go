package deferred

const (
	ToolSearch  = "tool_search"
	ToolDescribe = "tool_describe"
	ToolCall    = "tool_call"
)

const charsPerToken = 4

var bridgeNames = map[string]bool{
	ToolSearch:  true,
	ToolDescribe: true,
	ToolCall:    true,
}

func IsBridge(name string) bool {
	return bridgeNames[name]
}
