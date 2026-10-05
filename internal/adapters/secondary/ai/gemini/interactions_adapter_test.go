package gemini

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Ammar777782439/mujeeb24-backend-go/internal/application/ports"
)

func TestCustomerSalesUsesInteractionsAPIAndChainsState(t *testing.T) {
	var captured map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1beta/interactions" {
			t.Fatalf("expected /v1beta/interactions, got %s", r.URL.Path)
		}
		if r.Header.Get("x-goog-api-key") != "test-key" {
			t.Fatalf("missing x-goog-api-key header")
		}
		if err := json.NewDecoder(r.Body).Decode(&captured); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"id":"int-2",
			"status":"completed",
			"steps":[{"type":"model_output","content":[{"type":"text","text":"{\"status\":\"resolved\",\"action\":\"answer\",\"response_text\":\"تم\",\"selected\":[]}" }]}],
			"usage":{"total_input_tokens":21,"total_cached_tokens":4,"total_output_tokens":7,"total_tokens":28}
		}`))
	}))
	defer server.Close()

	client, err := NewGeminiHTTPClient(GeminiHTTPClientConfig{
		BaseURL: server.URL,
		APIKey: "test-key",
		Model: "gemini-test",
		RequestTimeout: 5 * time.Second,
	})
	if err != nil {
		t.Fatalf("build client: %v", err)
	}
	adapter, err := NewGeminiCustomerSalesAdapter(client, nil)
	if err != nil {
		t.Fatalf("build adapter: %v", err)
	}

	out, err := adapter.Decide(context.Background(), ports.CustomerSalesDecisionInput{
		Request: ports.CustomerSalesDecisionRequest{
			BusinessID: "business-1",
			ConversationID: "conversation-1",
			Text: "مرحبا",
		},
		GeminiInteraction: ports.GeminiInteractionContext{
			PreviousInteractionID: "int-1",
			Store: true,
		},
		EntityContractPayload: []byte(`{"entity_contract":{"catalog":{"id":"UUID"}}}`),
	})
	if err != nil {
		t.Fatalf("Decide: %v", err)
	}

	if captured["model"] != "gemini-test" {
		t.Fatalf("model mismatch: %#v", captured["model"])
	}
	if captured["previous_interaction_id"] != "int-1" {
		t.Fatalf("previous_interaction_id mismatch: %#v", captured["previous_interaction_id"])
	}
	if captured["store"] != true {
		t.Fatalf("store must be true")
	}
	if _, ok := captured["contents"]; ok {
		t.Fatal("Interactions request must not use generateContent contents")
	}
	if _, ok := captured["generationConfig"]; ok {
		t.Fatal("Interactions request must use generation_config snake_case")
	}
	responseFormat, ok := captured["response_format"].(map[string]any)
	if !ok || responseFormat["type"] != "text" || responseFormat["mime_type"] != "application/json" {
		t.Fatalf("invalid response_format: %#v", captured["response_format"])
	}
	if _, ok := responseFormat["schema"].(map[string]any); !ok {
		t.Fatalf("response_format.schema missing")
	}
	if out.GeminiInteraction.ResultingInteractionID != "int-2" {
		t.Fatalf("resulting interaction id = %q", out.GeminiInteraction.ResultingInteractionID)
	}
	if out.Usage.InputTokens != 21 || out.Usage.CachedTokens != 4 || out.Usage.OutputTokens != 7 {
		t.Fatalf("usage mismatch: %+v", out.Usage)
	}
	if out.Proposal.ResponseText != "تم" {
		t.Fatalf("proposal response = %q", out.Proposal.ResponseText)
	}
}

func TestCustomerSalesDropsPreviousInteractionWhenStoreDisabled(t *testing.T) {
	var captured map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&captured); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		_, _ = w.Write([]byte(`{"id":"int-new","status":"completed","steps":[{"type":"model_output","content":[{"type":"text","text":"{\"status\":\"resolved\",\"action\":\"answer\",\"response_text\":\"ok\"}"}]}]}`))
	}))
	defer server.Close()

	client, _ := NewGeminiHTTPClient(GeminiHTTPClientConfig{BaseURL: server.URL, APIKey: "k", Model: "m"})
	adapter, _ := NewGeminiCustomerSalesAdapter(client, nil)
	_, err := adapter.Decide(context.Background(), ports.CustomerSalesDecisionInput{
		Request: ports.CustomerSalesDecisionRequest{BusinessID: "b", ConversationID: "c", Text: "hello"},
		GeminiInteraction: ports.GeminiInteractionContext{PreviousInteractionID: "must-not-send", Store: false},
	})
	if err != nil {
		t.Fatalf("Decide: %v", err)
	}
	if _, ok := captured["previous_interaction_id"]; ok {
		t.Fatal("previous_interaction_id must be omitted when store=false")
	}
}

func TestCustomerSalesEnforcesMaxInputOnSerializedInteraction(t *testing.T) {
	called := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	client, err := NewGeminiHTTPClient(GeminiHTTPClientConfig{
		BaseURL: server.URL,
		APIKey: "test-key",
		Model: "gemini-test",
		MaxInputCharacters: 80,
		RequestTimeout: 5 * time.Second,
	})
	if err != nil {
		t.Fatalf("build client: %v", err)
	}
	adapter, err := NewGeminiCustomerSalesAdapter(client, nil)
	if err != nil {
		t.Fatalf("build adapter: %v", err)
	}

	_, err = adapter.Decide(context.Background(), ports.CustomerSalesDecisionInput{
		Request: ports.CustomerSalesDecisionRequest{
			BusinessID: "business-1",
			ConversationID: "conversation-1",
			Text: "hi",
		},
		EntityContractPayload: []byte(`{"entity_contract":{"catalog":{"description":"this makes the serialized system instruction intentionally larger than the configured limit"}}}`),
	})
	if err == nil {
		t.Fatal("expected serialized interaction input limit error")
	}
	if called {
		t.Fatal("HTTP request must not be sent when serialized interaction exceeds the configured input limit")
	}
}


func TestStrictCustomerProposalRejectsUnknownFields(t *testing.T) {
	var proposal ports.CustomerSalesProposal
	err := decodeStrictStructuredJSON([]byte(`{"status":"resolved","action":"answer","response_text":"ok","authorized":true}`), &proposal)
	if err == nil {
		t.Fatal("unknown top-level proposal field must be rejected")
	}
}

func TestStrictCustomerProposalRejectsUnknownSelectedFields(t *testing.T) {
	var proposal ports.CustomerSalesProposal
	err := decodeStrictStructuredJSON([]byte(`{"status":"resolved","action":"answer","response_text":"ok","selected":[{"item_id":"item-1","payment_confirmed":true}]}`), &proposal)
	if err == nil {
		t.Fatal("unknown selected[] field must be rejected")
	}
}

func TestCustomerProposalSchemaDisallowsAdditionalProperties(t *testing.T) {
	schema := contractProposalResponseSchema()
	if schema["additionalProperties"] != false {
		t.Fatalf("proposal schema must close top-level properties: %#v", schema["additionalProperties"])
	}
	selected := schema["properties"].(map[string]any)["selected"].(map[string]any)
	item := selected["items"].(map[string]any)
	if item["additionalProperties"] != false {
		t.Fatalf("selected item schema must close additional properties: %#v", item["additionalProperties"])
	}
}
