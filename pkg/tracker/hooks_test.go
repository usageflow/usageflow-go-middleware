package tracker

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReportCallRecordsDuration(t *testing.T) {
	Enable()
	ctx, store := WithTracking(context.Background(), &RequestContext{Method: "GET", URL: "/x"}, "rid-1")

	end := ReportCall(ctx, "listUsers", "main.go", "basic")
	end()

	chain := store.Snapshot()
	require.Len(t, chain, 1)
	assert.Equal(t, "listUsers", chain[0].FuncName)
	assert.Equal(t, "main.go", chain[0].FilePath)
	assert.Equal(t, "basic", chain[0].ModuleName)
	assert.Equal(t, "rid-1", chain[0].UsageflowRequestId)
	assert.GreaterOrEqual(t, chain[0].DurationMs, int64(0))
}

func TestReportCallNoopWithoutStore(t *testing.T) {
	Enable()
	end := ReportCall(context.Background(), "f", "f.go", "m")
	assert.NotPanics(t, func() { end() })
}
