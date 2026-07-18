// Command usageflow wraps the Go toolchain with UsageFlow compile-time instrumentation.
//
//	usageflow go build ./cmd/server
//	usageflow go test ./...
package main

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/usageflow/usageflow-go-middleware/v2/internal/instrument"
)

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(2)
	}

	switch os.Args[1] {
	case "go":
		os.Exit(runGo(os.Args[2:]))
	case "help", "-h", "--help":
		printUsage()
	default:
		fmt.Fprintf(os.Stderr, "usageflow: unknown command %q\n\n", os.Args[1])
		printUsage()
		os.Exit(2)
	}
}

func printUsage() {
	fmt.Fprintf(os.Stderr, `UsageFlow Go agent CLI

Instrument customer packages at build time (JS/Python-style call chains without Track/Wrap):

  usageflow go build ./cmd/server
  usageflow go test ./...

Runtime: use Gin middleware.RequestInterceptor() so request context + report_call_chain work.

v1 instruments functions whose first parameter is context.Context or *gin.Context.
`)
}

func runGo(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usageflow: missing go subcommand (e.g. build, test)")
		return 2
	}

	modulePath, err := currentModulePath()
	prefixes := []string{}
	if err != nil {
		fmt.Fprintf(os.Stderr, "usageflow: warning: could not detect module path: %v\n", err)
	} else if modulePath != "" {
		prefixes = []string{modulePath}
	}

	overlayFile, cleanup, err := instrument.PrepareOverlay(prefixes)
	if err != nil {
		fmt.Fprintf(os.Stderr, "usageflow: prepare overlay: %v\n", err)
		return 1
	}
	defer cleanup()

	// Prepend -overlay so go sees rewritten sources (and new tracker imports) in the import graph.
	goArgs := append([]string{}, args...)
	goArgs = insertOverlayFlag(goArgs, overlayFile)

	cmd := exec.Command("go", goArgs...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	cmd.Env = os.Environ()
	if modulePath != "" {
		cmd.Env = setEnv(cmd.Env, "USAGEFLOW_INSTRUMENT_MODULE", modulePath)
	}

	if os.Getenv("USAGEFLOW_INSTRUMENT_DEBUG") == "1" {
		fmt.Fprintf(os.Stderr, "usageflow: go %s\n", strings.Join(goArgs, " "))
	}

	if err := cmd.Run(); err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			return ee.ExitCode()
		}
		fmt.Fprintf(os.Stderr, "usageflow: %v\n", err)
		return 1
	}
	return 0
}

func insertOverlayFlag(args []string, overlayFile string) []string {
	// go build [build flags] packages
	// Insert after subcommand (build/test/run).
	if len(args) == 0 {
		return []string{"-overlay", overlayFile}
	}
	out := make([]string, 0, len(args)+2)
	out = append(out, args[0], "-overlay", overlayFile)
	out = append(out, args[1:]...)
	return out
}

func currentModulePath() (string, error) {
	out, err := exec.Command("go", "list", "-m").Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func setEnv(env []string, key, value string) []string {
	prefix := key + "="
	out := make([]string, 0, len(env)+1)
	found := false
	for _, e := range env {
		if strings.HasPrefix(e, prefix) {
			out = append(out, prefix+value)
			found = true
			continue
		}
		out = append(out, e)
	}
	if !found {
		out = append(out, prefix+value)
	}
	return out
}
