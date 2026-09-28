package clisetup

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"

	"github.com/google/uuid"
	toml "github.com/pelletier/go-toml/v2"
)

func init() {
	register(&Tool{ID: "claude", Name: "Claude Code", apply: applyClaude, reset: resetClaude, configured: claudeConfigured})
	register(&Tool{ID: "codex", Name: "OpenAI Codex CLI", NeedsModel: true, apply: applyCodex, reset: resetCodex, configured: codexConfigured})
	register(&Tool{ID: "opencode", Name: "OpenCode", NeedsModel: true, apply: applyOpenCode, reset: resetOpenCode, configured: openCodeConfigured})
	register(&Tool{ID: "hermes", Name: "Hermes Agent", NeedsModel: true, apply: applyHermes, reset: resetHermes, configured: hermesConfigured})
	register(&Tool{ID: "copilot", Name: "GitHub Copilot", NeedsModel: true, apply: applyCopilot, reset: resetCopilot, configured: copilotConfigured})
	register(&Tool{ID: "cowork", Name: "Claude Cowork", NeedsModel: true, apply: applyCowork, reset: resetCowork, configured: coworkConfigured})
}

// ---------------------------------------------------------------- Claude Code

var claudeResetKeys = []string{
	"ANTHROPIC_BASE_URL", "ANTHROPIC_AUTH_TOKEN", "ANTHROPIC_DEFAULT_OPUS_MODEL",
	"ANTHROPIC_DEFAULT_SONNET_MODEL", "ANTHROPIC_DEFAULT_HAIKU_MODEL", "API_TIMEOUT_MS",
	"CLAUDE_CODE_AUTO_COMPACT_WINDOW",
}

func claudePath(home string) string { return filepath.Join(home, ".claude", "settings.json") }

func applyClaude(home string, o Options) (Result, error) {
	path := claudePath(home)
	s := map[string]any{}
	if _, err := readJSON(path, &s); err != nil {
		return Result{}, err
	}
	s["hasCompletedOnboarding"] = true
	env := objectAt(s, "env")
	env["ANTHROPIC_BASE_URL"] = o.v1()
	env["ANTHROPIC_AUTH_TOKEN"] = o.APIKey
	if m := o.firstModel(); m != "" {
		env["ANTHROPIC_DEFAULT_OPUS_MODEL"] = m
		env["ANTHROPIC_DEFAULT_SONNET_MODEL"] = m
		env["ANTHROPIC_DEFAULT_HAIKU_MODEL"] = m
	}
	bak, err := writeJSON(path, s)
	return Result{Message: "Claude Code now routes through 9router. Start a new claude session.", ConfigPath: path, BackupPath: bak}, err
}

// claudeManagedKeys are the settings applyClaude writes over: the operator's
// own values live in the backup, so reset restores them.
var claudeManagedKeys = []string{
	"hasCompletedOnboarding",
}

func resetClaude(home string) (Result, error) {
	path := claudePath(home)
	s := map[string]any{}
	exists, err := readJSON(path, &s)
	if err != nil || !exists {
		return Result{Message: "No Claude Code settings to reset", ConfigPath: path}, err
	}
	plan := planJSON(path)
	restoreKeys(s, plan.region(), claudeManagedKeys...)
	if env, ok := s["env"].(map[string]any); ok {
		restoreKeys(env, plan.region("env"), claudeResetKeys...)
		pruneRestored(s, "env", plan.had("env"))
	}
	if err := writeJSONReset(path, s); err != nil {
		return Result{}, err
	}
	return Result{Message: resetMessage("Claude Code reset.", path, plan.mode()), ConfigPath: path}, nil
}

func claudeConfigured(home string) (bool, string) {
	path := claudePath(home)
	s := map[string]any{}
	if _, err := readJSON(path, &s); err != nil {
		return false, path
	}
	env, _ := s["env"].(map[string]any)
	v, _ := env["ANTHROPIC_BASE_URL"].(string)
	// A restored operator value (their own gateway) must not read as "Using
	// 9router", so the URL has to look like this gateway.
	return v != "" && (isLocalGatewayURL(v) || strings.Contains(v, "9router")), path
}

// ---------------------------------------------------------------- Codex CLI

func codexPath(home string) string { return filepath.Join(home, ".codex", "config.toml") }

func readTOML(path string) (map[string]any, bool, error) {
	b, err := readFile(path)
	if err != nil || b == nil {
		return map[string]any{}, false, err
	}
	cfg := map[string]any{}
	if err := toml.Unmarshal(b, &cfg); err != nil {
		return nil, true, &parseError{path: path, err: err}
	}
	return cfg, true, nil
}

type parseError struct {
	path string
	err  error
}

func (e *parseError) Error() string {
	return e.path + " is not valid, left unchanged: " + e.err.Error()
}

func writeTOML(path string, cfg map[string]any) (string, error) {
	b, err := toml.Marshal(cfg)
	if err != nil {
		return "", err
	}
	return writeFile(path, b)
}

// writeTOMLReset writes the config back without touching the install backup.
func writeTOMLReset(path string, cfg map[string]any) error {
	b, err := toml.Marshal(cfg)
	if err != nil {
		return err
	}
	return writeReset(path, b)
}

func applyCodex(home string, o Options) (Result, error) {
	path := codexPath(home)
	cfg, _, err := readTOML(path)
	if err != nil {
		return Result{}, err
	}
	cfg["model"] = o.firstModel()
	cfg["model_provider"] = "9router"
	// Custom providers ignore auth.json, so the key travels as a static header.
	objectAt(cfg, "model_providers")["9router"] = map[string]any{
		"name":         "9Router",
		"base_url":     o.v1(),
		"wire_api":     "responses",
		"http_headers": map[string]any{"Authorization": "Bearer " + o.APIKey},
	}
	agents := objectAt(cfg, "agents")
	delete(agents, "subagent")
	agents["default_subagent_model"] = o.firstModel()
	bak, err := writeTOML(path, cfg)
	return Result{Message: "Codex now uses the 9router provider.", ConfigPath: path, BackupPath: bak}, err
}

func resetCodex(home string) (Result, error) {
	path := codexPath(home)
	cfg, exists, err := readTOML(path)
	if err != nil || !exists {
		return Result{Message: "No Codex config to reset", ConfigPath: path}, err
	}
	plan := planTOML(path)
	// Only step aside when the file still points at this gateway; otherwise the
	// operator's own model_provider choice is none of our business.
	if cfg["model_provider"] == "9router" {
		restoreKeys(cfg, plan.region(), "model", "model_provider")
	}
	if p, ok := cfg["model_providers"].(map[string]any); ok {
		prev := plan.region("model_providers")
		restoreKeys(p, prev, "9router")
		pruneRestored(cfg, "model_providers", prev != nil)
	}
	if a, ok := cfg["agents"].(map[string]any); ok {
		prev := plan.region("agents")
		restoreKeys(a, prev, "default_subagent_model", "subagent")
		pruneRestored(cfg, "agents", prev != nil)
	}
	if err := writeTOMLReset(path, cfg); err != nil {
		return Result{}, err
	}
	return Result{Message: resetMessage("Codex reset.", path, plan.mode()), ConfigPath: path}, nil
}

func codexConfigured(home string) (bool, string) {
	path := codexPath(home)
	cfg, _, err := readTOML(path)
	if err != nil {
		return false, path
	}
	p, _ := cfg["model_providers"].(map[string]any)
	return cfg["model_provider"] == "9router" || p["9router"] != nil, path
}

// ---------------------------------------------------------------- OpenCode

func openCodePath(home string) string {
	return filepath.Join(home, ".config", "opencode", "opencode.json")
}

func applyOpenCode(home string, o Options) (Result, error) {
	path := openCodePath(home)
	cfg := map[string]any{}
	if _, err := readJSON(path, &cfg); err != nil {
		return Result{}, err
	}
	providers := objectAt(cfg, "provider")
	p, _ := providers["9router"].(map[string]any)
	if p == nil {
		p = map[string]any{"npm": "@ai-sdk/openai-compatible"}
	}
	opts := objectAt(p, "options")
	opts["baseURL"] = o.v1()
	opts["apiKey"] = o.APIKey
	models := objectAt(p, "models")
	for _, m := range o.Models {
		models[m] = map[string]any{"name": m, "modalities": map[string]any{"input": []any{"text", "image"}, "output": []any{"text"}}}
	}
	providers["9router"] = p
	cfg["model"] = "9router/" + o.firstModel()
	objectAt(cfg, "agent")["explorer"] = map[string]any{
		"description": "Fast explorer subagent for codebase exploration",
		"mode":        "subagent",
		"model":       "9router/" + o.firstModel(),
	}
	bak, err := writeJSON(path, cfg)
	return Result{Message: "OpenCode now has the 9router provider as its active model.", ConfigPath: path, BackupPath: bak}, err
}

func resetOpenCode(home string) (Result, error) {
	path := openCodePath(home)
	cfg := map[string]any{}
	exists, err := readJSON(path, &cfg)
	if err != nil || !exists {
		return Result{Message: "No OpenCode config to reset", ConfigPath: path}, err
	}
	plan := planJSON(path)
	if p, ok := cfg["provider"].(map[string]any); ok {
		prev := plan.region("provider")
		restoreKeys(p, prev, "9router")
		pruneRestored(cfg, "provider", prev != nil)
	}
	// applyOpenCode prefixes the active model and the explorer subagent with
	// "9router/"; anything else is the operator's own choice.
	if m, _ := cfg["model"].(string); strings.HasPrefix(m, "9router/") {
		restoreKeys(cfg, plan.region(), "model")
	}
	if a, ok := cfg["agent"].(map[string]any); ok {
		prev := plan.region("agent")
		if e, _ := a["explorer"].(map[string]any); e != nil {
			if m, _ := e["model"].(string); strings.HasPrefix(m, "9router/") {
				restoreKeys(a, prev, "explorer")
			}
		}
		pruneRestored(cfg, "agent", prev != nil)
	}
	if err := writeJSONReset(path, cfg); err != nil {
		return Result{}, err
	}
	return Result{Message: resetMessage("OpenCode reset.", path, plan.mode()), ConfigPath: path}, nil
}

func openCodeConfigured(home string) (bool, string) {
	path := openCodePath(home)
	cfg := map[string]any{}
	if _, err := readJSON(path, &cfg); err != nil {
		return false, path
	}
	p, _ := cfg["provider"].(map[string]any)
	return p["9router"] != nil, path
}

// ---------------------------------------------------------------- Hermes Agent

var hermesModelBlock = regexp.MustCompile(`(?m)^model:[ \t]*\r?\n((?:[ \t]+.*\r?\n?|[ \t]*\r?\n)*)`)

func hermesDir(home string) string { return filepath.Join(home, ".hermes") }

func applyHermes(home string, o Options) (Result, error) {
	cfgPath := filepath.Join(hermesDir(home), "config.yaml")
	envPath := filepath.Join(hermesDir(home), ".env")
	yaml, err := readFile(cfgPath)
	if err != nil {
		return Result{}, err
	}
	block := "model:\n  default: \"" + o.firstModel() + "\"\n  provider: \"custom\"\n  base_url: \"" + o.v1() + "\"\n  api_key: ${OPENAI_API_KEY}\n"
	text := string(yaml)
	if hermesModelBlock.MatchString(text) {
		text = hermesModelBlock.ReplaceAllLiteralString(text, block)
	} else if text != "" {
		text = block + "\n" + text
	} else {
		text = block
	}
	bak, err := writeFile(cfgPath, []byte(text))
	if err != nil {
		return Result{}, err
	}
	envText, err := readFile(envPath)
	if err != nil {
		return Result{}, err
	}
	if _, err := writeFile(envPath, []byte(upsertEnvLine(string(envText), "OPENAI_API_KEY", o.APIKey))); err != nil {
		return Result{}, err
	}
	return Result{Message: "Hermes now uses 9router as its custom model provider.", ConfigPath: cfgPath, BackupPath: bak}, nil
}

func upsertEnvLine(text, key, value string) string {
	re := regexp.MustCompile(`(?m)^` + regexp.QuoteMeta(key) + `=.*$`)
	line := key + "=" + value
	if re.MatchString(text) {
		return re.ReplaceAllLiteralString(text, line)
	}
	if text != "" && !strings.HasSuffix(text, "\n") {
		text += "\n"
	}
	return text + line + "\n"
}

// hermesModelIsOurs reports whether a model block is the one 9router wrote.
func hermesModelIsOurs(body string) bool {
	return strings.Contains(body, `provider: "custom"`) && isLocalGatewayURL(body)
}

func resetHermes(home string) (Result, error) {
	cfgPath := filepath.Join(hermesDir(home), "config.yaml")
	yaml, err := readFile(cfgPath)
	if err != nil || yaml == nil {
		return Result{Message: "No Hermes config to reset", ConfigPath: cfgPath}, err
	}
	text := string(yaml)
	plan := planText(cfgPath)
	// applyHermes replaced the operator's own "model:" block, so that block is
	// what reset has to put back; the block itself is the managed unit here.
	if m := hermesModelBlock.FindSubmatch(yaml); m != nil && hermesModelIsOurs(string(m[1])) {
		if block := hermesModelBlock.FindString(plan.text); plan.ok() && block != "" {
			text = hermesModelBlock.ReplaceAllLiteralString(text, block)
		} else {
			text = strings.TrimLeft(hermesModelBlock.ReplaceAllString(text, ""), "\n")
		}
	}
	if err := writeReset(cfgPath, []byte(text)); err != nil {
		return Result{}, err
	}

	envPath := filepath.Join(hermesDir(home), ".env")
	if envText, err := readFile(envPath); err == nil && envText != nil {
		prevEnv, hadEnvBackup := readBackupText(envPath)
		value, had := "", false
		if hadEnvBackup {
			value, had = envLine(prevEnv, "OPENAI_API_KEY")
		}
		if err := writeReset(envPath, []byte(restoreEnvLine(string(envText), "OPENAI_API_KEY", value, had))); err != nil {
			return Result{}, err
		}
	}
	return Result{Message: resetMessage("Hermes reset.", cfgPath, plan.mode()), ConfigPath: cfgPath}, nil
}

func hermesConfigured(home string) (bool, string) {
	cfgPath := filepath.Join(hermesDir(home), "config.yaml")
	yaml, err := readFile(cfgPath)
	if err != nil || yaml == nil {
		return false, cfgPath
	}
	m := hermesModelBlock.FindSubmatch(yaml)
	if m == nil {
		return false, cfgPath
	}
	return hermesModelIsOurs(string(m[1])), cfgPath
}

// ---------------------------------------------------------------- GitHub Copilot (VS Code)

func copilotPath(home string) string {
	switch runtime.GOOS {
	case "windows":
		base := os.Getenv("APPDATA")
		if base == "" {
			base = home
		}
		return filepath.Join(base, "Code", "User", "chatLanguageModels.json")
	case "darwin":
		return filepath.Join(home, "Library", "Application Support", "Code", "User", "chatLanguageModels.json")
	default:
		return filepath.Join(home, ".config", "Code", "User", "chatLanguageModels.json")
	}
}

func readCopilot(path string) ([]any, bool, error) {
	var entries []any
	exists, err := readJSON(path, &entries)
	return entries, exists, err
}

// copilotIsOurs identifies the 9Router entry 9router adds to the model list.
func copilotIsOurs(e any) bool {
	m, _ := e.(map[string]any)
	return m != nil && m["name"] == "9Router"
}

func withoutCopilot9Router(entries []any) []any {
	out := entries[:0:0]
	for _, e := range entries {
		if !copilotIsOurs(e) {
			out = append(out, e)
		}
	}
	return out
}

func applyCopilot(home string, o Options) (Result, error) {
	path := copilotPath(home)
	entries, _, err := readCopilot(path)
	if err != nil {
		return Result{}, err
	}
	endpoint := o.v1() + "/chat/completions#models.ai.azure.com"
	models := make([]any, 0, len(o.Models))
	for _, id := range o.Models {
		models = append(models, map[string]any{
			"id": id, "name": id, "url": endpoint, "toolCalling": true, "vision": false,
			"maxInputTokens": 128000, "maxOutputTokens": 16000,
		})
	}
	entries = append(withoutCopilot9Router(entries), map[string]any{
		"name": "9Router", "vendor": "azure", "apiKey": o.APIKey, "models": models,
	})
	bak, err := writeJSON(path, entries)
	return Result{Message: "Copilot model list updated. Reload VS Code to pick up the 9Router models.", ConfigPath: path, BackupPath: bak}, err
}

func resetCopilot(home string) (Result, error) {
	path := copilotPath(home)
	entries, exists, err := readCopilot(path)
	if err != nil || !exists {
		return Result{Message: "No Copilot model config to reset", ConfigPath: path}, err
	}
	// The managed unit is the entry named "9Router", which is 9router's own
	// namespace: applyCopilot drops the operator's entry of that name and
	// re-adds its own, so removing it is already the exact inverse. Every other
	// entry is untouched.
	if err := writeJSONReset(path, withoutCopilot9Router(entries)); err != nil {
		return Result{}, err
	}
	return Result{Message: resetMessage("Copilot model list reset.", path, modeNamespaceOnly), ConfigPath: path}, nil
}

func copilotConfigured(home string) (bool, string) {
	path := copilotPath(home)
	entries, _, err := readCopilot(path)
	if err != nil {
		return false, path
	}
	return len(withoutCopilot9Router(entries)) != len(entries), path
}

// ---------------------------------------------------------------- Claude Desktop (Cowork, 3p gateway mode)

func appSupportRoots(home, name string) []string {
	switch runtime.GOOS {
	case "darwin":
		return []string{filepath.Join(home, "Library", "Application Support", name)}
	case "windows":
		var out []string
		for _, env := range []string{"LOCALAPPDATA", "APPDATA"} {
			if base := os.Getenv(env); base != "" {
				out = append(out, filepath.Join(base, name))
			}
		}
		if len(out) == 0 {
			out = append(out, filepath.Join(home, "AppData", "Local", name))
		}
		return out
	default:
		return []string{filepath.Join(home, ".config", name)}
	}
}

func coworkWriteRoot(home string) string { return appSupportRoots(home, "Claude-3p")[0] }

func coworkReadRoot(home string) string {
	candidates := append(appSupportRoots(home, "Claude-3p"), appSupportRoots(home, "Claude")...)
	for _, dir := range candidates {
		if fi, err := os.Stat(filepath.Join(dir, "configLibrary")); err == nil && fi.IsDir() {
			return dir
		}
	}
	return candidates[0]
}

func coworkMeta(root string) (map[string]any, string, error) {
	meta := map[string]any{}
	_, err := readJSON(filepath.Join(root, "configLibrary", "_meta.json"), &meta)
	id, _ := meta["appliedId"].(string)
	return meta, id, err
}

func applyCowork(home string, o Options) (Result, error) {
	// Claude Desktop only reads configLibrary in third-party deployment mode.
	desktopCfgPath := filepath.Join(appSupportRoots(home, "Claude")[0], "claude_desktop_config.json")
	desktop := map[string]any{}
	if _, err := readJSON(desktopCfgPath, &desktop); err != nil {
		return Result{}, err
	}
	bootstrapped := desktop["deploymentMode"] != "3p"
	if bootstrapped {
		desktop["deploymentMode"] = "3p"
		if _, err := writeJSON(desktopCfgPath, desktop); err != nil {
			return Result{}, err
		}
	}

	root := coworkWriteRoot(home)
	meta, id, err := coworkMeta(root)
	if err != nil {
		return Result{}, err
	}
	if id == "" {
		if _, readID, err := coworkMeta(coworkReadRoot(home)); err == nil && readID != "" {
			id = readID
		} else {
			id = uuid.NewString()
		}
		meta = map[string]any{"appliedId": id, "entries": []any{map[string]any{"id": id, "name": "Default"}}}
		if _, err := writeJSON(filepath.Join(root, "configLibrary", "_meta.json"), meta); err != nil {
			return Result{}, err
		}
	}

	path := filepath.Join(root, "configLibrary", id+".json")
	cfg := map[string]any{}
	if _, err := readJSON(path, &cfg); err != nil {
		return Result{}, err
	}
	models := make([]any, 0, len(o.Models))
	for _, m := range o.Models {
		models = append(models, map[string]any{"name": m})
	}
	cfg["inferenceProvider"] = "gateway"
	cfg["inferenceGatewayBaseUrl"] = o.BaseURL
	cfg["inferenceGatewayApiKey"] = o.APIKey
	cfg["inferenceModels"] = models
	bak, err := writeJSON(path, cfg)
	msg := "Cowork gateway settings applied. Quit & reopen Claude Desktop."
	if bootstrapped {
		msg = "Claude Desktop switched to third-party gateway mode. Quit & reopen Claude Desktop."
	}
	return Result{Message: msg, ConfigPath: path, BackupPath: bak}, err
}

var coworkKeys = []string{"inferenceProvider", "inferenceGatewayBaseUrl", "inferenceGatewayApiKey", "inferenceModels"}

func resetCowork(home string) (Result, error) {
	root := coworkReadRoot(home)
	_, id, err := coworkMeta(root)
	if err != nil || id == "" {
		return Result{Message: "No active Cowork config to reset"}, err
	}
	path := filepath.Join(root, "configLibrary", id+".json")
	cfg := map[string]any{}
	exists, err := readJSON(path, &cfg)
	if err != nil || !exists {
		return Result{Message: "No active Cowork config to reset", ConfigPath: path}, err
	}
	plan := planJSON(path)
	restoreKeys(cfg, plan.region(), coworkKeys...)
	if err := writeJSONReset(path, cfg); err != nil {
		return Result{}, err
	}
	return Result{Message: resetMessage("Cowork gateway reset. Quit & reopen Claude Desktop.", path, plan.mode()), ConfigPath: path}, nil
}

func coworkConfigured(home string) (bool, string) {
	root := coworkReadRoot(home)
	_, id, err := coworkMeta(root)
	if err != nil || id == "" {
		return false, ""
	}
	path := filepath.Join(root, "configLibrary", id+".json")
	cfg := map[string]any{}
	if _, err := readJSON(path, &cfg); err != nil {
		return false, path
	}
	base, _ := cfg["inferenceGatewayBaseUrl"].(string)
	return cfg["inferenceProvider"] == "gateway" && base != "", path
}
