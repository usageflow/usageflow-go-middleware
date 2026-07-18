package tracker

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"time"
)

// ErrFunctionBlocked is returned when a rate-limited function policy denies allocation.
var ErrFunctionBlocked = errors.New("usageflow: function call blocked by policy")

// AllocationStartFunc is invoked when an instrumented function begins.
// Return an allocation id to settle later, or "" to skip use_allocation.
type AllocationStartFunc func(store *TrackingContext, funcName, filePath string) (allocationID string, block error)

// AllocationEndFunc is invoked when an instrumented function finishes.
type AllocationEndFunc func(store *TrackingContext, info FunctionEndInfo)

// FunctionEndInfo carries schemas + runtime in/out for use_allocation / Console traces.
type FunctionEndInfo struct {
	AllocationID string
	FuncName     string
	FilePath     string
	Args         []interface{}
	Result       interface{}
	IsError      bool
	DurationMs   int64
	ParamsSchema map[string]interface{}
	ResultSchema interface{}
	Usage        *TokenUsage
	AIModel      string
}

var (
	onFunctionStart AllocationStartFunc
	onFunctionEnd   AllocationEndFunc
)

// SetAllocationCallbacks wires per-function allocate/use handlers (JS setAllocationCallbacks parity).
func SetAllocationCallbacks(start AllocationStartFunc, end AllocationEndFunc) {
	onFunctionStart = start
	onFunctionEnd = end
}

// Args is a convenience helper for compile-time injected BeginCall sites.
func Args(values ...interface{}) []interface{} {
	return values
}

// Call is an in-flight instrumented function invocation.
type Call struct {
	store        *TrackingContext
	record       *FunctionCallRecord
	allocationID string
	funcName     string
	filePath     string
	args         []interface{}
	start        time.Time
}

// BeginCall starts timing/schema capture and optional function allocation.
// If a rate-limited function policy denies the call, block is non-nil (JS sendAsync denial parity).
func BeginCall(ctx context.Context, funcName, filePath, moduleName string, args []interface{}) (call *Call, block error) {
	if !IsEnabled() {
		return &Call{funcName: funcName, filePath: filePath, args: args, start: time.Now()}, nil
	}
	store := FromContext(ctx)
	if store == nil {
		return &Call{funcName: funcName, filePath: filePath, args: args, start: time.Now()}, nil
	}

	record := &FunctionCallRecord{
		FuncName:     funcName,
		FilePath:     filePath,
		ModuleName:   moduleName,
		ParamsSchema: ExtractSchemaForArgs(args),
	}
	if store.UsageflowRequestId != "" {
		record.UsageflowRequestId = store.UsageflowRequestId
	}
	store.AppendPtr(record)

	var allocationID string
	if onFunctionStart != nil {
		id, err := onFunctionStart(store, funcName, filePath)
		if err != nil {
			duration := int64(0)
			record.DurationMs = duration
			return nil, err
		}
		allocationID = id
	}

	return &Call{
		store:        store,
		record:       record,
		allocationID: allocationID,
		funcName:     funcName,
		filePath:     filePath,
		args:         args,
		start:        time.Now(),
	}, nil
}

// End finalizes duration, result schema, token usage hints, and use_allocation.
func (c *Call) End(results ...interface{}) {
	if c == nil {
		return
	}
	defer func() { _ = recover() }()

	duration := time.Since(c.start).Milliseconds()
	var result interface{}
	var isError bool
	if len(results) > 0 {
		// Prefer last non-error value as primary result; detect error returns.
		for i := len(results) - 1; i >= 0; i-- {
			if err, ok := results[i].(error); ok && err != nil {
				isError = true
				continue
			}
			if result == nil {
				result = results[i]
			}
		}
		if result == nil && !isError && len(results) > 0 {
			result = results[0]
		}
	}

	var resultSchema interface{}
	var usage *TokenUsage
	var aiModel string
	if c.record != nil {
		c.record.DurationMs = duration
		if !isError && result != nil {
			resultSchema = NormalizeResultSchema(ExtractSchema(result, "", 0, nil))
			c.record.ResultSchema = resultSchema
			if u := ExtractTokenUsage(result); u != nil {
				usage = u
				c.record.Usage = u
			}
			if model := ExtractAIModel(result); model != "" {
				aiModel = model
				c.record.AIModel = model
			}
		}
	}

	if c.allocationID != "" && onFunctionEnd != nil {
		var paramsSchema map[string]interface{}
		if c.record != nil {
			paramsSchema = c.record.ParamsSchema
		}
		onFunctionEnd(c.store, FunctionEndInfo{
			AllocationID: c.allocationID,
			FuncName:     c.funcName,
			FilePath:     c.filePath,
			Args:         c.args,
			Result:       result,
			IsError:      isError,
			DurationMs:   duration,
			ParamsSchema: paramsSchema,
			ResultSchema: resultSchema,
			Usage:        usage,
			AIModel:      aiModel,
		})
	}
}

// ReportCall is kept for backward compatibility; prefer BeginCall/End for schemas.
func ReportCall(ctx context.Context, funcName, filePath, moduleName string) func() {
	call, _ := BeginCall(ctx, funcName, filePath, moduleName, nil)
	return func() { call.End() }
}

// AppendPtr appends a record pointer so later End() can mutate schemas in place.
func (s *TrackingContext) AppendPtr(record *FunctionCallRecord) {
	if s == nil || record == nil {
		return
	}
	defer func() { _ = recover() }()
	s.mu.Lock()
	defer s.mu.Unlock()
	if record.UsageflowRequestId == "" && s.UsageflowRequestId != "" {
		record.UsageflowRequestId = s.UsageflowRequestId
	}
	s.CallChain = append(s.CallChain, *record)
	idx := len(s.CallChain) - 1
	s.liveRecords = append(s.liveRecords, liveRecord{idx: idx, ptr: record})
}

type liveRecord struct {
	idx int
	ptr *FunctionCallRecord
}

// FunctionBlockedError wraps a ledger denial for instrumented functions.
type FunctionBlockedError struct {
	Message string
	Code    string
}

func (e *FunctionBlockedError) Error() string {
	if e.Message == "" {
		return ErrFunctionBlocked.Error()
	}
	return fmt.Sprintf("%s: %s", ErrFunctionBlocked.Error(), e.Message)
}

func (e *FunctionBlockedError) Unwrap() error { return ErrFunctionBlocked }

// ExtractTokenUsage finds OpenAI-style usage objects on common result shapes.
func ExtractTokenUsage(result interface{}) *TokenUsage {
	if result == nil {
		return nil
	}
	rv := reflectValue(result)
	if !rv.IsValid() {
		return nil
	}
	if usage := tokenUsageFromValue(rv); usage != nil {
		return usage
	}
	if rv.Kind() == reflect.Struct {
		if f := rv.FieldByName("Usage"); f.IsValid() {
			return tokenUsageFromValue(f)
		}
	}
	if rv.Kind() == reflect.Map {
		if v := rv.MapIndex(reflect.ValueOf("usage")); v.IsValid() {
			return tokenUsageFromValue(v)
		}
	}
	return nil
}

// ExtractAIModel finds a model field on common LLM result shapes.
func ExtractAIModel(result interface{}) string {
	rv := reflectValue(result)
	if !rv.IsValid() {
		return ""
	}
	if rv.Kind() == reflect.Struct {
		if f := rv.FieldByName("Model"); f.IsValid() && f.Kind() == reflect.String {
			return f.String()
		}
	}
	if rv.Kind() == reflect.Map {
		if v := rv.MapIndex(reflect.ValueOf("model")); v.IsValid() && v.Kind() == reflect.String {
			return v.String()
		}
	}
	return ""
}

func reflectValue(v interface{}) reflect.Value {
	rv := reflect.ValueOf(v)
	for rv.Kind() == reflect.Pointer || rv.Kind() == reflect.Interface {
		if rv.IsNil() {
			return reflect.Value{}
		}
		rv = rv.Elem()
	}
	return rv
}

func tokenUsageFromValue(v reflect.Value) *TokenUsage {
	for v.Kind() == reflect.Pointer || v.Kind() == reflect.Interface {
		if v.IsNil() {
			return nil
		}
		v = v.Elem()
	}
	readInt := func(name string, jsonName string) *int {
		if v.Kind() == reflect.Struct {
			if f := v.FieldByName(name); f.IsValid() {
				return intPtrFromValue(f)
			}
		}
		if v.Kind() == reflect.Map {
			if x := v.MapIndex(reflect.ValueOf(jsonName)); x.IsValid() {
				return intPtrFromValue(x)
			}
		}
		return nil
	}
	prompt := readInt("PromptTokens", "prompt_tokens")
	if prompt == nil {
		prompt = readInt("InputTokens", "input_tokens")
	}
	completion := readInt("CompletionTokens", "completion_tokens")
	if completion == nil {
		completion = readInt("ResponseTokens", "response_tokens")
	}
	if completion == nil {
		completion = readInt("OutputTokens", "output_tokens")
	}
	total := readInt("TotalTokens", "total_tokens")
	if prompt == nil && completion == nil && total == nil {
		return nil
	}
	return &TokenUsage{PromptTokens: prompt, CompletionTokens: completion, TotalTokens: total}
}

func intPtrFromValue(v reflect.Value) *int {
	for v.Kind() == reflect.Pointer || v.Kind() == reflect.Interface {
		if v.IsNil() {
			return nil
		}
		v = v.Elem()
	}
	switch v.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		n := int(v.Int())
		return &n
	case reflect.Float32, reflect.Float64:
		n := int(v.Float())
		return &n
	default:
		return nil
	}
}
