package audit

import (
	"bytes"
	"testing"
	"time"

	"github.com/google/uuid"
)

func mkEntry(t *testing.T) Entry {
	t.Helper()
	tid := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	uid := uuid.MustParse("22222222-2222-2222-2222-222222222222")
	occurred, err := time.Parse(time.RFC3339Nano, "2026-05-22T10:30:00.123456789Z")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	return Entry{
		OccurredAt: occurred,
		TenantID:   &tid,
		UserID:     &uid,
		Action:     "auth.login.success",
		Target:     "demo@example.com",
		Method:     "POST",
		Path:       "/auth/login",
		Status:     200,
		DurationMS: 42,
		Metadata:   map[string]any{"ip": "127.0.0.1", "ua": "curl/8"},
	}
}

func TestZeroHash(t *testing.T) {
	z := ZeroHash()
	if len(z) != HashSize {
		t.Fatalf("len %d != %d", len(z), HashSize)
	}
	if !IsZeroHash(z) {
		t.Fatal("ZeroHash should be reported as zero")
	}
	z[0] = 1
	if IsZeroHash(z) {
		t.Fatal("modified zero hash should not be reported as zero")
	}
}

func TestHash_Deterministic(t *testing.T) {
	e := mkEntry(t)
	prev := ZeroHash()
	first := Hash(prev, e)
	for i := 0; i < 50; i++ {
		got := Hash(prev, e)
		if got != first {
			t.Fatalf("iter %d: hash changed", i)
		}
	}
}

func TestHash_MetadataKeyOrderInvariant(t *testing.T) {
	a := mkEntry(t)
	a.Metadata = map[string]any{"a": 1, "b": 2, "c": 3}
	b := a
	b.Metadata = map[string]any{"c": 3, "b": 2, "a": 1}
	ha := Hash(ZeroHash(), a)
	hb := Hash(ZeroHash(), b)
	if ha != hb {
		t.Fatalf("map key order changed hash: %x != %x", ha, hb)
	}
}

func TestHash_PrevHashAffectsHash(t *testing.T) {
	e := mkEntry(t)
	h1 := Hash(ZeroHash(), e)
	other := make([]byte, HashSize)
	other[0] = 1
	h2 := Hash(other, e)
	if h1 == h2 {
		t.Fatal("changing prev_hash should change hash")
	}
}

func TestHash_FieldsAffectHash(t *testing.T) {
	base := mkEntry(t)
	prev := ZeroHash()
	baseHash := Hash(prev, base)

	cases := []struct {
		name string
		mut  func(*Entry)
	}{
		{"occurred_at", func(e *Entry) { e.OccurredAt = e.OccurredAt.Add(time.Nanosecond) }},
		{"tenant_id", func(e *Entry) { u := uuid.New(); e.TenantID = &u }},
		{"tenant_nil", func(e *Entry) { e.TenantID = nil }},
		{"user_id", func(e *Entry) { u := uuid.New(); e.UserID = &u }},
		{"user_nil", func(e *Entry) { e.UserID = nil }},
		{"action", func(e *Entry) { e.Action = "other" }},
		{"target", func(e *Entry) { e.Target = "other" }},
		{"method", func(e *Entry) { e.Method = "GET" }},
		{"path", func(e *Entry) { e.Path = "/other" }},
		{"status", func(e *Entry) { e.Status = 500 }},
		{"duration_ms", func(e *Entry) { e.DurationMS = 9999 }},
		{"metadata_value", func(e *Entry) { e.Metadata["ip"] = "10.0.0.1" }},
		{"metadata_key", func(e *Entry) { e.Metadata["extra"] = 1 }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cp := base
			cp.Metadata = map[string]any{}
			for k, v := range base.Metadata {
				cp.Metadata[k] = v
			}
			tc.mut(&cp)
			h := Hash(prev, cp)
			if h == baseHash {
				t.Fatalf("%s mutation did not change hash", tc.name)
			}
		})
	}
}

func TestHash_RS_AvoidsAmbiguity(t *testing.T) {
	// Without a separator, {action:"a", target:"b"} would concatenate to "ab",
	// matching {action:"ab", target:""}. The RS byte prevents this collision.
	a := mkEntry(t)
	a.Action = "a"
	a.Target = "b"
	b := a
	b.Action = "ab"
	b.Target = ""
	ha := Hash(ZeroHash(), a)
	hb := Hash(ZeroHash(), b)
	if ha == hb {
		t.Fatal("field-boundary collision: RS separator failed")
	}
}

func TestHash_NilUUIDEncodesEmpty(t *testing.T) {
	e := mkEntry(t)
	e.TenantID = nil
	e.UserID = nil
	got := Canonical(ZeroHash(), e)
	// Should contain two adjacent RS bytes for the empty UUID fields (after occurred_at).
	// We don't assert exact position; just that nil + zero UUID differ.
	zero := uuid.Nil
	e2 := e
	e2.TenantID = &zero
	got2 := Canonical(ZeroHash(), e2)
	if bytes.Equal(got, got2) {
		t.Fatal("nil UUID and zero UUID pointer must encode differently")
	}
}

func TestHash_EmptyMetadataEncodesAsEmptyObject(t *testing.T) {
	a := mkEntry(t)
	a.Metadata = nil
	b := a
	b.Metadata = map[string]any{}
	ha := Hash(ZeroHash(), a)
	hb := Hash(ZeroHash(), b)
	if ha != hb {
		t.Fatal("nil metadata and empty map must encode identically")
	}
}

func TestHash_PrevHashBadLengthFallsBackToZero(t *testing.T) {
	e := mkEntry(t)
	shortPrev := []byte{1, 2, 3}
	got := Canonical(shortPrev, e)
	want := Canonical(ZeroHash(), e)
	if !bytes.Equal(got, want) {
		t.Fatal("invalid prev_hash length should fall back to ZeroHash")
	}
}
