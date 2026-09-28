package clisetup

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// These tests pin the behaviour Reset is documented to have: it restores the
// values the operator had before 9router wrote to the file, rather than
// deleting the keys 9router managed. Every installer is covered by the same
// harness, so a new installer cannot quietly regress to delete-and-hope.

// --- typed readers, so assertions name the key instead of the formatting ---

func readJSONMap(t *testing.T, path string) map[string]any {
	t.Helper()
	m := map[string]any{}
	if _, err := readJSON(path, &m); err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return m
}

func readTOMLMap(t *testing.T, path string) map[string]any {
	t.Helper()
	m, _, err := readTOML(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return m
}

func nested(t *testing.T, m map[string]any, keys ...string) map[string]any {
	t.Helper()
	for _, k := range keys {
		sub, ok := m[k].(map[string]any)
		if !ok {
			t.Fatalf("%q is not an object in %v", k, m)
		}
		m = sub
	}
	return m
}

func wantEq(t *testing.T, label string, got, want any) {
	t.Helper()
	if got != want {
		t.Errorf("%s = %#v, want %#v", label, got, want)
	}
}

func wantAbsent(t *testing.T, label string, m map[string]any, key string) {
	t.Helper()
	if v, ok := m[key]; ok {
		t.Errorf("%s still present after reset: %#v", label+"."+key, v)
	}
}

func writeJSONMust(t *testing.T, path string, v any) {
	t.Helper()
	if _, err := writeJSON(path, v); err != nil {
		t.Fatal(err)
	}
}

func appendText(t *testing.T, path, text string) {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(b, []byte(text)...), 0o644); err != nil {
		t.Fatal(err)
	}
}

// restoreRow seeds a tool's config with the operator's own settings, applies
// the 9router installer, lets the test make an unrelated post-install edit,
// then resets and checks the result.
type restoreRow struct {
	id string
	// seed maps a label to a path the test writes pre-install content into.
	seed func(home string) map[string]string
	// content maps the same labels to that pre-install content.
	content func(home string) map[string]string
	// backedUp lists the labels whose file the installer backs up. Anything
	// else (Cowork's _meta.json, for one) is written but never backed up, so
	// it is excluded from the backup invariants.
	backedUp []string
	// namespaceOnly marks an installer that only ever writes keys in 9router's
	// own namespace, so there is no operator value for reset to restore.
	namespaceOnly bool
	postInstall   func(t *testing.T, home string)
	check         func(t *testing.T, home string)
}

// backups returns the label->path pairs whose files carry an install backup.
func (r restoreRow) backups(home string) map[string]string {
	paths := r.seed(home)
	if r.backedUp == nil {
		return paths
	}
	out := map[string]string{}
	for _, label := range r.backedUp {
		out[label] = paths[label]
	}
	return out
}

func runRestoreCase(t *testing.T, row restoreRow) {
	t.Helper()
	home := testHome(t)
	t.Setenv("XDG_CONFIG_HOME", "")

	paths := row.seed(home)
	contents := row.content(home)
	for label, path := range paths {
		writeTestFile(t, path, contents[label])
	}

	tool, ok := Lookup(row.id)
	if !ok {
		t.Fatalf("installer %s not registered", row.id)
	}
	if _, err := tool.Apply(opts()); err != nil {
		t.Fatalf("%s apply: %v", row.id, err)
	}

	// The backup must still be the pre-install file, byte for byte.
	backups := row.backups(home)
	for label, path := range backups {
		bak, err := os.ReadFile(backupPath(path))
		if err != nil {
			t.Fatalf("%s backup for %s: %v", row.id, label, err)
		}
		if string(bak) != contents[label] {
			t.Errorf("%s backup for %s is not the pre-install original:\n got %s\nwant %s", row.id, label, bak, contents[label])
		}
	}

	before := snapshotBackups(t, backups)
	if row.postInstall != nil {
		row.postInstall(t, home)
	}

	res, err := tool.Reset()
	if err != nil {
		t.Fatalf("%s reset: %v", row.id, err)
	}
	switch {
	case row.namespaceOnly:
		if !strings.Contains(res.Message, "own settings") {
			t.Errorf("%s reset message should say only its own settings were removed, got %q", row.id, res.Message)
		}
	default:
		if !strings.Contains(res.Message, "restored from") {
			t.Errorf("%s reset message should mention restoration, got %q", row.id, res.Message)
		}
	}
	if ok, _ := tool.Status(); ok {
		t.Errorf("%s status should be cleared after reset", row.id)
	}
	row.check(t, home)

	// Reset must not create or refresh the backup: it has to stay the true
	// pre-9router original.
	for label, path := range backups {
		after := snapshotBackups(t, map[string]string{label: path})
		if after[label] != before[label] {
			t.Errorf("%s reset rewrote the backup for %s:\n before %s\n after  %s", row.id, label, before[label], after[label])
		}
		if b, err := os.ReadFile(backupPath(path)); err == nil && string(b) != contents[label] {
			t.Errorf("%s backup for %s changed to %s", row.id, label, b)
		}
	}
}

func snapshotBackups(t *testing.T, paths map[string]string) map[string]string {
	t.Helper()
	out := map[string]string{}
	for label, path := range paths {
		b, err := os.ReadFile(backupPath(path))
		if err != nil {
			t.Fatalf("read backup for %s: %v", label, err)
		}
		out[label] = string(b)
	}
	return out
}

// runNoBackupCase proves the fallback: without the backup the prior values are
// unknowable, so reset still clears the 9router settings and says honestly that
// it could not restore anything.
func runNoBackupCase(t *testing.T, row restoreRow) {
	t.Helper()
	home := testHome(t)
	t.Setenv("XDG_CONFIG_HOME", "")

	paths := row.seed(home)
	contents := row.content(home)
	for label, path := range paths {
		writeTestFile(t, path, contents[label])
	}
	tool, _ := Lookup(row.id)
	if _, err := tool.Apply(opts()); err != nil {
		t.Fatalf("%s apply: %v", row.id, err)
	}
	for _, path := range row.backups(home) {
		if err := os.Remove(backupPath(path)); err != nil {
			t.Fatalf("%s remove backup: %v", row.id, err)
		}
	}
	res, err := tool.Reset()
	if err != nil {
		t.Fatalf("%s reset without a backup: %v", row.id, err)
	}
	if !row.namespaceOnly && !strings.Contains(res.Message, "backup") {
		t.Errorf("%s no-backup message should say the backup was missing, got %q", row.id, res.Message)
	}
	if ok, _ := tool.Status(); ok {
		t.Errorf("%s status should still be cleared after a no-backup reset", row.id)
	}
	// With no prior values to restore, every 9router setting must be gone.
	for label, path := range paths {
		got := readTestFile(t, path)
		for _, marker := range []string{"localhost:20130", "9router", "9Router"} {
			if strings.Contains(got, marker) {
				t.Errorf("%s no-backup reset left %q in %s:\n%s", row.id, marker, label, got)
			}
		}
	}
}

// --- the installers ---

func TestResetRestoresPriorValues(t *testing.T) {
	rows := []restoreRow{
		{
			id:   "claude",
			seed: func(home string) map[string]string { return map[string]string{"settings": claudePath(home)} },
			content: func(string) map[string]string {
				return map[string]string{"settings": `{
  "theme": "dark",
  "env": {
    "ANTHROPIC_BASE_URL": "https://my-own-corp-gateway.example.com",
    "ANTHROPIC_AUTH_TOKEN": "corp-token-abc",
    "ANTHROPIC_DEFAULT_SONNET_MODEL": "claude-sonnet-user-choice",
    "KEEP_ME": "1"
  }
}`}
			},
			postInstall: func(t *testing.T, home string) {
				s := readJSONMap(t, claudePath(home))
				s["model"] = "opus"
				nested(t, s, "env")["PATH"] = "/usr/local/bin"
				writeJSONMust(t, claudePath(home), s)
			},
			check: func(t *testing.T, home string) {
				s := readJSONMap(t, claudePath(home))
				env := nested(t, s, "env")
				wantEq(t, "env.ANTHROPIC_BASE_URL", env["ANTHROPIC_BASE_URL"], "https://my-own-corp-gateway.example.com")
				wantEq(t, "env.ANTHROPIC_AUTH_TOKEN", env["ANTHROPIC_AUTH_TOKEN"], "corp-token-abc")
				wantEq(t, "env.ANTHROPIC_DEFAULT_SONNET_MODEL", env["ANTHROPIC_DEFAULT_SONNET_MODEL"], "claude-sonnet-user-choice")
				// Managed keys the operator never had are removed.
				wantAbsent(t, "env", env, "ANTHROPIC_DEFAULT_OPUS_MODEL")
				wantAbsent(t, "env", env, "ANTHROPIC_DEFAULT_HAIKU_MODEL")
				wantAbsent(t, "settings", s, "hasCompletedOnboarding")
				// Unrelated settings survive, both pre-existing and post-install.
				wantEq(t, "env.KEEP_ME", env["KEEP_ME"], "1")
				wantEq(t, "env.PATH", env["PATH"], "/usr/local/bin")
				wantEq(t, "settings.model", s["model"], "opus")
				wantEq(t, "settings.theme", s["theme"], "dark")
			},
		},
		{
			id:   "codex",
			seed: func(home string) map[string]string { return map[string]string{"config": codexPath(home)} },
			content: func(string) map[string]string {
				return map[string]string{"config": `model = "gpt-5-codex"
approval_policy = "never"

[model_providers.mine]
name = "Mine"
base_url = "https://mine.example.com/v1"
`}
			},
			postInstall: func(t *testing.T, home string) {
				m := readTOMLMap(t, codexPath(home))
				m["sandbox_mode"] = "read-only"
				writeTOMLMust(t, codexPath(home), m)
			},
			check: func(t *testing.T, home string) {
				m := readTOMLMap(t, codexPath(home))
				wantEq(t, "model", m["model"], "gpt-5-codex")
				// model_provider did not exist before the install.
				wantAbsent(t, "codex", m, "model_provider")
				providers := nested(t, m, "model_providers")
				wantAbsent(t, "model_providers", providers, "9router")
				wantEq(t, "model_providers.mine.base_url", nested(t, providers, "mine")["base_url"], "https://mine.example.com/v1")
				wantEq(t, "approval_policy", m["approval_policy"], "never")
				wantEq(t, "sandbox_mode", m["sandbox_mode"], "read-only")
			},
		},
		{
			id:   "opencode",
			seed: func(home string) map[string]string { return map[string]string{"config": openCodePath(home)} },
			content: func(string) map[string]string {
				return map[string]string{"config": `{"model":"anthropic/claude-sonnet-4","provider":{"anthropic":{"npm":"@ai-sdk/anthropic"}}}`}
			},
			postInstall: func(t *testing.T, home string) {
				s := readJSONMap(t, openCodePath(home))
				s["theme"] = "dark"
				writeJSONMust(t, openCodePath(home), s)
			},
			check: func(t *testing.T, home string) {
				s := readJSONMap(t, openCodePath(home))
				wantEq(t, "model", s["model"], "anthropic/claude-sonnet-4")
				wantAbsent(t, "provider", nested(t, s, "provider"), "9router")
				wantEq(t, "theme", s["theme"], "dark")
			},
		},
		{
			id: "hermes",
			seed: func(home string) map[string]string {
				return map[string]string{
					"config": filepath.Join(hermesDir(home), "config.yaml"),
					"env":    filepath.Join(hermesDir(home), ".env"),
				}
			},
			content: func(string) map[string]string {
				return map[string]string{
					"config": "model:\n  default: \"my-model\"\n  provider: \"openai\"\n  base_url: \"https://mine.example.com\"\nterminal:\n  backend: local\n",
					"env":    "OPENAI_API_KEY=user-own-key\nOTHER=1\n",
				}
			},
			postInstall: func(t *testing.T, home string) {
				appendText(t, filepath.Join(hermesDir(home), "config.yaml"), "extra:\n  flag: true\n")
			},
			check: func(t *testing.T, home string) {
				got := readTestFile(t, filepath.Join(hermesDir(home), "config.yaml"))
				mustContain(t, "hermes config", got, `default: "my-model"`, `provider: "openai"`, `base_url: "https://mine.example.com"`, "terminal:", "extra:", "flag: true")
				mustNotContain(t, "hermes config", got, "localhost")
				env := readTestFile(t, filepath.Join(hermesDir(home), ".env"))
				mustContain(t, "hermes env", env, "OPENAI_API_KEY=user-own-key", "OTHER=1")
			},
		},
		{
			id:            "copilot",
			namespaceOnly: true,
			seed:          func(home string) map[string]string { return map[string]string{"models": copilotPath(home)} },
			content: func(string) map[string]string {
				return map[string]string{"models": `[{"name":"Mine","vendor":"openai","apiKey":"mine"}]`}
			},
			postInstall: func(t *testing.T, home string) {
				var list []any
				if _, err := readJSON(copilotPath(home), &list); err != nil {
					t.Fatal(err)
				}
				list = append(list, map[string]any{"name": "Added later", "vendor": "openai"})
				if _, err := writeJSON(copilotPath(home), list); err != nil {
					t.Fatal(err)
				}
			},
			check: func(t *testing.T, home string) {
				got := readTestFile(t, copilotPath(home))
				mustContain(t, "copilot", got, `"Mine"`, `"Added later"`)
				mustNotContain(t, "copilot", got, "9Router")
			},
		},
		{
			id: "cowork",
			seed: func(home string) map[string]string {
				root := coworkWriteRoot(home)
				return map[string]string{
					"meta": filepath.Join(root, "configLibrary", "_meta.json"),
					"cfg":  filepath.Join(root, "configLibrary", "cfg1.json"),
				}
			},
			content: func(string) map[string]string {
				return map[string]string{
					"meta": `{"appliedId":"cfg1","entries":[{"id":"cfg1","name":"Default"}]}`,
					"cfg":  `{"inferenceProvider":"openai","inferenceGatewayBaseUrl":"https://mine.example.com","inferenceModels":[{"name":"my-model"}],"theme":"dark"}`,
				}
			},
			// applyCowork writes _meta.json without a backup; only the profile
			// file is managed through writeFile.
			backedUp: []string{"cfg"},
			postInstall: func(t *testing.T, home string) {
				p := filepath.Join(coworkWriteRoot(home), "configLibrary", "cfg1.json")
				s := readJSONMap(t, p)
				s["sidebarWidth"] = 300
				writeJSONMust(t, p, s)
			},
			check: func(t *testing.T, home string) {
				s := readJSONMap(t, filepath.Join(coworkWriteRoot(home), "configLibrary", "cfg1.json"))
				wantEq(t, "inferenceProvider", s["inferenceProvider"], "openai")
				wantEq(t, "inferenceGatewayBaseUrl", s["inferenceGatewayBaseUrl"], "https://mine.example.com")
				wantEq(t, "inferenceModels", len(s["inferenceModels"].([]any)), 1)
				// The API key was not in the file before the install.
				wantAbsent(t, "cfg", s, "inferenceGatewayApiKey")
				wantEq(t, "theme", s["theme"], "dark")
				wantEq(t, "sidebarWidth", s["sidebarWidth"], float64(300))
			},
		},
		{
			id:            "droid",
			namespaceOnly: true,
			seed:          func(home string) map[string]string { return map[string]string{"settings": droidPath(home)} },
			content: func(string) map[string]string {
				return map[string]string{"settings": `{"theme":"dark","customModels":[{"id":"custom:mine","model":"x"}]}`}
			},
			postInstall: func(t *testing.T, home string) {
				s := readJSONMap(t, droidPath(home))
				s["autoUpdate"] = true
				writeJSONMust(t, droidPath(home), s)
			},
			check: func(t *testing.T, home string) {
				s := readJSONMap(t, droidPath(home))
				list, _ := s["customModels"].([]any)
				wantEq(t, "customModels length", len(list), 1)
				wantEq(t, "customModels[0].id", list[0].(map[string]any)["id"], "custom:mine")
				wantEq(t, "theme", s["theme"], "dark")
				wantEq(t, "autoUpdate", s["autoUpdate"], true)
			},
		},
		{
			id: "openclaw",
			seed: func(home string) map[string]string {
				return map[string]string{
					"config": openClawPath(home),
					"models": filepath.Join(home, ".openclaw", "agents", "a", "models.json"),
				}
			},
			content: func(home string) map[string]string {
				agentDir := filepath.Join(home, ".openclaw", "agents", "a")
				return map[string]string{
					"config": `{"gateway":{"port":1},"agents":{"defaults":{"model":{"primary":"anthropic/claude"},"models":{"other/m":{}}},"list":[{"id":"a","agentDir":"` + agentDir + `"}]}}`,
					"models": `{"providers":{"other":{"baseUrl":"https://mine"}}}`,
				}
			},
			postInstall: func(t *testing.T, home string) {
				s := readJSONMap(t, openClawPath(home))
				s["logLevel"] = "debug"
				writeJSONMust(t, openClawPath(home), s)
			},
			check: func(t *testing.T, home string) {
				s := readJSONMap(t, openClawPath(home))
				wantEq(t, "logLevel", s["logLevel"], "debug")
				// The provider table is 9router's own namespace. It held only
				// the 9router entry, so the whole table is pruned away. nested
				// asserts the container exists, so this check cannot silently
				// stop asserting if the shape changes.
				wantAbsent(t, "models", nested(t, s, "models"), "providers")
				defaults := nested(t, s, "agents", "defaults")
				wantEq(t, "defaults.model.primary", nested(t, defaults, "model")["primary"], "anthropic/claude")
				allow := nested(t, defaults, "models")
				wantEq(t, "defaults.models keeps other/m", allow["other/m"] != nil, true)
				for k := range allow {
					if is9RouterModelKey(k) {
						t.Errorf("allow-list still has %q", k)
					}
				}
				// The per-agent provider file must be cleaned up too. nested
				// asserts the container exists, so this cannot become a no-op.
				agent := readJSONMap(t, filepath.Join(home, ".openclaw", "agents", "a", "models.json"))
				wantAbsent(t, "agent models", nested(t, agent, "providers"), "9router")
			},
		},
		{
			id:   "kilo",
			seed: func(home string) map[string]string { return map[string]string{"auth": kiloPath(home)} },
			content: func(string) map[string]string {
				return map[string]string{"auth": `{"openai-compatible":{"type":"api-key","apiKey":"mine","baseUrl":"https://api.example.com/v1"},"anthropic":{"type":"api-key","apiKey":"a"}}`}
			},
			postInstall: func(t *testing.T, home string) {
				s := readJSONMap(t, kiloPath(home))
				s["theme"] = "dark"
				writeJSONMust(t, kiloPath(home), s)
			},
			check: func(t *testing.T, home string) {
				s := readJSONMap(t, kiloPath(home))
				wantEq(t, "openai-compatible.baseUrl", nested(t, s, "openai-compatible")["baseUrl"], "https://api.example.com/v1")
				wantEq(t, "openai-compatible.apiKey", nested(t, s, "openai-compatible")["apiKey"], "mine")
				wantEq(t, "anthropic", nested(t, s, "anthropic")["apiKey"], "a")
				wantEq(t, "theme", s["theme"], "dark")
			},
		},
		{
			id: "cline",
			seed: func(home string) map[string]string {
				return map[string]string{
					"state":   filepath.Join(clineDir(home), "globalState.json"),
					"secrets": filepath.Join(clineDir(home), "secrets.json"),
				}
			},
			content: func(string) map[string]string {
				return map[string]string{
					"state":   `{"actModeApiProvider":"anthropic","planModeApiProvider":"bedrock","telemetry":"off"}`,
					"secrets": `{"openAiApiKey":"user-own-key","apiKey":"keep"}`,
				}
			},
			postInstall: func(t *testing.T, home string) {
				s := readJSONMap(t, filepath.Join(clineDir(home), "globalState.json"))
				s["autoApprove"] = true
				writeJSONMust(t, filepath.Join(clineDir(home), "globalState.json"), s)
			},
			check: func(t *testing.T, home string) {
				s := readJSONMap(t, filepath.Join(clineDir(home), "globalState.json"))
				// Both provider choices were the operator's own, so both come back.
				wantEq(t, "actModeApiProvider", s["actModeApiProvider"], "anthropic")
				wantEq(t, "planModeApiProvider", s["planModeApiProvider"], "bedrock")
				// The gateway keys were not there before the install.
				wantAbsent(t, "state", s, "openAiBaseUrl")
				wantAbsent(t, "state", s, "openAiModelId")
				wantAbsent(t, "state", s, "planModeOpenAiModelId")
				wantEq(t, "telemetry", s["telemetry"], "off")
				wantEq(t, "autoApprove", s["autoApprove"], true)
				secrets := readJSONMap(t, filepath.Join(clineDir(home), "secrets.json"))
				wantEq(t, "secrets.openAiApiKey", secrets["openAiApiKey"], "user-own-key")
				wantEq(t, "secrets.apiKey", secrets["apiKey"], "keep")
			},
		},
		{
			id:   "grok-build",
			seed: func(home string) map[string]string { return map[string]string{"config": grokPath(home)} },
			content: func(string) map[string]string {
				return map[string]string{"config": "# my grok config\n[models]\ndefault = \"grok-4\" # pinned\n\n[model.mine]\nmodel = \"m\"\n"}
			},
			postInstall: func(t *testing.T, home string) {
				appendText(t, grokPath(home), "\n[extra]\nvalue = \"1\"\n")
			},
			check: func(t *testing.T, home string) {
				got := readTestFile(t, grokPath(home))
				mustContain(t, "grok", got, "# my grok config", `default = "grok-4"`, "[model.mine]", "[extra]", `value = "1"`)
				mustNotContain(t, "grok", got, "9router", "localhost")
			},
		},
		{
			id:   "deepseek-tui",
			seed: func(home string) map[string]string { return map[string]string{"config": deepSeekPath(home)} },
			content: func(string) map[string]string {
				return map[string]string{"config": "provider = \"deepseek\"\ntheme = \"dark\"\n\n[providers.deepseek]\napi_key = \"ds\"\n\n[providers.openai]\nbase_url = \"https://mine.example.com/v1\"\n"}
			},
			postInstall: func(t *testing.T, home string) {
				m := readTOMLMap(t, deepSeekPath(home))
				m["verbose"] = true
				writeTOMLMust(t, deepSeekPath(home), m)
			},
			check: func(t *testing.T, home string) {
				m := readTOMLMap(t, deepSeekPath(home))
				wantEq(t, "provider", m["provider"], "deepseek")
				providers := nested(t, m, "providers")
				wantEq(t, "providers.openai.base_url", nested(t, providers, "openai")["base_url"], "https://mine.example.com/v1")
				wantEq(t, "providers.deepseek.api_key", nested(t, providers, "deepseek")["api_key"], "ds")
				wantEq(t, "theme", m["theme"], "dark")
				wantEq(t, "verbose", m["verbose"], true)
			},
		},
		{
			id: "jcode",
			seed: func(home string) map[string]string {
				return map[string]string{
					"config": jcodePath(home),
					"env":    jcodeEnvPath(home),
				}
			},
			content: func(string) map[string]string {
				return map[string]string{
					"config": "[providers.other]\ntype = 'x'\n",
					"env":    "OTHER=1\n",
				}
			},
			postInstall: func(t *testing.T, home string) {
				m := readTOMLMap(t, jcodePath(home))
				m["theme"] = "dark"
				writeTOMLMust(t, jcodePath(home), m)
			},
			check: func(t *testing.T, home string) {
				m := readTOMLMap(t, jcodePath(home))
				providers := nested(t, m, "providers")
				wantAbsent(t, "providers", providers, "9router")
				wantEq(t, "providers.other.type", nested(t, providers, "other")["type"], "x")
				wantEq(t, "theme", m["theme"], "dark")
				env := readTestFile(t, jcodeEnvPath(home))
				mustContain(t, "jcode env", env, "OTHER=1")
				mustNotContain(t, "jcode env", env, jcodeKeyEnv)
			},
		},
	}

	for _, row := range rows {
		t.Run(row.id, func(t *testing.T) { runRestoreCase(t, row) })
		t.Run(row.id+"/no-backup", func(t *testing.T) { runNoBackupCase(t, row) })
	}
}

func writeTOMLMust(t *testing.T, path string, m map[string]any) {
	t.Helper()
	if _, err := writeTOML(path, m); err != nil {
		t.Fatal(err)
	}
}

// TestResetWithoutBackupReportsHonestly pins the wording of the fallback, so an
// operator reading the dashboard message knows the prior values are gone.
func TestResetWithoutBackupReportsHonestly(t *testing.T) {
	home := testHome(t)
	path := claudePath(home)
	writeTestFile(t, path, `{"env":{"ANTHROPIC_BASE_URL":"https://mine.example.com"}}`)
	tool, _ := Lookup("claude")
	if _, err := tool.Apply(opts()); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(backupPath(path)); err != nil {
		t.Fatal(err)
	}
	res, err := tool.Reset()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.Message, "could not be restored") {
		t.Errorf("message = %q, want it to say the prior values could not be restored", res.Message)
	}
	mustNotContain(t, "claude", readTestFile(t, path), "ANTHROPIC")
}

// TestResetKeepsBackupOriginal guards the invariant directly: the backup is the
// pre-9router file and reset must never overwrite it with post-install state.
func TestResetKeepsBackupOriginal(t *testing.T) {
	home := testHome(t)
	path := codexPath(home)
	orig := "model = \"gpt-5-codex\"\napproval_policy = \"never\"\n"
	writeTestFile(t, path, orig)
	tool, _ := Lookup("codex")
	if _, err := tool.Apply(opts()); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if _, err := tool.Reset(); err != nil {
			t.Fatal(err)
		}
		if got := readTestFile(t, backupPath(path)); got != orig {
			t.Fatalf("backup after reset = %q, want the pre-install original", got)
		}
	}
}

// TestRestoreHelpers covers the primitives the installers share.
func TestRestoreHelpers(t *testing.T) {
	t.Run("restoreKeys", func(t *testing.T) {
		cur := map[string]any{"a": "ours", "b": "ours", "keep": "x"}
		prev := map[string]any{"a": "theirs"}
		restoreKeys(cur, prev, "a", "b")
		wantEq(t, "a", cur["a"], "theirs")
		if _, ok := cur["b"]; ok {
			t.Error("b should be removed: it was not in the backup")
		}
		wantEq(t, "keep", cur["keep"], "x")
	})

	t.Run("restoreKeys without a backup removes", func(t *testing.T) {
		cur := map[string]any{"a": "ours"}
		restoreKeys(cur, nil, "a")
		if len(cur) != 0 {
			t.Errorf("cur = %v, want empty", cur)
		}
	})

	t.Run("restoreValue skips 9router namespace values", func(t *testing.T) {
		cur := map[string]any{"model": "9router/cx/gpt-5"}
		prev := map[string]any{"model": "9router/stale"}
		restoreValue(cur, prev, "model", is9RouterModelID)
		if _, ok := cur["model"]; ok {
			t.Error("a backed-up 9router value must be removed, not restored")
		}
	})

	t.Run("env line round trip", func(t *testing.T) {
		wantEq(t, "restore", restoreEnvLine("A=1\nB=2\n", "B", "old", true), "A=1\nB=old\n")
		wantEq(t, "remove", restoreEnvLine("A=1\nB=2\n", "B", "", false), "A=1\n")
		if v, ok := envLine("A=1\nB=2\n", "B"); !ok || v != "2" {
			t.Errorf("envLine = %q, %v", v, ok)
		}
	})
}
