package sandbox

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLoadSeccompProfile_Parses(t *testing.T) {
	s, err := LoadSeccompProfile()
	require.NoError(t, err)
	require.NotEmpty(t, s)

	var top map[string]any
	require.NoError(t, json.Unmarshal([]byte(s), &top))
	require.Equal(t, "SCMP_ACT_ERRNO", top["defaultAction"], "must be default-deny")
	require.NotEmpty(t, top["syscalls"], "must have at least one allow block")
	require.False(t, strings.Contains(s, "\n\n\n"), "compact-ish JSON expected")
}

func TestSeccompProfile_DeniesDangerousSyscalls(t *testing.T) {
	allow := seccompAllowedSyscalls()
	require.NotNil(t, allow)
	dangerous := []string{
		"mount", "umount", "umount2", "pivot_root",
		"name_to_handle_at", "open_by_handle_at",
		"ptrace", "process_vm_readv", "process_vm_writev",
		"keyctl", "add_key", "request_key",
		"bpf",
		"init_module", "delete_module", "finit_module", "create_module",
		"kexec_load", "kexec_file_load",
		"userfaultfd",
		"perf_event_open",
	}
	for _, sc := range dangerous {
		if _, ok := allow[sc]; ok {
			t.Errorf("dangerous syscall %q still in allow list — profile drift", sc)
		}
	}
}

func TestSeccompProfile_AllowsCommonSyscalls(t *testing.T) {
	allow := seccompAllowedSyscalls()
	require.NotNil(t, allow)
	common := []string{
		"read", "write", "open", "openat", "close",
		"execve", "execveat", "fork", "clone", "exit", "exit_group",
		"brk", "mmap", "munmap", "mprotect",
		"stat", "fstat", "lstat", "newfstatat",
		"socket", "connect", "sendto", "recvfrom",
		"unshare", "setns",
	}
	for _, sc := range common {
		if _, ok := allow[sc]; !ok {
			t.Errorf("common syscall %q missing from allow list — over-trimmed", sc)
		}
	}
}
