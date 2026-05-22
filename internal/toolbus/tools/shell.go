package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"

	"github.com/yourorg/private-coding-agent/internal/sandbox"
	"github.com/yourorg/private-coding-agent/internal/toolbus"
)

type shellExec struct{ rt Runtime }

func NewShellExec(rt Runtime) toolbus.Tool { return &shellExec{rt: rt} }

func (t *shellExec) Name() string       { return "shell.exec" }
func (t *shellExec) IsMutating() bool   { return true }
func (t *shellExec) Description() string {
	return "Run a shell command inside the sandbox. Returns exit code, stdout, stderr."
}
func (t *shellExec) Schema() json.RawMessage {
	return json.RawMessage(`{
        "type":"object",
        "properties":{
            "sandbox_id":{"type":"string","format":"uuid"},
            "cmd":{"type":"array","items":{"type":"string"},"minItems":1},
            "working_dir":{"type":"string"},
            "timeout_sec":{"type":"integer","minimum":1,"maximum":600}
        },
        "required":["sandbox_id","cmd"],
        "additionalProperties":false
    }`)
}

type shellExecIn struct {
	SandboxID  uuid.UUID `json:"sandbox_id"`
	Cmd        []string  `json:"cmd"`
	WorkingDir string    `json:"working_dir"`
	TimeoutSec int       `json:"timeout_sec"`
}

type shellExecOut struct {
	ExitCode   int    `json:"exit_code"`
	Stdout     string `json:"stdout"`
	Stderr     string `json:"stderr"`
	Truncated  bool   `json:"truncated"`
	DurationMS int64  `json:"duration_ms"`
	TimedOut   bool   `json:"timed_out"`
}

func (t *shellExec) Invoke(ctx context.Context, tenantID, _ uuid.UUID, input json.RawMessage) (json.RawMessage, error) {
	var in shellExecIn
	if err := json.Unmarshal(input, &in); err != nil {
		return nil, fmt.Errorf("%w: %v", toolbus.ErrInvalidArguments, err)
	}
	if in.SandboxID == uuid.Nil {
		return nil, toolbus.ErrSandboxIDRequired
	}
	res, err := t.rt.Exec(ctx, tenantID, in.SandboxID, sandbox.ExecOpts{
		Cmd:        in.Cmd,
		WorkingDir: in.WorkingDir,
		TimeoutSec: in.TimeoutSec,
	})
	if err != nil {
		return nil, err
	}
	return json.Marshal(shellExecOut{
		ExitCode:   res.ExitCode,
		Stdout:     string(res.Stdout),
		Stderr:     string(res.Stderr),
		Truncated:  res.Truncated,
		DurationMS: res.DurationMS,
		TimedOut:   res.TimedOut,
	})
}
