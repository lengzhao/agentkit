package learning

import (
	"context"
	"fmt"

	capmemory "github.com/lengzhao/agentkit/cap/memory"
	"github.com/lengzhao/agentkit/cap/workspace"
	rtmem "github.com/lengzhao/agentkit/runtime/memory"
)

type testMemoryStub struct {
	ws workspace.Service
}

func newTestMemoryStub(ws workspace.Service) capmemory.Service {
	return &testMemoryStub{ws: ws}
}

func (m *testMemoryStub) Disabled() bool { return false }

func (m *testMemoryStub) BackgroundReviewRequiresStaging(context.Context) bool { return false }

func (m *testMemoryStub) ResolveRel(ctx context.Context, parts ...string) (string, error) {
	rel := rtmem.JoinUnderRoot(".", parts...)
	return m.ws.Resolve(ctx, rel)
}

func (m *testMemoryStub) LoadEntries(ctx context.Context) ([]capmemory.MemoryEntry, int, int, error) {
	return nil, 0, 2200, nil
}

func (m *testMemoryStub) PromptBody(ctx context.Context) (string, error) {
	return "", nil
}

func (m *testMemoryStub) MemoryTool(ctx context.Context, in capmemory.MemoryToolInput) (capmemory.MemoryToolOutput, error) {
	return capmemory.MemoryToolOutput{}, fmt.Errorf("stub memory tool")
}

func (m *testMemoryStub) CaptureMemoryAdd(ctx context.Context, text, source string) (string, error) {
	return "", fmt.Errorf("stub capture")
}

func (m *testMemoryStub) CaptureMemoryReplace(ctx context.Context, oldText, content, source string) (string, error) {
	return "", fmt.Errorf("stub capture")
}

func (m *testMemoryStub) CaptureMemoryRemove(ctx context.Context, oldText string) (string, error) {
	return "", fmt.Errorf("stub capture")
}

func (m *testMemoryStub) ListStaged(ctx context.Context) ([]capmemory.StagedEntry, error) {
	return nil, nil
}

func (m *testMemoryStub) ApproveStaged(ctx context.Context, id string) (string, error) {
	return "", fmt.Errorf("stub staging")
}

func (m *testMemoryStub) RejectStaged(ctx context.Context, id string) (string, error) {
	return "", fmt.Errorf("stub staging")
}
