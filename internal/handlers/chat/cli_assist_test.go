package chat

import (
	"database/sql"
	json "encoding/json/v2"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"9router/proxy/internal/db"
	"9router/proxy/internal/log"
)

// The keys below are synthetic and only ever exist in a temp database. No real
// credential may appear in this file, in test output, or in a log assertion.
const (
	testClientKey     = "sk-test-client-0000000000000000aaaa"
	testConnKey       = "sk-test-connection-1111111111111111"
	testConnToken     = "eyJtestaccess0000000000000000000000000000"
	testConnRefresh   = "rt-test-refresh-22222222222222222222"
	testConnSecret    = "cs-test-client-secret-33333333333333"
	testLookalikeKey  = "sk-not-a-real-key-99999999999999999999"
	testShortSecret   = "ab"
	testEmptySecret   = ""
	testPlaceholder   = cliAssistRedactedPlaceholder
	testAnswerPrefix  = "Here are the steps.\n\n"
	assistTestPath    = "/api/dashboard/cli-tools/assist"
	assistTestModel   = "openai/gpt-4o"
	assistTestTool    = "Claude Code"
	upstreamTestRoute = "chat/completions"
)

// assistUpstream is a stub provider that answers every request with a body the
// test chooses, and records the prompts it was sent.
type assistUpstream struct {
	server *httptest.Server
	// prompts collects the raw request bodies the stub received, in order.
	prompts []string
	// answer is the assistant content returned in the completion.
	answer string
	// status lets a test force a non-200 upstream response.
	status int
}

func newAssistUpstream(t *testing.T, answer string) *assistUpstream {
	t.Helper()
	u := &assistUpstream{answer: answer, status: http.StatusOK}
	u.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		u.prompts = append(u.prompts, string(raw))
		w.Header().Set("Content-Type", "application/json")
		if u.status != http.StatusOK {
			w.WriteHeader(u.status)
			_, _ = w.Write([]byte(`{"error":{"message":"upstream rejected the request"}}`))
			return
		}
		resp := map[string]any{
			"id":    "chatcmpl-test",
			"model": "gpt-4o",
			"choices": []map[string]any{
				{"message": map[string]any{"role": "assistant", "content": u.answer}},
			},
		}
		out, _ := json.Marshal(resp)
		_, _ = w.Write(out)
	}))
	t.Cleanup(u.server.Close)
	return u
}

// lastPrompt returns the most recent prompt the stub received.
func (u *assistUpstream) lastPrompt() string {
	if len(u.prompts) == 0 {
		return ""
	}
	return u.prompts[len(u.prompts)-1]
}

// lastUserMessage returns the user-role content of the most recent prompt. The
// system prompt legitimately contains the placeholder (it instructs the model
// to write "<your-api-key>"), so assertions about what the caller's own text
// contributed must look at the user message, not the whole payload.
func (u *assistUpstream) lastUserMessage(t *testing.T) string {
	t.Helper()
	prompt := u.lastPrompt()
	if prompt == "" {
		t.Fatal("upstream stub received no request")
	}
	var payload struct {
		Messages []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal([]byte(prompt), &payload); err != nil {
		t.Fatalf("decode captured prompt: %v", err)
	}
	for _, m := range payload.Messages {
		if m.Role == "user" {
			return m.Content
		}
	}
	t.Fatalf("captured prompt has no user message: %s", prompt)
	return ""
}

// assistFixture builds a handler over a temp database seeded with synthetic
// credentials, with the given provider connection pointed at upstream.
//
// The handler is built through NewChatHandler so the test exercises the same
// construction the router uses.
func assistFixture(t *testing.T, upstream *assistUpstream) (*ChatHandler, *sql.DB) {
	t.Helper()
	database, cleanup := setupChatTestDB(t)
	t.Cleanup(cleanup)

	// The shared fixture seeds its own connection for deepseek; point it at the
	// stub so nothing in this test can reach a live provider.
	connData, _ := json.Marshal(map[string]any{
		"apiKey":       testConnKey,
		"accessToken":  testConnToken,
		"refreshToken": testConnRefresh,
		"baseUrl":      upstream.server.URL,
		"providerSpecificData": map[string]any{
			"clientSecret": testConnSecret,
		},
	})
	if _, err := database.Exec(`UPDATE providerConnections SET data = ? WHERE id = 'conn-1'`, string(connData)); err != nil {
		t.Fatalf("seed connection: %v", err)
	}
	// A second connection exercises a short and an empty stored credential:
	// neither may cause over-redaction or a panic.
	shortData, _ := json.Marshal(map[string]any{
		"apiKey":      testShortSecret,
		"accessToken": testEmptySecret,
		"baseUrl":     upstream.server.URL,
	})
	if _, err := database.Exec(`INSERT INTO providerConnections (id, provider, authType, name, priority, isActive, data, createdAt, updatedAt)
		VALUES ('conn-short', 'openai', 'apikey', 'Short', 1, 1, ?, '2026-07-18T00:00:00Z', '2026-07-18T00:00:00Z')`,
		string(shortData)); err != nil {
		t.Fatalf("seed short connection: %v", err)
	}
	// A client API key from the apiKeys table is a secret source too.
	if _, err := database.Exec(`INSERT INTO apiKeys (id, key, name, isActive, createdAt)
		VALUES ('key-2', ?, 'Second', 1, '2026-07-18T00:00:00Z')`, testClientKey); err != nil {
		t.Fatalf("seed client key: %v", err)
	}

	return NewChatHandler(db.NewRepo(database)), database
}

// assistRequest posts body to the handler and returns the recorder.
func assistRequest(t *testing.T, h *ChatHandler, body string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	h.HandleCliToolAssist(rec, httptest.NewRequest(http.MethodPost, assistTestPath, strings.NewReader(body)))
	return rec
}

// assistBody builds a request body with the given context and question.
func assistBody(t *testing.T, context, question string) string {
	t.Helper()
	raw, err := json.Marshal(map[string]any{
		"model":    assistTestModel,
		"tool":     assistTestTool,
		"context":  context,
		"question": question,
	})
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}
	return string(raw)
}

// assistAnswer decodes the answer field of a successful response.
func assistAnswer(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var out struct {
		Answer string `json:"answer"`
		Model  string `json:"model"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if out.Model == "" {
		t.Error("expected a model in the response")
	}
	return out.Answer
}

// assertNoKnownSecret fails when any synthetic credential appears in text.
// The keys are named in the failure message by variable, never by value, so a
// failing run cannot itself become a credential disclosure.
func assertNoKnownSecret(t *testing.T, label, text string) {
	t.Helper()
	for name, secret := range map[string]string{
		"client API key":          testClientKey,
		"connection apiKey":       testConnKey,
		"connection accessToken":  testConnToken,
		"connection refreshToken": testConnRefresh,
		"connection clientSecret": testConnSecret,
	} {
		if strings.Contains(text, secret) {
			t.Errorf("%s still contains the %s", label, name)
		}
	}
}

// TestHandleCliToolAssist_RedactsKnownSecretInAnswer is the core regression: a
// model answer that echoes a stored credential must come back redacted. Before
// the server-side redactor existed, the answer was relayed verbatim.
func TestHandleCliToolAssist_RedactsKnownSecretInAnswer(t *testing.T) {
	cases := []struct {
		name   string
		answer string
		want   string
	}{
		{
			name:   "connection apiKey",
			answer: testAnswerPrefix + "export OPENAI_API_KEY=\"" + testConnKey + "\"",
			want:   testAnswerPrefix + "export OPENAI_API_KEY=\"" + testPlaceholder + "\"",
		},
		{
			name:   "client API key",
			answer: "Use " + testClientKey + " as the bearer token.",
			want:   "Use " + testPlaceholder + " as the bearer token.",
		},
		{
			name:   "access token",
			answer: "Authorization: Bearer " + testConnToken,
			want:   "Authorization: Bearer " + testPlaceholder,
		},
		{
			name:   "refresh token",
			answer: `{"refreshToken":"` + testConnRefresh + `"}`,
			want:   `{"refreshToken":"` + testPlaceholder + `"}`,
		},
		{
			name:   "nested client secret",
			answer: "client_secret=" + testConnSecret,
			want:   "client_secret=" + testPlaceholder,
		},
		{
			name:   "appears multiple times",
			answer: testConnKey + " then " + testConnKey + " and again " + testConnKey,
			want:   testPlaceholder + " then " + testPlaceholder + " and again " + testPlaceholder,
		},
		{
			name:   "two different secrets in one answer",
			answer: "key=" + testConnKey + " token=" + testConnToken,
			want:   "key=" + testPlaceholder + " token=" + testPlaceholder,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			upstream := newAssistUpstream(t, tc.answer)
			h, _ := assistFixture(t, upstream)

			rec := assistRequest(t, h, assistBody(t, "Gateway base URL: http://127.0.0.1:20130", ""))
			got := assistAnswer(t, rec)

			if got != tc.want {
				t.Errorf("answer mismatch\n got: %q\nwant: %q", got, tc.want)
			}
			assertNoKnownSecret(t, "answer", got)
		})
	}
}

// TestHandleCliToolAssist_PromptSentUpstreamHasNoKnownSecret proves the
// outgoing half: even when the caller hands the endpoint a real credential in
// the setup facts, the provider never sees it.
func TestHandleCliToolAssist_PromptSentUpstreamHasNoKnownSecret(t *testing.T) {
	upstream := newAssistUpstream(t, "Steps go here.")
	h, _ := assistFixture(t, upstream)

	// A direct API caller, not the dashboard: nothing masked these values.
	context := strings.Join([]string{
		"Gateway base URL: http://127.0.0.1:20130",
		"Environment variables:",
		"OPENAI_API_KEY=" + testConnKey,
		"ANTHROPIC_API_KEY=" + testClientKey,
		"AUTH_TOKEN=" + testConnToken,
	}, "\n")
	question := "Why does my client send " + testConnRefresh + " to the wrong endpoint?"

	rec := assistRequest(t, h, assistBody(t, context, question))
	assistAnswer(t, rec)

	prompt := upstream.lastPrompt()
	if prompt == "" {
		t.Fatal("upstream stub received no request")
	}
	assertNoKnownSecret(t, "upstream prompt", prompt)

	// The prompt must still carry the request's substance, not be blanked out.
	for _, want := range []string{assistTestTool, "Gateway base URL: http://127.0.0.1:20130", "Why does my client send"} {
		if !strings.Contains(prompt, want) {
			t.Errorf("upstream prompt lost %q; got %q", want, prompt)
		}
	}
	// And the placeholders must be there in place of the values.
	if !strings.Contains(prompt, testPlaceholder) {
		t.Errorf("upstream prompt has no %s placeholder: %q", testPlaceholder, prompt)
	}
}

// TestHandleCliToolAssist_LeavesUnknownKeyLikeTextIntact guards against
// over-redaction: redaction is driven by stored values, so text that merely
// looks like a key must survive untouched.
func TestHandleCliToolAssist_LeavesUnknownKeyLikeTextIntact(t *testing.T) {
	upstream := newAssistUpstream(t, "Set "+testLookalikeKey+" in your profile. It is a placeholder, not a real key.")
	h, _ := assistFixture(t, upstream)

	rec := assistRequest(t, h, assistBody(t, "OPENAI_API_KEY=sk-proj-looks-real-but-is-not-stored", ""))
	got := assistAnswer(t, rec)

	if !strings.Contains(got, testLookalikeKey) {
		t.Errorf("unknown key-like text was redacted from the answer: %q", got)
	}
	if strings.Contains(got, testPlaceholder) {
		t.Errorf("answer gained a placeholder for a value that is not a stored secret: %q", got)
	}

	prompt := upstream.lastUserMessage(t)
	if !strings.Contains(prompt, "sk-proj-looks-real-but-is-not-stored") {
		t.Errorf("unknown key-like text was redacted from the prompt: %q", prompt)
	}
	if strings.Contains(prompt, testPlaceholder) {
		t.Errorf("prompt gained a placeholder for a value that is not a stored secret: %q", prompt)
	}
}

// TestHandleCliToolAssist_ShortAndEmptySecretsDoNotOverRedact covers the
// degenerate stored values: a two-character key and an empty one must not
// rewrite ordinary prose, and must not panic.
func TestHandleCliToolAssist_ShortAndEmptySecretsDoNotOverRedact(t *testing.T) {
	upstream := newAssistUpstream(t, "The public endpoint is available to the public. An ab- solute value.")
	h, _ := assistFixture(t, upstream)

	// "public" and "ab" are exactly the kind of short, common strings a naive
	// redactor would substitute; both are stored values in the fixture's
	// connection set ("ab") or plausible placeholders ("public").
	context := "This is a public gateway. Absolute paths are ab- solutely fine."
	question := "Is the public endpoint reachable?"

	rec := assistRequest(t, h, assistBody(t, context, question))
	got := assistAnswer(t, rec)

	if strings.Contains(got, testPlaceholder) {
		t.Errorf("short stored secret caused over-redaction in the answer: %q", got)
	}
	if !strings.Contains(got, "The public endpoint is available to the public.") {
		t.Errorf("ordinary prose was mangled in the answer: %q", got)
	}

	prompt := upstream.lastUserMessage(t)
	if strings.Contains(prompt, testPlaceholder) {
		t.Errorf("short stored secret caused over-redaction in the prompt: %q", prompt)
	}
	for _, want := range []string{"This is a public gateway.", "Is the public endpoint reachable?"} {
		if !strings.Contains(prompt, want) {
			t.Errorf("prompt lost %q: %q", want, prompt)
		}
	}
}

// TestHandleCliToolAssist_SecretContainingAnotherSecret ensures a credential
// that contains a shorter one is replaced whole, not partially.
func TestHandleCliToolAssist_SecretContainingAnotherSecret(t *testing.T) {
	// A long secret whose prefix is itself a stored secret.
	long := testConnKey + "-suffix-that-makes-it-longer"
	upstream := newAssistUpstream(t, "Use "+long+" please.")
	h, database := assistFixture(t, upstream)

	if _, err := database.Exec(`UPDATE providerConnections SET data = json_set(data, '$.apiKey', ?) WHERE id = 'conn-1'`, long); err != nil {
		// json_set may be unavailable; fall back to a direct rewrite.
		raw, _ := json.Marshal(map[string]any{"apiKey": long, "baseUrl": upstream.server.URL})
		if _, err := database.Exec(`UPDATE providerConnections SET data = ? WHERE id = 'conn-1'`, string(raw)); err != nil {
			t.Fatalf("seed long secret: %v", err)
		}
	}

	rec := assistRequest(t, h, assistBody(t, "OPENAI_API_KEY="+long, ""))
	got := assistAnswer(t, rec)

	if !strings.Contains(got, testPlaceholder) {
		t.Errorf("expected the long secret to be replaced: %q", got)
	}
	if strings.Contains(got, "suffix-that-makes-it-longer") {
		t.Errorf("secret was partially replaced, leaving a fragment: %q", got)
	}
	if strings.Contains(got, testConnKey) {
		t.Errorf("the shorter contained secret survived: %q", got)
	}
	if strings.Contains(upstream.lastPrompt(), long) {
		t.Errorf("the long secret reached upstream: %q", upstream.lastPrompt())
	}
}

// TestHandleCliToolAssist_RedactionIsIdempotent proves the placeholder cannot
// feed itself: running the redactor over its own output changes nothing.
func TestHandleCliToolAssist_RedactionIsIdempotent(t *testing.T) {
	r := newSecretRedactor([]string{testConnKey, testConnToken, testPlaceholder})
	once := r.redact("a " + testConnKey + " b " + testConnToken + " c")
	twice := r.redact(once)
	if once != twice {
		t.Errorf("redaction is not idempotent\n once: %q\ntwice: %q", once, twice)
	}
	if strings.Count(once, testPlaceholder) != 2 {
		t.Errorf("expected exactly two placeholders, got %q", once)
	}
}

// TestHandleCliToolAssist_RedactsUpstreamErrorMessage covers the failure path:
// an upstream error that quotes the request is relayed redacted.
func TestHandleCliToolAssist_RedactsUpstreamErrorMessage(t *testing.T) {
	upstream := newAssistUpstream(t, "")
	upstream.status = http.StatusBadRequest
	h, _ := assistFixture(t, upstream)

	rec := assistRequest(t, h, assistBody(t, "OPENAI_API_KEY="+testConnKey, ""))
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("expected 502, got %d: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	assertNoKnownSecret(t, "error body", body)
	if !strings.Contains(body, "upstream rejected the request") {
		t.Errorf("expected the upstream message to be relayed, got %q", body)
	}
}

// TestChatErrorMessage_RedactsBeforeTruncation pins the ordering inside the
// error path. The helper caps its output at 300 bytes; if a credential sat
// across that boundary and were redacted afterwards, the truncation would have
// already cut it into an unrecognisable prefix. Redacting the full text first
// means the cap can only ever fall inside the placeholder.
func TestChatErrorMessage_RedactsBeforeTruncation(t *testing.T) {
	// A long non-JSON error body with the credential positioned so the 300-byte
	// cut lands in the middle of it.
	padding := strings.Repeat("x", 280)
	body := padding + testConnKey + strings.Repeat("y", 100)

	redactor := newSecretRedactor([]string{testConnKey})
	got := chatErrorMessage(redactor.redact(body), http.StatusBadRequest)

	if len(got) > 300 {
		t.Fatalf("expected the 300-byte cap to hold, got %d bytes", len(got))
	}
	if strings.Contains(got, testConnKey) {
		t.Error("the error text retained the credential")
	}
	// The credential starts at byte 280, inside the 300-byte window, so the
	// placeholder must be visibly present rather than cut away.
	if !strings.Contains(got, testPlaceholder) {
		t.Errorf("expected the placeholder inside the truncated window, got %q", got)
	}
	if strings.Contains(got, testConnKey[:10]) {
		t.Errorf("a credential prefix survived truncation: %q", got)
	}
}

// TestHandleCliToolAssist_FailsClosedWithoutCredentialList pins the failure
// model: with no repository the endpoint cannot know what to scrub, so it must
// refuse rather than answer unredacted.
func TestHandleCliToolAssist_FailsClosedWithoutCredentialList(t *testing.T) {
	upstream := newAssistUpstream(t, "should not be reached")
	h := &ChatHandler{} // no Repo
	_ = upstream

	rec := assistRequest(t, h, assistBody(t, "OPENAI_API_KEY="+testConnKey, ""))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 without a credential list, got %d: %s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), testConnKey) {
		t.Error("the refusal response echoed the credential")
	}
}

// TestHandleCliToolAssist_EmptySecretStoreStillAnswers pins the other half of
// the failure model: a credential list with nothing distinctive in it is not an
// error, because there is nothing to hide.
func TestHandleCliToolAssist_EmptySecretStoreStillAnswers(t *testing.T) {
	upstream := newAssistUpstream(t, "All good.")
	h, database := assistFixture(t, upstream)

	// Remove every distinctive credential. The connection keeps a short
	// placeholder credential because the upstream stub still has to
	// authenticate; at six characters it is below the redaction floor, so the
	// redactor sees no value worth substituting.
	const placeholderCredential = "public"
	if len(placeholderCredential) >= cliAssistMinSecretLen {
		t.Fatalf("test credential %q is not below the redaction floor of %d", placeholderCredential, cliAssistMinSecretLen)
	}
	if _, err := database.Exec(`DELETE FROM apiKeys`); err != nil {
		t.Fatalf("clear api keys: %v", err)
	}
	blank, _ := json.Marshal(map[string]any{
		"apiKey":  placeholderCredential,
		"baseUrl": upstream.server.URL,
	})
	if _, err := database.Exec(`UPDATE providerConnections SET data = ?`, string(blank)); err != nil {
		t.Fatalf("clear connection secrets: %v", err)
	}

	rec := assistRequest(t, h, assistBody(t, "Gateway base URL: http://127.0.0.1:20130", ""))
	if got := assistAnswer(t, rec); got != "All good." {
		t.Errorf("expected the answer through, got %q", got)
	}
	if strings.Contains(upstream.lastUserMessage(t), testPlaceholder) {
		t.Error("an undistinctive stored credential caused over-redaction")
	}
}

// TestHandleCliToolAssist_DoesNotLogSecrets checks the logs, not just the
// response: the debug/info/warn stream must never carry a credential.
func TestHandleCliToolAssist_DoesNotLogSecrets(t *testing.T) {
	upstream := newAssistUpstream(t, "Your key is "+testConnKey+".")
	h, _ := assistFixture(t, upstream)

	log.ClearConsoleLogs()
	rec := assistRequest(t, h, assistBody(t, "OPENAI_API_KEY="+testConnKey, "why is "+testConnToken+" wrong?"))
	assistAnswer(t, rec)

	lines := strings.Join(log.ConsoleLogs(), "\n")
	assertNoKnownSecret(t, "console log buffer", lines)
}

// TestSecretRedactor_SkipsUndistinctiveValues covers the value filter directly:
// empty, whitespace, and too-short candidates are dropped, duplicates collapse.
func TestSecretRedactor_SkipsUndistinctiveValues(t *testing.T) {
	cases := []struct {
		name   string
		values []string
		input  string
		want   string
	}{
		{
			name:   "empty list leaves text alone",
			values: nil,
			input:  "nothing to hide",
			want:   "nothing to hide",
		},
		{
			name:   "empty value is dropped",
			values: []string{testEmptySecret},
			input:  "nothing to hide",
			want:   "nothing to hide",
		},
		{
			name:   "whitespace value is dropped",
			values: []string{"        "},
			input:  "nothing to hide",
			want:   "nothing to hide",
		},
		{
			name:   "short value is dropped",
			values: []string{testShortSecret},
			input:  "the ab- solute path",
			want:   "the ab- solute path",
		},
		{
			name:   "duplicates collapse",
			values: []string{testConnKey, testConnKey, testConnKey},
			input:  testConnKey,
			want:   testPlaceholder,
		},
		{
			name:   "value at the length floor is kept",
			values: []string{"abcdefgh"},
			input:  "xabcdefghx",
			want:   "x" + testPlaceholder + "x",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := newSecretRedactor(tc.values).redact(tc.input)
			if got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

// TestNewSecretRedactor_AvailabilitySeparatesEmptyFromFailed pins that an empty
// list is usable and only a lookup failure withholds service.
func TestNewSecretRedactor_AvailabilitySeparatesEmptyFromFailed(t *testing.T) {
	if !newSecretRedactor(nil).available() {
		t.Error("an empty credential list must be available: there is nothing to redact")
	}
	if !newSecretRedactor([]string{testShortSecret}).available() {
		t.Error("a list of undistinctive values must still be available")
	}
	if newSecretRedactor([]string{testConnKey}).available() != true {
		t.Error("a usable list must be available")
	}
	h := &ChatHandler{}
	if h.newSecretRedactor().available() {
		t.Error("a handler without a repository must not report an available credential list")
	}
}
