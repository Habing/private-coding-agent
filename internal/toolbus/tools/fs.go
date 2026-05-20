// Package tools holds concrete Tool implementations registered with toolbus.Bus.
package tools

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/bmatcuk/doublestar/v4"
	"github.com/google/uuid"

	"github.com/yourorg/private-coding-agent/internal/sandbox"
	"github.com/yourorg/private-coding-agent/internal/toolbus"
)

// Runtime is the subset of sandbox.Runtime used by fs/shell/grep tools.
// Declared locally to keep tools narrowly typed.
type Runtime interface {
	Exec(ctx context.Context, tenantID, id uuid.UUID, opts sandbox.ExecOpts) (*sandbox.ExecResult, error)
	ReadFile(ctx context.Context, tenantID, id uuid.UUID, path string) ([]byte, error)
	WriteFile(ctx context.Context, tenantID, id uuid.UUID, path string, data []byte) error
}

// ---------- fs.read ----------

type fsRead struct{ rt Runtime }

func NewFSRead(rt Runtime) toolbus.Tool { return &fsRead{rt: rt} }

func (t *fsRead) Name() string { return "fs.read" }
func (t *fsRead) Description() string {
	return "Read a UTF-8 text file from the sandbox workspace. Path is relative to /workspace."
}
func (t *fsRead) Schema() json.RawMessage {
	return json.RawMessage(`{
        "type":"object",
        "properties":{
            "sandbox_id":{"type":"string","format":"uuid"},
            "path":{"type":"string"}
        },
        "required":["sandbox_id","path"],
        "additionalProperties":false
    }`)
}

type fsReadIn struct {
	SandboxID uuid.UUID `json:"sandbox_id"`
	Path      string    `json:"path"`
}

func (t *fsRead) Invoke(ctx context.Context, tenantID, _ uuid.UUID, input json.RawMessage) (json.RawMessage, error) {
	var in fsReadIn
	if err := json.Unmarshal(input, &in); err != nil {
		return nil, fmt.Errorf("%w: %v", toolbus.ErrInvalidArguments, err)
	}
	if in.SandboxID == uuid.Nil {
		return nil, toolbus.ErrSandboxIDRequired
	}
	data, err := t.rt.ReadFile(ctx, tenantID, in.SandboxID, in.Path)
	if err != nil {
		return nil, err
	}
	content := data
	if !utf8.Valid(data) {
		content = []byte(strings.ToValidUTF8(string(data), "\uFFFD"))
	}
	return json.Marshal(struct {
		Content string `json:"content"`
		Size    int    `json:"size"`
	}{Content: string(content), Size: len(data)})
}

// ---------- fs.write ----------

type fsWrite struct{ rt Runtime }

func NewFSWrite(rt Runtime) toolbus.Tool { return &fsWrite{rt: rt} }

func (t *fsWrite) Name() string { return "fs.write" }
func (t *fsWrite) Description() string {
	return "Write content to a file in the sandbox workspace. Creates intermediate directories. Overwrites if exists."
}
func (t *fsWrite) Schema() json.RawMessage {
	return json.RawMessage(`{
        "type":"object",
        "properties":{
            "sandbox_id":{"type":"string","format":"uuid"},
            "path":{"type":"string"},
            "content":{"type":"string"}
        },
        "required":["sandbox_id","path","content"],
        "additionalProperties":false
    }`)
}

type fsWriteIn struct {
	SandboxID uuid.UUID `json:"sandbox_id"`
	Path      string    `json:"path"`
	Content   string    `json:"content"`
}

func (t *fsWrite) Invoke(ctx context.Context, tenantID, _ uuid.UUID, input json.RawMessage) (json.RawMessage, error) {
	var in fsWriteIn
	if err := json.Unmarshal(input, &in); err != nil {
		return nil, fmt.Errorf("%w: %v", toolbus.ErrInvalidArguments, err)
	}
	if in.SandboxID == uuid.Nil {
		return nil, toolbus.ErrSandboxIDRequired
	}
	data := []byte(in.Content)
	if err := t.rt.WriteFile(ctx, tenantID, in.SandboxID, in.Path, data); err != nil {
		return nil, err
	}
	return json.Marshal(struct {
		BytesWritten int `json:"bytes_written"`
	}{BytesWritten: len(data)})
}

// ---------- fs.list ----------

type fsList struct{ rt Runtime }

func NewFSList(rt Runtime) toolbus.Tool { return &fsList{rt: rt} }

func (t *fsList) Name() string { return "fs.list" }
func (t *fsList) Description() string {
	return "List files and directories under a sandbox path. Non-recursive."
}
func (t *fsList) Schema() json.RawMessage {
	return json.RawMessage(`{
        "type":"object",
        "properties":{
            "sandbox_id":{"type":"string","format":"uuid"},
            "path":{"type":"string"}
        },
        "required":["sandbox_id"],
        "additionalProperties":false
    }`)
}

type fsListIn struct {
	SandboxID uuid.UUID `json:"sandbox_id"`
	Path      string    `json:"path"`
}

type fsListEntry struct {
	Name string `json:"name"`
	Type string `json:"type"` // "file" / "dir"
	Size *int   `json:"size,omitempty"`
}

func (t *fsList) Invoke(ctx context.Context, tenantID, _ uuid.UUID, input json.RawMessage) (json.RawMessage, error) {
	var in fsListIn
	if err := json.Unmarshal(input, &in); err != nil {
		return nil, fmt.Errorf("%w: %v", toolbus.ErrInvalidArguments, err)
	}
	if in.SandboxID == uuid.Nil {
		return nil, toolbus.ErrSandboxIDRequired
	}
	path := in.Path
	if path == "" {
		path = "."
	}
	root := "/workspace"
	if path != "." {
		root = "/workspace/" + strings.TrimPrefix(path, "/")
	}
	res, err := t.rt.Exec(ctx, tenantID, in.SandboxID, sandbox.ExecOpts{
		Cmd:        []string{"find", root, "-mindepth", "1", "-maxdepth", "1", "-printf", "%f\t%y\t%s\n"},
		TimeoutSec: 30,
	})
	if err != nil {
		return nil, err
	}
	if res.ExitCode != 0 {
		return nil, fmt.Errorf("%w: find exit %d: %s", toolbus.ErrToolFailed, res.ExitCode, string(res.Stderr))
	}
	entries := parseFindList(res.Stdout)
	return json.Marshal(struct {
		Entries []fsListEntry `json:"entries"`
	}{Entries: entries})
}

func parseFindList(stdout []byte) []fsListEntry {
	out := []fsListEntry{}
	sc := bufio.NewScanner(bytes.NewReader(stdout))
	for sc.Scan() {
		line := sc.Text()
		parts := strings.SplitN(line, "\t", 3)
		if len(parts) < 3 {
			continue
		}
		name := parts[0]
		typ := "file"
		if parts[1] == "d" {
			typ = "dir"
		}
		entry := fsListEntry{Name: name, Type: typ}
		if typ == "file" {
			if n, err := strconv.Atoi(parts[2]); err == nil {
				entry.Size = &n
			}
		}
		out = append(out, entry)
	}
	return out
}

// ---------- fs.glob ----------

type fsGlob struct{ rt Runtime }

func NewFSGlob(rt Runtime) toolbus.Tool { return &fsGlob{rt: rt} }

func (t *fsGlob) Name() string { return "fs.glob" }
func (t *fsGlob) Description() string {
	return "Find files in the sandbox matching a glob pattern (e.g. '**/*.go', 'src/**/*.test.ts')."
}
func (t *fsGlob) Schema() json.RawMessage {
	return json.RawMessage(`{
        "type":"object",
        "properties":{
            "sandbox_id":{"type":"string","format":"uuid"},
            "pattern":{"type":"string"},
            "path":{"type":"string"}
        },
        "required":["sandbox_id","pattern"],
        "additionalProperties":false
    }`)
}

type fsGlobIn struct {
	SandboxID uuid.UUID `json:"sandbox_id"`
	Pattern   string    `json:"pattern"`
	Path      string    `json:"path"`
}

func (t *fsGlob) Invoke(ctx context.Context, tenantID, _ uuid.UUID, input json.RawMessage) (json.RawMessage, error) {
	var in fsGlobIn
	if err := json.Unmarshal(input, &in); err != nil {
		return nil, fmt.Errorf("%w: %v", toolbus.ErrInvalidArguments, err)
	}
	if in.SandboxID == uuid.Nil {
		return nil, toolbus.ErrSandboxIDRequired
	}
	root := "/workspace"
	if in.Path != "" {
		root = "/workspace/" + strings.TrimPrefix(in.Path, "/")
	}
	res, err := t.rt.Exec(ctx, tenantID, in.SandboxID, sandbox.ExecOpts{
		Cmd:        []string{"sh", "-c", "cd " + shellEscape(root) + " && find . -type f -printf '%P\\n'"},
		TimeoutSec: 60,
	})
	if err != nil {
		return nil, err
	}
	if res.ExitCode != 0 {
		return nil, fmt.Errorf("%w: find exit %d", toolbus.ErrToolFailed, res.ExitCode)
	}

	matches := []string{}
	sc := bufio.NewScanner(bytes.NewReader(res.Stdout))
	for sc.Scan() {
		p := sc.Text()
		ok, _ := doublestar.PathMatch(in.Pattern, p)
		if ok {
			matches = append(matches, p)
		}
	}
	return json.Marshal(struct {
		Matches []string `json:"matches"`
	}{Matches: matches})
}

func shellEscape(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
