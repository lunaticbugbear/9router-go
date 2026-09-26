package tokensaver

import (
	json "encoding/json/v2"
	"errors"
	"strings"
)

// Caveman prompts adapted from open-sse/rtk/cavemanPrompts.js
const (
	CavemanLite = `Respond tersely. Keep grammar and full sentences but drop filler, hedging and pleasantries (just/really/basically/sure/of course/I'd be happy to). Pattern: state the thing, the action, the reason. Then next step. Code blocks, file paths, commands, errors, URLs: keep exact. Security warnings, irreversible action confirmations, multi-step ordered sequences: write normal. Resume terse style after. Not: "Sure! I'd be happy to help you with that. The issue you're experiencing is likely caused by..." Yes: "Bug in auth middleware. Token expiry check use '<' not '<='. Fix:" Auto-Clarity: drop caveman for security warnings, irreversible actions, multi-step sequences where fragment ambiguity risks misread, or when user repeats a question. Resume after the clear part. ACTIVE EVERY RESPONSE. No revert after many turns. No filler drift. Still active if unsure. No invented abbreviations. Standard well-known tech acronyms (DB, API, HTTP, URL, JSON, ID, OS, CPU) OK. Names of code symbols, function names, API names, error strings: keep verbatim. Preserve the user's dominant language. User wrote Vietnamese, reply Vietnamese. User wrote English, reply English. Code identifiers, error strings, file paths, commands: keep in their original form regardless of language. No self-reference. Do not name or announce the style (no "caveman mode", no "me caveman think", no "compressed mode active"). Just respond. No decorative emoji. No narrating tool calls ("I will now search", "I used X to find Y"). No status phrases ("Sure!", "Of course!", "I'd be happy to"). No causal arrow shorthand ("A -> B -> fails"). State the thing, the action, the reason. Then next step.`

	CavemanFull = `Respond like terse caveman. All technical substance stay exact, only fluff die. Drop: articles (a/an/the), filler (just/really/basically/actually/simply), pleasantries, hedging. Fragments OK. Short synonyms (big not extensive, fix not implement a solution for). Pattern: [thing] [action] [reason]. [next step]. Code blocks, file paths, commands, errors, URLs: keep exact. Security warnings, irreversible action confirmations, multi-step ordered sequences: write normal. Resume terse style after. Not: "Sure! I'd be happy to help you with that. The issue you're experiencing is likely caused by..." Yes: "Bug in auth middleware. Token expiry check use '<' not '<='. Fix:" Auto-Clarity: drop caveman for security warnings, irreversible actions, multi-step sequences where fragment ambiguity risks misread, or when user repeats a question. Resume after the clear part. ACTIVE EVERY RESPONSE. No revert after many turns. No filler drift. Still active if unsure. No invented abbreviations. Standard well-known tech acronyms (DB, API, HTTP, URL, JSON, ID, OS, CPU) OK. Names of code symbols, function names, API names, error strings: keep verbatim. Preserve the user's dominant language. User wrote Vietnamese, reply Vietnamese. User wrote English, reply English. Code identifiers, error strings, file paths, commands: keep in their original form regardless of language. No self-reference. Do not name or announce the style (no "caveman mode", no "me caveman think", no "compressed mode active"). Just respond. No decorative emoji. No narrating tool calls ("I will now search", "I used X to find Y"). No status phrases ("Sure!", "Of course!", "I'd be happy to"). No causal arrow shorthand ("A -> B -> fails"). State the thing, the action, the reason. Then next step.`

	CavemanUltra = `Respond ultra-terse. Maximum compression. Telegraphic. Strip conjunctions. One word when one word enough. Pattern: [thing] [action] [reason]. [next step]. Code blocks, file paths, commands, errors, URLs: keep exact. Security warnings, irreversible action confirmations, multi-step ordered sequences: write normal. Resume terse style after. Not: "Sure! I'd be happy to help you with that. The issue you're experiencing is likely caused by..." Yes: "Bug in auth middleware. Token expiry check use '<' not '<='. Fix:" Auto-Clarity: drop caveman for security warnings, irreversible actions, multi-step sequences where fragment ambiguity risks misread, or when user repeats a question. Resume after the clear part. ACTIVE EVERY RESPONSE. No revert after many turns. No filler drift. Still active if unsure. No invented abbreviations. Standard well-known tech acronyms (DB, API, HTTP, URL, JSON, ID, OS, CPU) OK. Names of code symbols, function names, API names, error strings: keep verbatim. Preserve the user's dominant language. User wrote Vietnamese, reply Vietnamese. User wrote English, reply English. Code identifiers, error strings, file paths, commands: keep in their original form regardless of language. No self-reference. Do not name or announce the style (no "caveman mode", no "me caveman think", no "compressed mode active"). Just respond. No decorative emoji. No narrating tool calls ("I will now search", "I used X to find Y"). No status phrases ("Sure!", "Of course!", "I'd be happy to"). No causal arrow shorthand ("A -> B -> fails"). State the thing, the action, the reason. Then next step.`

	// CavemanPrompt is the default level (full).
	CavemanPrompt = CavemanFull
)

// Ponytail prompts adapted from open-sse/rtk/ponytailPrompt.js
const (
	PonytailLite = `You are a lazy senior developer. Lazy means efficient, not careless. The best code is the code never written. Lite: build what's asked, but name the lazier alternative in one line. User picks. Before writing code, stop at the first rung that holds: 1) Does this need to exist at all? (YAGNI) 2) Stdlib does it? Use it. 3) Native platform feature covers it? Use it (CSS over JS, DB constraint over app code). 4) Already-installed dependency solves it? Use it; never add a new one for what a few lines can do. 5) Can it be one line? One line. 6) Only then: the minimum code that works. No unrequested abstractions (no interface with one implementation, no factory for one product, no config for a value that never changes). No boilerplate or scaffolding "for later". Deletion over addition. Boring over clever. Fewest files possible; shortest working diff wins. Two stdlib options the same size: take the edge-case-correct one. Mark deliberate simplifications with a ponytail: comment naming the ceiling and upgrade path. Code first. Then at most three short lines: what was skipped, when to add it. No essays or design notes. Pattern: code -> skipped: X, add when Y. Never simplify away: input validation at trust boundaries, error handling that prevents data loss, security, accessibility, anything explicitly requested. Non-trivial logic leaves ONE runnable check behind (an assert-based self-check or one small test file; no frameworks). Trivial one-liners need no test. ACTIVE EVERY RESPONSE. No drift back to over-building. Still active if unsure.`

	PonytailFull = `You are a lazy senior developer. Lazy means efficient, not careless. The best code is the code never written. Full: the ladder enforced. Stdlib and native first. Shortest diff, shortest explanation. Before writing code, stop at the first rung that holds: 1) Does this need to exist at all? (YAGNI) 2) Stdlib does it? Use it. 3) Native platform feature covers it? Use it (CSS over JS, DB constraint over app code). 4) Already-installed dependency solves it? Use it; never add a new one for what a few lines can do. 5) Can it be one line? One line. 6) Only then: the minimum code that works. No unrequested abstractions (no interface with one implementation, no factory for one product, no config for a value that never changes). No boilerplate or scaffolding "for later". Deletion over addition. Boring over clever. Fewest files possible; shortest working diff wins. Two stdlib options the same size: take the edge-case-correct one. Mark deliberate simplifications with a ponytail: comment naming the ceiling and upgrade path. Code first. Then at most three short lines: what was skipped, when to add it. No essays or design notes. Pattern: code -> skipped: X, add when Y. Never simplify away: input validation at trust boundaries, error handling that prevents data loss, security, accessibility, anything explicitly requested. Non-trivial logic leaves ONE runnable check behind (an assert-based self-check or one small test file; no frameworks). Trivial one-liners need no test. ACTIVE EVERY RESPONSE. No drift back to over-building. Still active if unsure.`

	PonytailUltra = `You are a lazy senior developer. Lazy means efficient, not careless. The best code is the code never written. Ultra: YAGNI extremist. Deletion before addition. Ship the one-liner and challenge the rest of the requirement in the same response. Before writing code, stop at the first rung that holds: 1) Does this need to exist at all? (YAGNI) 2) Stdlib does it? Use it. 3) Native platform feature covers it? Use it (CSS over JS, DB constraint over app code). 4) Already-installed dependency solves it? Use it; never add a new one for what a few lines can do. 5) Can it be one line? One line. 6) Only then: the minimum code that works. No unrequested abstractions (no interface with one implementation, no factory for one product, no config for a value that never changes). No boilerplate or scaffolding "for later". Deletion over addition. Boring over clever. Fewest files possible; shortest working diff wins. Two stdlib options the same size: take the edge-case-correct one. Mark deliberate simplifications with a ponytail: comment naming the ceiling and upgrade path. Code first. Then at most three short lines: what was skipped, when to add it. No essays or design notes. Pattern: code -> skipped: X, add when Y. Never simplify away: input validation at trust boundaries, error handling that prevents data loss, security, accessibility, anything explicitly requested. Non-trivial logic leaves ONE runnable check behind (an assert-based self-check or one small test file; no frameworks). Trivial one-liners need no test. ACTIVE EVERY RESPONSE. No drift back to over-building. Still active if unsure.`

	// PonytailPrompt is the default level (full).
	PonytailPrompt = PonytailFull
)

// GetCavemanPrompt returns the caveman system prompt for the specified level.
func GetCavemanPrompt(level string) string {
	switch level {
	case "lite":
		return CavemanLite
	case "ultra":
		return CavemanUltra
	case "full":
		return CavemanFull
	default:
		return CavemanFull
	}
}

// GetPonytailPrompt returns the ponytail system prompt for the specified level.
func GetPonytailPrompt(level string) string {
	switch level {
	case "lite":
		return PonytailLite
	case "ultra":
		return PonytailUltra
	case "full":
		return PonytailFull
	default:
		return PonytailFull
	}
}

// ErrUninjectable reports that a request body cannot carry a system prompt in
// any wire shape the target protocol reads. Callers that must not silently
// forward without the prompt (the bounty scope context) use it to reject the
// request before any upstream call.
var ErrUninjectable = errors.New("request body cannot carry a system instructions field")

// mergePrompt appends prompt to an existing system value, joining with a blank
// line, and reports whether the value already contained the prompt (in which
// case the caller should leave the body untouched).
func mergePrompt(existing, prompt string) (string, bool) {
	if strings.Contains(existing, prompt) {
		return existing, true
	}
	if existing == "" {
		return prompt, false
	}
	return existing + "\n\n" + prompt, false
}

// mutateChatSystem applies prompt to an OpenAI chat-completions body in place.
// It reads messages[] (and, only when allowInputFallback is set, input[] as a
// message array) and writes back to the same key it read. It reports whether
// the body changed and whether it was injectable at all.
func mutateChatSystem(req map[string]any, prompt string, allowInputFallback bool) (changed, ok bool) {
	key := "messages"
	arr := toMessageArray(req["messages"])
	if arr == nil && allowInputFallback {
		key = "input"
		arr = toMessageArray(req["input"])
	}
	if arr == nil {
		return false, false
	}

	clone := make([]any, len(arr))
	copy(clone, arr)

	for i, m := range clone {
		msg, isMap := m.(map[string]any)
		if !isMap {
			continue
		}
		role, _ := msg["role"].(string)
		if role != "system" && role != "developer" {
			continue
		}
		content, isStr := msg["content"].(string)
		if !isStr {
			// A valid OpenAI system message may express content as a text-parts
			// array. Preserve it byte-for-byte and prepend a separate system
			// message carrying the scope. Any other non-string content is a
			// malformed message.
			if _, isParts := msg["content"].([]any); !isParts {
				return false, false
			}
			if contentPartsContain(msg["content"], prompt) {
				return false, true
			}
			req[key] = append([]any{map[string]any{"role": "system", "content": prompt}}, clone...)
			return true, true
		}
		merged, already := mergePrompt(content, prompt)
		if already {
			return false, true
		}
		msg["content"] = merged
		clone[i] = msg
		req[key] = clone
		return true, true
	}

	req[key] = append([]any{map[string]any{"role": "system", "content": prompt}}, clone...)
	return true, true
}

// mutateClaudeSystem applies prompt to the top-level "system" field of a Claude
// Messages body in place. The Anthropic API forbids role:"system" inside
// messages[], so the prompt can only live in that top-level field (string or
// text blocks). It reports whether the body changed and whether it was
// injectable at all.
func mutateClaudeSystem(req map[string]any, prompt string) (changed, ok bool) {
	switch sys := req["system"].(type) {
	case nil:
		req["system"] = prompt
		return true, true
	case string:
		merged, already := mergePrompt(sys, prompt)
		if already {
			return false, true
		}
		req["system"] = merged
		return true, true
	case []any:
		for _, block := range sys {
			bMap, isMap := block.(map[string]any)
			if !isMap {
				continue
			}
			if t, isStr := bMap["text"].(string); isStr && strings.Contains(t, prompt) {
				return false, true
			}
		}
		req["system"] = append(sys, map[string]any{"type": "text", "text": prompt})
		return true, true
	default:
		return false, false
	}
}

// mutateResponsesInstructions applies prompt to the OpenAI Responses API
// top-level "instructions" field in place. That field is the only
// spec-compliant home for system instructions in this protocol; input[] is
// never modified because a role:"system" item there is invalid. It reports
// whether the body changed and whether it was injectable at all.
func mutateResponsesInstructions(req map[string]any, prompt string) (changed, ok bool) {
	switch instructions := req["instructions"].(type) {
	case nil:
		req["instructions"] = prompt
		return true, true
	case string:
		merged, already := mergePrompt(instructions, prompt)
		if already {
			return false, true
		}
		req["instructions"] = merged
		return true, true
	default:
		return false, false
	}
}

// InjectSystemPrompt adds a system prompt to an OpenAI-format request body.
// Handles messages[] (chat), input[] (responses), and instructions (responses string).
// Finds existing system/developer message and appends, or inserts at position 0.
// Returns modified body and true if any modification was made.
//
// This tolerant form is used by the token-saver paths, where an unshaped body is
// simply left alone. Callers that must not drop the prompt (the bounty scope
// context) use InjectSystemPromptStrict or InjectSystemPromptResponsesStrict.
func InjectSystemPrompt(body []byte, prompt string) ([]byte, bool) {
	var req map[string]any
	if err := json.Unmarshal(body, &req); err != nil || req == nil {
		return body, false
	}

	// OpenAI Responses API: top-level instructions string field.
	if instructions, ok := req["instructions"].(string); ok {
		if merged, already := mergePrompt(instructions, prompt); already {
			return body, false
		} else {
			req["instructions"] = merged
		}
		out, err := json.Marshal(req)
		if err != nil {
			return body, false
		}
		return out, true
	}

	changed, ok := mutateChatSystem(req, prompt, true)
	if !ok || !changed {
		return body, false
	}
	out, err := json.Marshal(req)
	if err != nil {
		return body, false
	}
	return out, true
}

// InjectSystemPromptClaude adds a system prompt to a Claude Messages-format
// request body. The Anthropic API forbids role:"system" inside messages[];
// the system prompt must be the top-level "system" field (string or text
// blocks). Merges into an existing value, skipping if the prompt is present.
// Returns modified body and true if any modification was made.
func InjectSystemPromptClaude(body []byte, prompt string) ([]byte, bool) {
	var req map[string]any
	if err := json.Unmarshal(body, &req); err != nil || req == nil {
		return body, false
	}
	changed, ok := mutateClaudeSystem(req, prompt)
	if !ok || !changed {
		return body, false
	}
	out, err := json.Marshal(req)
	if err != nil {
		return body, false
	}
	return out, true
}

// InjectSystemPromptResponsesStrict writes prompt into the OpenAI Responses API
// top-level "instructions" field, the only spec-compliant place for system
// instructions in that protocol. It preserves any caller-supplied instructions
// and never modifies input[].
//
// Returns (body, false, nil) when the prompt is already present, so repeated
// application is idempotent. Returns ErrUninjectable when the body is not a JSON
// object (including JSON `null`) or when instructions is present but not a
// string/null.
func InjectSystemPromptResponsesStrict(body []byte, prompt string) ([]byte, bool, error) {
	req := map[string]any{}
	if err := json.Unmarshal(body, &req); err != nil {
		return body, false, ErrUninjectable
	}
	if req == nil {
		// JSON `null` unmarshals into a nil map; writing to it would panic.
		return body, false, ErrUninjectable
	}
	changed, ok := mutateResponsesInstructions(req, prompt)
	if !ok {
		return body, false, ErrUninjectable
	}
	if !changed {
		return body, false, nil
	}
	out, err := json.Marshal(req)
	if err != nil {
		return body, false, ErrUninjectable
	}
	return out, true, nil
}

// InjectSystemPromptStrict is the error-returning OpenAI chat-completions
// injector. Chat Completions reads messages[]; input[] belongs to the Responses
// API, so a chat request carrying input[] cannot receive the context in a field
// the upstream would act on and is reported as ErrUninjectable.
//
// Returns (body, false, nil) when the prompt is already present (idempotent).
// Returns ErrUninjectable when the body is not a JSON object, when messages[] is
// not a message array, or when the target system message holds non-string
// content that cannot be extended.
func InjectSystemPromptStrict(body []byte, prompt string) ([]byte, bool, error) {
	req := map[string]any{}
	if err := json.Unmarshal(body, &req); err != nil {
		return body, false, ErrUninjectable
	}
	if req == nil {
		return body, false, ErrUninjectable
	}
	changed, ok := mutateChatSystem(req, prompt, false)
	if !ok {
		return body, false, ErrUninjectable
	}
	if !changed {
		return body, false, nil
	}
	out, err := json.Marshal(req)
	if err != nil {
		return body, false, ErrUninjectable
	}
	return out, true, nil
}

// InjectSystemPromptClaudeStrict is the error-returning Claude Messages
// injector. The Anthropic API forbids role:"system" inside messages[], so the
// prompt must go in the top-level "system" field (string or text blocks).
//
// Returns (body, false, nil) when the prompt is already present (idempotent).
// Returns ErrUninjectable for a non-object body (including JSON `null`), when
// messages[] is missing or is not a message array, or when "system" holds an
// unsupported type — a selected profile must never be dropped silently.
func InjectSystemPromptClaudeStrict(body []byte, prompt string) ([]byte, bool, error) {
	req := map[string]any{}
	if err := json.Unmarshal(body, &req); err != nil {
		return body, false, ErrUninjectable
	}
	if req == nil {
		return body, false, ErrUninjectable
	}
	if toMessageArray(req["messages"]) == nil {
		return body, false, ErrUninjectable
	}
	changed, ok := mutateClaudeSystem(req, prompt)
	if !ok {
		return body, false, ErrUninjectable
	}
	if !changed {
		return body, false, nil
	}
	out, err := json.Marshal(req)
	if err != nil {
		return body, false, ErrUninjectable
	}
	return out, true, nil
}

// ReplaceChatSystem overwrites the system instructions in an OpenAI
// chat-completions body with prompt. It is the explicit-replacement counterpart
// of InjectSystemPromptStrict and never appends: an existing system/developer
// message is swapped out, and when the body carries none a new system message
// is inserted at position 0.
//
// Returns (body, false, nil) when the body's first system message already holds
// exactly this prompt, so repeated application stays idempotent. Returns
// ErrUninjectable for the same bodies as InjectSystemPromptStrict — a
// replacement must not silently degrade into an append.
func ReplaceChatSystem(body []byte, prompt string) ([]byte, bool, error) {
	req, ok := decodeRequestObject(body)
	if !ok {
		return body, false, ErrUninjectable
	}
	changed, ok := replaceChatSystem(req, prompt)
	if !ok {
		return body, false, ErrUninjectable
	}
	if !changed {
		return body, false, nil
	}
	return marshalRequest(body, req)
}

// ReplaceClaudeSystem overwrites the top-level "system" field of a Claude
// Messages body with prompt. The Anthropic API forbids role:"system" inside
// messages[], so that field is the only home for system instructions.
func ReplaceClaudeSystem(body []byte, prompt string) ([]byte, bool, error) {
	req, ok := decodeRequestObject(body)
	if !ok {
		return body, false, ErrUninjectable
	}
	if toMessageArray(req["messages"]) == nil {
		return body, false, ErrUninjectable
	}
	isPrompt, ok := systemFieldIsPrompt(req["system"], prompt)
	if !ok {
		return body, false, ErrUninjectable
	}
	if isPrompt {
		return body, false, nil
	}
	req["system"] = prompt
	return marshalRequest(body, req)
}

// ReplaceResponsesInstructions overwrites the OpenAI Responses API top-level
// "instructions" field with prompt. input[] is never modified: a role:"system"
// item there is invalid for that protocol.
func ReplaceResponsesInstructions(body []byte, prompt string) ([]byte, bool, error) {
	req, ok := decodeRequestObject(body)
	if !ok {
		return body, false, ErrUninjectable
	}
	if instructions, isStr := req["instructions"].(string); (req["instructions"] == nil || isStr) && instructions == prompt {
		return body, false, nil
	}
	if req["instructions"] != nil {
		if _, isStr := req["instructions"].(string); !isStr {
			return body, false, ErrUninjectable
		}
	}
	req["instructions"] = prompt
	return marshalRequest(body, req)
}

// replaceChatSystem swaps the first system/developer message's content for
// prompt, inserting a new leading system message when the body has none. It
// reports whether the body changed and whether it was injectable at all.
func replaceChatSystem(req map[string]any, prompt string) (changed, ok bool) {
	arr := toMessageArray(req["messages"])
	if arr == nil {
		return false, false
	}
	clone := make([]any, len(arr))
	copy(clone, arr)

	for i, m := range clone {
		msg, isMap := m.(map[string]any)
		if !isMap {
			continue
		}
		role, _ := msg["role"].(string)
		if role != "system" && role != "developer" {
			continue
		}
		if content, isStr := msg["content"].(string); isStr && content == prompt {
			return false, true
		}
		msg["content"] = prompt
		delete(msg, "content_parts")
		clone[i] = msg
		req["messages"] = clone
		return true, true
	}

	req["messages"] = append([]any{map[string]any{"role": "system", "content": prompt}}, clone...)
	return true, true
}

// systemFieldIsPrompt reports whether an existing Claude "system" field already
// holds prompt, and whether that field is a type the replacement can overwrite
// (absent, null, a string, or text blocks). Any other type is uninjectable.
func systemFieldIsPrompt(system any, prompt string) (isPrompt, ok bool) {
	switch sys := system.(type) {
	case nil:
		return false, true
	case string:
		return sys == prompt, true
	case []any:
		return len(sys) == 1 && blockTextEquals(sys[0], prompt), true
	default:
		return false, false
	}
}

// blockTextEquals reports whether a Claude system text block carries exactly
// text. A non-block entry or a differently-typed content block is not a match.
func blockTextEquals(block any, text string) bool {
	bMap, isMap := block.(map[string]any)
	if !isMap || bMap["type"] != "text" {
		return false
	}
	t, isStr := bMap["text"].(string)
	return isStr && t == text
}

// decodeRequestObject parses body into a mutable JSON object. It reports false
// for a non-object body, including JSON `null`, which would panic on write.
func decodeRequestObject(body []byte) (map[string]any, bool) {
	req := map[string]any{}
	if err := json.Unmarshal(body, &req); err != nil {
		return nil, false
	}
	if req == nil {
		return nil, false
	}
	return req, true
}

// marshalRequest re-encodes a mutated request, falling back to the original body
// with ErrUninjectable when the object cannot be serialized.
func marshalRequest(original []byte, req map[string]any) ([]byte, bool, error) {
	out, err := json.Marshal(req)
	if err != nil {
		return original, false, ErrUninjectable
	}
	return out, true, nil
}

// contentPartsContain reports whether a non-string message content value (e.g.
// an OpenAI text-parts array) already carries prompt, so repeated injection
// stays idempotent instead of prepending a second scope message.
func contentPartsContain(content any, prompt string) bool {
	arr, ok := content.([]any)
	if !ok {
		return false
	}
	for _, part := range arr {
		pMap, isMap := part.(map[string]any)
		if !isMap {
			continue
		}
		if t, ok := pMap["text"].(string); ok && strings.Contains(t, prompt) {
			return true
		}
	}
	return false
}

// toMessageArray extracts []any from a JSON value that might be an array.
func toMessageArray(v any) []any {
	if v == nil {
		return nil
	}
	arr, ok := v.([]any)
	if !ok {
		return nil
	}
	return arr
}

// InjectPersonaInlineStrict delivers a persona as the leading content of the
// FIRST user message instead of as a system message.
//
// This exists because some OpenAI-compatible upstreams do not process a system
// message at all: measured against one provider, a 71 KB system prompt was
// acknowledged with prompt_tokens=42 and the model never saw it, while the same
// text in a user message was counted in full (16899 prompt tokens). Delivering a
// long persona through the system field is therefore silently useless there,
// whereas the user path is the one those providers actually read.
//
// The persona is still operator instruction text, so it is marked as such and
// the request is still fail-closed: a body with no user message returns
// ErrUninjectable rather than forwarding an unpersonified request.
//
// Order matters: the persona goes ABOVE the caller's own words in the same
// message, so the operator's doctrine frames the request and the caller's task
// reads as the thing being instructed.
func InjectPersonaInlineStrict(body []byte, prompt string) ([]byte, bool, error) {
	var req map[string]any
	if err := json.Unmarshal(body, &req); err != nil || req == nil {
		return body, false, ErrUninjectable
	}
	arr := toMessageArray(req["messages"])
	if arr == nil {
		return body, false, ErrUninjectable
	}
	for i, m := range arr {
		msg, isMap := m.(map[string]any)
		if !isMap {
			continue
		}
		if role, _ := msg["role"].(string); role != "user" {
			continue
		}
		content, isStr := msg["content"].(string)
		if !isStr {
			return body, false, ErrUninjectable
		}
		if strings.Contains(content, prompt) {
			return body, false, nil
		}
		clone := make([]any, len(arr))
		copy(clone, arr)
		msgCopy := make(map[string]any, len(msg))
		for k, v := range msg {
			msgCopy[k] = v
		}
		msgCopy["content"] = prompt + "\n\n---\n\n" + content
		clone[i] = msgCopy
		req["messages"] = clone
		out, err := json.Marshal(req)
		if err != nil {
			return body, false, ErrUninjectable
		}
		return out, true, nil
	}
	return body, false, ErrUninjectable
}
