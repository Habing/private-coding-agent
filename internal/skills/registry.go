package skills

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"log/slog"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	pcametrics "github.com/yourorg/private-coding-agent/internal/metrics"
)

// Registry is the in-memory index of loaded SKILL.md files, keyed by id.
type Registry struct {
	mu   sync.RWMutex
	byID map[string]*Skill
}

func NewRegistry() *Registry {
	return &Registry{byID: make(map[string]*Skill)}
}

// LoadFromDirs walks each root recursively, parses every `SKILL.md`, and
// inserts the result into the registry. Returns the count of skills
// successfully loaded plus per-file errors (parse failures, path escapes).
// Caller should slog.Warn each error but proceed.
func (r *Registry) LoadFromDirs(dirs []string) (int, []error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var errs []error
	loaded := 0
	for _, dir := range dirs {
		root, err := filepath.Abs(dir)
		if err != nil {
			errs = append(errs, fmt.Errorf("skills.abs %q: %w", dir, err))
			continue
		}
		walkErr := filepath.WalkDir(root, func(path string, d fs.DirEntry, werr error) error {
			if werr != nil {
				errs = append(errs, fmt.Errorf("skills.walk %s: %w", path, werr))
				return nil
			}
			if d.IsDir() {
				return nil
			}
			if filepath.Base(path) != "SKILL.md" {
				return nil
			}
			abs, err := filepath.Abs(path)
			if err != nil {
				errs = append(errs, fmt.Errorf("skills.abs %s: %w", path, err))
				return nil
			}
			abs = filepath.Clean(abs)
			if !strings.HasPrefix(abs, root+string(filepath.Separator)) && abs != root {
				errs = append(errs, fmt.Errorf("%w: %s", ErrPathEscape, abs))
				return nil
			}
			doc, err := ParseFile(abs)
			if err != nil {
				errs = append(errs, err)
				recordLoad("error")
				return nil
			}
			sk := docToSkill(doc)
			if prev, ok := r.byID[sk.ID]; ok {
				slog.Warn("skills.duplicate_id",
					"id", sk.ID,
					"prev_path", prev.SourcePath,
					"new_path", sk.SourcePath)
			}
			r.byID[sk.ID] = sk
			loaded++
			recordLoad("ok")
			return nil
		})
		if walkErr != nil {
			errs = append(errs, fmt.Errorf("skills.walk_root %s: %w", root, walkErr))
		}
	}
	return loaded, errs
}

func recordLoad(outcome string) {
	if pcametrics.SkillLoadTotal == nil {
		return
	}
	pcametrics.SkillLoadTotal.Add(context.Background(), 1,
		metric.WithAttributes(attribute.String("outcome", outcome)))
}

func docToSkill(d *Document) *Skill {
	h := sha256.Sum256([]byte(d.Body))
	return &Skill{
		Document:  *d,
		Version:   hex.EncodeToString(h[:])[:12],
		CharCount: len(d.Body),
	}
}

// Get returns the skill with the given id, or false.
func (r *Registry) Get(id string) (*Skill, bool) {
	if r == nil {
		return nil, false
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	s, ok := r.byID[id]
	return s, ok
}

// List returns all loaded skills as trimmed metadata, sorted by id.
func (r *Registry) List() []SkillMeta {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]SkillMeta, 0, len(r.byID))
	for _, s := range r.byID {
		out = append(out, SkillMeta{
			ID:          s.ID,
			Description: s.Description,
			Version:     s.Version,
			CharCount:   s.CharCount,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}
