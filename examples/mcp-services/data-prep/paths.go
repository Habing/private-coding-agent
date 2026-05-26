package main

import (
	"fmt"
	"path/filepath"
	"strings"
)

func safeInboxPath(inboxRoot, rel string) (string, error) {
	return safeUnderRoot(inboxRoot, rel)
}

func safeOutboxPath(outboxRoot, rel string) (string, error) {
	return safeUnderRoot(outboxRoot, rel)
}

func safeUnderRoot(root, rel string) (string, error) {
	rel = strings.TrimSpace(rel)
	if rel == "" {
		return "", fmt.Errorf("empty path")
	}
	if filepath.IsAbs(rel) {
		return "", fmt.Errorf("absolute paths not allowed")
	}
	clean := filepath.Clean(rel)
	if clean == "." || strings.HasPrefix(clean, "..") {
		return "", fmt.Errorf("invalid path")
	}
	full := filepath.Join(root, clean)
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	absFull, err := filepath.Abs(full)
	if err != nil {
		return "", err
	}
	if absFull != absRoot && !strings.HasPrefix(absFull, absRoot+string(filepath.Separator)) {
		return "", fmt.Errorf("path escapes root")
	}
	return absFull, nil
}
