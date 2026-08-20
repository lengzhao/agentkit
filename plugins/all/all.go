package all

import (
	_ "github.com/lengzhao/agentkit/plugins/agentsmd"
	_ "github.com/lengzhao/agentkit/plugins/approvalautodeny"
	_ "github.com/lengzhao/agentkit/plugins/approvalcli"
	_ "github.com/lengzhao/agentkit/plugins/dangerousshell"
	_ "github.com/lengzhao/agentkit/plugins/fslocal"
	_ "github.com/lengzhao/agentkit/plugins/fsmemory"
	_ "github.com/lengzhao/agentkit/plugins/readfile"
	_ "github.com/lengzhao/agentkit/plugins/shellbash"
	_ "github.com/lengzhao/agentkit/plugins/shelltool"
	_ "github.com/lengzhao/agentkit/plugins/writefile"
	_ "github.com/lengzhao/agentkit/runtime/agent"
	_ "github.com/lengzhao/agentkit/runtime/llm"
	_ "github.com/lengzhao/agentkit/runtime/loop"
	_ "github.com/lengzhao/agentkit/runtime/prompt"
	_ "github.com/lengzhao/agentkit/runtime/runner"
	_ "github.com/lengzhao/agentkit/runtime/session"
	_ "github.com/lengzhao/agentkit/runtime/tools"
)
