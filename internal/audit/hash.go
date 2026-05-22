package audit

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"strconv"
	"time"

	"github.com/google/uuid"
)

// HashSize is the length in bytes of an audit entry hash (SHA-256).
const HashSize = 32

// rs is the ASCII Record Separator used to delimit canonical-encoded fields.
// It does not appear in valid UTF-8 text fields, so concatenation is unambiguous.
const rs = 0x1E

// ZeroHash returns a 32-byte slice of zeros — the genesis prev_hash and the
// sentinel for pre-chain rows (rows written before migration 0021).
func ZeroHash() []byte { return make([]byte, HashSize) }

// IsZeroHash reports whether h is the 32-byte zero hash.
func IsZeroHash(h []byte) bool {
	if len(h) != HashSize {
		return false
	}
	for _, b := range h {
		if b != 0 {
			return false
		}
	}
	return true
}

// Canonical builds the deterministic byte representation that gets hashed for
// a single audit entry. The encoding is:
//
//	prev (32 raw bytes) || RS || occurred_at_rfc3339nano_utc || RS ||
//	tenant_id || RS || user_id || RS || action || RS || target || RS ||
//	method || RS || path || RS || status || RS || duration_ms || RS ||
//	canonical_metadata_json
//
// Nil UUID pointers encode as empty string. json.Marshal on map[string]any
// produces sorted keys, so metadata is implicitly canonicalized.
func Canonical(prev []byte, e Entry) []byte {
	if len(prev) != HashSize {
		prev = ZeroHash()
	}
	var b bytes.Buffer
	b.Grow(HashSize + 256)
	b.Write(prev)
	b.WriteByte(rs)
	b.WriteString(e.OccurredAt.UTC().Format(time.RFC3339Nano))
	b.WriteByte(rs)
	b.WriteString(uuidStr(e.TenantID))
	b.WriteByte(rs)
	b.WriteString(uuidStr(e.UserID))
	b.WriteByte(rs)
	b.WriteString(e.Action)
	b.WriteByte(rs)
	b.WriteString(e.Target)
	b.WriteByte(rs)
	b.WriteString(e.Method)
	b.WriteByte(rs)
	b.WriteString(e.Path)
	b.WriteByte(rs)
	b.WriteString(strconv.Itoa(e.Status))
	b.WriteByte(rs)
	b.WriteString(strconv.Itoa(e.DurationMS))
	b.WriteByte(rs)
	b.Write(canonicalMetadata(e.Metadata))
	return b.Bytes()
}

// Hash computes the SHA-256 chain hash for entry e given its predecessor's
// entry_hash (or ZeroHash() for the genesis row).
func Hash(prev []byte, e Entry) [HashSize]byte {
	return sha256.Sum256(Canonical(prev, e))
}

func uuidStr(p *uuid.UUID) string {
	if p == nil {
		return ""
	}
	return p.String()
}

func canonicalMetadata(m map[string]any) []byte {
	if len(m) == 0 {
		return []byte("{}")
	}
	out, err := json.Marshal(m)
	if err != nil {
		return []byte("{}")
	}
	return out
}
