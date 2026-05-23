package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/yourorg/private-coding-agent/internal/memory"
	"github.com/yourorg/private-coding-agent/internal/toolbus"
)

// MemoryService is the subset of *memory.Service used by the memory.* tools.
// Declared locally so tests can supply a mock without standing up a DB.
type MemoryService interface {
	Create(ctx context.Context, tenantID, userID uuid.UUID, req memory.CreateRequest) (*memory.CreateResult, error)
	Search(ctx context.Context, tenantID, userID uuid.UUID, req memory.SearchRequest) ([]memory.SearchResult, error)
	List(ctx context.Context, tenantID, userID uuid.UUID, f memory.ListFilter) ([]memory.Memory, error)
	Delete(ctx context.Context, tenantID, userID, id uuid.UUID) error
}

// wrapMemoryErr converts memory validation errors into toolbus.ErrInvalidArguments
// so the bus surfaces them as 4xx, leaving NotFound / internal errors untouched.
func wrapMemoryErr(err error) error {
	if errors.Is(err, memory.ErrEmptyContent) ||
		errors.Is(err, memory.ErrInvalidType) ||
		errors.Is(err, memory.ErrEmptySearch) ||
		errors.Is(err, memory.ErrInvalidSearchMode) {
		return fmt.Errorf("%w: %v", toolbus.ErrInvalidArguments, err)
	}
	return err
}

// ---------- memory.save ----------

type memorySave struct{ svc MemoryService }

func NewMemorySave(svc MemoryService) toolbus.Tool { return &memorySave{svc: svc} }

func (t *memorySave) Name() string       { return "memory.save" }
func (t *memorySave) IsMutating() bool   { return true }
func (t *memorySave) Description() string {
	return "为当前用户保存一条记忆（画像/偏好/知识/经验），返回新记录 ID。"
}
func (t *memorySave) Schema() json.RawMessage {
	return json.RawMessage(`{
        "type":"object",
        "properties":{
            "type":{"type":"string","enum":["profile","preference","knowledge","lesson"]},
            "content":{"type":"string","minLength":1},
            "tags":{"type":"array","items":{"type":"string"}},
            "source":{"type":"string"},
            "source_msg_id":{"type":"string","format":"uuid"}
        },
        "required":["type","content"],
        "additionalProperties":false
    }`)
}

type memorySaveIn struct {
	Type        string     `json:"type"`
	Content     string     `json:"content"`
	Tags        []string   `json:"tags,omitempty"`
	Source      string     `json:"source,omitempty"`
	SourceMsgID *uuid.UUID `json:"source_msg_id,omitempty"`
}

func (t *memorySave) Invoke(ctx context.Context, tenantID, userID uuid.UUID, input json.RawMessage) (json.RawMessage, error) {
	var in memorySaveIn
	if err := json.Unmarshal(input, &in); err != nil {
		return nil, fmt.Errorf("%w: %v", toolbus.ErrInvalidArguments, err)
	}
	src := in.Source
	if src == "" {
		src = memory.SourceAgent
	}
	res, err := t.svc.Create(ctx, tenantID, userID, memory.CreateRequest{
		Type: in.Type, Content: in.Content, Tags: in.Tags,
		Source: src, SourceMsgID: in.SourceMsgID,
	})
	if err != nil {
		return nil, wrapMemoryErr(err)
	}
	return json.Marshal(struct {
		ID      uuid.UUID `json:"id"`
		Created bool      `json:"created"`
	}{ID: res.Memory.ID, Created: res.Created})
}

// ---------- memory.search ----------

type memorySearch struct{ svc MemoryService }

func NewMemorySearch(svc MemoryService) toolbus.Tool { return &memorySearch{svc: svc} }

func (t *memorySearch) Name() string { return "memory.search" }
func (t *memorySearch) Description() string {
	return "检索当前用户的记忆。默认向量语义搜索；mode=keyword 时用关键词匹配。至少需提供 query、type 或 tags 之一。"
}
func (t *memorySearch) Schema() json.RawMessage {
	return json.RawMessage(`{
        "type":"object",
        "properties":{
            "query":{"type":"string"},
            "type":{"type":"string","enum":["profile","preference","knowledge","lesson"]},
            "tags":{"type":"array","items":{"type":"string"}},
            "limit":{"type":"integer","minimum":1,"maximum":50},
            "mode":{"type":"string","enum":["vector","keyword"]}
        },
        "additionalProperties":false
    }`)
}

type memoryItem struct {
	ID         uuid.UUID `json:"id"`
	Type       string    `json:"type"`
	Content    string    `json:"content"`
	Tags       []string  `json:"tags"`
	LastUsedAt string    `json:"last_used_at"`
	Score      float64   `json:"score,omitempty"`
}

type memorySearchIn struct {
	Query string   `json:"query,omitempty"`
	Type  string   `json:"type,omitempty"`
	Tags  []string `json:"tags,omitempty"`
	Limit int      `json:"limit,omitempty"`
	Mode  string   `json:"mode,omitempty"`
}

func (t *memorySearch) Invoke(ctx context.Context, tenantID, userID uuid.UUID, input json.RawMessage) (json.RawMessage, error) {
	var in memorySearchIn
	if err := json.Unmarshal(input, &in); err != nil {
		return nil, fmt.Errorf("%w: %v", toolbus.ErrInvalidArguments, err)
	}
	hits, err := t.svc.Search(ctx, tenantID, userID, memory.SearchRequest{
		Query: in.Query, Type: in.Type, Tags: in.Tags, Limit: in.Limit, Mode: in.Mode,
	})
	if err != nil {
		return nil, wrapMemoryErr(err)
	}
	items := make([]memoryItem, len(hits))
	for i, m := range hits {
		items[i] = memoryItem{
			ID: m.ID, Type: m.Type, Content: m.Content, Tags: m.Tags,
			LastUsedAt: m.LastUsedAt.UTC().Format("2006-01-02T15:04:05Z"),
			Score:      m.Score,
		}
	}
	return json.Marshal(struct {
		Items []memoryItem `json:"items"`
	}{Items: items})
}

// ---------- memory.list ----------

type memoryList struct{ svc MemoryService }

func NewMemoryList(svc MemoryService) toolbus.Tool { return &memoryList{svc: svc} }

func (t *memoryList) Name() string { return "memory.list" }
func (t *memoryList) Description() string {
	return "浏览当前用户的记忆列表，可按类型、标签过滤；精确查找请用 memory.search。"
}
func (t *memoryList) Schema() json.RawMessage {
	return json.RawMessage(`{
        "type":"object",
        "properties":{
            "type":{"type":"string","enum":["profile","preference","knowledge","lesson"]},
            "tags":{"type":"array","items":{"type":"string"}},
            "limit":{"type":"integer","minimum":1,"maximum":50},
            "offset":{"type":"integer","minimum":0}
        },
        "additionalProperties":false
    }`)
}

type memoryListIn struct {
	Type   string   `json:"type,omitempty"`
	Tags   []string `json:"tags,omitempty"`
	Limit  int      `json:"limit,omitempty"`
	Offset int      `json:"offset,omitempty"`
}

func (t *memoryList) Invoke(ctx context.Context, tenantID, userID uuid.UUID, input json.RawMessage) (json.RawMessage, error) {
	var in memoryListIn
	if err := json.Unmarshal(input, &in); err != nil {
		return nil, fmt.Errorf("%w: %v", toolbus.ErrInvalidArguments, err)
	}
	rows, err := t.svc.List(ctx, tenantID, userID, memory.ListFilter{
		Type: in.Type, Tags: in.Tags, Limit: in.Limit, Offset: in.Offset,
	})
	if err != nil {
		return nil, wrapMemoryErr(err)
	}
	items := make([]memoryItem, len(rows))
	for i, m := range rows {
		items[i] = memoryItem{
			ID: m.ID, Type: m.Type, Content: m.Content, Tags: m.Tags,
			LastUsedAt: m.LastUsedAt.UTC().Format("2006-01-02T15:04:05Z"),
		}
	}
	return json.Marshal(struct {
		Items []memoryItem `json:"items"`
	}{Items: items})
}

// ---------- memory.delete ----------

type memoryDelete struct{ svc MemoryService }

func NewMemoryDelete(svc MemoryService) toolbus.Tool { return &memoryDelete{svc: svc} }

func (t *memoryDelete) Name() string       { return "memory.delete" }
func (t *memoryDelete) IsMutating() bool   { return true }
func (t *memoryDelete) Description() string {
	return "按 ID 删除一条记忆。"
}
func (t *memoryDelete) Schema() json.RawMessage {
	return json.RawMessage(`{
        "type":"object",
        "properties":{
            "id":{"type":"string","format":"uuid"}
        },
        "required":["id"],
        "additionalProperties":false
    }`)
}

type memoryDeleteIn struct {
	ID uuid.UUID `json:"id"`
}

func (t *memoryDelete) Invoke(ctx context.Context, tenantID, userID uuid.UUID, input json.RawMessage) (json.RawMessage, error) {
	var in memoryDeleteIn
	if err := json.Unmarshal(input, &in); err != nil {
		return nil, fmt.Errorf("%w: %v", toolbus.ErrInvalidArguments, err)
	}
	if err := t.svc.Delete(ctx, tenantID, userID, in.ID); err != nil {
		return nil, wrapMemoryErr(err)
	}
	return json.Marshal(struct {
		OK bool `json:"ok"`
	}{OK: true})
}
