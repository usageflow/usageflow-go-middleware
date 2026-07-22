// Package instrument rewrites Go source to inject UsageFlow call-chain hooks.
package instrument

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"path"
	"path/filepath"
	"strconv"
	"strings"
)

const (
	trackerImportPath = "github.com/usageflow/usageflow-go-middleware/v2/pkg/tracker"
	trackerAlias      = "__uftracker"
	callVarPrefix     = "__ufCall"
	blockVarPrefix    = "__ufBlockErr"
)

// RewriteFile instruments eligible functions in src and returns rewritten Go source.
// pkgPath is the package import path. modulePath is the containing module path (may be empty).
// filePath recorded in hooks is module-relative (e.g. pkg/web/server.go) for ledger uniqueness.
func RewriteFile(src []byte, filename, pkgPath, modulePath string) ([]byte, bool, error) {
	if shouldSkipFile(src, filename) {
		return src, false, nil
	}

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, filename, src, parser.ParseComments)
	if err != nil {
		return nil, false, err
	}

	recordPath := computeRecordPath(filename, pkgPath, modulePath)
	moduleName := path.Base(pkgPath)
	if moduleName == "" || moduleName == "." {
		moduleName = file.Name.Name
	}

	type pending struct {
		fn      *ast.FuncDecl
		ctxExpr ast.Expr
	}
	var targets []pending
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Body == nil {
			continue
		}
		ctxExpr, ok := contextExpr(fn)
		if !ok || alreadyInstrumented(fn) {
			continue
		}
		targets = append(targets, pending{fn: fn, ctxExpr: ctxExpr})
	}
	if len(targets) == 0 {
		return src, false, nil
	}

	trackerName := ensureTrackerImport(file)
	for i, t := range targets {
		injectHook(t.fn, t.ctxExpr, recordPath, pkgPath, moduleName, i, trackerName)
	}

	var buf bytes.Buffer
	if err := format.Node(&buf, fset, file); err != nil {
		return nil, false, err
	}
	return buf.Bytes(), true, nil
}

func computeRecordPath(filename, pkgPath, modulePath string) string {
	base := filepath.Base(filename)
	if modulePath != "" && (pkgPath == modulePath || strings.HasPrefix(pkgPath, modulePath+"/")) {
		rest := strings.TrimPrefix(pkgPath, modulePath)
		rest = strings.TrimPrefix(rest, "/")
		if rest == "" {
			return base
		}
		return path.Join(rest, base)
	}
	if pkgPath != "" && pkgPath != "main" && pkgPath != "command-line-arguments" {
		return path.Join(path.Base(pkgPath), base)
	}
	return base
}

func shouldSkipFile(src []byte, filename string) bool {
	base := filepath.Base(filename)
	if strings.HasSuffix(base, "_test.go") {
		return true
	}
	if strings.Contains(string(src), "Code generated") && strings.Contains(string(src), "DO NOT EDIT") {
		return true
	}
	// Never rewrite UsageFlow's own tracker package (would recurse / break builds).
	if strings.Contains(filename, "usageflow-go-middleware") && strings.Contains(filename, "/pkg/tracker") {
		return true
	}
	return false
}

func alreadyInstrumented(fn *ast.FuncDecl) bool {
	for _, stmt := range fn.Body.List {
		if containsReportCall(stmt) {
			return true
		}
	}
	return false
}

func containsReportCall(n ast.Node) bool {
	found := false
	ast.Inspect(n, func(n ast.Node) bool {
		sel, ok := n.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		if sel.Sel.Name == "ReportCall" || sel.Sel.Name == "BeginCall" {
			found = true
			return false
		}
		return true
	})
	return found
}

// contextExpr returns an expression that yields context.Context for the function.
func contextExpr(fn *ast.FuncDecl) (ast.Expr, bool) {
	if fn.Type.Params == nil || len(fn.Type.Params.List) == 0 {
		return nil, false
	}
	first := fn.Type.Params.List[0]
	if len(first.Names) == 0 {
		// Unnamed param — name it so we can reference it.
		first.Names = []*ast.Ident{ast.NewIdent("ctx")}
	}
	name := first.Names[0]
	if name.Name == "_" {
		name.Name = "__ufCtx"
	}

	if isContextType(first.Type) {
		return name, true
	}
	if isGinContextType(first.Type) {
		// c.Request.Context()
		return &ast.CallExpr{
			Fun: &ast.SelectorExpr{
				X: &ast.SelectorExpr{
					X:   name,
					Sel: ast.NewIdent("Request"),
				},
				Sel: ast.NewIdent("Context"),
			},
		}, true
	}
	return nil, false
}

func isContextType(expr ast.Expr) bool {
	sel, ok := expr.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	pkg, ok := sel.X.(*ast.Ident)
	return ok && pkg.Name == "context" && sel.Sel.Name == "Context"
}

func isGinContextType(expr ast.Expr) bool {
	star, ok := expr.(*ast.StarExpr)
	if !ok {
		return false
	}
	sel, ok := star.X.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	pkg, ok := sel.X.(*ast.Ident)
	return ok && pkg.Name == "gin" && sel.Sel.Name == "Context"
}

func ginContextIdent(fn *ast.FuncDecl) *ast.Ident {
	if fn.Type.Params == nil || len(fn.Type.Params.List) == 0 {
		return nil
	}
	first := fn.Type.Params.List[0]
	if !isGinContextType(first.Type) || len(first.Names) == 0 {
		return nil
	}
	return first.Names[0]
}

func injectHook(fn *ast.FuncDecl, ctxExpr ast.Expr, fileBase, pkgPath, moduleName string, index int, trackerPkg string) {
	funcName := fn.Name.Name
	if fn.Recv != nil && len(fn.Recv.List) > 0 {
		recvType := typeString(fn.Recv.List[0].Type)
		funcName = fmt.Sprintf("(%s).%s", recvType, fn.Name.Name)
	}

	resultNames := ensureNamedResults(fn)
	argExprs := nonContextArgs(fn)
	callVar := ast.NewIdent(fmt.Sprintf("%s_%d", callVarPrefix, index))
	blockVar := ast.NewIdent(fmt.Sprintf("%s_%d", blockVarPrefix, index))

	beginCall := &ast.CallExpr{
		Fun: &ast.SelectorExpr{
			X:   ast.NewIdent(trackerPkg),
			Sel: ast.NewIdent("BeginCall"),
		},
		Args: []ast.Expr{
			ctxExpr,
			stringLit(funcName),
			stringLit(fileBase),
			stringLit(moduleName),
			&ast.CallExpr{
				Fun: &ast.SelectorExpr{
					X:   ast.NewIdent(trackerPkg),
					Sel: ast.NewIdent("Args"),
				},
				Args: argExprs,
			},
		},
	}

	prelude := []ast.Stmt{}

	// If the function returns error, honor policy blocks like JS sendAsync denials.
	if errName, ok := lastErrorResult(resultNames, fn); ok {
		assign := &ast.AssignStmt{
			Lhs: []ast.Expr{callVar, blockVar},
			Tok: token.DEFINE,
			Rhs: []ast.Expr{beginCall},
		}
		assignErr := &ast.AssignStmt{
			Lhs: []ast.Expr{ast.NewIdent(errName)},
			Tok: token.ASSIGN,
			Rhs: []ast.Expr{blockVar},
		}
		prelude = append(prelude, assign, &ast.IfStmt{
			Cond: &ast.BinaryExpr{
				X:  blockVar,
				Op: token.NEQ,
				Y:  ast.NewIdent("nil"),
			},
			Body: &ast.BlockStmt{List: []ast.Stmt{assignErr, &ast.ReturnStmt{}}},
		})
	} else if ginIdent := ginContextIdent(fn); ginIdent != nil {
		// Gin handlers have no error return — abort the HTTP request on denial.
		assign := &ast.AssignStmt{
			Lhs: []ast.Expr{callVar, blockVar},
			Tok: token.DEFINE,
			Rhs: []ast.Expr{beginCall},
		}
		abortCall := &ast.ExprStmt{X: &ast.CallExpr{
			Fun: &ast.SelectorExpr{
				X:   ginIdent,
				Sel: ast.NewIdent("AbortWithStatusJSON"),
			},
			Args: []ast.Expr{
				&ast.BasicLit{Kind: token.INT, Value: "429"},
				&ast.CompositeLit{
					Type: &ast.MapType{
						Key:   ast.NewIdent("string"),
						Value: &ast.InterfaceType{Methods: &ast.FieldList{}},
					},
					Elts: []ast.Expr{
						&ast.KeyValueExpr{
							Key:   stringLit("error"),
							Value: stringLit("rate_limit_exceeded"),
						},
						&ast.KeyValueExpr{
							Key: stringLit("message"),
							Value: &ast.CallExpr{
								Fun: &ast.SelectorExpr{
									X:   blockVar,
									Sel: ast.NewIdent("Error"),
								},
							},
						},
					},
				},
			},
		}}
		prelude = append(prelude, assign, &ast.IfStmt{
			Cond: &ast.BinaryExpr{
				X:  blockVar,
				Op: token.NEQ,
				Y:  ast.NewIdent("nil"),
			},
			Body: &ast.BlockStmt{List: []ast.Stmt{abortCall, &ast.ReturnStmt{}}},
		})
	} else {
		// No error return and not gin — discard block error (cannot abort without changing signature).
		assign := &ast.AssignStmt{
			Lhs: []ast.Expr{callVar, ast.NewIdent("_")},
			Tok: token.DEFINE,
			Rhs: []ast.Expr{beginCall},
		}
		prelude = append(prelude, assign)
	}

	endArgs := make([]ast.Expr, 0, len(resultNames))
	for _, name := range resultNames {
		endArgs = append(endArgs, ast.NewIdent(name))
	}
	deferStmt := &ast.DeferStmt{
		Call: &ast.CallExpr{
			Fun: &ast.FuncLit{
				Type: &ast.FuncType{Params: &ast.FieldList{}},
				Body: &ast.BlockStmt{
					List: []ast.Stmt{
						&ast.ExprStmt{
							X: &ast.CallExpr{
								Fun: &ast.SelectorExpr{
									X:   callVar,
									Sel: ast.NewIdent("End"),
								},
								Args: endArgs,
							},
						},
					},
				},
			},
		},
	}
	prelude = append(prelude, deferStmt)

	body := make([]ast.Stmt, 0, len(fn.Body.List)+len(prelude))
	body = append(body, prelude...)
	body = append(body, fn.Body.List...)
	fn.Body.List = body
	_ = pkgPath
}

func nonContextArgs(fn *ast.FuncDecl) []ast.Expr {
	if fn.Type.Params == nil {
		return nil
	}
	var out []ast.Expr
	for i, field := range fn.Type.Params.List {
		if i == 0 && (isContextType(field.Type) || isGinContextType(field.Type)) {
			continue
		}
		if len(field.Names) == 0 {
			continue
		}
		for _, name := range field.Names {
			if name.Name == "_" {
				continue
			}
			out = append(out, name)
		}
	}
	return out
}

func ensureNamedResults(fn *ast.FuncDecl) []string {
	if fn.Type.Results == nil || len(fn.Type.Results.List) == 0 {
		return nil
	}
	var names []string
	idx := 0
	for _, field := range fn.Type.Results.List {
		if len(field.Names) == 0 {
			name := fmt.Sprintf("__ufR%d", idx)
			if isErrorType(field.Type) {
				name = "__ufErr"
			}
			field.Names = []*ast.Ident{ast.NewIdent(name)}
			names = append(names, name)
			idx++
			continue
		}
		for _, id := range field.Names {
			if id.Name == "_" {
				id.Name = fmt.Sprintf("__ufR%d", idx)
			}
			names = append(names, id.Name)
			idx++
		}
	}
	return names
}

func lastErrorResult(names []string, fn *ast.FuncDecl) (string, bool) {
	if len(names) == 0 || fn.Type.Results == nil {
		return "", false
	}
	// Walk results in order to find last error-typed result name.
	var lastErr string
	i := 0
	for _, field := range fn.Type.Results.List {
		n := len(field.Names)
		if n == 0 {
			n = 1
		}
		for j := 0; j < n; j++ {
			if i < len(names) && isErrorType(field.Type) {
				lastErr = names[i]
			}
			i++
		}
	}
	if lastErr == "" {
		return "", false
	}
	return lastErr, true
}

func isErrorType(expr ast.Expr) bool {
	if id, ok := expr.(*ast.Ident); ok && id.Name == "error" {
		return true
	}
	return false
}

func typeString(expr ast.Expr) string {
	switch t := expr.(type) {
	case *ast.StarExpr:
		return "*" + typeString(t.X)
	case *ast.Ident:
		return t.Name
	case *ast.SelectorExpr:
		return typeString(t.X) + "." + t.Sel.Name
	default:
		return "T"
	}
}

func stringLit(s string) *ast.BasicLit {
	return &ast.BasicLit{Kind: token.STRING, Value: strconv.Quote(s)}
}

// ensureTrackerImport returns the identifier used to reference the tracker package.
// Prefer an existing import name; otherwise add an aliased import.
func ensureTrackerImport(file *ast.File) string {
	for _, imp := range file.Imports {
		path, err := strconv.Unquote(imp.Path.Value)
		if err != nil || path != trackerImportPath {
			continue
		}
		if imp.Name == nil {
			return "tracker" // default name from package clause
		}
		if imp.Name.Name == "." || imp.Name.Name == "_" {
			// Cannot use blank/dot import for qualified ReportCall — add aliased import.
			break
		}
		return imp.Name.Name
	}

	newImp := &ast.ImportSpec{
		Name: ast.NewIdent(trackerAlias),
		Path: &ast.BasicLit{Kind: token.STRING, Value: strconv.Quote(trackerImportPath)},
	}

	var importDecl *ast.GenDecl
	for _, decl := range file.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if ok && gd.Tok == token.IMPORT {
			importDecl = gd
			break
		}
	}
	if importDecl == nil {
		importDecl = &ast.GenDecl{Tok: token.IMPORT, Lparen: token.Pos(1)}
		file.Decls = append([]ast.Decl{importDecl}, file.Decls...)
	}
	importDecl.Specs = append(importDecl.Specs, newImp)
	file.Imports = append(file.Imports, newImp)
	return trackerAlias
}
