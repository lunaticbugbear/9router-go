package db

import (
	"errors"
	"fmt"
	"strings"

	json "encoding/json/v2"
)

// secretFieldNames are the credential-shaped connection-data keys that carry no
// cue in their spelling. They are the names the OAuth and import writers
// actually persist (see internal/handlers/oauth and internal/proxy/oauth):
// apiKey/accessToken on every connection, refreshToken/idToken/authToken on the
// OAuth ones, clientSecret in providerSpecificData, cookie on the cookie-based
// imports.
var secretFieldNames = map[string]bool{
	"apikey":          true,
	"api_key":         true,
	"accesstoken":     true,
	"access_token":    true,
	"refreshtoken":    true,
	"refresh_token":   true,
	"authtoken":       true,
	"auth_token":      true,
	"idtoken":         true,
	"id_token":        true,
	"copilottoken":    true,
	"firebaseidtoken": true,
	"clientsecret":    true,
	"client_secret":   true,
	"codeverifier":    true,
	"code_verifier":   true,
	"privatekey":      true,
	"private_key":     true,
	"cookie":          true,
	"password":        true,
	"passphrase":      true,
	"authorization":   true,
	"secret":          true,
}

// secretFieldSuffixes catch credential fields this package does not know by
// name, so a newly added provider cannot silently store an unredactable token.
// The match is a suffix rather than a substring on purpose: "max_tokens",
// "inputTokens" and "tokenEndpoint" are ordinary fields, while "accessToken",
// "copilotToken" and "clientSecret" are not.
var secretFieldSuffixes = []string{"token", "secret", "apikey", "password", "_key"}

// secretScanMaxDepth bounds the JSON walk over one connection blob. Real blobs
// nest two levels (data -> providerSpecificData -> field); anything deeper is
// malformed or hostile and is left alone rather than walked without limit.
const secretScanMaxDepth = 8

// SecretValues returns every plaintext credential the gateway stores: each
// client API key from apiKeys, and every credential field in each
// providerConnections.data blob (including the ones nested under
// providerSpecificData).
//
// Callers use it to scrub a credential it already knows out of text before that
// text is handed to a third party or returned to a client. It returns values
// rather than a pattern deliberately: guessing "anything that looks like a key"
// both misses real keys and mangles innocent prose, while an exact value list
// can only ever match text that genuinely contains the credential.
//
// Only credential columns are read — no names, emails, or priorities — and the
// connection blob is parsed for its secret fields alone, so a caller cannot
// accidentally log more of a row than it asked for. Values are returned
// unredacted and in no particular order; deciding what is too short to redact,
// and keeping the values out of logs, is the caller's job.
//
// A table that does not exist yet is treated as holding no secrets, not as an
// error: a fresh DATA_DIR has no apiKeys or providerConnections table until the
// dashboard creates them, and "there are no credentials" is a safe answer for a
// redactor. Any other read failure is reported, so a caller that must not leak
// can refuse to proceed rather than trust a partial list.
func (r *Repo) SecretValues() ([]string, error) {
	if r == nil || r.db == nil {
		return nil, fmt.Errorf("secret values: repository is not available")
	}

	var (
		out  []string
		errs []error
	)

	keys, err := r.clientAPIKeyValues()
	if err != nil {
		errs = append(errs, err)
	}
	out = append(out, keys...)

	conns, err := r.connectionSecretValues()
	if err != nil {
		errs = append(errs, err)
	}
	out = append(out, conns...)

	if len(errs) > 0 {
		return out, errors.Join(errs...)
	}
	return out, nil
}

// clientAPIKeyValues reads the key column of apiKeys. The key column alone is
// selected: the row also carries a name and a machine id, and neither belongs in
// a list that exists to be matched against arbitrary text.
func (r *Repo) clientAPIKeyValues() ([]string, error) {
	exists, err := r.tableExists("apiKeys")
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, nil
	}

	rows, err := r.db.Query(`SELECT key FROM apiKeys`)
	if err != nil {
		return nil, fmt.Errorf("read client API keys: %w", err)
	}
	defer rows.Close()

	var out []string
	for rows.Next() {
		var key string
		if err := rows.Scan(&key); err != nil {
			return out, fmt.Errorf("scan client API key: %w", err)
		}
		out = append(out, key)
	}
	if err := rows.Err(); err != nil {
		return out, fmt.Errorf("iterate client API keys: %w", err)
	}
	return out, nil
}

// connectionSecretValues reads the data column of providerConnections and
// extracts the credential fields from each blob.
func (r *Repo) connectionSecretValues() ([]string, error) {
	exists, err := r.tableExists("providerConnections")
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, nil
	}

	rows, err := r.db.Query(`SELECT data FROM providerConnections`)
	if err != nil {
		return nil, fmt.Errorf("read provider connections: %w", err)
	}
	defer rows.Close()

	var out []string
	for rows.Next() {
		var data string
		if err := rows.Scan(&data); err != nil {
			return out, fmt.Errorf("scan provider connection data: %w", err)
		}
		out = collectConnectionSecrets(data, out)
	}
	if err := rows.Err(); err != nil {
		return out, fmt.Errorf("iterate provider connections: %w", err)
	}
	return out, nil
}

// tableExists reports whether name is a table in the open database. It lets the
// credential readers tell "not created yet" (no secrets, nothing to hide) apart
// from a genuine read failure, which the caller must not paper over.
func (r *Repo) tableExists(name string) (bool, error) {
	var count int
	err := r.db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = ?`, name).Scan(&count)
	if err != nil {
		return false, fmt.Errorf("inspect table %s: %w", name, err)
	}
	return count > 0, nil
}

// collectConnectionSecrets appends the credentials found in one
// providerConnections.data blob. A blob that does not parse yields nothing: it
// is not a source of credentials, and guessing at its bytes would risk treating
// arbitrary text as a secret.
func collectConnectionSecrets(raw string, out []string) []string {
	if strings.TrimSpace(raw) == "" {
		return out
	}
	var data map[string]any
	if err := json.Unmarshal([]byte(raw), &data); err != nil {
		return out
	}
	return collectSecretValues(data, out, 0)
}

// collectSecretValues walks a decoded connection blob, appending every string
// held under a secret-shaped key. A secret-shaped key whose value is a nested
// object is still walked, because the credential is often one level below the
// key that names it (providerSpecificData -> accessToken).
func collectSecretValues(node any, out []string, depth int) []string {
	if depth > secretScanMaxDepth {
		return out
	}
	switch v := node.(type) {
	case map[string]any:
		for key, value := range v {
			if isSecretFieldName(key) {
				if s, ok := value.(string); ok {
					out = append(out, s)
					continue
				}
			}
			out = collectSecretValues(value, out, depth+1)
		}
	case []any:
		for _, item := range v {
			out = collectSecretValues(item, out, depth+1)
		}
	}
	return out
}

// isSecretFieldName reports whether a connection-data field name denotes a
// credential.
func isSecretFieldName(name string) bool {
	key := strings.ToLower(strings.TrimSpace(name))
	if secretFieldNames[key] {
		return true
	}
	for _, suffix := range secretFieldSuffixes {
		if strings.HasSuffix(key, suffix) {
			return true
		}
	}
	return false
}
