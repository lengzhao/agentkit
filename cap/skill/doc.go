// Package skill defines injectable boundaries for skill discovery and loading.
//
// skill/filesystem (*plugins/skill.Registry) implements Registry via deps.fs and deps.workspace.
//
// prompt/section/skills and tool/skill require cap/skill.Registry (typically skills.default).
// tool/skill also requires cap/session.Skills for RenderSkillContent and AppendSkillLoad.
//
// Parsing and bundle layout live in runtime/skill; only skill/filesystem imports that package.
package skill
