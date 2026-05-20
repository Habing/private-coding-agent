package modelgw

import (
	"context"
	"time"
)

// usageWriteTimeout 是 UsageRecorder.Record 写库的硬上限。
const usageWriteTimeout = 5 * time.Second

// UsageRecorder 异步写 model_usage,使用 detached ctx 避免请求 ctx 被取消时丢记录。
type UsageRecorder struct {
	repo  *UsageRepo
	onErr func(error)
}

func NewUsageRecorder(repo *UsageRepo, onErr func(error)) *UsageRecorder {
	return &UsageRecorder{repo: repo, onErr: onErr}
}

// Record 写一行 model_usage。
//
// 不阻塞调用方:用 detached ctx + 5s timeout。失败仅通过 onErr 报告,
// 不影响 HTTP 响应。
func (r *UsageRecorder) Record(e CallEvent) {
	ctx, cancel := context.WithTimeout(context.Background(), usageWriteTimeout)
	defer cancel()
	if err := r.repo.Insert(ctx, e); err != nil && r.onErr != nil {
		r.onErr(err)
	}
}
