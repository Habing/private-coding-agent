package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSafeUnderRootRejectsTraversal(t *testing.T) {
	root := t.TempDir()
	_, err := safeInboxPath(root, "../etc/passwd")
	if err == nil {
		t.Fatal("expected error for traversal")
	}
	ok, err := safeInboxPath(root, "batch.json")
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(ok) != "batch.json" {
		t.Fatalf("got %s", ok)
	}
}

func TestParseJSONArray(t *testing.T) {
	recs, err := parseJSONArray([]byte(`[{"id":"1","name":"a"}]`))
	if err != nil || len(recs) != 1 {
		t.Fatalf("got %v err=%v", recs, err)
	}
}

func TestMockDenoiseDropsNoise(t *testing.T) {
	in := []map[string]any{
		{"id": "1", "name": "ok"},
		{"id": "1", "name": "dup"},
		{"noise": true},
		{"name": ""},
	}
	out := mockDenoise(in, "test")
	if len(out.Records) != 1 {
		t.Fatalf("want 1 record, got %d", len(out.Records))
	}
}

func TestLoadRecordsJSONFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "x.json")
	if err := os.WriteFile(path, []byte(`[{"id":"a"}]`), 0o644); err != nil {
		t.Fatal(err)
	}
	recs, err := loadRecords(path, "json")
	if err != nil || len(recs) != 1 {
		t.Fatalf("%v %v", recs, err)
	}
}
