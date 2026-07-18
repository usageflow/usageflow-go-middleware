package tracker

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

func TestExtractSchema_PrimitivesAndObject(t *testing.T) {
	type Out struct {
		Message string `json:"message"`
		Count   int    `json:"count"`
	}
	schema := ExtractSchema(Out{Message: "hi", Count: 2}, "", 0, nil)
	raw, err := json.Marshal(schema)
	if err != nil {
		t.Fatal(err)
	}
	s := string(raw)
	// JS-compatible flat object map (not {type:object, properties:{...}}).
	if !containsAll(s, `"message"`, `"path":"message"`, `"type":"string"`, `"count"`, `"path":"count"`, `"type":"number"`) {
		t.Fatalf("unexpected schema: %s", s)
	}
	root, ok := schema.(map[string]interface{})
	if !ok {
		t.Fatalf("root should be flat map, got %T", schema)
	}
	if _, hasType := root["type"]; hasType {
		t.Fatalf("root must not have type wrapper: %s", s)
	}
}

func TestNormalizeResultSchema_Scalar(t *testing.T) {
	norm := NormalizeResultSchema("integer")
	m, ok := norm.(map[string]interface{})
	if !ok {
		t.Fatalf("got %T", norm)
	}
	ret, ok := m["return"].(map[string]interface{})
	if !ok || ret["path"] != "return" || ret["type"] != "integer" {
		t.Fatalf("got %#v", norm)
	}
}

func TestExtractSchemaForArgs(t *testing.T) {
	schema := ExtractSchemaForArgs([]interface{}{"user-1", 42})
	if schema["arg0"] != "string" {
		t.Fatalf("arg0=%v", schema["arg0"])
	}
	if schema["arg1"] != "integer" {
		t.Fatalf("arg1=%v", schema["arg1"])
	}
}

func TestBeginCallCapturesSchemasAndAllocation(t *testing.T) {
	Enable()
	defer SetAllocationCallbacks(nil, nil)

	var started, ended int
	var gotAllocID string
	SetAllocationCallbacks(
		func(store *TrackingContext, funcName, filePath string) (string, error) {
			started++
			return "alloc-fn-1", nil
		},
		func(store *TrackingContext, info FunctionEndInfo) {
			ended++
			gotAllocID = info.AllocationID
			if info.IsError {
				t.Errorf("unexpected error end")
			}
			if info.Args == nil || len(info.Args) != 2 {
				t.Errorf("expected args, got %#v", info.Args)
			}
		},
	)

	ctx, store := WithTracking(context.Background(), &RequestContext{Method: "POST", URL: "/api/chat"}, "req-1")
	call, block := BeginCall(ctx, "Reply", "chat.go", "chat", Args("hello", 3))
	if block != nil {
		t.Fatal(block)
	}
	time.Sleep(2 * time.Millisecond)
	type reply struct {
		Text string `json:"text"`
	}
	call.End(reply{Text: "world"}, error(nil))

	chain := store.Snapshot()
	if len(chain) != 1 {
		t.Fatalf("len=%d", len(chain))
	}
	rec := chain[0]
	if rec.ParamsSchema == nil || rec.ParamsSchema["arg0"] != "string" {
		t.Fatalf("paramsSchema=%v", rec.ParamsSchema)
	}
	if rec.ResultSchema == nil {
		t.Fatal("missing resultSchema")
	}
	if rec.DurationMs < 0 {
		t.Fatal("missing duration")
	}
	if started != 1 || ended != 1 || gotAllocID != "alloc-fn-1" {
		t.Fatalf("alloc callbacks start=%d end=%d id=%s", started, ended, gotAllocID)
	}
}

func TestBeginCallBlocksOnPolicy(t *testing.T) {
	Enable()
	defer SetAllocationCallbacks(nil, nil)
	SetAllocationCallbacks(
		func(store *TrackingContext, funcName, filePath string) (string, error) {
			return "", &FunctionBlockedError{Message: "quota exceeded", Code: "NOT_ENOUGH_QUOTA"}
		},
		nil,
	)
	ctx, _ := WithTracking(context.Background(), &RequestContext{Method: "POST", URL: "/x"}, "req-2")
	call, block := BeginCall(ctx, "Paid", "paid.go", "app", nil)
	if block == nil {
		t.Fatal("expected block error")
	}
	if call != nil {
		t.Fatal("expected nil call when blocked")
	}
}

func containsAll(s string, parts ...string) bool {
	for _, p := range parts {
		if !contains(s, p) {
			return false
		}
	}
	return true
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 || indexOf(s, sub) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
