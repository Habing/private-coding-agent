package workflow

import (
	"context"
	"fmt"
	"log/slog"
	"time"
)

// StartRunsRetention launches a background loop that deletes workflow_runs rows
// older than retentionDays. retentionDays <= 0 disables the loop entirely.
func StartRunsRetention(ctx context.Context, repo *Repo, retentionDays int, interval time.Duration) {
	if retentionDays <= 0 {
		return
	}
	if interval <= 0 {
		interval = 24 * time.Hour
	}
	purge := func() {
		cutoff := time.Now().AddDate(0, 0, -retentionDays)
		n, err := repo.DeleteRunsOlderThan(ctx, cutoff)
		if err != nil {
			slog.Warn("workflow: retention purge failed", "err", err.Error())
			return
		}
		if n > 0 {
			slog.Info("workflow: retention purge", "deleted", n, "older_than_days", retentionDays)
		}
	}
	purge()
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				purge()
			}
		}
	}()
}

// DeleteRunsOlderThan removes run history before cutoff. Returns rows deleted.
func (r *Repo) DeleteRunsOlderThan(ctx context.Context, cutoff time.Time) (int64, error) {
	tag, err := r.pool.Exec(ctx, `
DELETE FROM workflow_runs WHERE started_at < $1`, cutoff)
	if err != nil {
		return 0, fmt.Errorf("delete old workflow runs: %w", err)
	}
	return tag.RowsAffected(), nil
}
