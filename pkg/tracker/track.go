package tracker

import (
	"context"
	"fmt"
	"reflect"
	"time"
)

// Options configures a tracked function call.
type Options struct {
	FuncName     string
	FilePath     string
	ModuleName   string
	ParamsSchema map[string]interface{}
	// CaptureResultSchema, when true, stores a shallow type schema of the return value.
	CaptureResultSchema bool
	Usage               *TokenUsage
	AIModel             string
	UserPrompt          string
}

// Track runs fn and appends a FunctionCallRecord to the request call chain.
// Tracking failures never affect the return value of fn (fail soft).
func Track[T any](ctx context.Context, opts Options, fn func() (T, error)) (T, error) {
	store := FromContext(ctx)
	if store == nil || !IsEnabled() {
		return fn()
	}

	start := time.Now()
	result, err := fn()
	duration := time.Since(start).Milliseconds()

	record := FunctionCallRecord{
		FuncName:     opts.FuncName,
		FilePath:     opts.FilePath,
		ModuleName:   opts.ModuleName,
		ParamsSchema: opts.ParamsSchema,
		Usage:        opts.Usage,
		AIModel:      opts.AIModel,
		UserPrompt:   opts.UserPrompt,
		DurationMs:   duration,
	}
	if opts.CaptureResultSchema && err == nil {
		record.ResultSchema = ExtractSchema(result, "", 0, nil)
	}
	store.Append(record)
	return result, err
}

// TrackErr is Track for functions that only return an error.
func TrackErr(ctx context.Context, opts Options, fn func() error) error {
	_, err := Track(ctx, opts, func() (struct{}, error) {
		return struct{}{}, fn()
	})
	return err
}

// Wrap returns a context-aware wrapper that records each invocation via Track.
func Wrap[T any](opts Options, fn func(ctx context.Context) (T, error)) func(ctx context.Context) (T, error) {
	return func(ctx context.Context) (T, error) {
		return Track(ctx, opts, func() (T, error) {
			return fn(ctx)
		})
	}
}

// WrapErr returns a context-aware wrapper for error-only functions.
func WrapErr(opts Options, fn func(ctx context.Context) error) func(ctx context.Context) error {
	return func(ctx context.Context) error {
		return TrackErr(ctx, opts, func() error {
			return fn(ctx)
		})
	}
}

func shallowSchema(v interface{}) interface{} {
	if v == nil {
		return map[string]interface{}{"type": "null"}
	}
	rv := reflect.ValueOf(v)
	for rv.Kind() == reflect.Pointer {
		if rv.IsNil() {
			return map[string]interface{}{"type": "null"}
		}
		rv = rv.Elem()
	}
	switch rv.Kind() {
	case reflect.Struct:
		fields := make(map[string]interface{}, rv.NumField())
		rt := rv.Type()
		for i := 0; i < rv.NumField(); i++ {
			f := rt.Field(i)
			if f.PkgPath != "" {
				continue // unexported
			}
			fields[f.Name] = map[string]interface{}{"type": typeName(f.Type)}
		}
		return map[string]interface{}{"type": "object", "fields": fields}
	case reflect.Slice, reflect.Array:
		return map[string]interface{}{"type": "array", "elem": typeName(rv.Type().Elem())}
	case reflect.Map:
		return map[string]interface{}{"type": "map", "key": typeName(rv.Type().Key()), "elem": typeName(rv.Type().Elem())}
	default:
		return map[string]interface{}{"type": typeName(rv.Type())}
	}
}

func typeName(t reflect.Type) string {
	if t == nil {
		return "null"
	}
	if t.Name() != "" {
		if t.PkgPath() != "" {
			return fmt.Sprintf("%s.%s", t.PkgPath(), t.Name())
		}
		return t.Name()
	}
	return t.String()
}
