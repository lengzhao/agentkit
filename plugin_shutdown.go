package agentkit

// ShutdownHookProvider contributes process-shutdown hooks. The runner collects
// providers from the build graph and invokes their hooks on shutdown, so
// plugins with background goroutines (for example hook/background-review) can
// stop in-flight work without the runner importing them.
type ShutdownHookProvider interface {
	// ShutdownHooks returns functions called once when the runner shuts down.
	// Hooks must be non-blocking: cancel contexts, do not wait for completion.
	ShutdownHooks() []func()
}
