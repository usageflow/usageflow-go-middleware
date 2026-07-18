package middleware

import (
	"bytes"
	"encoding/json"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/usageflow/usageflow-go-middleware/v2/pkg/tracker"
)

const maxCapturedResponseBytes = 512 * 1024

// bodyCaptureWriter tees the response body so we can derive responseSchema (JS parity).
type bodyCaptureWriter struct {
	gin.ResponseWriter
	buf        *bytes.Buffer
	truncated  bool
	statusCode int
}

func (w *bodyCaptureWriter) WriteHeader(code int) {
	w.statusCode = code
	w.ResponseWriter.WriteHeader(code)
}

func (w *bodyCaptureWriter) Write(b []byte) (int, error) {
	if !w.truncated {
		remaining := maxCapturedResponseBytes - w.buf.Len()
		if remaining > 0 {
			if len(b) > remaining {
				w.buf.Write(b[:remaining])
				w.truncated = true
			} else {
				w.buf.Write(b)
			}
		} else {
			w.truncated = true
		}
	}
	return w.ResponseWriter.Write(b)
}

func (w *bodyCaptureWriter) WriteString(s string) (int, error) {
	return w.Write([]byte(s))
}

func attachBodyCapture(c *gin.Context) *bodyCaptureWriter {
	blw := &bodyCaptureWriter{
		ResponseWriter: c.Writer,
		buf:            &bytes.Buffer{},
		statusCode:     http.StatusOK,
	}
	c.Writer = blw
	return blw
}

func parseCapturedResponseBody(blw *bodyCaptureWriter) interface{} {
	if blw == nil || blw.buf.Len() == 0 {
		return nil
	}
	if blw.truncated {
		return map[string]interface{}{"_truncated": true, "length": blw.buf.Len()}
	}
	raw := blw.buf.Bytes()
	var asJSON interface{}
	if err := json.Unmarshal(raw, &asJSON); err == nil {
		return asJSON
	}
	s := string(raw)
	if len(s) > 500 {
		return map[string]interface{}{"_truncated": true, "length": len(s)}
	}
	return s
}

func enrichFulfillMetadataWithResponse(metadata map[string]interface{}, blw *bodyCaptureWriter, responseTrackingField string) float64 {
	amount := 1.0
	body := parseCapturedResponseBody(blw)
	if body == nil {
		return amount
	}
	// Console: metadata.body = actual response body; request stays under requestBody.
	metadata["body"] = body
	metadata["responseSchema"] = tracker.ExtractSchema(body, "", 0, nil)

	// Promote common token fields onto metadata so list/trace token chips work.
	if m, ok := body.(map[string]interface{}); ok {
		if v, ok := toFloat64(m["promptTokens"]); ok {
			metadata["promptTokens"] = int(v)
			metadata["prompt_tokens"] = int(v)
		}
		if v, ok := toFloat64(m["responseTokens"]); ok {
			metadata["completionTokens"] = int(v)
			metadata["completion_tokens"] = int(v)
		} else if v, ok := toFloat64(m["completionTokens"]); ok {
			metadata["completionTokens"] = int(v)
			metadata["completion_tokens"] = int(v)
		}
		if model, ok := m["model"].(string); ok && model != "" {
			metadata["model"] = model
			metadata["aiModel"] = model
		}
		pt, _ := metadata["promptTokens"].(int)
		ct, _ := metadata["completionTokens"].(int)
		if pt > 0 || ct > 0 {
			metadata["totalTokens"] = pt + ct
			metadata["total_tokens"] = pt + ct
		}
	}

	if responseTrackingField != "" {
		if extracted, ok := getValueByPath(body, responseTrackingField); ok {
			if n, ok := toFloat64(extracted); ok {
				amount = n
				metadata["amount"] = n
			}
		}
	}
	return amount
}
