package instrument_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestOverlayBuildRecordsHooks(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	repoRoot := findRepoRoot(t)
	tmp := t.TempDir()

	cliBin := filepath.Join(tmp, "usageflow")
	if runtime.GOOS == "windows" {
		cliBin += ".exe"
	}
	build := exec.Command("go", "build", "-o", cliBin, "./cmd/usageflow")
	build.Dir = repoRoot
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build usageflow cli: %v\n%s", err, out)
	}

	modDir := filepath.Join(tmp, "app")
	if err := os.MkdirAll(modDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// No manual tracker import — overlay instrumentation must add it.
	goMod := `module example.com/ufapp

go 1.23.1

require github.com/usageflow/usageflow-go-middleware/v2 v2.0.0

replace github.com/usageflow/usageflow-go-middleware/v2 => ` + repoRoot + `
`
	mainGo := `package main

import (
	"context"
	"fmt"

	"github.com/usageflow/usageflow-go-middleware/v2/pkg/tracker"
)

func greet(ctx context.Context, name string) string {
	return "hi " + name
}

func main() {
	tracker.Enable()
	ctx, store := tracker.WithTracking(context.Background(), &tracker.RequestContext{Method: "GET", URL: "/"}, "req")
	_ = greet(ctx, "world")
	chain := store.Snapshot()
	if len(chain) == 0 {
		fmt.Print("EMPTY")
		return
	}
	fmt.Print(chain[0].FuncName)
}
`
	if err := os.WriteFile(filepath.Join(modDir, "go.mod"), []byte(goMod), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(modDir, "main.go"), []byte(mainGo), 0o644); err != nil {
		t.Fatal(err)
	}

	tidy := exec.Command("go", "mod", "tidy")
	tidy.Dir = modDir
	if out, err := tidy.CombinedOutput(); err != nil {
		t.Fatalf("go mod tidy: %v\n%s", err, out)
	}

	outBin := filepath.Join(modDir, "appbin")
	buildApp := exec.Command(cliBin, "go", "build", "-o", outBin, ".")
	buildApp.Dir = modDir
	buildApp.Env = cleanEnv()
	if out, err := buildApp.CombinedOutput(); err != nil {
		t.Fatalf("usageflow go build failed: %v\n%s", err, out)
	}

	run := exec.Command(outBin)
	out, err := run.CombinedOutput()
	if err != nil {
		t.Fatalf("run app: %v\n%s", err, out)
	}
	got := strings.TrimSpace(string(out))
	if got != "greet" {
		t.Fatalf("expected call chain funcName greet, got %q", got)
	}
}

func TestOverlayBuildWithoutTrackerImport(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	repoRoot := findRepoRoot(t)
	tmp := t.TempDir()

	cliBin := filepath.Join(tmp, "usageflow")
	build := exec.Command("go", "build", "-o", cliBin, "./cmd/usageflow")
	build.Dir = repoRoot
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build usageflow cli: %v\n%s", err, out)
	}

	modDir := filepath.Join(tmp, "app2")
	if err := os.MkdirAll(modDir, 0o755); err != nil {
		t.Fatal(err)
	}

	goMod := `module example.com/ufapp2

go 1.23.1

require github.com/usageflow/usageflow-go-middleware/v2 v2.0.0

replace github.com/usageflow/usageflow-go-middleware/v2 => ` + repoRoot + `
`
	// Application code with zero UsageFlow imports in the service function file.
	mainGo := `package main

import (
	"context"
	"fmt"
	"os"

	"github.com/usageflow/usageflow-go-middleware/v2/pkg/tracker"
)

func greet(ctx context.Context, name string) string {
	return "hi " + name
}

func main() {
	tracker.Enable()
	ctx, store := tracker.WithTracking(context.Background(), nil, "req")
	msg := greet(ctx, "world")
	fmt.Print(msg, "|", len(store.Snapshot()))
	_ = os.Args
}
`
	svcGo := `package main

import "context"

func helper(ctx context.Context) string {
	return "ok"
}
`
	_ = svcGo
	if err := os.WriteFile(filepath.Join(modDir, "go.mod"), []byte(goMod), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(modDir, "main.go"), []byte(mainGo), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(modDir, "svc.go"), []byte(svcGo), 0o644); err != nil {
		t.Fatal(err)
	}

	tidy := exec.Command("go", "mod", "tidy")
	tidy.Dir = modDir
	if out, err := tidy.CombinedOutput(); err != nil {
		t.Fatalf("tidy: %v\n%s", err, out)
	}

	outBin := filepath.Join(modDir, "appbin")
	buildApp := exec.Command(cliBin, "go", "build", "-o", outBin, ".")
	buildApp.Dir = modDir
	buildApp.Env = cleanEnv()
	if out, err := buildApp.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}

	run := exec.Command(outBin)
	out, err := run.CombinedOutput()
	if err != nil {
		t.Fatalf("run: %v\n%s", err, out)
	}
	// greet + helper both context funcs; main only calls greet → at least 1
	if !strings.Contains(string(out), "|1") && !strings.Contains(string(out), "|2") {
		t.Fatalf("expected recorded calls, got %q", out)
	}
}

func cleanEnv(extra ...string) []string {
	out := make([]string, 0, len(os.Environ())+len(extra))
	for _, e := range os.Environ() {
		if strings.HasPrefix(e, "USAGEFLOW_INSTRUMENT_DEBUG=") {
			continue
		}
		out = append(out, e)
	}
	return append(out, extra...)
}

func findRepoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("no caller")
	}
	dir := filepath.Dir(file)
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.mod not found")
		}
		dir = parent
	}
}
