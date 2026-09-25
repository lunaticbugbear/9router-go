package tokensaver

import (
	json "encoding/json/v2"
	"strings"
	"testing"
)

// Replacement injectors must overwrite the system instructions rather than
// append, and must stay idempotent so re-applying the same persona is a no-op.
func TestReplaceChatSystem(t *testing.T) {
	const prompt = "persona block"

	tests := []struct {
		name        string
		body        string
		wantChanged bool
		// wantSystem is the content of the first message after the call.
		wantSystem string
	}{
		{
			name:        "replaces built-in system message",
			body:        `{"model":"m","messages":[{"role":"system","content":"built-in"},{"role":"user","content":"hi"}]}`,
			wantChanged: true,
			wantSystem:  prompt,
		},
		{
			name:        "replaces developer message",
			body:        `{"model":"m","messages":[{"role":"developer","content":"dev rules"}]}`,
			wantChanged: true,
			wantSystem:  prompt,
		},
		{
			name:        "inserts when no system message exists",
			body:        `{"model":"m","messages":[{"role":"user","content":"hi"}]}`,
			wantChanged: true,
			wantSystem:  prompt,
		},
		{
			name:        "idempotent when already the system content",
			body:        `{"model":"m","messages":[{"role":"system","content":"persona block"},{"role":"user","content":"hi"}]}`,
			wantChanged: false,
			wantSystem:  prompt,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			out, changed, err := ReplaceChatSystem([]byte(tc.body), prompt)
			if err != nil {
				t.Fatalf("replace failed: %v", err)
			}
			if changed != tc.wantChanged {
				t.Fatalf("changed = %v, want %v", changed, tc.wantChanged)
			}
			var req map[string]any
			if err := json.Unmarshal(out, &req); err != nil {
				t.Fatal(err)
			}
			messages, ok := req["messages"].([]any)
			if !ok || len(messages) == 0 {
				t.Fatalf("no messages in result: %s", out)
			}
			first, _ := messages[0].(map[string]any)
			if first["content"] != tc.wantSystem {
				t.Fatalf("first message content = %v, want %q", first["content"], tc.wantSystem)
			}
			// A replacement must never leave the old system text behind.
			if strings.Contains(string(out), "built-in") || strings.Contains(string(out), "dev rules") {
				t.Fatalf("caller system text survived a replacement: %s", out)
			}
		})
	}
}

// Replacement must not silently degrade into an append when the body cannot
// carry the prompt: callers rely on the error to fail a request closed.
func TestReplaceChatSystem_UninjectableBodies(t *testing.T) {
	for _, body := range []string{
		`{"model":"m"}`,
		`{"model":"m","messages":"nope"}`,
		`not-json`,
		`null`,
	} {
		if _, _, err := ReplaceChatSystem([]byte(body), "p"); err != ErrUninjectable {
			t.Errorf("ReplaceChatSystem(%q) error = %v, want ErrUninjectable", body, err)
		}
	}
}

func TestReplaceClaudeSystem(t *testing.T) {
	const prompt = "persona block"

	tests := []struct {
		name        string
		body        string
		wantChanged bool
		wantSystem  string
	}{
		{
			name:        "replaces existing string system",
			body:        `{"model":"m","system":"caller prompt","messages":[{"role":"user","content":"hi"}]}`,
			wantChanged: true,
			wantSystem:  prompt,
		},
		{
			name:        "sets system when absent",
			body:        `{"model":"m","messages":[{"role":"user","content":"hi"}]}`,
			wantChanged: true,
			wantSystem:  prompt,
		},
		{
			name:        "replaces text blocks",
			body:        `{"model":"m","system":[{"type":"text","text":"caller prompt"}],"messages":[{"role":"user","content":"hi"}]}`,
			wantChanged: true,
			wantSystem:  prompt,
		},
		{
			name:        "idempotent for identical system string",
			body:        `{"model":"m","system":"persona block","messages":[{"role":"user","content":"hi"}]}`,
			wantChanged: false,
			wantSystem:  prompt,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			out, changed, err := ReplaceClaudeSystem([]byte(tc.body), prompt)
			if err != nil {
				t.Fatalf("replace failed: %v", err)
			}
			if changed != tc.wantChanged {
				t.Fatalf("changed = %v, want %v", changed, tc.wantChanged)
			}
			var req map[string]any
			if err := json.Unmarshal(out, &req); err != nil {
				t.Fatal(err)
			}
			if req["system"] != tc.wantSystem {
				t.Fatalf("system = %v, want %q", req["system"], tc.wantSystem)
			}
			// messages[] must never gain a system entry: Anthropic forbids it.
			for _, m := range req["messages"].([]any) {
				if mm, ok := m.(map[string]any); ok && mm["role"] == "system" {
					t.Fatalf("system role inserted into Claude messages[]: %s", out)
				}
			}
		})
	}
}

func TestReplaceClaudeSystem_UninjectableBodies(t *testing.T) {
	for _, body := range []string{
		`{"model":"m"}`,
		`{"model":"m","messages":[],"system":42}`,
		`not-json`,
	} {
		if _, _, err := ReplaceClaudeSystem([]byte(body), "p"); err != ErrUninjectable {
			t.Errorf("ReplaceClaudeSystem(%q) error = %v, want ErrUninjectable", body, err)
		}
	}
}

func TestReplaceResponsesInstructions(t *testing.T) {
	const prompt = "persona block"

	tests := []struct {
		name        string
		body        string
		wantChanged bool
	}{
		{"replaces existing instructions", `{"model":"m","instructions":"caller","input":"go"}`, true},
		{"sets instructions when absent", `{"model":"m","input":"go"}`, true},
		{"replaces null instructions", `{"model":"m","instructions":null,"input":"go"}`, true},
		{"idempotent for identical instructions", `{"model":"m","instructions":"persona block","input":"go"}`, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			out, changed, err := ReplaceResponsesInstructions([]byte(tc.body), prompt)
			if err != nil {
				t.Fatalf("replace failed: %v", err)
			}
			if changed != tc.wantChanged {
				t.Fatalf("changed = %v, want %v", changed, tc.wantChanged)
			}
			var req map[string]any
			if err := json.Unmarshal(out, &req); err != nil {
				t.Fatal(err)
			}
			if req["instructions"] != prompt {
				t.Fatalf("instructions = %v, want %q", req["instructions"], prompt)
			}
			// input[] must be untouched and never gain a system item.
			if req["input"] != "go" && req["input"] != nil {
				t.Fatalf("input changed: %v", req["input"])
			}
			if _, ok := req["messages"]; ok {
				t.Fatalf("Responses body gained messages[]: %s", out)
			}
		})
	}
}

func TestReplaceResponsesInstructions_UninjectableBodies(t *testing.T) {
	for _, body := range []string{
		`{"model":"m","instructions":42,"input":"go"}`,
		`not-json`,
		`null`,
	} {
		if _, _, err := ReplaceResponsesInstructions([]byte(body), "p"); err != ErrUninjectable {
			t.Errorf("ReplaceResponsesInstructions(%q) error = %v, want ErrUninjectable", body, err)
		}
	}
}
