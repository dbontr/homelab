package agentflow

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

const (
	maxCodeMapFiles = 24
	maxCodeFileSize = 64 << 10
	maxCodeMapBytes = 12 << 10
)

type CodeSymbol struct {
	Name string `json:"name"`
	Kind string `json:"kind"`
	Line int    `json:"line"`
}

type CodeOutline struct {
	Path      string       `json:"path"`
	Language  string       `json:"language"`
	Symbols   []CodeSymbol `json:"symbols,omitempty"`
	Imports   []string     `json:"imports,omitempty"`
	Partial   bool         `json:"partial,omitempty"`
	Truncated bool         `json:"truncated,omitempty"`
}

type CodeMap struct {
	Scope     string        `json:"scope"`
	Files     []CodeOutline `json:"files"`
	Truncated bool          `json:"truncated,omitempty"`
}

var (
	rustDeclarations = regexp.MustCompile(`^\s*(?:pub(?:\([^)]*\))?\s+)?(?:async\s+)?(fn|struct|enum|trait|type|const|static|mod)\s+([A-Za-z_][A-Za-z0-9_]*)`)
	rustImports      = regexp.MustCompile(`^\s*(?:pub(?:\([^)]*\))?\s+)?use\s+([^;]+);`)
	pythonDeclarations = regexp.MustCompile(`^\s*(?:async\s+)?(def|class)\s+([A-Za-z_][A-Za-z0-9_]*)`)
	pythonImports      = regexp.MustCompile(`^\s*(?:from\s+([A-Za-z_][A-Za-z0-9_.]*)\s+import\b|import\s+([A-Za-z_][A-Za-z0-9_., ]*))`)
	scriptDeclarations = regexp.MustCompile(`^\s*(?:(?:export|declare|default|async|abstract)\s+)*(function|class|interface|type|enum|const|let)\s+([A-Za-z_$][A-Za-z0-9_$]*)`)
	scriptImports      = regexp.MustCompile(`^\s*(?:import\b.*?\bfrom\s+|import\s+|(?:const|let|var)\s+.*?=\s*require\s*\()\s*["']([^"']+)`)
	javaDeclarations   = regexp.MustCompile(`^\s*(?:(?:public|protected|private|static|final|abstract|sealed)\s+)*(class|interface|enum|record)\s+([A-Za-z_][A-Za-z0-9_]*)`)
	javaImports        = regexp.MustCompile(`^\s*import\s+(?:static\s+)?([A-Za-z_][A-Za-z0-9_.*]*)\s*;`)
	cDeclarations      = regexp.MustCompile(`^\s*(?:static\s+|inline\s+|extern\s+)*[A-Za-z_][A-Za-z0-9_ *]*\s+([A-Za-z_][A-Za-z0-9_]*)\s*\([^;]*\)\s*(?:\{|$)`)
	cImports           = regexp.MustCompile(`^\s*#include\s+[<"]([^>"]+)[>"]`)
	vhdlDeclarations   = regexp.MustCompile(`(?i)^\s*(entity|architecture|package|component|signal|function|procedure)\s+([a-z_][a-z0-9_]*)`)
	vhdlImports        = regexp.MustCompile(`(?i)^\s*use\s+([^;]+);`)
	codeIdentifier     = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,79}$`)
)

func sourceLanguage(filename string) string {
	switch strings.ToLower(path.Ext(filename)) {
	case ".go":
		return "go"
	case ".rs":
		return "rust"
	case ".py":
		return "python"
	case ".ts", ".tsx":
		return "typescript"
	case ".js", ".jsx":
		return "javascript"
	case ".java":
		return "java"
	case ".c", ".cc", ".cpp", ".h", ".hpp":
		return "c-family"
	case ".vhd", ".vhdl":
		return "vhdl"
	default:
		return ""
	}
}

// outlineSource extracts top-level declarations and imports. Go source uses
// the standard AST; other languages use bounded textual outlines rather than
// claiming the precision of a compiler's symbol resolver.
func outlineSource(filename, contents string) (CodeOutline, error) {
	out := CodeOutline{Path: filename, Language: sourceLanguage(filename)}
	if !validSourcePath(filename) || sensitiveSourcePath(filename) || out.Language == "" {
		return out, errors.New("unsupported or protected source path")
	}
	if len(contents) > maxCodeFileSize {
		return out, errors.New("source file exceeds indexing limit")
	}
	addSymbol := func(name, kind string, line int) {
		if name == "" || line < 1 || len(name) > 128 {
			return
		}
		if len(out.Symbols) >= 24 {
			out.Truncated = true
			return
		}
		out.Symbols = append(out.Symbols, CodeSymbol{Name: name, Kind: kind, Line: line})
	}
	addImport := func(spec string) {
		spec = strings.TrimSpace(spec)
		if spec == "" {
			return
		}
		if len(spec) > 180 {
			spec = spec[:180]
		}
		if len(out.Imports) >= 16 {
			out.Truncated = true
			return
		}
		out.Imports = append(out.Imports, spec)
	}
	if out.Language == "go" {
		fset := token.NewFileSet()
		file, parseErr := parser.ParseFile(fset, filename, contents, parser.SkipObjectResolution)
		out.Partial = parseErr != nil
		if file == nil {
			return out, nil
		}
		for _, decl := range file.Decls {
			switch d := decl.(type) {
			case *ast.FuncDecl:
				kind := "function"
				if d.Recv != nil {
					kind = "method"
				}
				addSymbol(d.Name.Name, kind, fset.Position(d.Pos()).Line)
			case *ast.GenDecl:
				for _, spec := range d.Specs {
					switch item := spec.(type) {
					case *ast.ImportSpec:
						quoted, err := strconv.Unquote(item.Path.Value)
						if err == nil {
							addImport(quoted)
						}
					case *ast.TypeSpec:
						addSymbol(item.Name.Name, "type", fset.Position(item.Pos()).Line)
					case *ast.ValueSpec:
						for _, name := range item.Names {
							addSymbol(name.Name, strings.ToLower(d.Tok.String()), fset.Position(name.Pos()).Line)
						}
					}
				}
			}
		}
		return out, nil
	}

	for i, line := range strings.Split(contents, "\n") {
		n := i + 1
		switch out.Language {
		case "rust":
			if m := rustDeclarations.FindStringSubmatch(line); m != nil {
				addSymbol(m[2], m[1], n)
			}
			if m := rustImports.FindStringSubmatch(line); m != nil {
				addImport(m[1])
			}
		case "python":
			if m := pythonDeclarations.FindStringSubmatch(line); m != nil {
				addSymbol(m[2], m[1], n)
			}
			if m := pythonImports.FindStringSubmatch(line); m != nil {
				if m[1] != "" {
					addImport(m[1])
				} else {
					addImport(m[2])
				}
			}
		case "typescript", "javascript":
			if m := scriptDeclarations.FindStringSubmatch(line); m != nil {
				addSymbol(m[2], m[1], n)
			}
			if m := scriptImports.FindStringSubmatch(line); m != nil {
				addImport(m[1])
			}
		case "java":
			if m := javaDeclarations.FindStringSubmatch(line); m != nil {
				addSymbol(m[2], m[1], n)
			}
			if m := javaImports.FindStringSubmatch(line); m != nil {
				addImport(m[1])
			}
		case "c-family":
			if m := cDeclarations.FindStringSubmatch(line); m != nil {
				addSymbol(m[1], "function", n)
			}
			if m := cImports.FindStringSubmatch(line); m != nil {
				addImport(m[1])
			}
		case "vhdl":
			if m := vhdlDeclarations.FindStringSubmatch(line); m != nil {
				addSymbol(m[2], strings.ToLower(m[1]), n)
			}
			if m := vhdlImports.FindStringSubmatch(line); m != nil {
				addImport(m[1])
			}
		}
	}
	return out, nil
}

func validCodeScope(scope string) bool {
	return scope == "" || validSourcePath(scope) && !sensitiveSourcePath(scope)
}

// repositoryMap enumerates source files in a checkout and produces a bounded
// structural summary. The model cannot select a different repository root.
func (e *Engine) repositoryMap(ctx context.Context, w Workflow, brief AgentBrief, scope string) (string, error) {
	if !validCodeScope(scope) {
		return "", errors.New("invalid repository map scope")
	}
	root := repoRoot(brief.Repository)
	target := "."
	if scope != "" {
		target = "./" + scope
	}
	command := "cd " + shellQuote(root) + " && find " + shellQuote(target) +
		" -maxdepth 10 \\( -name .git -o -name .ssh -o -name .aws -o -name .gnupg -o -name node_modules -o -name vendor -o -name target -o -name dist -o -name build \\) -prune -o -type f \\( " +
		"-name '*.go' -o -name '*.rs' -o -name '*.py' -o -name '*.ts' -o -name '*.tsx' -o -name '*.js' -o -name '*.jsx' -o -name '*.java' -o -name '*.c' -o -name '*.cc' -o -name '*.cpp' -o -name '*.h' -o -name '*.hpp' -o -name '*.vhd' -o -name '*.vhdl' " +
		"\\) -print | LC_ALL=C sort | head -n 33"
	result, err := e.workspaceAction(ctx, w, "command", map[string]any{"command": command})
	if err != nil {
		return "", err
	}
	if code, ok := result["exit_code"].(float64); !ok || code != 0 {
		return "", errors.New("source enumeration failed")
	}
	mapped := CodeMap{Scope: scope, Files: make([]CodeOutline, 0, maxCodeMapFiles)}
	rows := strings.Split(strings.TrimSpace(str(result["stdout"])), "\n")
	if len(rows) > maxCodeMapFiles {
		mapped.Truncated = true
	}
	for _, row := range rows {
		if len(mapped.Files) >= maxCodeMapFiles {
			break
		}
		filename := strings.TrimPrefix(strings.TrimSpace(row), "./")
		if !validSourcePath(filename) || sensitiveSourcePath(filename) || sourceLanguage(filename) == "" {
			continue
		}
		// Hidden directories are not source-code discovery targets.
		hidden := false
		for _, component := range strings.Split(filename, "/") {
			if strings.HasPrefix(component, ".") {
				hidden = true
				break
			}
		}
		if hidden {
			continue
		}
		file, err := e.workspaceAction(ctx, w, "read_file", map[string]any{"path": path.Join(root, filename)})
		if err != nil {
			return "", fmt.Errorf("repository map could not read %q: %w", filename, err)
		}
		outline, err := outlineSource(filename, str(file["content"]))
		if err != nil {
			mapped.Truncated = true
			continue
		}
		proposed := append(mapped.Files, outline)
		payload, err := json.Marshal(CodeMap{Scope: scope, Files: proposed, Truncated: mapped.Truncated})
		if err != nil {
			return "", err
		}
		if len(payload) > maxCodeMapBytes {
			mapped.Truncated = true
			break
		}
		mapped.Files = proposed
	}
	raw, err := json.Marshal(mapped)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

func (e *Engine) repositorySymbols(ctx context.Context, w Workflow, brief AgentBrief, filename string) (string, error) {
	if !validSourcePath(filename) || sensitiveSourcePath(filename) || sourceLanguage(filename) == "" {
		return "", errors.New("unsupported source outline path")
	}
	result, err := e.workspaceAction(ctx, w, "read_file", map[string]any{"path": path.Join(repoRoot(brief.Repository), filename)})
	if err != nil {
		return "", err
	}
	outline, err := outlineSource(filename, str(result["content"]))
	if err != nil {
		return "", err
	}
	raw, err := json.Marshal(outline)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

func (e *Engine) repositoryReferences(ctx context.Context, w Workflow, brief AgentBrief, symbol, scope string) (string, error) {
	if !codeIdentifier.MatchString(symbol) || !validCodeScope(scope) {
		return "", errors.New("invalid symbol reference query")
	}
	root := repoRoot(brief.Repository)
	target := "."
	if scope != "" {
		target = "./" + scope
	}
	// No shell expansion of model-supplied content. Search only source files;
	// suppress common credential and dependency directories.
	cmd := "cd " + shellQuote(root) + " && LC_ALL=C grep -R -n -w -F -m 12 " +
		"--exclude-dir=.* --exclude-dir=node_modules --exclude-dir=vendor --exclude-dir=target " +
		"--exclude-dir=dist --exclude-dir=build --include='*.go' --include='*.rs' --include='*.py' " +
		"--include='*.ts' --include='*.tsx' --include='*.js' --include='*.jsx' --include='*.java' " +
		"--include='*.c' --include='*.cc' --include='*.cpp' --include='*.h' --include='*.hpp' " +
		"--include='*.vhd' --include='*.vhdl' -- " + shellQuote(symbol) + " " + shellQuote(target) + " | head -n 80"
	result, err := e.workspaceAction(ctx, w, "command", map[string]any{"command": cmd})
	if err != nil {
		return "", err
	}
	if exit, ok := result["exit_code"].(float64); !ok || exit > 1 || exit < 0 {
		return "", errors.New("symbol reference search failed")
	}
	return boundObservation(result, 8000), nil
}

// Map results are intentionally bounded and heuristic. They help the model
// select further reads; they are not an authoritative compiler dependency graph.
func (m CodeMap) SourceDependencies() []string {
	set := map[string]bool{}
	for _, file := range m.Files {
		for _, imported := range file.Imports {
			set[imported] = true
		}
	}
	deps := make([]string, 0, len(set))
	for imported := range set {
		deps = append(deps, imported)
	}
	sort.Strings(deps)
	return deps
}
