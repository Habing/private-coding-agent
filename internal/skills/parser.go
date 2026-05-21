package skills

import (
	"bytes"
	"fmt"
	"os"
	"regexp"
	"strings"
)

const maxSkillIDLen = 64

var skillIDRE = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,62}[a-z0-9])?$|^[a-z0-9]$`)

// ParseFile reads a SKILL.md and returns its Document. The file must begin
// with a YAML-ish frontmatter delimited by `---` lines and containing at
// least `name` and `description` keys (the only two keys we honor).
func ParseFile(path string) (*Document, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("skills: read %s: %w", path, err)
	}
	name, desc, body, err := splitFrontmatter(raw)
	if err != nil {
		return nil, fmt.Errorf("%w: %s: %v", ErrInvalidFrontmatter, path, err)
	}
	if !skillIDRE.MatchString(name) || len(name) > maxSkillIDLen {
		return nil, fmt.Errorf("%w: %q in %s", ErrInvalidSkillID, name, path)
	}
	return &Document{
		ID:          name,
		Description: desc,
		Body:        strings.TrimSpace(body),
		SourcePath:  path,
	}, nil
}

// splitFrontmatter parses a minimal YAML subset: lines `key: value` between
// `---\n` markers. Only `name` and `description` are recognized; quoted
// values (single or double) have their quotes stripped.
func splitFrontmatter(raw []byte) (name, desc, body string, err error) {
	// require leading "---\n"
	if !bytes.HasPrefix(raw, []byte("---\n")) && !bytes.HasPrefix(raw, []byte("---\r\n")) {
		return "", "", "", fmt.Errorf("missing leading ---")
	}
	// strip leading delim
	rest := raw
	if bytes.HasPrefix(rest, []byte("---\r\n")) {
		rest = rest[5:]
	} else {
		rest = rest[4:]
	}
	// find closing ---
	closeIdx := bytes.Index(rest, []byte("\n---"))
	if closeIdx < 0 {
		return "", "", "", fmt.Errorf("missing closing ---")
	}
	fm := string(rest[:closeIdx])
	bodyStart := closeIdx + len("\n---")
	// consume rest of the closing line (\n or \r\n)
	if bodyStart < len(rest) && rest[bodyStart] == '\r' {
		bodyStart++
	}
	if bodyStart < len(rest) && rest[bodyStart] == '\n' {
		bodyStart++
	}
	body = string(rest[bodyStart:])

	for _, line := range strings.Split(fm, "\n") {
		line = strings.TrimRight(line, "\r")
		if strings.TrimSpace(line) == "" {
			continue
		}
		colon := strings.IndexByte(line, ':')
		if colon < 0 {
			continue
		}
		key := strings.TrimSpace(line[:colon])
		val := strings.TrimSpace(line[colon+1:])
		val = unquote(val)
		switch key {
		case "name":
			name = val
		case "description":
			desc = val
		}
	}
	if name == "" {
		return "", "", "", fmt.Errorf("missing name")
	}
	return name, desc, body, nil
}

func unquote(s string) string {
	if len(s) >= 2 {
		if (s[0] == '"' && s[len(s)-1] == '"') || (s[0] == '\'' && s[len(s)-1] == '\'') {
			return s[1 : len(s)-1]
		}
	}
	return s
}
