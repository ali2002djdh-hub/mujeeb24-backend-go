//go:build gemini_live

package gemini

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/Ammar777782439/mujeeb24-backend-go/internal/application/ports"
)

// TestGeminiLiveSmoke tests a REAL Gemini API call.
//
// This test is ONLY compiled + run when the build tag `gemini_live` is set:
//   go test -tags=gemini_live -v -run TestGeminiLiveSmoke ./internal/adapters/secondary/ai/gemini/
//
// The CI workflow runs it on workflow_dispatch only with
// secrets.GEMINI_API_KEY.
//
// Behavior:
// - If GEMINI_API_KEY is NOT set → t.Skip (can't test without a key).
// - If GEMINI_API_KEY IS set but Gemini returns an error → t.Fatalf
//   (FAIL, not Skip — a real API error must not be hidden).
// - If Gemini succeeds → verifies non-empty response + positive tokens.

func TestGeminiLiveSmoke(t *testing.T) {
	apiKey := os.Getenv("GEMINI_API_KEY")
	if apiKey == "" {
		t.Skip("GEMINI_API_KEY not set — skipping live Gemini smoke test")
	}

	client, err := NewGeminiHTTPClient(GeminiHTTPClientConfig{
		BaseURL:        "https://generativelanguage.googleapis.com",
		APIKey:         apiKey,
		Model:          "gemini-3.5-flash",
		RequestTimeout: 30 * time.Second,
	})
	if err != nil {
		t.Fatalf("build client: %v", err)
	}
	cc, err := NewGeminiCustomerSalesAdapter(client, nil)
	if err != nil {
		t.Fatalf("build contract client: %v", err)
	}

	out, err := cc.Decide(context.Background(), ports.CustomerSalesDecisionInput{
		Request: ports.CustomerSalesDecisionRequest{
			BusinessID:     "test-biz",
			ConversationID: "test-conv",
			Text:           "Hello, please say 'ok'.",
		},
	})
	if err != nil {
		// Per the spec: "إذا GEMINI_API_KEY موجود ثم Gemini يرجع error: FAIL"
		// A real API error (auth, geo-block, rate limit, etc.) MUST
		// be a FAIL — not a Skip. Hiding it would make the CI green
		// when Gemini is actually broken.
		t.Fatalf("Gemini API returned error: %v", err)
	}

	if out.Proposal.ResponseText == "" {
		t.Errorf("expected non-empty response from Gemini")
	}
	if out.Usage.InputTokens <= 0 {
		t.Errorf("expected positive input tokens, got %d", out.Usage.InputTokens)
	}
	t.Logf("Gemini live smoke: model=%s input=%d output=%d latency=%dms response=%q",
		out.Usage.Model, out.Usage.InputTokens, out.Usage.OutputTokens, out.LatencyMs, out.Proposal.ResponseText)
}
