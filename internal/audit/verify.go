package audit

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
)

// VerifyResult is the JSON contract for GET /admin/audit/verify. OK=true means
// every chain row's prev_hash and entry_hash matched what the canonical encoding
// re-computes — i.e. nothing has been mutated under the chain since the rows
// were written. On the first detected break we stop and return the row's id
// and the specific mismatch reason so the auditor knows where to investigate.
type VerifyResult struct {
	OK            bool   `json:"ok"`
	RowsChecked   int    `json:"rows_checked"`
	PreChainRows  int    `json:"pre_chain_rows"`
	ChainStartID  int64  `json:"chain_start_id,omitempty"`
	ChainEndID    int64  `json:"chain_end_id,omitempty"`
	FirstBrokenID int64  `json:"first_broken_id,omitempty"`
	Reason        string `json:"reason,omitempty"`
	ExpectedHash  string `json:"expected_hash,omitempty"`
	ActualHash    string `json:"actual_hash,omitempty"`
}

const (
	reasonPrevMismatch  = "prev_hash_mismatch"
	reasonEntryMismatch = "entry_hash_mismatch"
)

// Verify walks the audit_log chain from fromID (inclusive; pass 0 to start at
// the table head) and recomputes each row's SHA-256 against its predecessor's
// entry_hash. Rows whose stored entry_hash AND prev_hash are both zero are
// treated as pre-chain (written before migration 0021) and skipped without
// affecting the running hash state. The chain proper starts at the first row
// with a non-zero entry_hash; from there any mismatch — in the stored
// prev_hash pointer or in the recomputed entry_hash — is reported as the first
// broken id with a reason string the admin UI surfaces verbatim.
//
// fromID semantics:
//   - fromID == 0: full-table verify. The first chain row's prev_hash MUST be
//     ZeroHash (i.e. the genesis link is required).
//   - fromID  > 0: suffix verify. The first chain row's prev_hash is trusted
//     as the seed and we validate only that the suffix is internally
//     consistent. This lets admins skip past a known break to confirm later
//     rows haven't been further tampered with.
func (r *Repo) Verify(ctx context.Context, fromID int64) (*VerifyResult, error) {
	rows, err := r.pool.Query(ctx, `
SELECT id, occurred_at, tenant_id, user_id, action, target, method, path,
       status, duration_ms, metadata, prev_hash, entry_hash
FROM audit_log
WHERE id >= $1
ORDER BY id`, fromID)
	if err != nil {
		return nil, fmt.Errorf("query audit_log: %w", err)
	}
	defer rows.Close()

	res := &VerifyResult{}
	running := ZeroHash()
	inChain := false
	suffix := fromID > 0

	for rows.Next() {
		var id int64
		var e Entry
		var meta []byte
		var prevHash, entryHash []byte
		if err := rows.Scan(&id, &e.OccurredAt, &e.TenantID, &e.UserID, &e.Action,
			&e.Target, &e.Method, &e.Path, &e.Status, &e.DurationMS, &meta,
			&prevHash, &entryHash); err != nil {
			return nil, fmt.Errorf("scan audit_log: %w", err)
		}

		if !inChain {
			if IsZeroHash(prevHash) && IsZeroHash(entryHash) {
				res.PreChainRows++
				continue
			}
			inChain = true
			res.ChainStartID = id
			if suffix {
				running = append(running[:0], prevHash...)
			}
		}

		if len(meta) > 0 {
			_ = json.Unmarshal(meta, &e.Metadata)
		}

		if !bytes.Equal(prevHash, running) {
			res.FirstBrokenID = id
			res.Reason = reasonPrevMismatch
			res.ExpectedHash = hex.EncodeToString(running)
			res.ActualHash = hex.EncodeToString(prevHash)
			return res, nil
		}

		expected := Hash(running, e)
		if !bytes.Equal(expected[:], entryHash) {
			res.FirstBrokenID = id
			res.Reason = reasonEntryMismatch
			res.ExpectedHash = hex.EncodeToString(expected[:])
			res.ActualHash = hex.EncodeToString(entryHash)
			return res, nil
		}

		running = entryHash
		res.RowsChecked++
		res.ChainEndID = id
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate audit_log: %w", err)
	}

	res.OK = true
	return res, nil
}

func (h *Handler) verify(c *gin.Context) {
	var fromID int64
	if v := c.Query("from_id"); v != "" {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil || n < 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "validation: from_id"})
			return
		}
		fromID = n
	}
	res, err := h.svc.Verify(c.Request.Context(), fromID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal"})
		return
	}
	c.JSON(http.StatusOK, res)
}
