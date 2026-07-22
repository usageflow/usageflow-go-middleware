package instrument

import (
	"strings"
	"testing"
)

func TestRewriteFile_InjectsBeginCall(t *testing.T) {
	src := []byte(`package sample

import "context"

func ListUsers(ctx context.Context) ([]string, error) {
	return []string{"a"}, nil
}

func NoContext(id string) string {
	return id
}
`)
	out, changed, err := RewriteFile(src, "users.go", "example.com/app/sample", "example.com/app")
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("expected rewrite")
	}
	s := string(out)
	if !strings.Contains(s, `__uftracker.BeginCall`) {
		t.Fatalf("missing BeginCall hook:\n%s", s)
	}
	if !strings.Contains(s, `__uftracker.Args`) {
		t.Fatalf("missing Args capture:\n%s", s)
	}
	if !strings.Contains(s, `.End(`) {
		t.Fatalf("missing End defer:\n%s", s)
	}
	if !strings.Contains(s, trackerImportPath) {
		t.Fatalf("missing tracker import:\n%s", s)
	}
	if !strings.Contains(s, `"sample/users.go"`) {
		t.Fatalf("missing module-relative path:\n%s", s)
	}
	if !strings.Contains(s, `"ListUsers"`) {
		t.Fatalf("missing func name:\n%s", s)
	}
	// Block-on-call for functions returning error
	if !strings.Contains(s, `__ufBlockErr_0`) {
		t.Fatalf("missing block-on-call check:\n%s", s)
	}
	// NoContext should remain without hook
	if strings.Count(s, "BeginCall") != 1 {
		t.Fatalf("expected exactly one BeginCall, got source:\n%s", s)
	}
}

func TestRewriteFile_GinContext(t *testing.T) {
	src := []byte(`package sample

import "github.com/gin-gonic/gin"

func Handle(c *gin.Context) {
	c.Status(200)
}
`)
	out, changed, err := RewriteFile(src, "handler.go", "example.com/app", "example.com/app")
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("expected rewrite")
	}
	s := string(out)
	if !strings.Contains(s, "c.Request.Context()") {
		t.Fatalf("expected gin context extraction:\n%s", s)
	}
	if !strings.Contains(s, "AbortWithStatusJSON") {
		t.Fatalf("expected gin abort on function policy block:\n%s", s)
	}
	if !strings.Contains(s, "429") {
		t.Fatalf("expected HTTP 429 on function policy block:\n%s", s)
	}
}

func TestRewriteFile_Idempotent(t *testing.T) {
	src := []byte(`package sample

import "context"

func ListUsers(ctx context.Context) ([]string, error) {
	return []string{"a"}, nil
}
`)
	once, _, err := RewriteFile(src, "users.go", "example.com/app", "example.com/app")
	if err != nil {
		t.Fatal(err)
	}
	twice, changed, err := RewriteFile(once, "users.go", "example.com/app", "example.com/app")
	if err != nil {
		t.Fatal(err)
	}
	if changed {
		t.Fatalf("second rewrite should be no-op:\n%s", twice)
	}
}

func TestRewriteFile_MethodReceiver(t *testing.T) {
	src := []byte(`package sample

import "context"

type Svc struct{}

func (s *Svc) Get(ctx context.Context, id string) (string, error) {
	return id, nil
}
`)
	out, changed, err := RewriteFile(src, "svc.go", "example.com/app", "example.com/app")
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("expected rewrite")
	}
	if !strings.Contains(string(out), `"(*Svc).Get"`) {
		t.Fatalf("expected method name:\n%s", out)
	}
}

func TestShouldInstrumentPackage(t *testing.T) {
	if !shouldInstrumentPackage("example.com/myapp/svc", []string{"example.com/myapp"}) {
		t.Fatal("expected customer package")
	}
	if !shouldInstrumentPackage("main", []string{"example.com/myapp"}) {
		t.Fatal("command package main must be instrumented")
	}
	if shouldInstrumentPackage("github.com/usageflow/usageflow-go-middleware/v2/pkg/tracker", []string{"github.com/usageflow/usageflow-go-middleware/v2"}) {
		t.Fatal("must skip agent pkg")
	}
	if !shouldInstrumentPackage("github.com/usageflow/usageflow-go-middleware/v2/examples/basic", []string{"github.com/usageflow/usageflow-go-middleware/v2"}) {
		t.Fatal("examples should be allowed when under module prefix")
	}
}
