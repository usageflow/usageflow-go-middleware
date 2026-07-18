package instrument

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Overlay is a go build -overlay file mapping original sources to rewritten ones.
type Overlay struct {
	Replace map[string]string `json:"Replace"`
}

// PackageInfo is a subset of go list -json output.
type PackageInfo struct {
	Dir        string
	ImportPath string
	GoFiles    []string
	Module     *struct {
		Path string
	}
}

// PrepareOverlay rewrites instrumentable sources under the current module and
// writes an overlay JSON for `go build -overlay=...`.
// Caller must call cleanup when done.
func PrepareOverlay(modulePrefixes []string) (overlayFile string, cleanup func(), err error) {
	pkgs, err := listModulePackages()
	if err != nil {
		return "", nil, err
	}
	if len(modulePrefixes) == 0 {
		if m, err := currentModule(); err == nil && m != "" {
			modulePrefixes = []string{m}
		}
	}

	workDir, err := os.MkdirTemp("", "usageflow-overlay-*")
	if err != nil {
		return "", nil, err
	}
	cleanup = func() { _ = os.RemoveAll(workDir) }

	overlay := Overlay{Replace: map[string]string{}}
	n := 0
	for _, pkg := range pkgs {
		if !shouldInstrumentPackage(pkg.ImportPath, modulePrefixes) {
			continue
		}
		for _, name := range pkg.GoFiles {
			srcPath := filepath.Join(pkg.Dir, name)
			src, err := os.ReadFile(srcPath)
			if err != nil {
				continue
			}
			modulePath := ""
			if pkg.Module != nil {
				modulePath = pkg.Module.Path
			}
			out, changed, err := RewriteFile(src, srcPath, pkg.ImportPath, modulePath)
			if err != nil || !changed {
				continue
			}
			dst := filepath.Join(workDir, fmt.Sprintf("%d_%s", n, name))
			n++
			if err := os.WriteFile(dst, out, 0o644); err != nil {
				cleanup()
				return "", nil, err
			}
			absSrc, err := filepath.Abs(srcPath)
			if err != nil {
				absSrc = srcPath
			}
			overlay.Replace[absSrc] = dst
		}
	}

	overlayPath := filepath.Join(workDir, "overlay.json")
	data, err := json.MarshalIndent(overlay, "", "  ")
	if err != nil {
		cleanup()
		return "", nil, err
	}
	if err := os.WriteFile(overlayPath, data, 0o644); err != nil {
		cleanup()
		return "", nil, err
	}
	return overlayPath, cleanup, nil
}

func listModulePackages() ([]PackageInfo, error) {
	cmd := exec.Command("go", "list", "-json=Dir,ImportPath,GoFiles,Module", "./...")
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("go list: %w", err)
	}
	dec := json.NewDecoder(strings.NewReader(string(out)))
	var pkgs []PackageInfo
	for dec.More() {
		var p PackageInfo
		if err := dec.Decode(&p); err != nil {
			return nil, err
		}
		pkgs = append(pkgs, p)
	}
	return pkgs, nil
}

func currentModule() (string, error) {
	out, err := exec.Command("go", "list", "-m").Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}
