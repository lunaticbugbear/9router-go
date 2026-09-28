package chat

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"

	json "encoding/json/v2"

	"9router/proxy/internal/handlerutil"
	"9router/proxy/internal/log"
)

const (
	cliAssistMaxBody      = 32 << 10
	cliAssistMaxContext   = 8000
	cliAssistMaxQuestion  = 2000
	cliAssistSystemPrompt = "You help an operator configure a coding tool on their own machine to use the local 9router-go gateway " +
		"(an OpenAI/Anthropic-compatible proxy). Answer with short numbered steps and copy-pasteable shell commands or config " +
		"snippets. Use the base URL and values given in the setup facts. Write <your-api-key> wherever a key is needed and never " +
		"invent a real key. If you are unsure a flag, file path, or setting exists for this tool, say so instead of guessing."

	// cliAssistRedactedPlaceholder is the literal the dashboard UI, the CLI
	// Tools copy and CHANGELOG already promise in place of a credential.
	cliAssistRedactedPlaceholder = "<your-api-key>"

	// cliAssistMinSecretLen is the shortest stored value this handler will
	// scrub. Below it the value is not distinctive enough to replace safely:
	// stored placeholder credentials like "public" are shorter than this, and
	// substituting them would shred ordinary prose ("the public endpoint") in
	// exchange for hiding nothing. Real keys on this gateway are 32+ characters,
	// so nothing that matters falls under the floor.
	cliAssistMinSecretLen = 8
)

// HandleCliToolAssist handles POST /api/dashboard/cli-tools/assist: asks a
// configured model for setup steps for one CLI/IDE tool. Nothing is executed
// or written on the host; the answer is returned for the operator to review.
func (h *ChatHandler) HandleCliToolAssist(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, cliAssistMaxBody+1))
	if err != nil || len(body) > cliAssistMaxBody {
		handlerutil.WriteJSON(w, http.StatusBadRequest, map[string]any{"error": "request body too large or unreadable"})
		return
	}
	defer r.Body.Close()

	var req struct {
		Model    string `json:"model"`
		Tool     string `json:"tool"`
		Context  string `json:"context"`
		Question string `json:"question"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		handlerutil.WriteJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid JSON"})
		return
	}
	req.Model = strings.TrimSpace(req.Model)
	req.Tool = strings.TrimSpace(req.Tool)
	req.Question = strings.TrimSpace(req.Question)
	switch {
	case req.Model == "":
		handlerutil.WriteJSON(w, http.StatusBadRequest, map[string]any{"error": "model is required"})
		return
	case req.Tool == "" || len(req.Tool) > 100:
		handlerutil.WriteJSON(w, http.StatusBadRequest, map[string]any{"error": "tool is required (max 100 bytes)"})
		return
	case len(req.Context) > cliAssistMaxContext:
		handlerutil.WriteJSON(w, http.StatusBadRequest, map[string]any{"error": "context too long"})
		return
	case len(req.Question) > cliAssistMaxQuestion:
		handlerutil.WriteJSON(w, http.StatusBadRequest, map[string]any{"error": "question too long"})
		return
	}
	if req.Question == "" {
		req.Question = "Walk me through setting this tool up to use the gateway, then how to verify it works."
	}

	// Defense in depth, outgoing half: the tool name, setup facts and operator
	// question are attacker-controlled text that may quote a real credential, so
	// the assembled prompt is scrubbed before it can leave the gateway toward a
	// provider. Scrubbing the finished string rather than the individual fields
	// is deliberate — a credential split across a field boundary, or carried by
	// a field added later, is still caught.
	//
	// The dashboard masks key-shaped environment variables before it sends them,
	// but that is a client-side convenience any direct API caller skips. This
	// list is built from what the gateway actually stores, so it holds no matter
	// how the request arrived.
	redactor := h.newSecretRedactor()
	if !redactor.available() {
		// Redaction is the promise this endpoint makes. Without the credential
		// list it cannot be kept, so refuse rather than forward unscrubbed text.
		log.Warn("cli-assist", "credential list unavailable; refusing to build an unredacted prompt")
		handlerutil.WriteJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "credential redaction unavailable"})
		return
	}

	user := redactor.redact("Tool: " + req.Tool + "\n\nSetup facts:\n" + strings.TrimSpace(req.Context) +
		"\n\nOperator request: " + req.Question)
	payload, _ := json.Marshal(map[string]any{
		"model": req.Model,
		"messages": []map[string]string{
			{"role": "system", "content": cliAssistSystemPrompt},
			{"role": "user", "content": user},
		},
		"max_tokens": 1500,
		"stream":     false,
	})

	upstreamReq := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(payload))
	upstreamReq = upstreamReq.WithContext(r.Context())
	upstreamReq.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.HandleChatCompletions(rec, upstreamReq)

	if rec.Code != http.StatusOK {
		// The upstream's error text can quote the request it rejected, so it is
		// scrubbed with the same list before it is relayed — and before the
		// helper truncates it, so a credential cannot survive as a prefix when
		// the cut lands mid-key.
		handlerutil.WriteJSON(w, http.StatusBadGateway, map[string]any{
			"error": chatErrorMessage(redactor.redact(rec.Body.String()), rec.Code),
		})
		return
	}
	var out struct {
		Model   string `json:"model"`
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil || len(out.Choices) == 0 ||
		strings.TrimSpace(out.Choices[0].Message.Content) == "" {
		handlerutil.WriteJSON(w, http.StatusBadGateway, map[string]any{"error": "model returned an empty answer"})
		return
	}
	model := out.Model
	if model == "" {
		model = req.Model
	}
	// Defense in depth, returning half: a model can echo a credential back —
	// from the setup facts it was shown, from its own training, or from an
	// upstream that injected it — so the answer is scrubbed on the way out
	// rather than trusted because the prompt was already clean.
	handlerutil.WriteJSON(w, http.StatusOK, map[string]any{
		"answer": redactor.redact(strings.TrimSpace(out.Choices[0].Message.Content)),
		"model":  model,
	})
}

// secretRedactor replaces known credential values with
// cliAssistRedactedPlaceholder. The zero value redacts nothing but reports
// itself available, which is the correct reading of "no credentials were
// found": there is nothing to scrub, and the endpoint can still answer. Build
// one with newSecretRedactor.
type secretRedactor struct {
	replacer *strings.Replacer
	// err is non-nil only when the credential list could not be obtained at
	// all. That is a different situation from an empty list and must fail
	// closed: the caller cannot tell whether a credential is present, so it
	// cannot promise the text is clean.
	err error
}

// newSecretRedactor builds a redactor over every credential the gateway stores.
// A repository that cannot be read yields an unavailable redactor rather than
// one that silently redacts nothing.
func (h *ChatHandler) newSecretRedactor() secretRedactor {
	if h == nil || h.Repo == nil {
		return secretRedactor{err: errNoSecretSource}
	}
	values, err := h.Repo.SecretValues()
	if err != nil {
		// Log the failure, never the values: the error names the query, not the
		// credentials it was reading.
		log.Warn("cli-assist", "could not enumerate stored credentials", "error", err)
		return secretRedactor{err: err}
	}
	return newSecretRedactor(values)
}

// errNoSecretSource marks a handler with no repository to enumerate secrets
// from. It is a sentinel rather than a message so callers can tell "no database"
// apart from "the database could not be read".
var errNoSecretSource = errors.New("no repository configured for credential redaction")

// newSecretRedactor builds a redactor from a set of candidate credential
// values. Values that are empty, whitespace-only, or shorter than
// cliAssistMinSecretLen are dropped: they are not distinctive enough to
// substitute safely, and a two-character "secret" would rewrite unrelated text.
// Duplicates collapse, so the same value stored on several connections is
// replaced by one rule.
func newSecretRedactor(values []string) secretRedactor {
	seen := make(map[string]bool, len(values))
	uniq := make([]string, 0, len(values))
	for _, v := range values {
		if len(v) < cliAssistMinSecretLen || strings.TrimSpace(v) == "" {
			continue
		}
		if seen[v] {
			continue
		}
		seen[v] = true
		uniq = append(uniq, v)
	}
	if len(uniq) == 0 {
		// No credential is long enough to be worth matching. Nothing to do,
		// and nothing withheld from the caller.
		return secretRedactor{}
	}
	// Longest first. strings.Replacer already prefers the longest match at each
	// position, but sorting keeps that guarantee independent of the builder's
	// internals: a credential that contains another credential must not be
	// half-replaced into a string that no longer matches anything.
	sort.Slice(uniq, func(i, j int) bool {
		if len(uniq[i]) != len(uniq[j]) {
			return len(uniq[i]) > len(uniq[j])
		}
		return uniq[i] < uniq[j]
	})
	pairs := make([]string, 0, len(uniq)*2)
	for _, v := range uniq {
		pairs = append(pairs, v, cliAssistRedactedPlaceholder)
	}
	return secretRedactor{replacer: strings.NewReplacer(pairs...)}
}

// available reports whether the credential list was obtained. An empty list is
// available: there is nothing to redact, but the endpoint knows that for
// certain and may proceed. Only a failed lookup is unavailable.
func (r secretRedactor) available() bool { return r.err == nil }

// redact replaces every occurrence of every known credential with the
// placeholder and returns the result. It is safe on a zero-value redactor
// (returns the input unchanged) and is idempotent: the placeholder contains no
// credential, so re-running it cannot consume its own output.
func (r secretRedactor) redact(s string) string {
	if r.replacer == nil || s == "" {
		return s
	}
	return r.replacer.Replace(s)
}

// chatErrorMessage extracts the human-readable message from an upstream error
// body. It takes the body as a string rather than the recorder so the caller
// can scrub credentials out of it first: the 300-byte cap below would otherwise
// be able to cut a credential in half, leaving a prefix behind that no later
// redaction pass could recognise.
func chatErrorMessage(raw string, status int) string {
	raw = strings.TrimSpace(raw)
	var obj struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
		Message string `json:"message"`
	}
	if json.Unmarshal([]byte(raw), &obj) == nil {
		if obj.Error.Message != "" {
			return obj.Error.Message
		}
		if obj.Message != "" {
			return obj.Message
		}
	}
	if raw == "" {
		return http.StatusText(status)
	}
	if len(raw) > 300 {
		raw = raw[:300]
	}
	return raw
}
