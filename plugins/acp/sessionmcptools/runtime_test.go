package sessionmcptools

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/lengzhao/agentkit"
	capacp "github.com/lengzhao/agentkit/cap/acp"
	"github.com/lengzhao/agentkit/plugins/tools/deferred"
	"github.com/lengzhao/agentkit/runtime/rctx"
	"github.com/lengzhao/agentkit/runtime/tools"
	"github.com/lengzhao/agentkit/testing/agenttest"
	mcpclient "github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/client/transport"
	mcp "github.com/mark3labs/mcp-go/mcp"
)

type testRuntime struct{}

func (testRuntime) Visible(ctx context.Context) ([]agentkit.ToolSpec, error) {
	specs := []agentkit.ToolSpec{{Name: "search", Description: string(rctx.SessionIDFromContext(ctx)), InputSchema: agentkit.JSONSchema{Type: "object"}}}
	if _, ok := rctx.StateFrom(ctx).Get("revealed"); ok {
		specs = append(specs, agentkit.ToolSpec{Name: "target", Description: "revealed", InputSchema: agentkit.JSONSchema{Type: "object"}})
	}
	return specs, nil
}
func (testRuntime) Execute(ctx context.Context, call agentkit.ToolCall) (agentkit.ToolResult, error) {
	switch call.Name {
	case "search":
		rctx.StateFrom(ctx).Set("revealed", true)
	case "target":
		if _, ok := rctx.StateFrom(ctx).Get("revealed"); !ok {
			return agentkit.ToolResult{}, errors.New("not revealed")
		}
	}
	return agentkit.ResultFromCall(call, string(rctx.SessionIDFromContext(ctx))), nil
}
func runtimeContext(ctx context.Context, id string) context.Context {
	return rctx.ApplyEnvelopeToContext(ctx, agentkit.TurnEnvelope{Conversation: id, AgentID: "remote"})
}
func runtimeClient(t *testing.T, ctx context.Context, p capacp.SessionMCPProvider) *mcpclient.Client {
	t.Helper()
	specs, err := p.SessionMCPServers(ctx)
	if err != nil {
		t.Fatal(err)
	}
	c, err := mcpclient.NewStreamableHttpClient(specs[0].HTTP.URL, transport.WithHTTPHeaders(specs[0].HTTP.Headers))
	if err != nil {
		t.Fatal(err)
	}
	if err = c.Start(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	var init mcp.InitializeRequest
	init.Params.ProtocolVersion = "2025-03-26"
	init.Params.ClientInfo = mcp.Implementation{Name: "runtime-test", Version: "1"}
	if _, err = c.Initialize(ctx, init); err != nil {
		t.Fatal(err)
	}
	return c
}
func runtimeCall(t *testing.T, ctx context.Context, c *mcpclient.Client, name string) *mcp.CallToolResult {
	t.Helper()
	var req mcp.CallToolRequest
	req.Params.Name = name
	out, err := c.CallTool(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	return out
}
func TestRuntimeTurnDisclosureAndIsolation(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	p, err := New(Config{}, Deps{Runtime: testRuntime{}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { closeProvider(t, p) })
	lifecycle := p.(capacp.SessionMCPTurnProvider)
	a, endA, err := lifecycle.BeginMCPTurn(runtimeContext(ctx, "a"))
	if err != nil {
		t.Fatal(err)
	}
	defer endA()
	b, endB, err := lifecycle.BeginMCPTurn(runtimeContext(ctx, "b"))
	if err != nil {
		t.Fatal(err)
	}
	defer endB()
	ca, cb := runtimeClient(t, a, p), runtimeClient(t, b, p)
	if out := runtimeCall(t, ctx, ca, "search"); out.IsError {
		t.Fatalf("search: %+v", out)
	}
	list, err := ca.ListTools(ctx, mcp.ListToolsRequest{})
	if err != nil || len(list.Tools) != 2 {
		t.Fatalf("revealed: %+v %v", list, err)
	}
	list, err = cb.ListTools(ctx, mcp.ListToolsRequest{})
	if err != nil || len(list.Tools) != 1 || list.Tools[0].Description != "b" {
		t.Fatalf("isolation: %+v %v", list, err)
	}
	if out := runtimeCall(t, ctx, ca, "target"); out.IsError {
		t.Fatalf("target: %+v", out)
	}
	if err = endA(); err != nil {
		t.Fatal(err)
	}
	if out := runtimeCall(t, ctx, ca, "target"); !out.IsError {
		t.Fatal("ended turn executed")
	}
	_, endNext, err := lifecycle.BeginMCPTurn(runtimeContext(ctx, "a"))
	if err != nil {
		t.Fatal(err)
	}
	defer endNext()
	list, err = ca.ListTools(ctx, mcp.ListToolsRequest{})
	if err != nil || len(list.Tools) != 1 {
		t.Fatalf("turn reset: %+v %v", list, err)
	}
	if _, _, err = lifecycle.BeginMCPTurn(runtimeContext(ctx, "a")); err == nil {
		t.Fatal("overlapping turns accepted")
	}
}

type functionRuntime struct {
	visible func(context.Context) ([]agentkit.ToolSpec, error)
	execute func(context.Context, agentkit.ToolCall) (agentkit.ToolResult, error)
}

func (r functionRuntime) Visible(ctx context.Context) ([]agentkit.ToolSpec, error) {
	return r.visible(ctx)
}
func (r functionRuntime) Execute(ctx context.Context, c agentkit.ToolCall) (agentkit.ToolResult, error) {
	return r.execute(ctx, c)
}
func oneSpec(context.Context) ([]agentkit.ToolSpec, error) {
	return []agentkit.ToolSpec{{Name: "probe", Description: "probe", InputSchema: agentkit.JSONSchema{Type: "object"}}}, nil
}
func TestRuntimeErrorsAndCancellation(t *testing.T) {
	for _, kind := range []string{"deny", "timeout", "error", "abort", "cancel", "parent", "http"} {
		t.Run(kind, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			parent, cancelParent := context.WithCancel(ctx)
			defer cancelParent()
			started := make(chan struct{})
			rt := functionRuntime{visible: oneSpec, execute: func(exec context.Context, call agentkit.ToolCall) (agentkit.ToolResult, error) {
				switch kind {
				case "abort":
					return agentkit.ToolResult{}, agentkit.AbortTurn(errors.New("approval infrastructure failed"))
				case "cancel":
					return agentkit.ToolResult{}, context.Canceled
				case "error":
					return agentkit.ToolResult{}, errors.New("diagnostic from runtime")
				case "parent", "http":
					close(started)
					<-exec.Done()
					return agentkit.ResultFromCall(call, "ignored cancellation"), nil
				default:
					return agentkit.ToolResult{Content: "diagnostic from runtime", Audit: map[string]string{"decision": kind}}, nil
				}
			}}
			p, err := New(Config{}, Deps{Runtime: rt})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { closeProvider(t, p) })
			turn, end, err := p.(capacp.SessionMCPTurnProvider).BeginMCPTurn(runtimeContext(parent, "errors"))
			if err != nil {
				t.Fatal(err)
			}
			defer end()
			c := runtimeClient(t, turn, p)
			callCtx, cancelCall := context.WithCancel(ctx)
			defer cancelCall()
			done := make(chan error, 1)
			go func() {
				var req mcp.CallToolRequest
				req.Params.Name = "probe"
				out, err := c.CallTool(callCtx, req)
				if err == nil && !out.IsError {
					err = errors.New("failure mapped to success")
				}
				done <- err
			}()
			if kind == "parent" || kind == "http" {
				select {
				case <-started:
				case <-ctx.Done():
					t.Fatal(ctx.Err())
				}
				if kind == "parent" {
					cancelParent()
				} else {
					cancelCall()
				}
			}
			select {
			case err := <-done:
				if err != nil && kind != "http" {
					t.Fatal(err)
				}
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			endErr := end()
			if (kind == "abort" || kind == "cancel" || kind == "parent") && endErr == nil {
				t.Fatal("turn-fatal error lost")
			}
		})
	}
}

func TestRuntimeCatalogChanges(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	specs, _ := oneSpec(ctx)
	rt := functionRuntime{visible: func(context.Context) ([]agentkit.ToolSpec, error) { return specs, nil }, execute: func(_ context.Context, c agentkit.ToolCall) (agentkit.ToolResult, error) {
		return agentkit.ResultFromCall(c, "ok"), nil
	}}
	p, err := New(Config{}, Deps{Runtime: rt})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { closeProvider(t, p) })
	turn, end, err := p.(capacp.SessionMCPTurnProvider).BeginMCPTurn(runtimeContext(ctx, "catalog"))
	if err != nil {
		t.Fatal(err)
	}
	defer end()
	c := runtimeClient(t, turn, p)
	scope, _ := p.(*Provider).scope(turn, false)
	old := scope.catalog
	specs[0].Description = "changed"
	specs[0].InputSchema = agentkit.JSONSchema{Type: "object", Raw: map[string]any{"type": "object", "properties": map[string]any{"x": map[string]any{"type": "string"}}}}
	if err = p.(*Provider).Refresh(turn); err != nil {
		t.Fatal(err)
	}
	list, err := c.ListTools(ctx, mcp.ListToolsRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if len(list.Tools) != 1 || list.Tools[0].Description != "changed" || scope.catalog == old {
		t.Fatalf("schema/description unchanged: %+v", list)
	}
	specs = nil
	if err = p.(*Provider).Refresh(turn); err != nil {
		t.Fatal(err)
	}
	list, err = c.ListTools(ctx, mcp.ListToolsRequest{})
	if err != nil || len(list.Tools) != 0 {
		t.Fatalf("removed tool remains: %+v %v", list, err)
	}
}

func TestRuntimeRejectsBothBackends(t *testing.T) {
	if p, err := New(Config{}, Deps{Provider: stubProvider(), Runtime: testRuntime{}}); err == nil {
		closeProvider(t, p)
		t.Fatal("ambiguous backends accepted")
	}
}

func TestRuntimeConnectionCanBePreparedBeforeTurn(t *testing.T) {
	ctx, cancel := context.WithTimeout(runtimeContext(context.Background(), "prewarm"), 5*time.Second)
	defer cancel()
	p, err := New(Config{}, Deps{Runtime: testRuntime{}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { closeProvider(t, p) })
	c := runtimeClient(t, ctx, p)
	if list, err := c.ListTools(ctx, mcp.ListToolsRequest{}); err != nil || len(list.Tools) != 0 {
		t.Fatalf("prewarmed catalog: %+v %v", list, err)
	}
	_, end, err := p.(capacp.SessionMCPTurnProvider).BeginMCPTurn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer end()
	if out := runtimeCall(t, ctx, c, "search"); out.IsError {
		t.Fatalf("prewarmed connection failed: %+v", out)
	}
}

type notificationProbe struct {
	id string
	ch chan mcp.JSONRPCNotification
}

func (n *notificationProbe) Initialize()                                         {}
func (n *notificationProbe) Initialized() bool                                   { return true }
func (n *notificationProbe) SessionID() string                                   { return n.id }
func (n *notificationProbe) NotificationChannel() chan<- mcp.JSONRPCNotification { return n.ch }
func expectNotification(t *testing.T, n *notificationProbe, changed bool) {
	t.Helper()
	select {
	case msg := <-n.ch:
		if !changed || msg.Method != "notifications/tools/list_changed" {
			t.Fatalf("unexpected notification: %+v", msg)
		}
	default:
		if changed {
			t.Fatal("missing list_changed")
		}
	}
}
func TestProviderRefreshNotifiesOnlyOnChange(t *testing.T) {
	source := &stubProviderImpl{tools: []agentkit.Tool{stubTool{"one"}}}
	p, err := New(Config{}, Deps{Provider: source})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { closeProvider(t, p) })
	n := &notificationProbe{id: "notify", ch: make(chan mcp.JSONRPCNotification, 10)}
	if err = p.(*Provider).bridge.mcp.RegisterSession(context.Background(), n); err != nil {
		t.Fatal(err)
	}
	if err = p.(*Provider).Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	expectNotification(t, n, false)
	source.tools = []agentkit.Tool{stubTool{"two"}}
	if err = p.(*Provider).Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	expectNotification(t, n, true)
	expectNotification(t, n, false)
}
func TestRuntimeRefreshNotificationsAreScopedAndIdempotent(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	specs, _ := oneSpec(ctx)
	fail := false
	rt := functionRuntime{visible: func(context.Context) ([]agentkit.ToolSpec, error) {
		if fail {
			return nil, errors.New("discovery failed")
		}
		return specs, nil
	}, execute: func(_ context.Context, c agentkit.ToolCall) (agentkit.ToolResult, error) {
		return agentkit.ResultFromCall(c, "ok"), nil
	}}
	p, err := New(Config{}, Deps{Runtime: rt})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { closeProvider(t, p) })
	a, endA, err := p.(capacp.SessionMCPTurnProvider).BeginMCPTurn(runtimeContext(ctx, "a"))
	if err != nil {
		t.Fatal(err)
	}
	defer endA()
	b, endB, err := p.(capacp.SessionMCPTurnProvider).BeginMCPTurn(runtimeContext(ctx, "b"))
	if err != nil {
		t.Fatal(err)
	}
	defer endB()
	sa, _ := p.(*Provider).scope(a, false)
	sb, _ := p.(*Provider).scope(b, false)
	na := &notificationProbe{id: "a", ch: make(chan mcp.JSONRPCNotification, 10)}
	nb := &notificationProbe{id: "b", ch: make(chan mcp.JSONRPCNotification, 10)}
	if err = sa.mcp.RegisterSession(ctx, na); err != nil {
		t.Fatal(err)
	}
	if err = sb.mcp.RegisterSession(ctx, nb); err != nil {
		t.Fatal(err)
	}
	if err = p.(*Provider).Refresh(a); err != nil {
		t.Fatal(err)
	}
	expectNotification(t, na, false)
	specs[0].Description = "new description"
	if err = p.(*Provider).Refresh(a); err != nil {
		t.Fatal(err)
	}
	expectNotification(t, na, true)
	expectNotification(t, nb, false)
	if err = p.(*Provider).Refresh(a); err != nil {
		t.Fatal(err)
	}
	expectNotification(t, na, false)
	fail = true
	before := sa.catalog
	if err = p.(*Provider).Refresh(a); err == nil {
		t.Fatal("discovery error lost")
	}
	if sa.catalog != before {
		t.Fatal("failed refresh changed catalog")
	}
	expectNotification(t, na, false)
}
func TestRealDeferredRuntimeOverHTTP(t *testing.T) {
	for _, bridge := range []string{"tool_search", "tool_describe"} {
		t.Run(bridge, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			rt, err := deferred.New(deferred.Config{DisclosureConfig: deferred.DisclosureConfig{Enabled: deferred.EnabledOn}}, tools.RuntimeDeps{Tools: []agentkit.Tool{stubTool{"read"}}, DynamicTools: []agentkit.ToolProvider{stubProvider(stubTool{"mcp__ping"})}})
			if err != nil {
				t.Fatal(err)
			}
			p, err := New(Config{}, Deps{Runtime: rt})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { closeProvider(t, p) })
			turn, end, err := p.(capacp.SessionMCPTurnProvider).BeginMCPTurn(runtimeContext(ctx, "deferred"))
			if err != nil {
				t.Fatal(err)
			}
			defer end()
			c := runtimeClient(t, turn, p)
			var req mcp.CallToolRequest
			req.Params.Name = bridge
			if bridge == "tool_search" {
				req.Params.Arguments = map[string]any{"queries": []string{"ping"}}
			} else {
				req.Params.Arguments = map[string]any{"names": []string{"mcp__ping"}}
			}
			out, err := c.CallTool(ctx, req)
			if err != nil || out.IsError {
				t.Fatalf("discovery: %+v %v", out, err)
			}
			list, err := c.ListTools(ctx, mcp.ListToolsRequest{})
			if err != nil {
				t.Fatal(err)
			}
			found := false
			for _, spec := range list.Tools {
				found = found || spec.Name == "mcp__ping"
			}
			if !found {
				t.Fatalf("not revealed: %+v", list.Tools)
			}
			if out = runtimeCall(t, ctx, c, "mcp__ping"); out.IsError {
				t.Fatalf("direct call: %+v", out)
			}
		})
	}
}
func TestRealRuntimePolicyAndApprovalAreEnforced(t *testing.T) {
	for _, ask := range []bool{false, true} {
		t.Run(map[bool]string{false: "deny", true: "approval rejection"}[ask], func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			called := false
			tool, err := agentkit.NewTool[struct{}, string]("probe", func(context.Context, struct{}) (string, error) { called = true; return "bypassed", nil }).Build()
			if err != nil {
				t.Fatal(err)
			}
			policy := agenttest.DenyAllToolsPolicy("blocked")
			if ask {
				policy = agenttest.AskAllToolsPolicy("approval")
			}
			rt, err := tools.NewRuntime(tools.RuntimeConfig{}, tools.RuntimeDeps{Tools: []agentkit.Tool{tool}, Policies: []agentkit.Policy{policy}, Approval: agenttest.DenyAll{}})
			if err != nil {
				t.Fatal(err)
			}
			p, err := New(Config{}, Deps{Runtime: rt})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { closeProvider(t, p) })
			turn, end, err := p.(capacp.SessionMCPTurnProvider).BeginMCPTurn(runtimeContext(ctx, "policy"))
			if err != nil {
				t.Fatal(err)
			}
			defer end()
			if out := runtimeCall(t, ctx, runtimeClient(t, turn, p), "probe"); !out.IsError || called {
				t.Fatalf("policy bypassed: %+v called=%v", out, called)
			}
		})
	}
}

func TestRuntimeReservedHeaderAndConnectionOwnership(t *testing.T) {
	ctx, cancel := context.WithTimeout(runtimeContext(context.Background(), "same-session"), 5*time.Second)
	defer cancel()
	p, err := New(Config{Headers: map[string]string{"x-agentkit-mcp-scope": "stale"}}, Deps{Runtime: testRuntime{}})
	if err != nil {
		t.Fatal(err)
	}
	defer closeProvider(t, p)
	oldCtx, oldEnd, err := p.(capacp.SessionMCPTurnProvider).BeginMCPTurn(capacp.WithSessionMCPBinding(ctx, "old"))
	if err != nil {
		t.Fatal(err)
	}
	if out := runtimeCall(t, ctx, runtimeClient(t, oldCtx, p), "search"); out.IsError {
		t.Fatal(out)
	}
	if err = oldEnd(); err != nil {
		t.Fatal(err)
	}
	newCtx, newEnd, err := p.(capacp.SessionMCPTurnProvider).BeginMCPTurn(capacp.WithSessionMCPBinding(ctx, "new"))
	if err != nil {
		t.Fatal(err)
	}
	defer newEnd()
	c := runtimeClient(t, newCtx, p)
	p.(*Provider).ReleaseMCPSession(oldCtx)
	if out := runtimeCall(t, ctx, c, "search"); out.IsError {
		t.Fatalf("old cleanup revoked new owner: %+v", out)
	}
}

func TestRuntimeConnectionTracksWorkspaceChanges(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	rt := functionRuntime{visible: oneSpec, execute: func(exec context.Context, c agentkit.ToolCall) (agentkit.ToolResult, error) {
		return agentkit.ResultFromCall(c, rctx.WorkspaceFromContext(exec)), nil
	}}
	p, err := New(Config{}, Deps{Runtime: rt})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { closeProvider(t, p) })
	old := capacp.WithSessionMCPBinding(rctx.ApplyEnvelopeToContext(ctx, agentkit.TurnEnvelope{Conversation: "same", AgentID: "remote", Workspace: "old"}), "bridge")
	turn, end, err := p.(capacp.SessionMCPTurnProvider).BeginMCPTurn(old)
	if err != nil {
		t.Fatal(err)
	}
	c := runtimeClient(t, turn, p)
	if err = end(); err != nil {
		t.Fatal(err)
	}
	next := capacp.WithSessionMCPBinding(rctx.ApplyEnvelopeToContext(ctx, agentkit.TurnEnvelope{Conversation: "same", AgentID: "remote", Workspace: "new"}), "bridge")
	_, endNext, err := p.(capacp.SessionMCPTurnProvider).BeginMCPTurn(next)
	if err != nil {
		t.Fatal(err)
	}
	defer endNext()
	out := runtimeCall(t, ctx, c, "probe")
	if out.IsError {
		t.Fatalf("workspace change lost connection: %+v", out)
	}
	text, ok := out.Content[0].(mcp.TextContent)
	if !ok || text.Text != "new" {
		t.Fatalf("stale workspace: %+v", out)
	}
}
