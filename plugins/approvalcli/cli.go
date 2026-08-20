package approvalcli

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/pluginkit"
)

func init() {
	pluginkit.Register("approval/cli", New)
}

func New() (agentkit.Approval, error) {
	return &CLI{}, nil
}

type CLI struct{}

func (c *CLI) Ask(_ context.Context, req agentkit.ApprovalRequest) (agentkit.ApprovalDecision, error) {
	tool := ""
	if req.ToolCall != nil {
		tool = req.ToolCall.Name
	}
	fmt.Fprintf(os.Stderr, "Approval required for tool %q: %s [y/N] ", tool, req.Reason)
	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil {
		return agentkit.ApprovalDecision{}, err
	}
	allowed := strings.EqualFold(strings.TrimSpace(line), "y")
	return agentkit.ApprovalDecision{Allowed: allowed, Reason: req.Reason}, nil
}
