package telemetry_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/yourorg/private-coding-agent/internal/telemetry"
)

func TestSetup_NoEndpoint_NoOp(t *testing.T) {
	shutdown, err := telemetry.Setup(context.Background(), telemetry.Config{ServiceName: "x"})
	require.NoError(t, err)
	require.NotNil(t, shutdown)
	require.NoError(t, shutdown(context.Background()))
}
