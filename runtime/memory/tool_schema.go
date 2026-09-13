package memory

import (
	"strings"

	capmemory "github.com/lengzhao/agentkit/cap/memory"
)

type MemoryToolInput = capmemory.MemoryToolInput
type MemoryToolOutput = capmemory.MemoryToolOutput

// MemoryToolDescription is the main-agent memory tool prompt (aligned with Hermes WHEN/HOW/SKIP, adapted for memory.md + per-turn prompt freeze).
const MemoryToolDescription = `Save durable facts to memory.md (global + tenant local, merged for prompts) that survive across sessions. Keep entries compact and high-signal. There is no read action — tool responses show live entries and usage; the memory prompt section is a frozen snapshot for this turn (new writes appear in the system prompt on the next user turn).

WHEN — call in the same turn; do not only acknowledge verbally:
- Stable facts for every session: environment, standing conventions, tool quirks
- User identity, preferences, communication style (name, tone, reply format — e.g. prefix every assistant reply with a name)
- User corrections and explicit requests: "remember", "don't forget", "from now on always…"

SKIP: trivia, one-off debugging, secrets, raw dumps, task progress. Reusable procedures for a kind of work belong in skills (skill tools / /learn skill), not memory.md — memory is injected every turn and must stay small.

HOW: action add | replace | remove. For replace/remove, old_text is a short unique substring of exactly one §-delimited entry. If add fails at capacity, remove or replace stale entries then add again in the same turn until it succeeds.`

func NormalizeMemoryToolAction(action string) string {
	return strings.ToLower(strings.TrimSpace(action))
}
