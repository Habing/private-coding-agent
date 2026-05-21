package skills

import (
	"strings"

	"github.com/yourorg/private-coding-agent/internal/modelgw"
)

// InjectResult is what BuildSystemMessages returns: the system messages to
// prepend to the chat (0 or 1 entries), plus metadata for audit/telemetry.
type InjectResult struct {
	Messages  []modelgw.ChatMessage
	SkillIDs  []string
	CharCount int
	Truncated bool
}

const skillsHeader = "\n\n## Active Skills\n"

// BuildSystemMessages composes a single merged system message:
//
//	{profilePrompt}
//
//	## Active Skills
//
//	### Skill: {id}
//	{description}
//
//	{body}
//
// When the running total exceeds maxChars (counting profilePrompt first),
// the current skill's body is truncated and subsequent skills are dropped.
// Truncated=true is set in that case.
//
// Edge cases:
//   - profilePrompt empty + 0 skills      -> no messages (caller adds none)
//   - profilePrompt empty + N skills      -> 1 message starting with "## Active Skills"
//   - profilePrompt non-empty + 0 skills  -> 1 message = profilePrompt only
func BuildSystemMessages(profilePrompt string, skills []*Skill, maxChars int) InjectResult {
	res := InjectResult{}
	if profilePrompt == "" && len(skills) == 0 {
		return res
	}
	var b strings.Builder
	b.Grow(len(profilePrompt) + 256)
	if profilePrompt != "" {
		b.WriteString(profilePrompt)
	}

	budget := maxChars
	if budget <= 0 {
		budget = 24000
	}

	if len(skills) > 0 {
		b.WriteString(skillsHeader)
		for i, sk := range skills {
			if i == 0 {
				b.WriteString("\n")
			}
			block := renderBlock(sk)
			remaining := budget - b.Len()
			if remaining <= 0 {
				res.Truncated = true
				break
			}
			if len(block) <= remaining {
				b.WriteString(block)
				res.SkillIDs = append(res.SkillIDs, sk.ID)
				continue
			}
			// truncate this block, drop the rest
			b.WriteString(block[:remaining])
			b.WriteString("\n…[truncated]\n")
			res.SkillIDs = append(res.SkillIDs, sk.ID)
			res.Truncated = true
			break
		}
	}

	content := b.String()
	res.Messages = []modelgw.ChatMessage{{
		Role:    modelgw.RoleSystem,
		Content: content,
	}}
	res.CharCount = len(content)
	return res
}

func renderBlock(s *Skill) string {
	var b strings.Builder
	b.Grow(64 + len(s.Body))
	b.WriteString("### Skill: ")
	b.WriteString(s.ID)
	b.WriteString("\n")
	if s.Description != "" {
		b.WriteString(s.Description)
		b.WriteString("\n\n")
	}
	b.WriteString(s.Body)
	b.WriteString("\n\n")
	return b.String()
}
