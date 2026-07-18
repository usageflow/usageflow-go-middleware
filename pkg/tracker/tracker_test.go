package tracker

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTrackRecordsCallChain(t *testing.T) {
	Enable()
	ctx, store := WithTracking(context.Background(), &RequestContext{Method: "GET", URL: "/api/users"}, "req-1")

	greet := Wrap(Options{
		FuncName:            "greet",
		FilePath:            "example/greet.go",
		ModuleName:          "example",
		CaptureResultSchema: true,
	}, func(ctx context.Context) (string, error) {
		return "hello", nil
	})

	out, err := greet(ctx)
	require.NoError(t, err)
	assert.Equal(t, "hello", out)

	chain := store.Snapshot()
	require.Len(t, chain, 1)
	assert.Equal(t, "greet", chain[0].FuncName)
	assert.Equal(t, "example/greet.go", chain[0].FilePath)
	assert.Equal(t, "example", chain[0].ModuleName)
	assert.Equal(t, "req-1", chain[0].UsageflowRequestId)
	assert.GreaterOrEqual(t, chain[0].DurationMs, int64(0))
	assert.NotNil(t, chain[0].ResultSchema)
}

func TestTrackPassThroughWhenDisabled(t *testing.T) {
	Disable()
	t.Cleanup(Enable)

	ctx, store := WithTracking(context.Background(), nil, "")
	_, err := Track(ctx, Options{FuncName: "noop", FilePath: "x.go"}, func() (int, error) {
		return 42, nil
	})
	require.NoError(t, err)
	assert.Empty(t, store.Snapshot())
}

func TestTrackPassThroughWithoutContext(t *testing.T) {
	Enable()
	val, err := Track(context.Background(), Options{FuncName: "noop", FilePath: "x.go"}, func() (int, error) {
		return 7, nil
	})
	require.NoError(t, err)
	assert.Equal(t, 7, val)
}

func TestTrackPropagatesError(t *testing.T) {
	Enable()
	ctx, _ := WithTracking(context.Background(), nil, "")
	boom := errors.New("boom")
	_, err := Track(ctx, Options{FuncName: "fail", FilePath: "x.go"}, func() (int, error) {
		return 0, boom
	})
	assert.ErrorIs(t, err, boom)
	chain := GetCallChain(ctx)
	require.Len(t, chain, 1)
	assert.Nil(t, chain[0].ResultSchema)
}

func TestSetUsageflowRequestID(t *testing.T) {
	Enable()
	ctx, _ := WithTracking(context.Background(), nil, "")
	SetUsageflowRequestID(ctx, "later-id")
	_, err := Track(ctx, Options{FuncName: "f", FilePath: "f.go"}, func() (struct{}, error) {
		return struct{}{}, nil
	})
	require.NoError(t, err)
	chain := GetCallChain(ctx)
	require.Len(t, chain, 1)
	assert.Equal(t, "later-id", chain[0].UsageflowRequestId)
}
