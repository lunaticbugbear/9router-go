package chat

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"

	json "encoding/json/v2"

	"9router/proxy/internal/handlerutil"
)

const (
	cliAssistMaxBody      = 32 << 10
	cliAssistMaxContext   = 8000
	cliAssistMaxQuestion  = 2000
	cliAssistSystemPrompt = "You help an operator configure a coding tool on their own machine to use the local 9router-go gateway " +
		"(an OpenAI/Anthropic-compatible proxy). Answer with short numbered steps and copy-pasteable shell commands or config " +
		"snippets. Use the base URL and values given in the setup facts. Write <your-api-key> wherever a key is needed and never " +
		"invent a real key. If you are unsure a flag, file path, or setting exists for this tool, say so instead of guessing."
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

	user := "Tool: " + req.Tool + "\n\nSetup facts:\n" + strings.TrimSpace(req.Context) + "\n\nOperator request: " + req.Question
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
		handlerutil.WriteJSON(w, http.StatusBadGateway, map[string]any{"error": chatErrorMessage(rec)})
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
	handlerutil.WriteJSON(w, http.StatusOK, map[string]any{
		"answer": strings.TrimSpace(out.Choices[0].Message.Content),
		"model":  model,
	})
}

func chatErrorMessage(rec *httptest.ResponseRecorder) string {
	raw := strings.TrimSpace(rec.Body.String())
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
		return http.StatusText(rec.Code)
	}
	if len(raw) > 300 {
		raw = raw[:300]
	}
	return raw
}
