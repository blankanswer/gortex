package hooks

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestRunKimiUserPromptSubmitPlainStdout(t *testing.T) {
	cwd := writeGortexProjectMarker(t, t.TempDir())
	restore := stubUserPromptProbe(t, []grepSymbolHit{
		{Name: "ValidateToken", Kind: "function", FilePath: "internal/auth/token.go", Line: 42},
	}, nil)
	defer restore()

	out := captureStdout(t, func() {
		runKimi(kimiPromptPayload(cwd, "fix auth token validation"), 0)
	})
	if out == "" {
		t.Fatal("expected Kimi prompt context, got empty output")
	}
	if strings.Contains(out, "hookSpecificOutput") || strings.Contains(out, "additionalContext") {
		t.Fatalf("Kimi dispatcher should emit plain stdout, got JSON-shaped output:\n%s", out)
	}
	if !strings.Contains(out, "ValidateToken") {
		t.Fatalf("missing symbol context:\n%s", out)
	}
}

func TestRunKimiUserPromptSubmitContentParts(t *testing.T) {
	root := writeKimiProjectMCP(t, t.TempDir())
	cwd := filepath.Join(root, "nested", "worktree")
	if err := os.MkdirAll(cwd, 0o755); err != nil {
		t.Fatal(err)
	}
	restore := stubUserPromptProbe(t, []grepSymbolHit{
		{Name: "AuthMiddleware", Kind: "type", FilePath: "internal/auth/mw.go"},
	}, nil)
	defer restore()

	out := captureStdout(t, func() {
		runKimi([]byte(`{"hook_event_name":"UserPromptSubmit","cwd":`+strconv.Quote(cwd)+`,"prompt":[{"type":"text","text":"trace auth middleware"},{"type":"image","source":"ignored"}]}`), 0)
	})
	if !strings.Contains(out, "AuthMiddleware") {
		t.Fatalf("content-parts prompt did not produce context:\n%s", out)
	}
}

func TestRunKimiPreToolUseGortexReadPlainStdout(t *testing.T) {
	withForceCompress(t, true)
	cwd := writeGortexProjectMarker(t, t.TempDir())
	tests := []struct {
		name string
		tool string
	}{
		{
			name: "kimi plain read_file",
			tool: "read_file",
		},
		{
			name: "kimi plain get_editing_context",
			tool: "get_editing_context",
		},
		{
			name: "prefixed read_file",
			tool: gortexReadFileTool,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out := captureStdout(t, func() {
				runKimi(kimiPreToolPayload(cwd, tt.tool, `{"path":"internal/a.go"}`), 0)
			})
			if out == "" {
				t.Fatal("expected Kimi MCP read PreToolUse guidance, got empty output")
			}
			for _, want := range []string{"compress_bodies", "search_text", "keep"} {
				if !strings.Contains(out, want) {
					t.Fatalf("stdout guidance missing %q: %q", want, out)
				}
			}
			for _, notWant := range []string{"hookSpecificOutput", "additionalContext", "permissionDecision"} {
				if strings.Contains(out, notWant) {
					t.Fatalf("Kimi PreToolUse nudge must stay plain stdout, got %q", out)
				}
			}
		})
	}
}

func TestRunKimiPreToolUseGortexReadSilentShapes(t *testing.T) {
	withForceCompress(t, false)
	cwd := writeGortexProjectMarker(t, t.TempDir())
	tests := []struct {
		name  string
		cwd   string
		tool  string
		input string
	}{
		{
			name:  "compressed",
			cwd:   cwd,
			tool:  "read_file",
			input: `{"path":"internal/a.go","compress_bodies":true}`,
		},
		{
			name:  "capped",
			cwd:   cwd,
			tool:  "read_file",
			input: `{"path":"internal/a.go","max_lines":80}`,
		},
		{
			name:  "non-source file",
			cwd:   cwd,
			tool:  "read_file",
			input: `{"path":"README.md"}`,
		},
		{
			name:  "other tool",
			cwd:   cwd,
			tool:  "search_text",
			input: `{"query":"Foo"}`,
		},
		{
			name:  "outside project",
			cwd:   t.TempDir(),
			tool:  "read_file",
			input: `{"path":"internal/a.go"}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out := captureStdout(t, func() {
				runKimi(kimiPreToolPayload(tt.cwd, tt.tool, tt.input), 0)
			})
			if out != "" {
				t.Fatalf("expected silent no-op, got %q", out)
			}
		})
	}
}

func TestRunKimiPostToolUseReadFilePlainStdout(t *testing.T) {
	cwd := writeGortexProjectMarker(t, t.TempDir())
	port := stubBridge(t, map[string]int{"internal/a.go": 3}, nil, map[string]int{"internal/a.go": 2})

	out := captureStdout(t, func() {
		runKimi(kimiPostToolPayload(cwd, "ReadFile", `{"path":"internal/a.go"}`, "package internal\n"), port)
	})
	if out == "" {
		t.Fatal("expected Kimi ReadFile PostToolUse graph context, got empty output")
	}
	if !strings.Contains(out, "Graph footprint for internal/a.go") || !strings.Contains(out, "3 indexed symbol(s)") {
		t.Fatalf("missing file footprint context:\n%s", out)
	}
	assertKimiPlainStdout(t, out)
}

func TestRunKimiPostToolUseGlobPlainStdout(t *testing.T) {
	cwd := writeGortexProjectMarker(t, t.TempDir())
	port := stubBridge(t, map[string]int{
		"src/big.go":   42,
		"src/small.go": 3,
	}, nil, nil)

	out := captureStdout(t, func() {
		runKimi(kimiPostToolPayload(cwd, "Glob", `{"pattern":"*.go"}`, "src/big.go\nsrc/small.go\n"), port)
	})
	if out == "" {
		t.Fatal("expected Kimi Glob PostToolUse graph context, got empty output")
	}
	if !strings.Contains(out, "Indexed 2/2 Glob match(es)") || !strings.Contains(out, "src/big.go") {
		t.Fatalf("missing Glob summary context:\n%s", out)
	}
	assertKimiPlainStdout(t, out)
}

func TestRunKimiPostToolUseGrepContentPlainStdout(t *testing.T) {
	cwd := writeGortexProjectMarker(t, t.TempDir())
	port := stubBridge(t, nil, map[string]struct{ ID, Name, Kind string }{
		"src/a.go:7": {ID: "src/a.go::MyType", Name: "MyType", Kind: "type"},
	}, nil)

	out := captureStdout(t, func() {
		runKimi(kimiPostToolPayload(cwd, "Grep", `{"pattern":"MyType","output_mode":"content"}`, "src/a.go:7:type MyType struct{}\n"), port)
	})
	if out == "" {
		t.Fatal("expected Kimi Grep content PostToolUse graph context, got empty output")
	}
	if !strings.Contains(out, "Graph context") || !strings.Contains(out, "type MyType") {
		t.Fatalf("missing Grep enclosing-symbol context:\n%s", out)
	}
	assertKimiPlainStdout(t, out)
}

func TestRunKimiPostToolUseGrepFilesWithMatchesUsesGlobSummary(t *testing.T) {
	cwd := writeGortexProjectMarker(t, t.TempDir())
	port := stubBridge(t, map[string]int{"src/a.go": 5}, nil, nil)

	out := captureStdout(t, func() {
		runKimi(kimiPostToolPayload(cwd, "Grep", `{"pattern":"MyType"}`, "src/a.go\n"), port)
	})
	if out == "" {
		t.Fatal("expected Kimi Grep file-list PostToolUse graph context, got empty output")
	}
	if !strings.Contains(out, "Indexed 1/1 Glob match(es)") || !strings.Contains(out, "src/a.go") {
		t.Fatalf("missing Grep file-list summary context:\n%s", out)
	}
	assertKimiPlainStdout(t, out)
}

func TestRunKimiPostToolUseSilentShapes(t *testing.T) {
	cwd := writeGortexProjectMarker(t, t.TempDir())
	port := stubBridge(t, nil, nil, nil)
	tests := []struct {
		name   string
		cwd    string
		tool   string
		input  string
		output string
	}{
		{
			name:   "outside project",
			cwd:    t.TempDir(),
			tool:   "ReadFile",
			input:  `{"path":"internal/a.go"}`,
			output: "package internal\n",
		},
		{
			name:   "unsupported tool",
			cwd:    cwd,
			tool:   "WriteFile",
			input:  `{"path":"internal/a.go"}`,
			output: "ok",
		},
		{
			name:   "grep count matches",
			cwd:    cwd,
			tool:   "Grep",
			input:  `{"pattern":"Foo","output_mode":"count_matches"}`,
			output: "src/a.go:12\n",
		},
		{
			name:   "read file not indexed",
			cwd:    cwd,
			tool:   "ReadFile",
			input:  `{"path":"internal/a.go"}`,
			output: "package internal\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out := captureStdout(t, func() {
				runKimi(kimiPostToolPayload(tt.cwd, tt.tool, tt.input, tt.output), port)
			})
			if out != "" {
				t.Fatalf("expected silent no-op, got %q", out)
			}
		})
	}
}

func TestRunKimiNoopShapes(t *testing.T) {
	cwd := writeGortexProjectMarker(t, t.TempDir())
	restore := stubUserPromptProbe(t, nil, errors.New("should not be called"))
	defer restore()

	cases := [][]byte{
		[]byte(`{`),
		[]byte(`{"hook_event_name":"PreToolUse","cwd":` + strconv.Quote(cwd) + `,"prompt":"fix auth"}`),
		kimiPromptPayload(cwd, "/clear"),
	}
	for _, tc := range cases {
		out := captureStdout(t, func() { runKimi(tc, 0) })
		if out != "" {
			t.Fatalf("expected no output for %s, got %q", tc, out)
		}
	}
}

func TestRunKimiNoopOutsideGortexProject(t *testing.T) {
	var calls int
	restore := stubUserPromptProbeFunc(t, func(string, time.Duration) ([]grepSymbolHit, error) {
		calls++
		return []grepSymbolHit{
			{Name: "ShouldNotAppear", Kind: "function", FilePath: "internal/nope.go"},
		}, nil
	})
	defer restore()

	out := captureStdout(t, func() {
		runKimi(kimiPromptPayload(t.TempDir(), "fix auth token validation"), 0)
	})
	if out != "" {
		t.Fatalf("expected no output outside a Gortex-enabled project, got %q", out)
	}
	if calls != 0 {
		t.Fatalf("expected no daemon probe outside a Gortex-enabled project, got %d calls", calls)
	}
}

func TestKimiGortexEnabledProjectMarkers(t *testing.T) {
	root := t.TempDir()
	if kimiGortexEnabledProject(root) {
		t.Fatal("bare temp dir should not be treated as a Gortex-enabled project")
	}
	if err := os.Mkdir(filepath.Join(root, ".gortex"), 0o755); err != nil {
		t.Fatal(err)
	}
	if kimiGortexEnabledProject(root) {
		t.Fatal("bare .gortex directory should not be enough to enable the global Kimi hook")
	}
	if err := os.WriteFile(filepath.Join(root, ".gortex.yaml"), []byte("project: test\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !kimiGortexEnabledProject(root) {
		t.Fatal(".gortex.yaml should mark a Gortex-enabled project")
	}
}

func kimiPromptPayload(cwd, prompt string) []byte {
	return []byte(`{"hook_event_name":"UserPromptSubmit","cwd":` + strconv.Quote(cwd) + `,"prompt":` + strconv.Quote(prompt) + `}`)
}

func kimiPreToolPayload(cwd, toolName, input string) []byte {
	return []byte(`{"hook_event_name":"PreToolUse","cwd":` + strconv.Quote(cwd) + `,"tool_name":` + strconv.Quote(toolName) + `,"tool_input":` + input + `}`)
}

func kimiPostToolPayload(cwd, toolName, input, output string) []byte {
	return []byte(`{"hook_event_name":"PostToolUse","cwd":` + strconv.Quote(cwd) + `,"tool_name":` + strconv.Quote(toolName) + `,"tool_input":` + input + `,"tool_output":` + strconv.Quote(output) + `}`)
}

func assertKimiPlainStdout(t *testing.T, out string) {
	t.Helper()
	for _, notWant := range []string{"hookSpecificOutput", "additionalContext", "permissionDecision"} {
		if strings.Contains(out, notWant) {
			t.Fatalf("Kimi dispatcher should emit plain stdout, got JSON-shaped output:\n%s", out)
		}
	}
}

func writeGortexProjectMarker(t *testing.T, dir string) string {
	t.Helper()
	gortexDir := filepath.Join(dir, ".gortex")
	if err := os.MkdirAll(gortexDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(gortexDir, ".gitignore"), []byte("# Gortex-managed: local index state, do not commit\n*\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func writeKimiProjectMCP(t *testing.T, dir string) string {
	t.Helper()
	kimiDir := filepath.Join(dir, ".kimi-code")
	if err := os.MkdirAll(kimiDir, 0o755); err != nil {
		t.Fatal(err)
	}
	data := []byte(`{"mcpServers":{"gortex":{"command":"gortex","args":["mcp"]}}}`)
	if err := os.WriteFile(filepath.Join(kimiDir, "mcp.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func stubUserPromptProbe(t *testing.T, hits []grepSymbolHit, err error) func() {
	t.Helper()
	return stubUserPromptProbeFunc(t, func(string, time.Duration) ([]grepSymbolHit, error) {
		return hits, err
	})
}

func stubUserPromptProbeFunc(t *testing.T, fn grepProbeFn) func() {
	t.Helper()
	old := userPromptProbe
	userPromptProbe = fn
	return func() { userPromptProbe = old }
}
