package db_test

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/yourorg/private-coding-agent/internal/db"
)

func TestMigrate_RespectsCanceledCtx(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // 立刻取消

	// DSN 无关紧要，因为 ctx 已取消 Migrate 应直接退出
	err := db.Migrate(ctx, "postgres://app:app@localhost:1/app?sslmode=disable")
	require.Error(t, err)
	// 错误应来自 ctx，而非网络/DSN
	require.True(t,
		strings.Contains(err.Error(), "context canceled") ||
			strings.Contains(err.Error(), "ctx:"),
		"expected ctx error, got: %v", err)
}
