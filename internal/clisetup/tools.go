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

func resetClaude(home string) (Result, error) {
	path := claudePath(home)
	s := map[string]any{}
	exists, err := readJSON(path, &s)
	if err != nil || !exists {
		return Result{Message: "No Claude Code settings to reset", ConfigPath: path}, err
	}
	if env, ok := s["env"].(map[string]any); ok {
		for _, k := range claudeResetKeys {
			delete(env, k)
		}
		if len(env) == 0 {
			delete(s, "env")
		}
	}
	_, err = writeJSON(path, s)
	return Result{Message: "9router settings removed from Claude Code", ConfigPath: path}, err
}

func claudeConfigured(home string) (bool, string) {
	path := claudePath(home)
	s := map[string]any{}
	if _, err := readJSON(path, &s); err != nil {
		return false, path
	}
	env, _ := s["env"].(map[string]any)
	v, _ := env["ANTHROPIC_BASE_URL"].(string)
	return v != "", path
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
	if cfg["model_provider"] == "9router" {
		delete(cfg, "model")
		delete(cfg, "model_provider")
	}
	if p, ok := cfg["model_providers"].(map[string]any); ok {
		delete(p, "9router")
		if len(p) == 0 {
			delete(cfg, "model_providers")
		}
	}
	if a, ok := cfg["agents"].(map[string]any); ok {
		delete(a, "default_subagent_model")
		delete(a, "subagent")
		if len(a) == 0 {
			delete(cfg, "agents")
		}
	}
	_, err = writeTOML(path, cfg)
	return Result{Message: "9router settings removed from Codex", ConfigPath: path}, err
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
	if p, ok := cfg["provider"].(map[string]any); ok {
		delete(p, "9router")
		if len(p) == 0 {
			delete(cfg, "provider")
		}
	}
	if m, _ := cfg["model"].(string); strings.HasPrefix(m, "9router/") {
		delete(cfg, "model")
	}
	if a, ok := cfg["agent"].(map[string]any); ok {
		if e, _ := a["explorer"].(map[string]any); e != nil {
			if m, _ := e["model"].(string); strings.HasPrefix(m, "9router/") {
				delete(a, "explorer")
			}
		}
		if len(a) == 0 {
			delete(cfg, "agent")
		}
	}
	_, err = writeJSON(path, cfg)
	return Result{Message: "9router settings removed from OpenCode", ConfigPath: path}, err
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

func resetHermes(home string) (Result, error) {
	cfgPath := filepath.Join(hermesDir(home), "config.yaml")
	yaml, err := readFile(cfgPath)
	if err != nil || yaml == nil {
		return Result{Message: "No Hermes config to reset", ConfigPath: cfgPath}, err
	}
	text := strings.TrimLeft(hermesModelBlock.ReplaceAllString(string(yaml), ""), "\n")
	_, err = writeFile(cfgPath, []byte(text))
	return Result{Message: "9router model block removed from Hermes", ConfigPath: cfgPath}, err
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
	body := string(m[1])
	return strings.Contains(body, `provider: "custom"`) && isLocalGatewayURL(body), cfgPath
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

func withoutCopilot9Router(entries []any) []any {
	out := entries[:0:0]
	for _, e := range entries {
		if m, _ := e.(map[string]any); m != nil && m["name"] == "9Router" {
			continue
		}
		out = append(out, e)
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
	_, err = writeJSON(path, withoutCopilot9Router(entries))
	return Result{Message: "9Router removed from Copilot models", ConfigPath: path}, err
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
	for _, k := range coworkKeys {
		delete(cfg, k)
	}
	_, err = writeJSON(path, cfg)
	return Result{Message: "9router gateway removed from Cowork. Quit & reopen Claude Desktop.", ConfigPath: path}, err
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
