// Package memory defines injectable boundaries for memory/default and related plugins.
//
// memory/default (*plugins/memory.Service) implements Service, Tool, Capture, Reader, Staging,
// and CommitObserverRegistrar (learning/default registers CommitObserver in New).
//
// tool/memory requires cap/memory.Tool (typically memory.default).
// prompt/section/memory requires cap/memory.Reader.
// hook/background-review requires cap/memory.Capture (typically memory.default).
package memory
