// Package learning defines injectable boundaries for learning/default and related plugins.
//
// learning/default (*plugins/learning.Service) implements SkillProposer, ReviewHost,
// and DreamSweepScheduler. Config deps use json key "learning".
//
// memory/default implements cap/memory.Capture; hook/background-review deps both learning and memory.
//
// hook/background-review requires ReviewHost + cap/memory.Capture (learn_capture is built in-hook).
// learning/dream-sweep requires DreamSweepScheduler.
package learning
