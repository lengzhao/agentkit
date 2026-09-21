// Package memory defines injectable boundaries for memory/default and related plugins.
//
// memory/default (*plugins/memory.Service) implements Service, Tool, Capture, Reader,
// and CommitObserverRegistrar (learning/default registers CommitObserver in New).
//
// tool/memory requires cap/memory.Tool (typically memory.default).
// prompt/section/memory requires cap/memory.Reader.
// hook/background-review requires cap/memory.Capture (typically memory.default).
// learning/default uses Reader.PreviewAddOutcome for dreaming signal dedup against memory.md.
package memory
