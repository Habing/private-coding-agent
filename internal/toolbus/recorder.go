package toolbus

import (
	"context"
	"time"
)

// invocationWriteTimeout 是 InvocationRecorder.Record 写库的硬上限。
const invocationWriteTimeout = 5 * time.Second

// InvocationRecorder 异步写 tool_invocations,使用 detached ctx 避免请求 ctx
// 被取消时丢记录。
type InvocationRecorder struct {
	repo  *InvocationRepo
	onErr func(error)
}

func NewInvocationRecorder(repo *InvocationRepo, onErr func(error)) *InvocationRecorder {
	return &InvocationRecorder{repo: repo, onErr: onErr}
}

// Record 写一行 tool_invocations。
//
// 不阻塞调用方:用 detached ctx + 5s timeout。失败仅通过 onErr 报告,
// 不影响调用方语义。
func (r *InvocationRecorder) Record(e InvocationEvent) {
	ctx, cancel := context.WithTimeout(context.Background(), invocationWriteTimeout)
	defer cancel()
	if err := r.repo.Insert(ctx, e); err != nil && r.onErr != nil {
		r.onErr(err)
	}
}
