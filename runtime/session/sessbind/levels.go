package sessbind

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/cap/workspace"
	"github.com/lengzhao/agentkit/runtime/rctx"
)

// Level is one rung of the dynamic-binding stack, coarse to fine. Thread and
// User are mutually exclusive siblings: a delivery id carries :t: or :u:, not
// both, so at most one of them is ever present in a turn's BindKeys.
type Level int

const (
	LevelGlobal Level = iota
	LevelChannel
	LevelThread
	LevelUser
	LevelSession
)

// LevelNone marks "no level set a value".
const LevelNone Level = -1

func (l Level) String() string {
	switch l {
	case LevelNone:
		return "none"
	case LevelGlobal:
		return "global"
	case LevelChannel:
		return "channel"
	case LevelThread:
		return "thread"
	case LevelUser:
		return "user"
	case LevelSession:
		return "session"
	default:
		return fmt.Sprintf("level(%d)", int(l))
	}
}

// Priority lists every level coarse-to-fine. Resolution walks it in reverse so
// finer scopes win. It is the single ordering shared by every binding kind
// (agent, model, …), replacing per-kind if-chains.
var Priority = []Level{LevelGlobal, LevelChannel, LevelThread, LevelUser, LevelSession}

// Binding identifies one dynamic-override kind. Kinds share the level stack,
// the key computation (rctx.BindKeys) and the priority order; only the value
// type and per-kind storage differ.
type Binding interface {
	// Key is a stable identifier for the kind ("agent", "model"), used in
	// messages and storage keys.
	Key() string
	// Read returns the value stored at level for key, or "" when unset.
	Read(ctx context.Context, level Level, key agentkit.SessionID, ws workspace.Service, store agentkit.SessionStore) (string, error)
	// Write stores value at level for key ("" clears).
	Write(ctx context.Context, level Level, key agentkit.SessionID, ws workspace.Service, store agentkit.SessionStore, value string) error
}

// BindStack is the keys a single resolution reads across every level.
type BindStack struct {
	Keys rctx.BindKeys
	WS   workspace.Service
}

// Effective resolves value across Priority (fine wins), returning the winner and
// the raw hit at every level (named in the map so callers can render "session
// override / channel override / …" uniformly). A level whose key is empty
// (e.g. Thread on a non-topic delivery) is skipped, not treated as unset.
func (s BindStack) Effective(ctx context.Context, store agentkit.SessionStore, kind Binding) (effective string, hits map[Level]string, err error) {
	effective, _, hits, err = s.EffectiveWithLevel(ctx, store, kind)
	return effective, hits, err
}

// EffectiveWithLevel is Effective plus the winning level (LevelNone when nothing
// is set), so callers can report which scope actually won instead of guessing
// from a non-empty finer hit.
func (s BindStack) EffectiveWithLevel(ctx context.Context, store agentkit.SessionStore, kind Binding) (effective string, winner Level, hits map[Level]string, err error) {
	winner = LevelNone
	hits = map[Level]string{}
	for _, level := range Priority {
		key, ok := s.keyFor(level)
		if !ok {
			continue
		}
		val, err := kind.Read(ctx, level, key, s.WS, store)
		if err != nil {
			return "", LevelNone, nil, err
		}
		val = strings.TrimSpace(val)
		if val == "" {
			continue
		}
		hits[level] = val
		effective = val
		winner = level
	}
	return effective, winner, hits, nil
}

// Set writes value at level, resolving the level's key from the stack.
func (s BindStack) Set(ctx context.Context, store agentkit.SessionStore, kind Binding, level Level, value string) error {
	key, ok := s.keyFor(level)
	if !ok {
		switch level {
		case LevelGlobal:
			// global needs no route key
		default:
			return fmt.Errorf("%s binding at %s level requires a delivery key", kind.Key(), level)
		}
	}
	return kind.Write(ctx, level, key, s.WS, store, value)
}

// keyFor returns the resolution key for a level; ok=false when the level has no
// key in this stack (Session may be empty before a conversation is resolved).
func (s BindStack) keyFor(level Level) (agentkit.SessionID, bool) {
	switch level {
	case LevelGlobal:
		return "", true
	case LevelChannel:
		return s.Keys.Channel, s.Keys.Channel != ""
	case LevelThread:
		return s.Keys.Thread, s.Keys.Thread != ""
	case LevelUser:
		return s.Keys.User, s.Keys.User != ""
	case LevelSession:
		return s.Keys.Session, s.Keys.Session != ""
	default:
		return "", false
	}
}

// ---- agent kind ----

// AgentBinding is the Binding for the default-agent override.
type AgentBinding struct{}

func (AgentBinding) Key() string { return "agent" }

func (AgentBinding) Read(ctx context.Context, level Level, key agentkit.SessionID, ws workspace.Service, store agentkit.SessionStore) (string, error) {
	switch level {
	case LevelGlobal:
		id, err := GlobalAgentBind(ctx, ws)
		return string(id), err
	case LevelChannel:
		id, err := ChannelAgentBind(ctx, store, ws, key)
		return string(id), err
	case LevelThread, LevelUser, LevelSession:
		id, err := AgentBind(ctx, store, scopedBindKey(level, "agent", key))
		return string(id), err
	default:
		return "", nil
	}
}

func (AgentBinding) Write(ctx context.Context, level Level, key agentkit.SessionID, ws workspace.Service, store agentkit.SessionStore, value string) error {
	switch level {
	case LevelGlobal:
		return SetGlobalAgentBind(ctx, ws, agentkit.AgentID(value))
	case LevelChannel:
		return SetChannelAgentBind(ctx, store, ws, key, agentkit.AgentID(value))
	case LevelThread, LevelUser, LevelSession:
		return SetSessionAgentBind(ctx, store, scopedBindKey(level, "agent", key), agentkit.AgentID(value))
	default:
		return errors.New("unsupported agent binding level")
	}
}

// ---- model kind ----

// ModelBinding is the Binding for the per-session default-model override. The
// global level is per-agent (models[<agentId>] in the workspace runtime file),
// so it carries its own key handling.
type ModelBinding struct{ AgentID agentkit.AgentID }

func (ModelBinding) Key() string { return "model" }

func (b ModelBinding) Read(ctx context.Context, level Level, key agentkit.SessionID, ws workspace.Service, store agentkit.SessionStore) (string, error) {
	switch level {
	case LevelGlobal:
		if ws == nil || b.AgentID == "" {
			return "", nil
		}
		return GlobalModelBind(ctx, ws, b.AgentID)
	case LevelChannel:
		return ChannelModelBind(ctx, store, key)
	case LevelThread, LevelUser, LevelSession:
		return ModelBind(ctx, store, scopedBindKey(level, "model", key))
	default:
		return "", nil
	}
}

func (b ModelBinding) Write(ctx context.Context, level Level, key agentkit.SessionID, ws workspace.Service, store agentkit.SessionStore, value string) error {
	switch level {
	case LevelGlobal:
		if b.AgentID == "" {
			return errors.New("agent id is required for global model binding")
		}
		return SetGlobalModelBind(ctx, ws, b.AgentID, value)
	case LevelChannel:
		return SetChannelModelBind(ctx, store, key, value)
	case LevelThread, LevelUser, LevelSession:
		return SetSessionModelBind(ctx, store, scopedBindKey(level, "model", key), value)
	default:
		return errors.New("unsupported model binding level")
	}
}

// StackFromContext builds the level stack for the current turn: keys from
// rctx.BindKeysFromContext plus the workspace handle.
func StackFromContext(ctx context.Context, ws workspace.Service) BindStack {
	return BindStack{Keys: rctx.BindKeysFromContext(ctx), WS: ws}
}

// StackWithKeys overrides stack keys, e.g. an explicit session id or a
// caller-provided channel entry key.
func (s BindStack) WithSession(session agentkit.SessionID) BindStack {
	s.Keys.Session = session
	return s
}

// WithChannel overrides the channel key when non-empty (legacy callers pass an
// explicit entry key; the turn-derived channel key is kept otherwise).
func (s BindStack) WithChannel(channel agentkit.SessionID) BindStack {
	if channel != "" {
		s.Keys.Channel = channel
	}
	return s
}

// HitsFor renders non-empty hits in Priority order (fine first) for uniform
// "override" listings across binding kinds.
func HitsFor(hits map[Level]string) []Level {
	var out []Level
	for i := len(Priority) - 1; i >= 0; i-- {
		if strings.TrimSpace(hits[Priority[i]]) != "" {
			out = append(out, Priority[i])
		}
	}
	return out
}

// scopedBindKey keeps persistent thread/user defaults separate from the initial
// session, whose ID can equal the route key. Session keys retain their existing
// format so existing per-session overrides stay session-local.
func scopedBindKey(level Level, kind string, key agentkit.SessionID) agentkit.SessionID {
	if key == "" {
		return ""
	}
	switch level {
	case LevelThread, LevelUser:
		return agentkit.SessionID(level.String() + "-" + kind + ":" + string(key))
	default:
		return key
	}
}
