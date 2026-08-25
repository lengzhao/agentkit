package approval

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/lengzhao/agentkit"
)

// NewCLI registers approval/cli: Prompt on stderr for y/n before a gated tool call.
//
// Best practices:
//   - Blocks forever without a terminal; never use it in a worker or timer preset.
func NewCLI() (agentkit.Approval, error) {
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
