package deferred

const (
	ToolSearch   = "tool_search"
	ToolList     = "tool_list"
	ToolDescribe = "tool_describe"
	ToolCall     = "tool_call"
)

const charsPerToken = 4

var bridgeNames = map[string]bool{
	ToolSearch:   true,
	ToolList:     true,
	ToolDescribe: true,
	ToolCall:     true,
}

func IsBridge(name string) bool {
	return bridgeNames[name]
}
