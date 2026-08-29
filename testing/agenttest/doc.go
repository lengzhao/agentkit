// Package agenttest provides shared helpers for AgentKit tests.
//
// Tests should still assemble plugins through pluginkit/build; this package
// only removes duplicated wiring for scripted LLM runs, session assertions, and
// tool calls. See docs/guides/testing.zh.md for the full pyramid and CI tiers.
package agenttest
