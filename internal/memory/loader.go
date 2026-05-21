package memory

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
)

// LoaderConfig caps auto-injection on the first user turn of a session.
type LoaderConfig struct {
	TopK     int // max memories to retrieve; 0 → 5
	MaxChars int // max chars in injected block; 0 → 4000
}

// LoadResult is the formatted injection block plus metadata for audit.
type LoadResult struct {
	Section   string
	IDs       []uuid.UUID
	CharCount int
	Truncated bool
}

// Loader retrieves relevant memories for session context injection (slice 16).
type Loader struct {
	svc *Service
	cfg LoaderConfig
}

func NewLoader(svc *Service, cfg LoaderConfig) *Loader {
	if cfg.TopK <= 0 {
		cfg.TopK = 5
	}
	if cfg.MaxChars <= 0 {
		cfg.MaxChars = 4000
	}
	return &Loader{svc: svc, cfg: cfg}
}

// LoadForSession searches memories using the first user message as the query.
// Empty query returns an empty result without error.
func (l *Loader) LoadForSession(ctx context.Context, tenantID, userID uuid.UUID, userQuery string) (LoadResult, error) {
	q := strings.TrimSpace(userQuery)
	if q == "" {
		return LoadResult{}, nil
	}
	results, err := l.svc.Search(ctx, tenantID, userID, SearchRequest{
		Query: q,
		Limit: l.cfg.TopK,
		Mode:  SearchModeAuto,
	})
	if err != nil {
		return LoadResult{}, err
	}
	if len(results) == 0 {
		return LoadResult{}, nil
	}
	section, ids, chars, truncated := FormatRelevantMemories(results, l.cfg.MaxChars)
	return LoadResult{
		Section: section, IDs: ids, CharCount: chars, Truncated: truncated,
	}, nil
}

// FormatRelevantMemories builds the system-inject block consumed by the agent composer.
func FormatRelevantMemories(results []SearchResult, maxChars int) (section string, ids []uuid.UUID, charCount int, truncated bool) {
	if len(results) == 0 {
		return "", nil, 0, false
	}
	var b strings.Builder
	b.WriteString("## Relevant memories\n")
	for _, r := range results {
		ids = append(ids, r.ID)
		line := fmt.Sprintf("- [%s] %s", r.Type, strings.TrimSpace(r.Content))
		if r.Score > 0 {
			line += fmt.Sprintf(" (score=%.2f)", r.Score)
		}
		b.WriteString(line)
		b.WriteByte('\n')
	}
	section = b.String()
	runes := []rune(section)
	if maxChars > 0 && len(runes) > maxChars {
		if maxChars <= 1 {
			section = "…"
		} else {
			section = string(runes[:maxChars-1]) + "…"
		}
		truncated = true
	}
	charCount = len([]rune(section))
	return section, ids, charCount, truncated
}
