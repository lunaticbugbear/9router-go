package executor

import (
	json "encoding/json/v2"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"9router/proxy/internal/proxy"
)

func ForwardGitHub(w http.ResponseWriter, req *Request) error {
	resp, err := proxy.ForwardOpenAI(req.Ctx, req.Client, req.Config, req.APIKey, req.Body, req.IsStream)
	if err == nil {
		defer resp.Body.Close()
		if req.IsStream {
			return execSSEStream(w, resp.Body, req)
		}
		return jsonResponse(req.Ctx, w, resp.Body, req.TranslateResp, req.ResponseBuf)
	}
	var upstream *proxy.UpstreamError
	if !errors.As(err, &upstream) || upstream.StatusCode != http.StatusBadRequest {
		return fmt.Errorf("ForwardGitHub upstream: %w", err)
	}
	var rejection struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	var request struct {
		Model string `json:"model"`
	}
	_ = json.Unmarshal(upstream.Body, &rejection)
	_ = json.Unmarshal(req.Body, &request)
	model := strings.ToLower(request.Model)
	if rejection.Error.Code != "unsupported_api_for_model" || model == "" ||
		strings.Contains(model, "claude") || strings.Contains(model, "gemini") {
		return fmt.Errorf("ForwardGitHub upstream: %w", err)
	}
	body, _, err := buildResponsesBody(req.Body)
	if err != nil {
		return fmt.Errorf("transform GitHub responses request: %w", err)
	}
	cfg := *req.Config
	cfg.BaseURL = strings.TrimSuffix(strings.TrimRight(cfg.BaseURL, "/"), "/chat/completions") + "/responses"
	resp, err = proxy.ForwardOpenAI(req.Ctx, req.Client, &cfg, req.APIKey, body, true)
	if err != nil {
		return fmt.Errorf("ForwardGitHub responses upstream: %w", err)
	}
	defer resp.Body.Close()
	return handleCodexStream(w, req, resp.Body)
}
