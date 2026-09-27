// Package clisetup writes (and removes) the 9router gateway configuration in
// the local config files of supported CLI/IDE tools, porting the original
// 9router per-tool "<tool>-settings" installers.
//
// Every write keeps unrelated settings, backs up the pre-9router file once as
// "<file>.9router.bak", and refuses to touch a file it cannot parse rather
// than replacing the operator's config.
package clisetup

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"encoding/json/jsontext"
	json "encoding/json/v2"
)

// Options carries the gateway values written into a tool's config.
type Options struct {
	BaseURL string   // gateway origin without /v1, e.g. http://localhost:20130
	APIKey  string   // client key accepted by the gateway
	Models  []string // first entry is the active/default model
}

// Result describes what an apply/reset changed.
type Result struct {
	Message    string `json:"message"`
	ConfigPath string `json:"configPath"`
	BackupPath string `json:"backupPath,omitempty"`
}

// Tool is one supported installer.
type Tool struct {
	ID         string
	Name       string
	NeedsModel bool
	apply      func(home string, o Options) (Result, error)
	reset      func(home string) (Result, error)
	configured func(home string) (bool, string)
}

var tools = map[string]*Tool{}

func register(t *Tool) { tools[t.ID] = t }

// Lookup returns the installer for a dashboard tool id.
func Lookup(id string) (*Tool, bool) {
	t, ok := tools[id]
	return t, ok
}

// Status reports whether the tool's config already points at 9router.
func (t *Tool) Status() (configured bool, configPath string) {
	home, err := os.UserHomeDir()
	if err != nil {
		return false, ""
	}
	return t.configured(home)
}

// Apply validates options and writes the tool's config.
func (t *Tool) Apply(o Options) (Result, error) {
	u, err := url.Parse(strings.TrimSpace(o.BaseURL))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return Result{}, errors.New("baseUrl must be an http(s) URL")
	}
	o.BaseURL = strings.TrimSuffix(strings.TrimRight(u.String(), "/"), "/v1")
	if strings.TrimSpace(o.APIKey) == "" {
		return Result{}, errors.New("apiKey is required")
	}
	models := o.Models[:0:0]
	for _, m := range o.Models {
		if m = strings.TrimSpace(m); m == "" {
			continue
		}
		// Values are written into YAML/.env/TOML text; keep them to plain ids.
		if len(m) > 200 || strings.ContainsAny(m, "\"'\\`$\r\n\t") {
			return Result{}, fmt.Errorf("invalid model id %q", m)
		}
		models = append(models, m)
	}
	o.Models = models
	if strings.ContainsAny(o.APIKey, "\"'\\`$\r\n\t ") {
		return Result{}, errors.New("apiKey contains unsupported characters")
	}
	if t.NeedsModel && len(o.Models) == 0 {
		return Result{}, errors.New("a model is required for " + t.Name)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return Result{}, err
	}
	return t.apply(home, o)
}

// Reset removes only the 9router-managed settings.
func (t *Tool) Reset() (Result, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return Result{}, err
	}
	return t.reset(home)
}

func (o Options) v1() string { return o.BaseURL + "/v1" }

func (o Options) firstModel() string {
	if len(o.Models) == 0 {
		return ""
	}
	return o.Models[0]
}

// --- file helpers ---

var trailingComma = regexp.MustCompile(`,(\s*[}\]])`)

// readFile returns (nil, nil) when the file does not exist.
func readFile(path string) ([]byte, error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	return b, err
}

// readJSON decodes a JSON (or trailing-comma JSONC) file into v. A missing or
// empty file leaves v untouched; an unparseable file is an error.
func readJSON(path string, v any) (exists bool, err error) {
	b, err := readFile(path)
	if err != nil || b == nil {
		return false, err
	}
	if len(bytes.TrimSpace(b)) == 0 {
		return true, nil
	}
	if err := json.Unmarshal(trailingComma.ReplaceAll(b, []byte("$1")), v); err != nil {
		return true, fmt.Errorf("%s is not valid JSON, left unchanged: %w", path, err)
	}
	return true, nil
}

func writeJSON(path string, v any) (string, error) {
	b, err := json.Marshal(v, jsontext.WithIndent("  "), json.Deterministic(true))
	if err != nil {
		return "", err
	}
	return writeFile(path, append(b, '\n'))
}

// writeFile writes atomically, backing up the original once. New files are
// 0600 because they carry the gateway key; existing files keep their mode.
func writeFile(path string, data []byte) (backup string, err error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", err
	}
	mode := fs.FileMode(0o600)
	if fi, err := os.Stat(path); err == nil {
		mode = fi.Mode().Perm()
		bak := path + ".9router.bak"
		if _, err := os.Stat(bak); errors.Is(err, fs.ErrNotExist) {
			orig, err := os.ReadFile(path)
			if err != nil {
				return "", err
			}
			if err := os.WriteFile(bak, orig, 0o600); err != nil {
				return "", err
			}
		}
		backup = bak
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return "", err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return "", err
	}
	if err := tmp.Chmod(mode); err != nil {
		tmp.Close()
		return "", err
	}
	if err := tmp.Close(); err != nil {
		return "", err
	}
	return backup, os.Rename(tmp.Name(), path)
}

func objectAt(m map[string]any, key string) map[string]any {
	if v, ok := m[key].(map[string]any); ok {
		return v
	}
	v := map[string]any{}
	m[key] = v
	return v
}

func isLocalGatewayURL(s string) bool {
	return strings.Contains(s, "localhost") || strings.Contains(s, "127.0.0.1") || strings.Contains(s, "0.0.0.0")
}
