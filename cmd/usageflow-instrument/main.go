// Command usageflow-instrument is an optional Go -toolexec wrapper that injects
// UsageFlow call-chain hooks at compile time.
//
// Prefer the higher-level CLI (recommended):
//
//	usageflow go build ./cmd/server
//
// which uses a source overlay so newly injected tracker imports are part of the
// module graph. Use this binary with -toolexec only for advanced builds where
// packages already import pkg/tracker (or via USAGEFLOW_INSTRUMENT_BIN).
package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/usageflow/usageflow-go-middleware/v2/internal/instrument"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usageflow-instrument: expected tool path as first argument (Go -toolexec)")
		os.Exit(2)
	}

	tool := os.Args[1]
	args := os.Args[2:]
	base := filepath.Base(tool)

	// Go may pass compile as "compile" or "compile.exe".
	if base == "compile" || strings.HasPrefix(base, "compile") {
		if err := instrument.Compile(tool, args, instrument.ModulePrefixesFromEnv()); err != nil {
			if ee, ok := err.(*exec.ExitError); ok {
				os.Exit(ee.ExitCode())
			}
			fmt.Fprintf(os.Stderr, "usageflow-instrument: %v\n", err)
			os.Exit(1)
		}
		return
	}

	cmd := exec.Command(tool, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	if err := cmd.Run(); err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			os.Exit(ee.ExitCode())
		}
		fmt.Fprintf(os.Stderr, "usageflow-instrument: %v\n", err)
		os.Exit(1)
	}
}
