package clisetup

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

func init() {
	register(&Tool{ID: "droid", Name: "Factory Droid", NeedsModel: true, apply: applyDroid, reset: resetDroid, configured: droidConfigured})
	register(&Tool{ID: "openclaw", Name: "Open Claw", NeedsModel: true, apply: applyOpenClaw, reset: resetOpenClaw, configured: openClawConfigured})
	register(&Tool{ID: "kilo", Name: "Kilo Code", NeedsModel: true, apply: applyKilo, reset: resetKilo, configured: kiloConfigured})
	register(&Tool{ID: "cline", Name: "Cline", NeedsModel: true, apply: applyCline, reset: resetCline, configured: clineConfigured})
	register(&Tool{ID: "grok-build", Name: "Grok Build", NeedsModel: true, apply: applyGrok, reset: resetGrok, configured: grokConfigured})
	register(&Tool{ID: "deepseek-tui", Name: "DeepSeek TUI", NeedsModel: true, apply: applyDeepSeek, reset: resetDeepSeek, configured: deepSeekConfigured})
	register(&Tool{ID: "jcode", Name: "jcode", NeedsModel: true, apply: applyJcode, reset: resetJcode, configured: jcodeConfigured})
}

func pruneEmpty(m map[string]any, key string) {
	switch v := m[key].(type) {
	case map[string]any:
		if len(v) == 0 {
			delete(m, key)
		}
	case []any:
		if len(v) == 0 {
			delete(m, key)
		}
	}
}

// ---------------------------------------------------------------- Factory Droid

func droidPath(home string) string { return filepath.Join(home, ".factory", "settings.json") }

func isDroid9Router(e any) bool {
	m, _ := e.(map[string]any)
	id, _ := m["id"].(string)
	return strings.HasPrefix(id, "custom:9Router")
}

func withoutDroid9Router(list []any) []any {
	out := list[:0:0]
	for _, e := range list {
		if !isDroid9Router(e) {
			out = append(out, e)
		}
	}
	return out
}

func applyDroid(home string, o Options) (Result, error) {
	path := droidPath(home)
	s := map[string]any{}
	if _, err := readJSON(path, &s); err != nil {
		return Result{}, err
	}
	existing, _ := s["customModels"].([]any)
	models := make([]any, 0, len(o.Models))
	for i, m := range o.Models {
		models = append(models, map[string]any{
			"model": m, "id": "custom:9Router-" + strconv.Itoa(i), "index": i, "baseUrl": o.v1(),
			"apiKey": o.APIKey, "displayName": m, "maxOutputTokens": 131072, "noImageSupport": false, "provider": "openai",
		})
	}
	// 9router entries go first so the chosen model is Droid's default custom model.
	s["customModels"] = append(models, withoutDroid9Router(existing)...)
	bak, err := writeJSON(path, s)
	return Result{Message: "Factory Droid custom models now route through 9router. Pick it with /model in droid.", ConfigPath: path, BackupPath: bak}, err
}

func resetDroid(home string) (Result, error) {
	path := droidPath(home)
	s := map[string]any{}
	exists, err := readJSON(path, &s)
	if err != nil || !exists {
		return Result{Message: "No Factory Droid settings to reset", ConfigPath: path}, err
	}
	if list, ok := s["customModels"].([]any); ok {
		s["customModels"] = withoutDroid9Router(list)
		pruneEmpty(s, "customModels")
	}
	_, err = writeJSON(path, s)
	return Result{Message: "9router models removed from Factory Droid", ConfigPath: path}, err
}

func droidConfigured(home string) (bool, string) {
	path := droidPath(home)
	s := map[string]any{}
	if _, err := readJSON(path, &s); err != nil {
		return false, path
	}
	list, _ := s["customModels"].([]any)
	return len(withoutDroid9Router(list)) != len(list), path
}

// ---------------------------------------------------------------- Open Claw

func openClawPath(home string) string { return filepath.Join(home, ".openclaw", "openclaw.json") }

func agentModelID(v any) string {
	switch m := v.(type) {
	case string:
		return m
	case map[string]any:
		s, _ := m["primary"].(string)
		return s
	}
	return ""
}

func applyOpenClaw(home string, o Options) (Result, error) {
	path := openClawPath(home)
	s := map[string]any{}
	if _, err := readJSON(path, &s); err != nil {
		return Result{}, err
	}
	agents := objectAt(s, "agents")
	defaults := objectAt(agents, "defaults")
	objectAt(defaults, "model")["primary"] = "9router/" + o.firstModel()
	allow := objectAt(defaults, "models")
	for k := range allow {
		if strings.HasPrefix(k, "9router/") {
			delete(allow, k)
		}
	}
	models := make([]any, 0, len(o.Models))
	for _, m := range o.Models {
		allow["9router/"+m] = map[string]any{}
		models = append(models, map[string]any{"id": m, "name": m[strings.LastIndex(m, "/")+1:]})
	}
	provider := map[string]any{"baseUrl": o.v1(), "apiKey": o.APIKey, "api": "openai-completions", "models": models}
	objectAt(objectAt(s, "models"), "providers")["9router"] = provider

	if list, ok := agents["list"].([]any); ok {
		for _, a := range list {
			agent, _ := a.(map[string]any)
			if agent == nil {
				continue
			}
			if strings.HasPrefix(agentModelID(agent["model"]), "9router/") {
				delete(agent, "model")
			}
			// Per-agent models.json is how agents with their own dir see providers.
			dir, _ := agent["agentDir"].(string)
			if dir == "" || !filepath.IsAbs(dir) || !strings.HasPrefix(filepath.Clean(dir), home+string(os.PathSeparator)) {
				continue
			}
			mp := filepath.Join(dir, "models.json")
			am := map[string]any{}
			if _, err := readJSON(mp, &am); err != nil {
				return Result{}, err
			}
			objectAt(am, "providers")["9router"] = map[string]any{
				"baseUrl": o.v1(), "apiKey": o.APIKey, "api": "openai-completions", "models": models[:1],
			}
			if _, err := writeJSON(mp, am); err != nil {
				return Result{}, err
			}
		}
	}
	bak, err := writeJSON(path, s)
	return Result{Message: "Open Claw default model now routes through 9router.", ConfigPath: path, BackupPath: bak}, err
}

func resetOpenClaw(home string) (Result, error) {
	path := openClawPath(home)
	s := map[string]any{}
	exists, err := readJSON(path, &s)
	if err != nil || !exists {
		return Result{Message: "No Open Claw settings to reset", ConfigPath: path}, err
	}
	if m, ok := s["models"].(map[string]any); ok {
		if p, ok := m["providers"].(map[string]any); ok {
			delete(p, "9router")
			pruneEmpty(m, "providers")
		}
	}
	if agents, ok := s["agents"].(map[string]any); ok {
		if d, ok := agents["defaults"].(map[string]any); ok {
			if allow, ok := d["models"].(map[string]any); ok {
				for k := range allow {
					if strings.HasPrefix(k, "9router/") {
						delete(allow, k)
					}
				}
				pruneEmpty(d, "models")
			}
			if mdl, ok := d["model"].(map[string]any); ok {
				if p, _ := mdl["primary"].(string); strings.HasPrefix(p, "9router/") {
					delete(mdl, "primary")
				}
			}
		}
	}
	_, err = writeJSON(path, s)
	return Result{Message: "9router removed from Open Claw", ConfigPath: path}, err
}

func openClawConfigured(home string) (bool, string) {
	path := openClawPath(home)
	s := map[string]any{}
	if _, err := readJSON(path, &s); err != nil {
		return false, path
	}
	m, _ := s["models"].(map[string]any)
	p, _ := m["providers"].(map[string]any)
	return p["9router"] != nil, path
}

// ---------------------------------------------------------------- Kilo Code CLI

func kiloPath(home string) string { return filepath.Join(home, ".local", "share", "kilo", "auth.json") }

func applyKilo(home string, o Options) (Result, error) {
	path := kiloPath(home)
	auth := map[string]any{}
	if _, err := readJSON(path, &auth); err != nil {
		return Result{}, err
	}
	auth["openai-compatible"] = map[string]any{"type": "api-key", "apiKey": o.APIKey, "baseUrl": o.v1(), "model": o.firstModel()}
	bak, err := writeJSON(path, auth)
	return Result{Message: "Kilo Code CLI now uses 9router as its OpenAI-compatible provider.", ConfigPath: path, BackupPath: bak}, err
}

func kiloEntryIsOurs(auth map[string]any) bool {
	e, _ := auth["openai-compatible"].(map[string]any)
	u, _ := e["baseUrl"].(string)
	return isLocalGatewayURL(u) || strings.Contains(u, "9router")
}

func resetKilo(home string) (Result, error) {
	path := kiloPath(home)
	auth := map[string]any{}
	exists, err := readJSON(path, &auth)
	if err != nil || !exists {
		return Result{Message: "No Kilo Code auth to reset", ConfigPath: path}, err
	}
	// Only drop the entry when it points at this gateway, not the user's own endpoint.
	if kiloEntryIsOurs(auth) {
		delete(auth, "openai-compatible")
	}
	delete(auth, "9router")
	_, err = writeJSON(path, auth)
	return Result{Message: "9router removed from Kilo Code", ConfigPath: path}, err
}

func kiloConfigured(home string) (bool, string) {
	path := kiloPath(home)
	auth := map[string]any{}
	if _, err := readJSON(path, &auth); err != nil {
		return false, path
	}
	return kiloEntryIsOurs(auth), path
}

// ---------------------------------------------------------------- Cline CLI

func clineDir(home string) string { return filepath.Join(home, ".cline", "data") }

func clineIsOurs(gs map[string]any) bool {
	act, _ := gs["actModeApiProvider"].(string)
	plan, _ := gs["planModeApiProvider"].(string)
	u, _ := gs["openAiBaseUrl"].(string)
	return (act == "openai" || plan == "openai") && (isLocalGatewayURL(u) || strings.Contains(u, "9router"))
}

func applyCline(home string, o Options) (Result, error) {
	statePath := filepath.Join(clineDir(home), "globalState.json")
	secretsPath := filepath.Join(clineDir(home), "secrets.json")
	gs, secrets := map[string]any{}, map[string]any{}
	if _, err := readJSON(statePath, &gs); err != nil {
		return Result{}, err
	}
	if _, err := readJSON(secretsPath, &secrets); err != nil {
		return Result{}, err
	}
	gs["actModeApiProvider"] = "openai"
	gs["planModeApiProvider"] = "openai"
	gs["openAiBaseUrl"] = o.BaseURL // Cline appends /v1 itself.
	gs["openAiModelId"] = o.firstModel()
	gs["planModeOpenAiModelId"] = o.firstModel()
	bak, err := writeJSON(statePath, gs)
	if err != nil {
		return Result{}, err
	}
	secrets["openAiApiKey"] = o.APIKey
	if _, err := writeJSON(secretsPath, secrets); err != nil {
		return Result{}, err
	}
	return Result{Message: "Cline (act and plan mode) now uses 9router.", ConfigPath: statePath, BackupPath: bak}, nil
}

func resetCline(home string) (Result, error) {
	statePath := filepath.Join(clineDir(home), "globalState.json")
	secretsPath := filepath.Join(clineDir(home), "secrets.json")
	gs := map[string]any{}
	exists, err := readJSON(statePath, &gs)
	if err != nil || !exists {
		return Result{Message: "No Cline settings to reset", ConfigPath: statePath}, err
	}
	if !clineIsOurs(gs) {
		return Result{Message: "Cline is not using 9router; nothing changed", ConfigPath: statePath}, nil
	}
	for _, k := range []string{"openAiBaseUrl", "openAiModelId", "planModeOpenAiModelId"} {
		delete(gs, k)
	}
	gs["actModeApiProvider"] = "cline"
	gs["planModeApiProvider"] = "cline"
	if _, err := writeJSON(statePath, gs); err != nil {
		return Result{}, err
	}
	secrets := map[string]any{}
	if exists, err := readJSON(secretsPath, &secrets); err == nil && exists {
		delete(secrets, "openAiApiKey")
		if _, err := writeJSON(secretsPath, secrets); err != nil {
			return Result{}, err
		}
	}
	return Result{Message: "9router removed from Cline", ConfigPath: statePath}, nil
}

func clineConfigured(home string) (bool, string) {
	statePath := filepath.Join(clineDir(home), "globalState.json")
	gs := map[string]any{}
	if _, err := readJSON(statePath, &gs); err != nil {
		return false, statePath
	}
	return clineIsOurs(gs), statePath
}

// ---------------------------------------------------------------- DeepSeek TUI

func deepSeekPath(home string) string { return filepath.Join(home, ".deepseek", "config.toml") }

func applyDeepSeek(home string, o Options) (Result, error) {
	path := deepSeekPath(home)
	cfg, _, err := readTOML(path)
	if err != nil {
		return Result{}, err
	}
	cfg["provider"] = "openai"
	objectAt(cfg, "providers")["openai"] = map[string]any{"base_url": o.v1(), "api_key": o.APIKey, "model": o.firstModel()}
	bak, err := writeTOML(path, cfg)
	return Result{Message: "DeepSeek TUI now uses 9router through its OpenAI provider.", ConfigPath: path, BackupPath: bak}, err
}

func deepSeekIsOurs(cfg map[string]any) bool {
	p, _ := cfg["providers"].(map[string]any)
	oa, _ := p["openai"].(map[string]any)
	u, _ := oa["base_url"].(string)
	return cfg["provider"] == "openai" && isLocalGatewayURL(u)
}

func resetDeepSeek(home string) (Result, error) {
	path := deepSeekPath(home)
	cfg, exists, err := readTOML(path)
	if err != nil || !exists {
		return Result{Message: "No DeepSeek TUI config to reset", ConfigPath: path}, err
	}
	if !deepSeekIsOurs(cfg) {
		return Result{Message: "DeepSeek TUI is not using 9router; nothing changed", ConfigPath: path}, nil
	}
	cfg["provider"] = "deepseek"
	p := cfg["providers"].(map[string]any)
	delete(p, "openai")
	pruneEmpty(cfg, "providers")
	_, err = writeTOML(path, cfg)
	return Result{Message: "DeepSeek TUI switched back to its DeepSeek provider", ConfigPath: path}, err
}

func deepSeekConfigured(home string) (bool, string) {
	path := deepSeekPath(home)
	cfg, _, err := readTOML(path)
	if err != nil {
		return false, path
	}
	return deepSeekIsOurs(cfg), path
}

// ---------------------------------------------------------------- jcode

const jcodeKeyEnv = "JCODE_9ROUTER_API_KEY"

func jcodePath(home string) string { return filepath.Join(home, ".jcode", "config.toml") }

func jcodeEnvPath(home string) string {
	base := os.Getenv("XDG_CONFIG_HOME")
	if base == "" {
		base = filepath.Join(home, ".config")
	}
	return filepath.Join(base, "jcode", "provider-9router.env")
}

func applyJcode(home string, o Options) (Result, error) {
	path := jcodePath(home)
	cfg, _, err := readTOML(path)
	if err != nil {
		return Result{}, err
	}
	objectAt(cfg, "providers")["9router"] = map[string]any{
		"type": "openai-compatible", "base_url": o.v1(), "auth": "bearer", "api_key_env": jcodeKeyEnv,
		"env_file": "provider-9router.env", "default_model": o.firstModel(), "requires_api_key": true,
	}
	bak, err := writeTOML(path, cfg)
	if err != nil {
		return Result{}, err
	}
	envPath := jcodeEnvPath(home)
	envText, err := readFile(envPath)
	if err != nil {
		return Result{}, err
	}
	if _, err := writeFile(envPath, []byte(upsertEnvLine(string(envText), jcodeKeyEnv, `"`+o.APIKey+`"`))); err != nil {
		return Result{}, err
	}
	return Result{Message: "jcode has a 9router provider profile. Run: jcode --provider-profile 9router", ConfigPath: path, BackupPath: bak}, nil
}

func resetJcode(home string) (Result, error) {
	path := jcodePath(home)
	cfg, exists, err := readTOML(path)
	if err != nil || !exists {
		return Result{Message: "No jcode config to reset", ConfigPath: path}, err
	}
	if p, ok := cfg["providers"].(map[string]any); ok {
		delete(p, "9router")
		pruneEmpty(cfg, "providers")
	}
	if _, err := writeTOML(path, cfg); err != nil {
		return Result{}, err
	}
	envPath := jcodeEnvPath(home)
	if envText, err := readFile(envPath); err == nil && envText != nil {
		lines := strings.Split(string(envText), "\n")
		kept := lines[:0]
		for _, l := range lines {
			if !strings.HasPrefix(strings.TrimSpace(l), jcodeKeyEnv+"=") {
				kept = append(kept, l)
			}
		}
		if _, err := writeFile(envPath, []byte(strings.Join(kept, "\n"))); err != nil {
			return Result{}, err
		}
	}
	return Result{Message: "9router provider removed from jcode", ConfigPath: path}, nil
}

func jcodeConfigured(home string) (bool, string) {
	path := jcodePath(home)
	cfg, _, err := readTOML(path)
	if err != nil {
		return false, path
	}
	p, _ := cfg["providers"].(map[string]any)
	return p["9router"] != nil, path
}
