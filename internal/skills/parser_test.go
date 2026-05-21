package skills

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeTemp(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "SKILL.md")
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatalf("write temp: %v", err)
	}
	return p
}

func TestParseFile_Minimal(t *testing.T) {
	p := writeTemp(t, "---\nname: hello-world\ndescription: a friendly greeter\n---\n\n# Body\n\nlorem ipsum\n")
	doc, err := ParseFile(p)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if doc.ID != "hello-world" {
		t.Fatalf("id = %q, want hello-world", doc.ID)
	}
	if doc.Description != "a friendly greeter" {
		t.Fatalf("desc = %q", doc.Description)
	}
	if !strings.Contains(doc.Body, "lorem ipsum") {
		t.Fatalf("body missing payload: %q", doc.Body)
	}
	if doc.SourcePath != p {
		t.Fatalf("source = %q", doc.SourcePath)
	}
}

func TestParseFile_MissingName(t *testing.T) {
	p := writeTemp(t, "---\ndescription: nameless\n---\nbody\n")
	_, err := ParseFile(p)
	if !errors.Is(err, ErrInvalidFrontmatter) {
		t.Fatalf("want ErrInvalidFrontmatter, got %v", err)
	}
}

func TestParseFile_InvalidName(t *testing.T) {
	for _, bad := range []string{"Bad_Name", "with space", "-leading-dash", "trailing-dash-", "UPPER", "a/b", ""} {
		t.Run(bad, func(t *testing.T) {
			p := writeTemp(t, "---\nname: "+bad+"\ndescription: x\n---\nbody\n")
			_, err := ParseFile(p)
			if !errors.Is(err, ErrInvalidSkillID) && !errors.Is(err, ErrInvalidFrontmatter) {
				t.Fatalf("want id/frontmatter error for %q, got %v", bad, err)
			}
		})
	}
}

func TestParseFile_NoFrontmatter(t *testing.T) {
	p := writeTemp(t, "# Just a markdown doc, no frontmatter\n")
	_, err := ParseFile(p)
	if !errors.Is(err, ErrInvalidFrontmatter) {
		t.Fatalf("want ErrInvalidFrontmatter, got %v", err)
	}
}

func TestParseFile_ValidNames(t *testing.T) {
	for _, name := range []string{"a", "1", "ab", "good-name", "x1y2", "platform-coding-standards"} {
		t.Run(name, func(t *testing.T) {
			p := writeTemp(t, "---\nname: "+name+"\ndescription: ok\n---\nbody\n")
			doc, err := ParseFile(p)
			if err != nil {
				t.Fatalf("unexpected err for %q: %v", name, err)
			}
			if doc.ID != name {
				t.Fatalf("id mismatch %q vs %q", doc.ID, name)
			}
		})
	}
}

func TestParseFile_NameTooLong(t *testing.T) {
	long := strings.Repeat("a", 65)
	p := writeTemp(t, "---\nname: "+long+"\ndescription: x\n---\nbody\n")
	_, err := ParseFile(p)
	if !errors.Is(err, ErrInvalidSkillID) {
		t.Fatalf("want ErrInvalidSkillID for 65-char name, got %v", err)
	}
}

func TestParseFile_EmptyBodyAllowed(t *testing.T) {
	p := writeTemp(t, "---\nname: noop\ndescription: nothing\n---\n")
	doc, err := ParseFile(p)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if doc.Body != "" {
		t.Fatalf("body should be empty, got %q", doc.Body)
	}
}

func TestParseFile_QuotedValues(t *testing.T) {
	p := writeTemp(t, "---\nname: \"quoted-id\"\ndescription: \"a, with comma\"\n---\nbody\n")
	doc, err := ParseFile(p)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if doc.ID != "quoted-id" {
		t.Fatalf("id = %q", doc.ID)
	}
	if doc.Description != "a, with comma" {
		t.Fatalf("desc = %q", doc.Description)
	}
}
