package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/usageflow/usageflow-go-middleware/v2/pkg/tracker"
)

func TestRequestInterceptor_TracksWrappedFunction(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tracker.Enable()

	api := New("test-api-key")
	defer api.socketManager.Close()

	// Empty monitoring map → early c.Next(), but tracking context still active.
	api.monitoringPathsMap = map[string]map[string]bool{}

	r := gin.New()
	r.Use(api.RequestInterceptor())

	var sawChain bool
	r.GET("/ping", func(c *gin.Context) {
		out, err := tracker.Track(c.Request.Context(), tracker.Options{
			FuncName: "greet",
			FilePath: "tracking_test.go",
		}, func() (string, error) {
			return "ok", nil
		})
		require.NoError(t, err)
		assert.Equal(t, "ok", out)
		chain := tracker.GetCallChain(c.Request.Context())
		sawChain = len(chain) == 1 && chain[0].FuncName == "greet"
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/ping", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.True(t, sawChain, "expected Track to record into request call chain")
}

func TestBeginTrackingDisabled(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tracker.Disable()
	t.Cleanup(tracker.Enable)

	api := New("test-api-key")
	defer api.socketManager.Close()

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/x", nil)

	store := api.beginTracking(c, "GET", "/x")
	assert.Nil(t, store)
	assert.Nil(t, tracker.FromContext(c.Request.Context()))
}
