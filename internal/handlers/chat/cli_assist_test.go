package chat

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHandleCliToolAssist_ValidatesInput(t *testing.T) {
	h := &ChatHandler{}
	cases := map[string]string{
		"missing model":     `{"tool":"Claude Code"}`,
		"missing tool":      `{"model":"m"}`,
		"oversize context":  `{"model":"m","tool":"t","context":"` + strings.Repeat("x", cliAssistMaxContext+1) + `"}`,
		"oversize question": `{"model":"m","tool":"t","question":"` + strings.Repeat("x", cliAssistMaxQuestion+1) + `"}`,
		"invalid json":      `{`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			h.HandleCliToolAssist(rec, httptest.NewRequest(http.MethodPost, "/api/dashboard/cli-tools/assist", strings.NewReader(body)))
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("got %d: %s", rec.Code, rec.Body.String())
			}
		})
	}
}
