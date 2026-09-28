package clisetup

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func readTestFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func mustContain(t *testing.T, label, got string, want ...string) {
	t.Helper()
	for _, w := range want {
		if !strings.Contains(got, w) {
			t.Errorf("%s missing %q:\n%s", label, w, got)
		}
	}
}

func mustNotContain(t *testing.T, label, got string, bad ...string) {
	t.Helper()
	for _, w := range bad {
		if strings.Contains(got, w) {
			t.Errorf("%s still contains %q:\n%s", label, w, got)
		}
	}
}

// applyResetCycle applies, checks status, resets and checks status again.
func applyResetCycle(t *testing.T, id string, afterApply, afterReset func()) {
	t.Helper()
	tool, ok := Lookup(id)
	if !ok {
		t.Fatalf("installer %s not registered", id)
	}
	if _, err := tool.Apply(opts()); err != nil {
		t.Fatalf("%s apply: %v", id, err)
	}
	if ok, _ := tool.Status(); !ok {
		t.Errorf("%s status should be configured", id)
	}
	afterApply()
	if _, err := tool.Reset(); err != nil {
		t.Fatalf("%s reset: %v", id, err)
	}
	if ok, _ := tool.Status(); ok {
		t.Errorf("%s status should be cleared after reset", id)
	}
	afterReset()
}

func TestDroidInstaller(t *testing.T) {
	home := testHome(t)
	path := droidPath(home)
	writeTestFile(t, path, `{"theme":"dark","customModels":[{"id":"custom:mine","model":"x"},{"id":"custom:9Router-0","model":"old"}]}`)
	applyResetCycle(t, "droid", func() {
		got := readTestFile(t, path)
		mustContain(t, "droid", got, `"custom:mine"`, `"model": "cx/gpt-5"`, `"baseUrl": "http://localhost:20130/v1"`, `"theme": "dark"`)
		mustNotContain(t, "droid", got, `"old"`)
	}, func() {
		got := readTestFile(t, path)
		mustContain(t, "droid reset", got, `"custom:mine"`)
		mustNotContain(t, "droid reset", got, "9Router")
	})
}

func TestOpenClawInstaller(t *testing.T) {
	home := testHome(t)
	path := openClawPath(home)
	agentDir := filepath.Join(home, ".openclaw", "agents", "a")
	writeTestFile(t, path, `{"gateway":{"port":1},"agents":{"defaults":{"models":{"9router/old":{},"other/m":{}}},"list":[{"id":"a","model":"9router/old","agentDir":"`+agentDir+`"},{"id":"b","agentDir":"/etc"}]}}`)
	applyResetCycle(t, "openclaw", func() {
		got := readTestFile(t, path)
		mustContain(t, "openclaw", got, `"primary": "9router/cx/gpt-5"`, `"9router/cx/gpt-5": {}`, `"other/m": {}`, `"api": "openai-completions"`, `"name": "gpt-5"`, `"port": 1`)
		mustNotContain(t, "openclaw", got, "9router/old")
		mustContain(t, "agent models.json", readTestFile(t, filepath.Join(agentDir, "models.json")), `"9router"`)
		if _, err := os.Stat("/etc/models.json"); err == nil {
			t.Error("must not write outside home")
		}
	}, func() {
		got := readTestFile(t, path)
		mustContain(t, "openclaw reset", got, `"other/m": {}`)
		mustNotContain(t, "openclaw reset", got, "9router")
	})
}

func TestKiloInstaller(t *testing.T) {
	home := testHome(t)
	path := kiloPath(home)
	writeTestFile(t, path, `{"anthropic":{"type":"api-key","apiKey":"a"}}`)
	applyResetCycle(t, "kilo", func() {
		mustContain(t, "kilo", readTestFile(t, path), `"openai-compatible"`, `"baseUrl": "http://localhost:20130/v1"`, `"anthropic"`)
	}, func() {
		got := readTestFile(t, path)
		mustContain(t, "kilo reset", got, `"anthropic"`)
		mustNotContain(t, "kilo reset", got, "openai-compatible")
	})

	// A user's own OpenAI-compatible endpoint survives reset.
	writeTestFile(t, path, `{"openai-compatible":{"baseUrl":"https://api.example.com/v1"}}`)
	tool, _ := Lookup("kilo")
	if _, err := tool.Reset(); err != nil {
		t.Fatal(err)
	}
	mustContain(t, "kilo foreign", readTestFile(t, path), "api.example.com")
}

func TestClineInstaller(t *testing.T) {
	home := testHome(t)
	state := filepath.Join(clineDir(home), "globalState.json")
	secrets := filepath.Join(clineDir(home), "secrets.json")
	writeTestFile(t, state, `{"actModeApiProvider":"anthropic","telemetry":"off"}`)
	writeTestFile(t, secrets, `{"apiKey":"keep"}`)
	applyResetCycle(t, "cline", func() {
		mustContain(t, "cline", readTestFile(t, state), `"actModeApiProvider": "openai"`, `"openAiBaseUrl": "http://localhost:20130"`, `"planModeOpenAiModelId": "cx/gpt-5"`, `"telemetry": "off"`)
		mustContain(t, "cline secrets", readTestFile(t, secrets), `"openAiApiKey": "sk-test"`, `"apiKey": "keep"`)
	}, func() {
		got := readTestFile(t, state)
		// The fixture's prior value was "anthropic"; reset must restore it, not
		// hardcode "cline". planModeApiProvider was absent before the install,
		// so that one is removed.
		mustContain(t, "cline reset", got, `"actModeApiProvider": "anthropic"`, `"telemetry": "off"`)
		mustNotContain(t, "cline reset", got, "openAiBaseUrl", "planModeApiProvider")
		mustNotContain(t, "cline secrets reset", readTestFile(t, secrets), "openAiApiKey")
	})
}

func TestDeepSeekInstaller(t *testing.T) {
	home := testHome(t)
	path := deepSeekPath(home)
	writeTestFile(t, path, "provider = \"deepseek\"\ntheme = \"dark\"\n\n[providers.deepseek]\napi_key = \"ds\"\n")
	applyResetCycle(t, "deepseek-tui", func() {
		mustContain(t, "deepseek", readTestFile(t, path), `provider = 'openai'`, `base_url = 'http://localhost:20130/v1'`, `api_key = 'ds'`, `theme = 'dark'`)
	}, func() {
		got := readTestFile(t, path)
		mustContain(t, "deepseek reset", got, `provider = 'deepseek'`, `api_key = 'ds'`)
		mustNotContain(t, "deepseek reset", got, "localhost")
	})
}

func TestJcodeInstaller(t *testing.T) {
	home := testHome(t)
	t.Setenv("XDG_CONFIG_HOME", "")
	path := jcodePath(home)
	env := jcodeEnvPath(home)
	writeTestFile(t, path, "[providers.other]\ntype = 'x'\n")
	writeTestFile(t, env, "OTHER=1\n")
	applyResetCycle(t, "jcode", func() {
		mustContain(t, "jcode", readTestFile(t, path), `[providers.9router]`, `default_model = 'cx/gpt-5'`, `[providers.other]`)
		mustContain(t, "jcode env", readTestFile(t, env), `JCODE_9ROUTER_API_KEY="sk-test"`, "OTHER=1")
	}, func() {
		mustNotContain(t, "jcode reset", readTestFile(t, path), "9router")
		got := readTestFile(t, env)
		mustContain(t, "jcode env reset", got, "OTHER=1")
		mustNotContain(t, "jcode env reset", got, "JCODE_9ROUTER_API_KEY")
	})
}

func TestGrokInstallerKeepsCommentsAndRestoresDefault(t *testing.T) {
	home := testHome(t)
	path := grokPath(home)
	orig := "# my grok config\n[models]\ndefault = \"grok-4\" # pinned\n\n[model.mine]\nmodel = \"m\"\n"
	writeTestFile(t, path, orig)
	applyResetCycle(t, "grok-build", func() {
		got := readTestFile(t, path)
		mustContain(t, "grok", got, "# my grok config", `default = "9router"`, "[model.9router]", `base_url = "http://localhost:20130/v1"`, `# 9router-prev-default = "grok-4"`, "[model.mine]")
	}, func() {
		got := readTestFile(t, path)
		mustContain(t, "grok reset", got, "# my grok config", `default = "grok-4"`, "[model.mine]")
		mustNotContain(t, "grok reset", got, "9router")
	})

	// Re-applying keeps a single model section.
	tool, _ := Lookup("grok-build")
	for range 2 {
		if _, err := tool.Apply(opts()); err != nil {
			t.Fatal(err)
		}
	}
	if n := strings.Count(readTestFile(t, path), "[model.9router]"); n != 1 {
		t.Errorf("model section count = %d, want 1", n)
	}
}
