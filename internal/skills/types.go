package skills

// Document is the parsed result of a SKILL.md file before hashing.
type Document struct {
	ID          string // frontmatter `name`
	Description string // frontmatter `description`
	Body        string // markdown body after the closing `---`
	SourcePath  string // file path (audit / debug only)
}

// Skill is a Document indexed in the Registry, with derived metadata.
type Skill struct {
	Document
	Version   string // sha256(body)[:12] — content fingerprint
	CharCount int    // len(Body), for budgeting at inject time
}

// SkillMeta is the trimmed view returned by Registry.List() and GET /skills.
// It deliberately excludes Body and SourcePath.
type SkillMeta struct {
	ID          string `json:"id"`
	Description string `json:"description"`
	Version     string `json:"version"`
	CharCount   int    `json:"char_count"`
}
