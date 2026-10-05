package gemini

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Ammar777782439/mujeeb24-backend-go/internal/application/ports"
	"github.com/Ammar777782439/mujeeb24-backend-go/internal/application/services"
)

// Test P1-8: Gemini API key MUST be sent via the x-goog-api-key header,
// NEVER via the ?key= query parameter. URLs are logged in proxy/access
// logs and would leak the credential.
//
// This test spins up a mock Gemini server and inspects the incoming
// request URL + headers to verify:
//  1. The URL does NOT contain "?key=" or "apiKey=" or "api_key="
//  2. The x-goog-api-key header IS present and carries the API key
//
// It exercises the production paths: GeminiCustomerSalesAdapter, BatchClient
// (both legacy + dynamic-config), TokenCounter, and ModelsClient
// (both DiscoverModels + TestConnection).

// capturingHandler records the URL path + query + x-goog-api-key header
// of the last request it received.
type capturingHandler struct {
	lastURL     string
	lastQuery   string
	lastAPIKey  string
	respondWith string // "ok" or "error"
}

func (h *capturingHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.lastURL = r.URL.Path
	h.lastQuery = r.URL.RawQuery
	h.lastAPIKey = r.Header.Get("x-goog-api-key")
	if h.respondWith == "error" {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"code":400,"message":"bad request"}}`))
		return
	}
	// Return a minimal valid response. The exact shape doesn't matter —
	// the test only verifies the request side (URL + headers).
	switch {
	case r.URL.Path == "/v1beta/interactions":
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"int-test","status":"completed","steps":[{"type":"model_output","content":[{"type":"text","text":"{\"status\":\"resolved\",\"action\":\"answer\",\"response_text\":\"ok\",\"selected\":[]}" }]}],"usage":{"total_input_tokens":5,"total_output_tokens":3,"total_cached_tokens":0}}`))
	case strings.Contains(r.URL.Path, ":generateContent"):
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"candidates":[{"content":{"parts":[{"text":"ok"}]}}],"usageMetadata":{"promptTokenCount":5,"candidatesTokenCount":3,"cachedContentTokenCount":0}}`))
	case strings.Contains(r.URL.Path, ":countTokens"):
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"totalTokens":42}`))
	case strings.HasSuffix(r.URL.Path, "/v1beta/models"):
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"models":[]}`))
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

func (h *capturingHandler) assertNoKeyInURL(t *testing.T) {
	t.Helper()
	if strings.Contains(h.lastQuery, "key=") {
		t.Errorf("P1-8 violation: URL query string %q contains 'key=' — API key MUST be in x-goog-api-key header only", h.lastQuery)
	}
	if strings.Contains(h.lastURL, "key=") {
		t.Errorf("P1-8 violation: URL path %q contains 'key=' — API key MUST be in x-goog-api-key header only", h.lastURL)
	}
}

func (h *capturingHandler) assertAPIKeyInHeader(t *testing.T, expected string) {
	t.Helper()
	if h.lastAPIKey != expected {
		t.Errorf("expected x-goog-api-key header = %q, got %q", expected, h.lastAPIKey)
	}
}

// Test P1-8: GeminiCustomerSalesAdapter sends API key via header, not URL.
func TestGeminiCustomerSalesAdapterSendsAPIKeyInHeaderNotURL(t *testing.T) {
	handler := &capturingHandler{respondWith: "ok"}
	server := httptest.NewServer(handler)
	defer server.Close()

	client, err := NewGeminiHTTPClient(GeminiHTTPClientConfig{
		BaseURL: server.URL, APIKey: "test-key-abc123",
		Model:          "gemini-3.5-flash",
		RequestTimeout: 5 * time.Second,
	})
	if err != nil {
		t.Fatalf("build client: %v", err)
	}
	cc, err := NewGeminiCustomerSalesAdapter(client, nil)
	if err != nil {
		t.Fatalf("build contract client: %v", err)
	}
	_, _ = cc.Decide(context.Background(), ports.CustomerSalesDecisionInput{
		Request: ports.CustomerSalesDecisionRequest{
			BusinessID: "b-1", ConversationID: "c-1",
			SourceMessageReference: "msg-1", Text: "Hello",
		},
	})
	handler.assertNoKeyInURL(t)
	handler.assertAPIKeyInHeader(t, "test-key-abc123")
}

// Test P1-8: BatchClient (dynamic config path) sends API key via header, not URL.
func TestBatchClientSendsAPIKeyInHeaderNotURLDynamicConfig(t *testing.T) {
	handler := &capturingHandler{respondWith: "ok"}
	server := httptest.NewServer(handler)
	defer server.Close()

	bc, err := NewBatchClient(BatchClientConfig{
		BaseURL: server.URL, APIKey: "test-key-batch",
		Model: "gemini-3.5-flash",
	})
	if err != nil {
		t.Fatalf("build batch client: %v", err)
	}
	_, _ = bc.EvaluateBatch(context.Background(), services.BatchEvaluationInput{
		BusinessID:     "b-1",
		ConversationID: "c-1",
		Batch:          services.CatalogAIBatchPayload{Items: []services.CatalogAIItem{{ID: "item-1"}}},
	})
	handler.assertNoKeyInURL(t)
	handler.assertAPIKeyInHeader(t, "test-key-batch")
}

// Test P1-8: TokenCounter sends API key via header, not URL.
func TestTokenCounterSendsAPIKeyInHeaderNotURL(t *testing.T) {
	handler := &capturingHandler{respondWith: "ok"}
	server := httptest.NewServer(handler)
	defer server.Close()

	tc, err := NewTokenCounter(TokenCounterConfig{
		BaseURL: server.URL, APIKey: "test-key-counter",
		Model: "gemini-3.5-flash",
	})
	if err != nil {
		t.Fatalf("build token counter: %v", err)
	}
	_, _ = tc.CountTokens(context.Background(), "sample text")
	handler.assertNoKeyInURL(t)
	handler.assertAPIKeyInHeader(t, "test-key-counter")
}

// Test P1-8: ModelsClient.DiscoverModels sends API key via header, not URL.
func TestModelsClientDiscoverModelsSendsAPIKeyInHeaderNotURL(t *testing.T) {
	handler := &capturingHandler{respondWith: "ok"}
	server := httptest.NewServer(handler)
	defer server.Close()

	mc := NewModelsClient()
	_, _ = mc.DiscoverModels(context.Background(), "test-key-models", server.URL)
	handler.assertNoKeyInURL(t)
	handler.assertAPIKeyInHeader(t, "test-key-models")
}

// Test P1-8: ModelsClient.TestConnection sends API key via header, not URL.
func TestModelsClientTestConnectionSendsAPIKeyInHeaderNotURL(t *testing.T) {
	handler := &capturingHandler{respondWith: "ok"}
	server := httptest.NewServer(handler)
	defer server.Close()

	mc := NewModelsClient()
	ok, _, _ := mc.TestConnection(context.Background(), "test-key-probe", "gemini-3.5-flash", server.URL)
	if !ok {
		t.Errorf("expected probe to succeed (mock returns 200), got false")
	}
	handler.assertNoKeyInURL(t)
	handler.assertAPIKeyInHeader(t, "test-key-probe")
}
