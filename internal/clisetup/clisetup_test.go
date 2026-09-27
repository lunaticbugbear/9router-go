package clisetup

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	toml "github.com/pelletier/go-toml/v2"
)

func testHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	return home
}

func opts() Options {
	return Options{BaseURL: "http://localhost:20130/", APIKey: "sk-test", Models: []string{"cx/gpt-5"}}
}

func TestClaudeApplyKeepsSettingsBacksUpAndResets(t *testing.T) {
	home := testHome(t)
	path := claudePath(home)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	orig := `{"theme":"dark","env":{"FOO":"1",},}`
	if err := os.WriteFile(path, []byte(orig), 0o644); err != nil {
		t.Fatal(err)
	}
	tool, _ := Lookup("claude")
	res, err := tool.Apply(opts())
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	b, _ := os.ReadFile(path)
	for _, want := range []string{`"theme": "dark"`, `"FOO": "1"`, `"ANTHROPIC_BASE_URL": "http://localhost:20130/v1"`, `"ANTHROPIC_AUTH_TOKEN": "sk-test"`} {
		if !strings.Contains(string(b), want) {
			t.Errorf("settings missing %s:\n%s", want, b)
		}
	}
	if bak, _ := os.ReadFile(res.BackupPath); string(bak) != orig {
		t.Errorf("backup = %q, want original", bak)
	}
	if ok, _ := tool.Status(); !ok {
		t.Error("status should report configured")
	}
	if _, err := tool.Reset(); err != nil {
		t.Fatalf("reset: %v", err)
	}
	b, _ = os.ReadFile(path)
	if strings.Contains(string(b), "ANTHROPIC") || !strings.Contains(string(b), `"FOO": "1"`) {
		t.Errorf("reset should drop only 9router keys:\n%s", b)
	}
	if ok, _ := tool.Status(); ok {
		t.Error("status should report not configured after reset")
	}
}

func TestInvalidConfigIsLeftUntouched(t *testing.T) {
	home := testHome(t)
	path := openCodePath(home)
	os.MkdirAll(filepath.Dir(path), 0o755)
	os.WriteFile(path, []byte("{ not json"), 0o644)
	tool, _ := Lookup("opencode")
	if _, err := tool.Apply(opts()); err == nil {
		t.Fatal("expected an error for an unparseable config")
	}
	if b, _ := os.ReadFile(path); string(b) != "{ not json" {
		t.Errorf("config was modified: %q", b)
	}
}

func TestCodexApplyAndReset(t *testing.T) {
	home := testHome(t)
	path := codexPath(home)
	os.MkdirAll(filepath.Dir(path), 0o755)
	os.WriteFile(path, []byte("approval_policy = \"never\"\n"), 0o644)
	tool, _ := Lookup("codex")
	if _, err := tool.Apply(opts()); err != nil {
		t.Fatalf("apply: %v", err)
	}
	var cfg map[string]any
	b, _ := os.ReadFile(path)
	if err := toml.Unmarshal(b, &cfg); err != nil {
		t.Fatalf("written TOML invalid: %v\n%s", err, b)
	}
	p := cfg["model_providers"].(map[string]any)["9router"].(map[string]any)
	if cfg["model"] != "cx/gpt-5" || cfg["approval_policy"] != "never" || p["base_url"] != "http://localhost:20130/v1" {
		t.Errorf("unexpected config: %v", cfg)
	}
	if _, err := tool.Reset(); err != nil {
		t.Fatalf("reset: %v", err)
	}
	b, _ = os.ReadFile(path)
	if strings.Contains(string(b), "9router") || !strings.Contains(string(b), "approval_policy") {
		t.Errorf("reset result:\n%s", b)
	}
}

func TestHermesReplacesModelBlockOnly(t *testing.T) {
	home := testHome(t)
	cfgPath := filepath.Join(hermesDir(home), "config.yaml")
	os.MkdirAll(filepath.Dir(cfgPath), 0o755)
	os.WriteFile(cfgPath, []byte("model:\n  default: \"old\"\n  provider: \"openai\"\nterminal:\n  backend: local\n"), 0o644)
	tool, _ := Lookup("hermes")
	if _, err := tool.Apply(opts()); err != nil {
		t.Fatalf("apply: %v", err)
	}
	b, _ := os.ReadFile(cfgPath)
	s := string(b)
	if strings.Contains(s, "old") || !strings.Contains(s, "terminal:\n  backend: local") || !strings.Contains(s, `default: "cx/gpt-5"`) {
		t.Errorf("unexpected yaml:\n%s", s)
	}
	env, _ := os.ReadFile(filepath.Join(hermesDir(home), ".env"))
	if string(env) != "OPENAI_API_KEY=sk-test\n" {
		t.Errorf(".env = %q", env)
	}
	if ok, _ := tool.Status(); !ok {
		t.Error("status should report configured")
	}
}

func TestApplyValidation(t *testing.T) {
	testHome(t)
	tool, _ := Lookup("codex")
	cases := []Options{
		{BaseURL: "ftp://x", APIKey: "k", Models: []string{"m"}},
		{BaseURL: "http://localhost:20130", APIKey: "k"},
		{BaseURL: "http://localhost:20130", APIKey: "k", Models: []string{"bad\"model"}},
		{BaseURL: "http://localhost:20130", APIKey: "", Models: []string{"m"}},
	}
	for i, o := range cases {
		if _, err := tool.Apply(o); err == nil {
			t.Errorf("case %d: expected validation error", i)
		}
	}
}
