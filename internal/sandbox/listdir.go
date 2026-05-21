package sandbox

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/google/uuid"
)

// FileEntry is one row in a non-recursive directory listing.
type FileEntry struct {
	Name string `json:"name"`
	Type string `json:"type"` // "file" | "dir"
	Size *int   `json:"size,omitempty"`
}

// ListDir lists immediate children under relPath in the sandbox workspace.
func ListDir(ctx context.Context, rt Runtime, tenantID, sandboxID uuid.UUID, relPath string) ([]FileEntry, error) {
	path := relPath
	if path == "" {
		path = "."
	}
	root := "/workspace"
	if path != "." {
		root = "/workspace/" + strings.TrimPrefix(path, "/")
	}
	res, err := rt.Exec(ctx, tenantID, sandboxID, ExecOpts{
		Cmd:        []string{"find", root, "-mindepth", "1", "-maxdepth", "1", "-printf", "%f\t%y\t%s\n"},
		TimeoutSec: 30,
	})
	if err != nil {
		return nil, err
	}
	if res.ExitCode != 0 {
		return nil, fmt.Errorf("find exit %d: %s", res.ExitCode, string(res.Stderr))
	}
	return ParseFindList(res.Stdout), nil
}

// ParseFindList parses find -printf "%f\t%y\t%s\n" output.
func ParseFindList(stdout []byte) []FileEntry {
	out := []FileEntry{}
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
		entry := FileEntry{Name: name, Type: typ}
		if typ == "file" {
			if n, err := strconv.Atoi(parts[2]); err == nil {
				entry.Size = &n
			}
		}
		out = append(out, entry)
	}
	return out
}
