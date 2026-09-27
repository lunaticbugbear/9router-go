package tokensaver

import (
	json "encoding/json/v2"
	"strings"
	"testing"

	"9router/proxy/internal/log"
)

// The Go port must mirror open-sse/rtk/index.js. These tests pin the shapes and
// guards that decide whether error traces and ordinary conversation text are
// left intact.

func init() {
	// Compression logs at warn/info level; keep test output readable.
	if lvl, ok := log.ParseLevel("error"); ok {
		log.SetLevel(lvl)
	}
}

// Upstream skips tool_result blocks with is_error:true so an error trace reaches
// the model intact. The Go port must keep that guard for both payload fields.
func TestCompressMessages_PreservesErrorToolResult(t *testing.T) {
	errText := longGenericText()

	for _, field := range []string{"content", "text"} {
		t.Run(field, func(t *testing.T) {
			block := map[string]any{
				"type":     "tool_result",
				"is_error": true,
			}
			block[field] = errText
			msg := map[string]any{
				"messages": []any{
					map[string]any{"content": []any{block}},
				},
			}
			in, _ := json.Marshal(msg)
			out, ok := CompressMessages(in)
			if ok {
				t.Error("expected no compression for an is_error tool_result")
			}
			if string(out) != string(in) {
				t.Errorf("error trace must be preserved, got %s", out)
			}
		})
	}
}

// Claude carries tool output in tool_result.content (string form). The Go port
// read "text" instead, so real Claude tool results were never compressed.
func TestCompressMessages_CompressesClaudeToolResultContentString(t *testing.T) {
	msg := map[string]any{
		"messages": []any{
			map[string]any{
				"content": []any{
					map[string]any{"type": "tool_result", "content": longGitDiff()},
				},
			},
		},
	}
	in, _ := json.Marshal(msg)
	out, ok := CompressMessages(in)
	if !ok {
		t.Fatal("expected true for a long Claude tool_result content string")
	}
	var res map[string]any
	if err := json.Unmarshal(out, &res); err != nil {
		t.Fatal(err)
	}
	block := res["messages"].([]any)[0].(map[string]any)["content"].([]any)[0].(map[string]any)
	assertContains(t, block["content"].(string), "... (")
}

// Claude array form: tool_result.content is a list of text blocks.
func TestCompressMessages_CompressesClaudeToolResultContentArray(t *testing.T) {
	msg := map[string]any{
		"messages": []any{
			map[string]any{
				"content": []any{
					map[string]any{
						"type": "tool_result",
						"content": []any{
							map[string]any{"type": "text", "text": longGitDiff()},
						},
					},
				},
			},
		},
	}
	in, _ := json.Marshal(msg)
	out, ok := CompressMessages(in)
	if !ok {
		t.Fatal("expected true for a long Claude tool_result content array")
	}
	var res map[string]any
	if err := json.Unmarshal(out, &res); err != nil {
		t.Fatal(err)
	}
	block := res["messages"].([]any)[0].(map[string]any)["content"].([]any)[0].(map[string]any)
	part := block["content"].([]any)[0].(map[string]any)
	assertContains(t, part["text"].(string), "... (")
}

// The toggle says "Compress tool output". Ordinary conversation text blocks are
// not tool output, so RTK must leave them byte-for-byte intact.
func TestCompressMessages_LeavesPlainTextBlocksAlone(t *testing.T) {
	long := longGenericText()
	msg := map[string]any{
		"messages": []any{
			map[string]any{"role": "user", "content": []any{map[string]any{"type": "text", "text": long}}},
			map[string]any{"role": "assistant", "content": []any{map[string]any{"type": "text", "text": long}}},
		},
	}
	in, _ := json.Marshal(msg)
	out, ok := CompressMessages(in)
	if ok {
		t.Error("expected no compression for plain conversation text blocks")
	}
	if string(out) != string(in) {
		t.Errorf("conversation text must be untouched, got %s", out)
	}
	if strings.Contains(string(out), "lines truncated") {
		t.Error("plain text was truncated by RTK")
	}
}
