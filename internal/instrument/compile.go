package instrument

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Compile intercepts a go tool compile invocation, rewrites eligible .go files,
// and runs the real compiler with the rewritten sources.
func Compile(compileBin string, args []string, modulePrefixes []string) error {
	pkgPath := packagePathFromArgs(args)
	debug := os.Getenv("USAGEFLOW_INSTRUMENT_DEBUG") == "1"
	if debug {
		fmt.Fprintf(os.Stderr, "usageflow-instrument: pkg=%q prefixes=%v instrument=%v\n",
			pkgPath, modulePrefixes, shouldInstrumentPackage(pkgPath, modulePrefixes))
	}
	if pkgPath == "" || !shouldInstrumentPackage(pkgPath, modulePrefixes) {
		return run(compileBin, args)
	}

	workDir, err := os.MkdirTemp("", "usageflow-instrument-*")
	if err != nil {
		return run(compileBin, args)
	}
	defer os.RemoveAll(workDir)

	newArgs := make([]string, 0, len(args))
	rewroteAny := false
	for _, arg := range args {
		if !isGoSourceArg(arg) {
			newArgs = append(newArgs, arg)
			continue
		}
		src, err := os.ReadFile(arg)
		if err != nil {
			newArgs = append(newArgs, arg)
			continue
		}
		out, changed, err := RewriteFile(src, arg, pkgPath, modulePathFromPrefixes(modulePrefixes, pkgPath))
		if err != nil {
			if debug {
				fmt.Fprintf(os.Stderr, "usageflow-instrument: rewrite error %s: %v\n", arg, err)
			}
			newArgs = append(newArgs, arg)
			continue
		}
		if !changed {
			newArgs = append(newArgs, arg)
			continue
		}
		dst := filepath.Join(workDir, filepath.Base(arg))
		// Avoid collisions when multiple files share a basename across dirs.
		if _, err := os.Stat(dst); err == nil {
			dst = filepath.Join(workDir, fmt.Sprintf("%d_%s", len(newArgs), filepath.Base(arg)))
		}
		if err := os.WriteFile(dst, out, 0o644); err != nil {
			newArgs = append(newArgs, arg)
			continue
		}
		if debug {
			fmt.Fprintf(os.Stderr, "usageflow-instrument: rewrote %s -> %s\n", arg, dst)
		}
		newArgs = append(newArgs, dst)
		rewroteAny = true
	}

	if !rewroteAny {
		if debug {
			fmt.Fprintf(os.Stderr, "usageflow-instrument: no files rewritten for %q\n", pkgPath)
		}
		return run(compileBin, args)
	}
	return run(compileBin, newArgs)
}

func packagePathFromArgs(args []string) string {
	for i := 0; i < len(args)-1; i++ {
		if args[i] == "-p" {
			return args[i+1]
		}
	}
	return ""
}

func shouldInstrumentPackage(pkgPath string, modulePrefixes []string) bool {
	if pkgPath == "" || isAgentInternal(pkgPath) {
		return false
	}
	// Command packages are compiled with -p main (not the module path).
	if pkgPath == "main" {
		return true
	}
	if len(modulePrefixes) == 0 {
		return !isStdOrInfra(pkgPath)
	}
	for _, prefix := range modulePrefixes {
		prefix = strings.TrimSpace(prefix)
		if prefix == "" {
			continue
		}
		if pkgPath == prefix || strings.HasPrefix(pkgPath, prefix+"/") {
			return true
		}
	}
	return false
}

func isAgentInternal(pkgPath string) bool {
	const root = "github.com/usageflow/usageflow-go-middleware"
	if !strings.HasPrefix(pkgPath, root) {
		return false
	}
	// Allow instrumenting examples and testdata inside this repo.
	if strings.Contains(pkgPath, "/examples/") || strings.Contains(pkgPath, "/testdata/") {
		return false
	}
	return strings.Contains(pkgPath, "/pkg/") ||
		strings.Contains(pkgPath, "/cmd/") ||
		strings.Contains(pkgPath, "/internal/")
}

func isStdOrInfra(pkgPath string) bool {
	if !strings.Contains(pkgPath, ".") {
		return true // stdlib
	}
	infra := []string{
		"github.com/gin-gonic/gin",
		"github.com/gorilla/",
		"golang.org/",
		"google.golang.org/",
		"github.com/usageflow/usageflow-go-middleware",
	}
	for _, p := range infra {
		if pkgPath == strings.TrimSuffix(p, "/") || strings.HasPrefix(pkgPath, p) {
			return true
		}
	}
	return false
}

func isGoSourceArg(arg string) bool {
	if strings.HasPrefix(arg, "-") {
		return false
	}
	return strings.HasSuffix(arg, ".go")
}

func run(bin string, args []string) error {
	cmd := exec.Command(bin, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	return cmd.Run()
}

// ModulePrefixesFromEnv reads USAGEFLOW_INSTRUMENT_MODULE / MODULES.
func ModulePrefixesFromEnv() []string {
	if v := strings.TrimSpace(os.Getenv("USAGEFLOW_INSTRUMENT_MODULES")); v != "" {
		parts := strings.Split(v, ",")
		out := make([]string, 0, len(parts))
		for _, p := range parts {
			p = strings.TrimSpace(p)
			if p != "" {
				out = append(out, p)
			}
		}
		return out
	}
	if v := strings.TrimSpace(os.Getenv("USAGEFLOW_INSTRUMENT_MODULE")); v != "" {
		return []string{v}
	}
	return nil
}

func modulePathFromPrefixes(prefixes []string, pkgPath string) string {
	for _, prefix := range prefixes {
		prefix = strings.TrimSpace(prefix)
		if prefix == "" {
			continue
		}
		if pkgPath == prefix || strings.HasPrefix(pkgPath, prefix+"/") {
			return prefix
		}
	}
	if len(prefixes) == 1 {
		return prefixes[0]
	}
	return ""
}
