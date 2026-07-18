package middleware

import (
	"os"
	"testing"
)

func TestMain(m *testing.M) {
	_ = os.Setenv("USAGEFLOW_DISABLE_WS", "1")
	os.Exit(m.Run())
}
