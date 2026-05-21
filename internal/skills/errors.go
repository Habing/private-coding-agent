// Package skills implements the Agent Skills subsystem (Slice 12a).
//
// Skills are read-only programs-of-knowledge (SOPs, checklists, domain
// best practices) parsed from SKILL.md files on disk and injected into the
// agent's system message at run time. They do NOT execute — they instruct.
package skills

import "errors"

var (
	ErrInvalidSkillID     = errors.New("skills: invalid skill id")
	ErrInvalidFrontmatter = errors.New("skills: invalid frontmatter")
	ErrPathEscape         = errors.New("skills: path outside skills root")
	ErrSkillNotFound      = errors.New("skills: skill not found")
)
