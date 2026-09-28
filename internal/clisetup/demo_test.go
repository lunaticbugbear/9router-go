package clisetup

import (
	"fmt"
	"path/filepath"
	"testing"
)

// TestDemonstrateResetRestores is a narrative, self-checking demonstration of
// the fixed behaviour for the three reported tools. It prints before/after
// states so the change is visible, and asserts each of the four guarantees.
func TestDemonstrateResetRestores(t *testing.T) {
	for _, d := range []struct {
		id, label string
		path      func(home string) string
		seed      string
	}{
		{"claude", "claude settings.json", claudePath, `{
  "theme": "dark",
  "env": {
    "ANTHROPIC_BASE_URL": "https://my-own-corp-gateway.example.com",
    "ANTHROPIC_AUTH_TOKEN": "corp-token-REDACTED",
    "ANTHROPIC_DEFAULT_SONNET_MODEL": "claude-sonnet-user-choice"
  }
}`},
		{"codex", "codex config.toml", codexPath, "model = \"gpt-5-codex\"\napproval_policy = \"never\"\n"},
		{"cline", "cline globalState.json", func(h string) string {
			return filepath.Join(clineDir(h), "globalState.json")
		}, `{"actModeApiProvider":"anthropic","planModeApiProvider":"bedrock","telemetry":"off"}`},
	} {
		t.Run(d.id, func(t *testing.T) {
			home := testHome(t)
			t.Setenv("XDG_CONFIG_HOME", "")
			path := d.path(home)
			writeTestFile(t, path, d.seed)
			original := readTestFile(t, path)

			tool, _ := Lookup(d.id)
			if _, err := tool.Apply(opts()); err != nil {
				t.Fatal(err)
			}
			fmt.Printf("\n=== %s (%s) ===\n-- before install --\n%s\n-- after install --\n%s", d.id, d.label, original, readTestFile(t, path))

			// An unrelated setting the operator adds after the install.
			switch d.id {
			case "claude":
				s := readJSONMap(t, path)
				s["model"] = "opus"
				writeJSONMust(t, path, s)
			case "codex":
				m := readTOMLMap(t, path)
				m["sandbox_mode"] = "read-only"
				writeTOMLMust(t, path, m)
			case "cline":
				s := readJSONMap(t, path)
				s["autoApprove"] = true
				writeJSONMust(t, path, s)
			}

			res, err := tool.Reset()
			if err != nil {
				t.Fatal(err)
			}
			after := readTestFile(t, path)
			fmt.Printf("-- after reset --\n%s-- message: %s\n", after, res.Message)

			// The backup still holds the true pre-9router original.
			bak := readTestFile(t, backupPath(path))
			if bak != original {
				t.Errorf("backup was rewritten:\n got %s\nwant %s", bak, original)
			}
			fmt.Printf("-- backup (%s) still the pre-install original: %v\n", filepath.Base(backupPath(path)), bak == original)

			switch d.id {
			case "claude":
				s := readJSONMap(t, path)
				env := nested(t, s, "env")
				wantEq(t, "prior ANTHROPIC_BASE_URL restored", env["ANTHROPIC_BASE_URL"], "https://my-own-corp-gateway.example.com")
				wantEq(t, "prior ANTHROPIC_AUTH_TOKEN restored", env["ANTHROPIC_AUTH_TOKEN"], "corp-token-REDACTED")
				wantEq(t, "prior sonnet model restored", env["ANTHROPIC_DEFAULT_SONNET_MODEL"], "claude-sonnet-user-choice")
				// ANTHROPIC_DEFAULT_OPUS_MODEL was absent before the install.
				wantAbsent(t, "absent-before key removed", env, "ANTHROPIC_DEFAULT_OPUS_MODEL")
				wantEq(t, "unrelated post-install edit kept", s["model"], "opus")
			case "codex":
				m := readTOMLMap(t, path)
				wantEq(t, "prior model restored", m["model"], "gpt-5-codex")
				wantAbsent(t, "absent-before key removed", m, "model_provider")
				// The backup had no model_providers table and the install added
				// only the 9router entry, so the emptied table is pruned rather
				// than left behind as a stray empty header.
				wantAbsent(t, "pruned model_providers table", m, "model_providers")
				wantEq(t, "unrelated post-install edit kept", m["sandbox_mode"], "read-only")
			case "cline":
				s := readJSONMap(t, path)
				wantEq(t, "prior actModeApiProvider restored", s["actModeApiProvider"], "anthropic")
				wantEq(t, "prior planModeApiProvider restored", s["planModeApiProvider"], "bedrock")
				wantAbsent(t, "absent-before key removed", s, "openAiBaseUrl")
				wantEq(t, "unrelated post-install edit kept", s["autoApprove"], true)
			}
		})
	}
}
