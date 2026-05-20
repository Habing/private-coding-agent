package tools

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/yourorg/private-coding-agent/internal/sandbox"
	"github.com/yourorg/private-coding-agent/internal/toolbus"
)

type grepTool struct{ rt Runtime }

func NewGrep(rt Runtime) toolbus.Tool { return &grepTool{rt: rt} }

func (t *grepTool) Name() string { return "grep" }
func (t *grepTool) Description() string {
	return "Search file contents in the sandbox using regex. Returns lines matching the pattern with file:line context."
}
func (t *grepTool) Schema() json.RawMessage {
	return json.RawMessage(`{
        "type":"object",
        "properties":{
            "sandbox_id":{"type":"string","format":"uuid"},
            "pattern":{"type":"string"},
            "path":{"type":"string"},
            "case_insensitive":{"type":"boolean"},
            "max_results":{"type":"integer","minimum":1,"maximum":1000}
        },
        "required":["sandbox_id","pattern"],
        "additionalProperties":false
    }`)
}

type grepIn struct {
	SandboxID       uuid.UUID `json:"sandbox_id"`
	Pattern         string    `json:"pattern"`
	Path            string    `json:"path"`
	CaseInsensitive bool      `json:"case_insensitive"`
	MaxResults      int       `json:"max_results"`
}

type grepMatch struct {
	Path string `json:"path"`
	Line int    `json:"line"`
	Text string `json:"text"`
}

func (t *grepTool) Invoke(ctx context.Context, tenantID, _ uuid.UUID, input json.RawMessage) (json.RawMessage, error) {
	var in grepIn
	if err := json.Unmarshal(input, &in); err != nil {
		return nil, fmt.Errorf("%w: %v", toolbus.ErrInvalidArguments, err)
	}
	if in.SandboxID == uuid.Nil {
		return nil, toolbus.ErrSandboxIDRequired
	}
	if in.MaxResults == 0 {
		in.MaxResults = 100
	}
	root := "/workspace"
	if in.Path != "" {
		root = "/workspace/" + strings.TrimPrefix(in.Path, "/")
	}
	args := []string{"rg", "--json", "-n"}
	if in.CaseInsensitive {
		args = append(args, "-i")
	}
	args = append(args, "--", in.Pattern, root)

	res, err := t.rt.Exec(ctx, tenantID, in.SandboxID, sandbox.ExecOpts{
		Cmd:        args,
		TimeoutSec: 60,
	})
	if err != nil {
		return nil, err
	}
	// ripgrep exit codes: 0=matches, 1=no matches, 2=error.
	if res.ExitCode == 1 {
		return json.Marshal(struct {
			Matches []grepMatch `json:"matches"`
		}{Matches: []grepMatch{}})
	}
	if res.ExitCode != 0 {
		return nil, fmt.Errorf("%w: rg exit %d: %s", toolbus.ErrToolFailed, res.ExitCode, string(res.Stderr))
	}

	matches := []grepMatch{}
	sc := bufio.NewScanner(bytes.NewReader(res.Stdout))
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	for sc.Scan() && len(matches) < in.MaxResults {
		var ev struct {
			Type string `json:"type"`
			Data struct {
				Path       struct{ Text string } `json:"path"`
				LineNumber int                   `json:"line_number"`
				Lines      struct{ Text string } `json:"lines"`
			} `json:"data"`
		}
		if err := json.Unmarshal(sc.Bytes(), &ev); err != nil {
			continue
		}
		if ev.Type != "match" {
			continue
		}
		matches = append(matches, grepMatch{
			Path: strings.TrimPrefix(ev.Data.Path.Text, root+"/"),
			Line: ev.Data.LineNumber,
			Text: strings.TrimRight(ev.Data.Lines.Text, "\n"),
		})
	}
	return json.Marshal(struct {
		Matches []grepMatch `json:"matches"`
	}{Matches: matches})
}
