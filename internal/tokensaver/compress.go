package tokensaver

import (
	"encoding/json/jsontext"
	json "encoding/json/v2"
	"regexp"
	"strconv"
	"strings"
)

const (
	MinCompressSize = 500
	// RawCap mirrors RAW_CAP in open-sse/rtk/constants.js: blobs above this are
	// left alone instead of being fed to a filter.
	RawCap          = 10 * 1024 * 1024
	SmartTruncHead  = 120
	SmartTruncTail  = 60
	SmartTruncMin   = 250
	GitDiffMaxLines = 100
	GitLogMaxLines  = 200
	GrepPerFileMax  = 10
	TreeMaxLines    = 200
)

// CompressMessages compresses tool_result content in LLM request bodies in-place.
// Returns modified body and true if any compression was applied.
//
// Shape parity with open-sse/rtk/index.js compressMessages:
//   - OpenAI Responses  {type:"function_call_output", output: string | [{type:"input_text",text}]}
//   - OpenAI tool       {role:"tool", content: string | [{type:"text",text}]}
//   - Claude blocks     {content:[{type:"tool_result", content: string | [{type:"text",text}]}]}
//
// Only tool output is compressed. Ordinary conversation text blocks are not
// tool output and must reach the model untouched, and tool_result blocks marked
// is_error:true are skipped so an error trace stays intact.
func CompressMessages(body []byte) ([]byte, bool) {
	var rawMap map[string]jsontext.Value
	if err := json.Unmarshal(body, &rawMap); err != nil {
		return body, false
	}
	// Unmarshal into map[string]jsontext.Value succeeds for "null" or "[]",
	// leaving rawMap nil. Marshal(nil map) yields "null", which would corrupt
	// the request body downstream, so bail out on non-object payloads.
	if rawMap == nil {
		return body, false
	}

	key := "messages"
	itemsBytes, ok := rawMap["messages"]
	if !ok {
		key = "input"
		itemsBytes, ok = rawMap["input"]
	}
	if !ok || len(itemsBytes) == 0 {
		return body, false
	}

	var items []map[string]any
	if err := json.Unmarshal(itemsBytes, &items); err != nil || len(items) == 0 {
		return body, false
	}

	compressed := false
	for _, msg := range items {
		// OpenAI Responses: function_call_output, string or text-block array.
		if msg["type"] == "function_call_output" {
			if compressOutputField(msg, "output", "input_text") {
				compressed = true
			}
			continue
		}

		// OpenAI tool message: string content, or an array of text blocks.
		if msg["role"] == "tool" {
			if content, ok := msg["content"].(string); ok {
				if c := CompressText(content); c != content {
					msg["content"] = c
					compressed = true
				}
			} else if contentArr, ok := msg["content"].([]any); ok {
				if compressTextBlocks(contentArr, "text") {
					compressed = true
				}
			}
			continue
		}

		// Claude: content[] carrying tool_result blocks.
		contentArr, ok := msg["content"].([]any)
		if !ok {
			continue
		}
		for _, part := range contentArr {
			block, ok := part.(map[string]any)
			if !ok || block["type"] != "tool_result" {
				continue
			}
			// Error traces are diagnostics, not bulk output: keep them intact.
			if isError, _ := block["is_error"].(bool); isError {
				continue
			}
			if text, ok := block["content"].(string); ok {
				if c := CompressText(text); c != text {
					block["content"] = c
					compressed = true
				}
			} else if nested, ok := block["content"].([]any); ok {
				if compressTextBlocks(nested, "text") {
					compressed = true
				}
			}
		}
	}

	if !compressed {
		return body, false
	}

	compressedBytes, err := json.Marshal(items)
	if err != nil {
		return body, false
	}
	rawMap[key] = jsontext.Value(compressedBytes)

	out, err := json.Marshal(rawMap)
	if err != nil {
		return body, false
	}
	return out, true
}

// compressOutputField compresses a top-level string field or the text parts of
// its block array. partType selects which block kind carries the text.
func compressOutputField(msg map[string]any, field, partType string) bool {
	if text, ok := msg[field].(string); ok {
		if c := CompressText(text); c != text {
			msg[field] = c
			return true
		}
		return false
	}
	if arr, ok := msg[field].([]any); ok {
		return compressTextBlocks(arr, partType)
	}
	return false
}

// compressTextBlocks compresses the text field of every block of partType.
func compressTextBlocks(blocks []any, partType string) bool {
	changed := false
	for _, part := range blocks {
		block, ok := part.(map[string]any)
		if !ok || block["type"] != partType {
			continue
		}
		text, ok := block["text"].(string)
		if !ok {
			continue
		}
		if c := CompressText(text); c != text {
			block["text"] = c
			changed = true
		}
	}
	return changed
}

// CompressText applies content-aware compression.
func CompressText(text string) string {
	if len(text) < MinCompressSize {
		return text
	}
	// Mirror RAW_CAP: oversized blobs are passed through rather than filtered.
	if len(text) > RawCap {
		return text
	}
	trimmed := strings.TrimSpace(text)

	out := applyFilter(trimmed)
	// Never-worse guard (core/guard.rs never_worse, index.js `out.length >= bytesIn`):
	// an empty or larger result is worse for the model than the raw text.
	if out == "" || len(out) >= len(text) {
		return text
	}
	return out
}

// applyFilter selects and runs the content-aware filter for a blob.
func applyFilter(trimmed string) string {
	switch {
	case isGitDiff(trimmed):
		return compressGitDiff(trimmed)
	case isGitLog(trimmed):
		return compressGitLog(trimmed)
	case isGrepOutput(trimmed):
		return compressGrep(trimmed)
	case isTreeOutput(trimmed):
		return compressTree(trimmed)
	default:
		return smartTruncate(trimmed)
	}
}

func isGitDiff(s string) bool {
	return strings.HasPrefix(s, "diff --git") || strings.HasPrefix(s, "--- a/")
}
func isGitLog(s string) bool {
	lines := strings.SplitN(s, "\n", 2)
	if len(lines) == 0 {
		return false
	}
	matched, _ := regexp.MatchString(`^([a-f0-9]{7,40}\s|commit\s[a-f0-9]{7,})`, lines[0])
	return matched
}
func isGrepOutput(s string) bool {
	lines := strings.SplitN(s, "\n", 4)
	if len(lines) < 2 {
		return false
	}
	count := 0
	for _, l := range lines[:3] {
		if len(strings.SplitN(l, ":", 3)) >= 2 {
			count++
		}
	}
	return count >= 2
}
func isTreeOutput(s string) bool {
	return strings.Contains(s, "├──") || strings.Contains(s, "└──")
}

func compressGitDiff(s string) string {
	lines := strings.Split(s, "\n")
	if len(lines) <= GitDiffMaxLines {
		return s
	}
	return strings.Join(lines[:GitDiffMaxLines], "\n") + "\n... (" + itoa(len(lines)-GitDiffMaxLines) + " more lines)"
}

func compressGitLog(s string) string {
	lines := strings.Split(s, "\n")
	if len(lines) <= GitLogMaxLines {
		return s
	}
	return strings.Join(lines[:GitLogMaxLines], "\n") + "\n... (" + itoa(len(lines)-GitLogMaxLines) + " more commits)"
}

func compressGrep(s string) string {
	lines := strings.Split(s, "\n")
	if len(lines) <= GrepPerFileMax*20 {
		return s
	}
	fileCount := make(map[string]int)
	result := make([]string, 0, len(lines))
	for _, line := range lines {
		parts := strings.SplitN(line, ":", 2)
		file := parts[0]
		if fileCount[file] >= GrepPerFileMax {
			continue
		}
		fileCount[file]++
		result = append(result, line)
	}
	if len(result) < len(lines) {
		result = append(result, "... ("+itoa(len(lines)-len(result))+" matches filtered)")
	}
	return strings.Join(result, "\n")
}

func compressTree(s string) string {
	lines := strings.Split(s, "\n")
	if len(lines) <= TreeMaxLines {
		return s
	}
	return strings.Join(lines[:TreeMaxLines], "\n") + "\n... (" + itoa(len(lines)-TreeMaxLines) + " more entries)"
}

func smartTruncate(s string) string {
	lines := strings.Split(s, "\n")
	if len(lines) <= SmartTruncMin {
		return s
	}
	head := lines[:SmartTruncHead]
	tail := lines[len(lines)-SmartTruncTail:]
	result := append(head, "... ("+itoa(len(lines)-SmartTruncHead-SmartTruncTail)+" lines truncated)")
	return strings.Join(append(result, tail...), "\n")
}

func itoa(n int) string {
	return strconv.Itoa(n)
}
