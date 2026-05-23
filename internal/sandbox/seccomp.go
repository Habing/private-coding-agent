package sandbox

import (
	_ "embed"
	"encoding/json"
	"fmt"
)

//go:embed seccomp.json
var seccompProfileJSON []byte

// seccompProfile is the parsed view used for boot-time validation. The raw
// bytes (seccompProfileJSON) are what gets passed to Docker via SecurityOpt.
type seccompProfile struct {
	DefaultAction string `json:"defaultAction"`
	Syscalls      []struct {
		Names  []string `json:"names"`
		Action string   `json:"action"`
	} `json:"syscalls"`
}

// LoadSeccompProfile returns the embedded sandbox seccomp profile JSON as a
// single-line string ready to drop into HostConfig.SecurityOpt as
// "seccomp=<json>". Boot-time parse-and-validate catches malformed profiles
// before any sandbox starts. Returns ("", err) on parse failure or if the
// profile drifted away from the SCMP_ACT_ERRNO default-deny posture.
func LoadSeccompProfile() (string, error) {
	var p seccompProfile
	if err := json.Unmarshal(seccompProfileJSON, &p); err != nil {
		return "", fmt.Errorf("parse embedded seccomp profile: %w", err)
	}
	if p.DefaultAction != "SCMP_ACT_ERRNO" {
		return "", fmt.Errorf("seccomp profile defaultAction must be SCMP_ACT_ERRNO, got %q", p.DefaultAction)
	}
	if len(p.Syscalls) == 0 {
		return "", fmt.Errorf("seccomp profile has no syscall allow blocks")
	}
	return string(seccompProfileJSON), nil
}

// seccompAllowedSyscalls returns the flat set of every syscall name that maps
// to SCMP_ACT_ALLOW in the embedded profile. Used by unit tests to assert
// (a) dangerous syscalls were removed from every allow block, and
// (b) common syscalls (read/write/execve/...) still pass.
func seccompAllowedSyscalls() map[string]struct{} {
	var p seccompProfile
	if err := json.Unmarshal(seccompProfileJSON, &p); err != nil {
		return nil
	}
	out := map[string]struct{}{}
	for _, b := range p.Syscalls {
		if b.Action != "SCMP_ACT_ALLOW" {
			continue
		}
		for _, n := range b.Names {
			out[n] = struct{}{}
		}
	}
	return out
}
