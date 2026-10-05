// Package gemini — Batch Gemini Client implementation (contract ② §5-6).
//
// Implements the services.BatchGeminiClient interface against the Gemini
// generateContent API with Structured Outputs (responseSchema) enforcement.
//
// Per contract ② §5, Gemini returns ONLY candidates (item_id, variant_ids,
// offer_ids, reason) — not the items back. Mujeeb already knows what it sent.
//
// Per contract ② §6, after all batches complete, a Final Gemini Evaluation
// runs over the aggregated candidate set + customer message + context.
//
// Per contract ② §8, batches are NOT chained via previous_interaction_id;
// each batch is an independent stateless generateContent request.
//
// Per contract ④ §8, Structured Outputs enforces the JSON shape via
// responseSchema; Mujeeb additionally validates the values per contract ⑥ §3.

package gemini

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/Ammar777782439/mujeeb24-backend-go/internal/application/ports"
	"github.com/Ammar777782439/mujeeb24-backend-go/internal/application/services"
	"github.com/Ammar777782439/mujeeb24-backend-go/internal/domain/ai/prompts"
)

// BatchClientConfig configures the BatchClient.
type BatchClientConfig struct {
	BaseURL        string
	APIKey         string
	Model          string
	HTTPClient     *http.Client
	RequestTimeout time.Duration
	SystemPrompt   string
}

// BatchClient implements services.BatchGeminiClient via the Gemini
// generateContent API with Structured Outputs.
type BatchClient struct {
	baseURL        string
	apiKey         string
	model          string
	httpClient     *http.Client
	requestTimeout time.Duration
	systemPrompt   string
	// configProvider, when set, is called at the start of every batch call
	// to get the ACTIVE runtime configuration. Per §1-2: the runtime gets
	// the active config from Configuration abstraction, not from static env.
	configProvider ports.AIConfigurationProvider
}

// SetConfigurationProvider wires the dynamic AIConfigurationProvider.
func (c *BatchClient) SetConfigurationProvider(provider ports.AIConfigurationProvider) {
	c.configProvider = provider
}

// resolveConfig returns the effective AI config for this batch call.
func (c *BatchClient) resolveConfig(ctx context.Context) (*resolvedAIConfig, error) {
	if c.configProvider != nil {
		cfg, err := c.configProvider.GetActiveConfig(ctx)
		if err != nil {
			return nil, fmt.Errorf("resolve AI config: %w", err)
		}
		return &resolvedAIConfig{
			apiKey:             cfg.APIKey,
			model:              cfg.Model,
			baseURL:            cfg.BaseURL,
			maxOutputTokens:    cfg.MaxOutputTokens,
			maxInputCharacters: cfg.MaxInputCharacters,
		}, nil
	}
	return &resolvedAIConfig{
		apiKey:  c.apiKey,
		model:   c.model,
		baseURL: c.baseURL,
	}, nil
}

// NewBatchClient wires the BatchClient with the Gemini API credentials.
func NewBatchClient(cfg BatchClientConfig) (*BatchClient, error) {
	baseURL := strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	if baseURL == "" {
		baseURL = defaultBaseURL
	}
	if strings.TrimSpace(cfg.APIKey) == "" {
		return nil, errors.New("gemini API key is required for batch evaluation")
	}
	model := strings.TrimSpace(cfg.Model)
	if model == "" {
		model = defaultModel
	}
	timeout := cfg.RequestTimeout
	if timeout <= 0 {
		timeout = 60 * time.Second
	}
	client := cfg.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: timeout}
	}
	systemPrompt := strings.TrimSpace(cfg.SystemPrompt)
	if systemPrompt == "" {
		systemPrompt = prompts.BatchEvaluationSystemPrompt
	}
	return &BatchClient{
		baseURL:        baseURL,
		apiKey:         cfg.APIKey,
		model:          model,
		httpClient:     client,
		requestTimeout: timeout,
		systemPrompt:   systemPrompt,
	}, nil
}

// buildBatchSystemInstruction builds the Gemini system_instruction content
// combining the Batch Evaluation system prompt + the Catalog Entity Contract
// JSON per contract ⑤ §7. Mirrors the customer-sales system-instruction contract
// — same authoritative ordering: part 0 = system prompt, part 1 = Catalog
// Entity Contract. Per ADR-047: before this fix, BatchClient ignored the
// EntityContract field available in BatchEvaluationInput, causing Gemini to
// evaluate catalog batches without authoritative field/type/enum definitions.
func (c *BatchClient) buildBatchSystemInstruction(entityContract services.CatalogEntityContractPayload) *batchContent {
	parts := []batchPart{{Text: c.systemPrompt}}
	contractJSON, err := json.Marshal(entityContract)
	if err == nil && len(contractJSON) > 0 {
		parts = append(parts, batchPart{
			Text: "\n\n# Catalog Entity Contract (contract ⑤ §7)\n\n" + string(contractJSON),
		})
	}
	return &batchContent{Role: "system", Parts: parts}
}

// buildBatchSystemInstructionWithSuffix is the same as buildBatchSystemInstruction
// but appends a suffix to the system prompt (used by FinalEvaluateWithDetails
// for the FINAL EVALUATION mode marker). The Catalog Entity Contract is always
// injected as part 1, same as the non-suffix variant.
func (c *BatchClient) buildBatchSystemInstructionWithSuffix(entityContract services.CatalogEntityContractPayload, suffix string) *batchContent {
	parts := []batchPart{{Text: c.systemPrompt + suffix}}
	contractJSON, err := json.Marshal(entityContract)
	if err == nil && len(contractJSON) > 0 {
		parts = append(parts, batchPart{
			Text: "\n\n# Catalog Entity Contract (contract ⑤ §7)\n\n" + string(contractJSON),
		})
	}
	return &batchContent{Role: "system", Parts: parts}
}

// defaultBatchSystemPrompt moved to internal/domain/ai/prompts/prompts.go
// (Day 5 Gap #13 — versioned system prompts as reviewable assets).

func (c *BatchClient) buildEvaluateBatchRequest(input services.BatchEvaluationInput, rc *resolvedAIConfig) (batchGeminiRequest, error) {
	batchJSON, err := json.Marshal(input.Batch)
	if err != nil {
		return batchGeminiRequest{}, fmt.Errorf("marshal batch payload: %w", err)
	}
	contextJSON, err := json.Marshal(catalogBatchPromptContextFrom(&input.ConversationContext))
	if err != nil {
		return batchGeminiRequest{}, fmt.Errorf("marshal conversation context for batch: %w", err)
	}
	phaseLabel := "Catalog batch"
	systemInstruction := c.buildBatchSystemInstruction(input.EntityContract)
	if input.Reduction {
		phaseLabel = "Candidate reduction batch"
		systemInstruction = c.buildBatchSystemInstructionWithSuffix(input.EntityContract, prompts.CandidateReductionSystemPromptSuffix)
	}
	userPrompt := fmt.Sprintf("Customer message: %s\n\nVerified conversation context:\n%s\n\n%s %d data:\n%s",
		input.CustomerMessage, string(contextJSON), phaseLabel, input.BatchNumber, string(batchJSON))
	return batchGeminiRequest{
		SystemInstruction: systemInstruction,
		Contents: []batchContent{
			{Role: "user", Parts: []batchPart{{Text: userPrompt}}},
		},
		GenerationConfig: batchGenerationConfig{
			ResponseMimeType: "application/json",
			ResponseSchema:   batchCandidateResponseSchema(),
			MaxOutputTokens:  rc.maxOutputTokens,
		},
	}, nil
}

// CountBatchTokens counts the exact generateContent request that EvaluateBatch
// will send. Google countTokens accepts generateContentRequest, so system
// instructions, entity contract, batch JSON and response configuration stay
// aligned with the real provider request.
func (c *BatchClient) CountBatchTokens(ctx context.Context, input services.BatchEvaluationInput) (int, error) {
	if c == nil {
		return 0, errors.New("batch client is not configured")
	}
	rc, err := c.resolveConfig(ctx)
	if err != nil {
		return 0, err
	}
	reqBody, err := c.buildEvaluateBatchRequest(input, rc)
	if err != nil {
		return 0, err
	}
	wrapper := struct {
		GenerateContentRequest batchGeminiRequest `json:"generateContentRequest"`
	}{GenerateContentRequest: reqBody}
	buf, err := json.Marshal(wrapper)
	if err != nil {
		return 0, fmt.Errorf("marshal exact countTokens request: %w", err)
	}

	url := fmt.Sprintf("%s/v1beta/models/%s:countTokens", rc.baseURL, rc.model)
	reqCtx, cancel := context.WithTimeout(ctx, c.requestTimeout)
	defer cancel()
	httpReq, err := http.NewRequestWithContext(reqCtx, http.MethodPost, url, bytes.NewReader(buf))
	if err != nil {
		return 0, fmt.Errorf("build exact countTokens request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-goog-api-key", rc.apiKey)
	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return 0, fmt.Errorf("send exact countTokens request: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	if err != nil {
		return 0, fmt.Errorf("read exact countTokens response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return 0, fmt.Errorf("exact countTokens status %d: %s", resp.StatusCode, string(body))
	}
	var out struct {
		TotalTokens int `json:"totalTokens"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return 0, fmt.Errorf("unmarshal exact countTokens response: %w", err)
	}
	return out.TotalTokens, nil
}

// EvaluateBatch implements services.BatchGeminiClient.EvaluateBatch per
// contract ② §5. Sends one batch to Gemini and returns the candidate set.
//
// Per contract ② §8, each batch is an independent stateless generateContent request — no
// previous_interaction_id chaining.
//
// Per contract ④ §8, Structured Outputs enforces the response shape via
// responseSchema. Per contract ⑥ §3, Mujeeb additionally validates the
// returned IDs against the actual batch items (the caller does this via
// PostgresReferenceValidator).
func (c *BatchClient) EvaluateBatch(ctx context.Context, input services.BatchEvaluationInput) (ports.CatalogBatchResult, error) {
	if c == nil {
		return ports.CatalogBatchResult{}, errors.New("batch client is not configured")
	}
	if len(input.Batch.Items) == 0 {
		return ports.CatalogBatchResult{BatchNumber: input.BatchNumber}, nil
	}
	// Per §1-2: resolve the ACTIVE runtime configuration.
	rc, err := c.resolveConfig(ctx)
	if err != nil {
		return ports.CatalogBatchResult{}, err
	}

	reqBody, err := c.buildEvaluateBatchRequest(input, rc)
	if err != nil {
		return ports.CatalogBatchResult{}, err
	}
	// Per P2-13: measure the wall-clock duration of the Gemini batch
	// call so the usage record carries a real latency (instead of
	// deriving it from EstimatedCostMicros which is always 0).
	sendBatchStart := time.Now()
	resp, err := c.sendRequestWithConfig(ctx, reqBody, rc)
	sendBatchEnd := time.Now()
	if err != nil {
		return ports.CatalogBatchResult{}, fmt.Errorf("evaluate batch %d: %w", input.BatchNumber, err)
	}
	candidates, err := parseBatchCandidates(resp)
	if err != nil {
		return ports.CatalogBatchResult{}, fmt.Errorf("parse batch %d candidates: %w", input.BatchNumber, err)
	}
	latencyMs := int64(sendBatchEnd.Sub(sendBatchStart) / time.Millisecond)
	return ports.CatalogBatchResult{
		BatchNumber: input.BatchNumber,
		Candidates:  candidates,
		Usage: ports.CustomerSalesUsageTelemetry{
			InputTokens:  resp.UsageMetadata.PromptTokenCount,
			CachedTokens: resp.UsageMetadata.CachedContentTokenCount,
			OutputTokens: resp.UsageMetadata.CandidatesTokenCount,
			Model:        rc.model,
			LatencyMs:    latencyMs,
		},
		LatencyMs: latencyMs,
	}, nil
}

// FinalEvaluate implements services.BatchGeminiClient.FinalEvaluate per
// contract ② §6. Runs the final evaluation over the aggregated candidate
// set + customer message + conversation context.
//
// Per contract ② §6: "الـFinal Gemini لا يحتاج أن يرى الـ1000 منتج مرة
// أخرى. يرى: Customer Message + Conversation Context + Candidate Results +
// الدليل التجاري المرتبط بالمرشحين. ثم يقوم بالقرار النهائي وصياغة الرد."
//
// Per contract ④ §4, the final output is an CustomerSalesProposal (status +
// action + response_text + selected[]).
// FinalEvaluateWithDetails runs the contract ② §6 final evaluation with
// a custom user prompt that includes BOTH candidate IDs AND full product
// details (names, prices, descriptions, variants, offers).
//
// Per contract ② §6: "يرى: Customer Message + Conversation Context +
// Candidate Results + الدليل التجاري المرتبط بالمرشحين"
//
// The "الدليل التجاري المرتبط بالمرشحين" = full product details for each
// candidate item. Without this, Gemini only sees IDs and can't compose
// a response with product names, prices, descriptions.
func (c *BatchClient) buildFinalEvaluateRequest(input services.FinalEvaluationInput, userPrompt string, rc *resolvedAIConfig) batchGeminiRequest {
	return batchGeminiRequest{
		SystemInstruction: c.buildBatchSystemInstructionWithSuffix(input.EntityContract, prompts.FinalEvaluationSystemPromptSuffix),
		Contents: []batchContent{
			{Role: "user", Parts: []batchPart{{Text: userPrompt}}},
		},
		GenerationConfig: batchGenerationConfig{
			ResponseMimeType: "application/json",
			ResponseSchema:   finalProposalResponseSchema(),
			MaxOutputTokens:  rc.maxOutputTokens,
		},
	}
}

func (c *BatchClient) CountFinalTokens(ctx context.Context, input services.FinalEvaluationInput, userPrompt string) (int, error) {
	if c == nil {
		return 0, errors.New("batch client is not configured")
	}
	rc, err := c.resolveConfig(ctx)
	if err != nil {
		return 0, err
	}
	reqBody := c.buildFinalEvaluateRequest(input, userPrompt, rc)
	wrapper := struct {
		GenerateContentRequest batchGeminiRequest `json:"generateContentRequest"`
	}{GenerateContentRequest: reqBody}
	buf, err := json.Marshal(wrapper)
	if err != nil {
		return 0, fmt.Errorf("marshal final countTokens request: %w", err)
	}
	url := fmt.Sprintf("%s/v1beta/models/%s:countTokens", rc.baseURL, rc.model)
	reqCtx, cancel := context.WithTimeout(ctx, c.requestTimeout)
	defer cancel()
	httpReq, err := http.NewRequestWithContext(reqCtx, http.MethodPost, url, bytes.NewReader(buf))
	if err != nil {
		return 0, fmt.Errorf("build final countTokens request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-goog-api-key", rc.apiKey)
	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return 0, fmt.Errorf("send final countTokens request: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	if err != nil {
		return 0, fmt.Errorf("read final countTokens response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return 0, fmt.Errorf("final countTokens status %d: %s", resp.StatusCode, string(body))
	}
	var out struct {
		TotalTokens int `json:"totalTokens"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return 0, fmt.Errorf("unmarshal final countTokens response: %w", err)
	}
	return out.TotalTokens, nil
}

func (c *BatchClient) FinalEvaluateWithDetails(ctx context.Context, input services.FinalEvaluationInput, userPrompt string) (ports.CustomerSalesProposal, ports.CustomerSalesUsageTelemetry, error) {
	if c == nil {
		return ports.CustomerSalesProposal{}, ports.CustomerSalesUsageTelemetry{}, errors.New("batch client is not configured")
	}
	rc, err := c.resolveConfig(ctx)
	if err != nil {
		return ports.CustomerSalesProposal{}, ports.CustomerSalesUsageTelemetry{}, err
	}
	reqBody := c.buildFinalEvaluateRequest(input, userPrompt, rc)

	finalStart := time.Now()
	resp, err := c.sendRequestWithConfig(ctx, reqBody, rc)
	finalEnd := time.Now()
	if err != nil {
		return ports.CustomerSalesProposal{}, ports.CustomerSalesUsageTelemetry{}, fmt.Errorf("final evaluate: %w", err)
	}
	proposal, err := parseFinalProposal(resp)
	if err != nil {
		return ports.CustomerSalesProposal{}, ports.CustomerSalesUsageTelemetry{}, fmt.Errorf("parse final proposal: %w", err)
	}
	usage := ports.CustomerSalesUsageTelemetry{
		InputTokens:  resp.UsageMetadata.PromptTokenCount,
		CachedTokens: resp.UsageMetadata.CachedContentTokenCount,
		OutputTokens: resp.UsageMetadata.CandidatesTokenCount,
		Model:        rc.model,
		LatencyMs:    int64(finalEnd.Sub(finalStart) / time.Millisecond),
	}
	return proposal, usage, nil
}

// FinalEvaluate is kept for backward compatibility but delegates to
// FinalEvaluateWithDetails with a basic prompt.
func (c *BatchClient) FinalEvaluate(ctx context.Context, input services.FinalEvaluationInput) (ports.CustomerSalesProposal, error) {
	candidatesJSON, _ := json.Marshal(input.CandidateResults)
	userPrompt := fmt.Sprintf("Customer message: %s\n\nAggregated candidate set from catalog evaluation:\n%s\n\nBased on the candidates above, produce your final proposal.",
		input.CustomerMessage, string(candidatesJSON))
	proposal, _, err := c.FinalEvaluateWithDetails(ctx, input, userPrompt)
	return proposal, err
}

// sendRequest is the HTTP call to the Gemini generateContent API.
// sendRequestWithConfig is the dynamic-config version of sendRequest.
// Per §1-2: uses the resolved config (apiKey, model, baseURL) from the
// AIConfigurationProvider instead of static struct fields.
func (c *BatchClient) sendRequestWithConfig(ctx context.Context, reqBody batchGeminiRequest, rc *resolvedAIConfig) (batchGeminiResponse, error) {
	buf, err := json.Marshal(reqBody)
	if err != nil {
		return batchGeminiResponse{}, fmt.Errorf("marshal request: %w", err)
	}
	// P1-8: API key sent via x-goog-api-key header only — never in URL.
	url := fmt.Sprintf("%s/v1beta/models/%s:generateContent", rc.baseURL, rc.model)
	reqCtx, cancel := context.WithTimeout(ctx, c.requestTimeout)
	defer cancel()
	httpReq, err := http.NewRequestWithContext(reqCtx, http.MethodPost, url, bytes.NewReader(buf))
	if err != nil {
		return batchGeminiResponse{}, fmt.Errorf("build request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-goog-api-key", rc.apiKey)
	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return batchGeminiResponse{}, fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return batchGeminiResponse{}, fmt.Errorf("read response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return batchGeminiResponse{}, fmt.Errorf("gemini api status %d: %s", resp.StatusCode, string(body))
	}
	var geminiResp batchGeminiResponse
	if err := json.Unmarshal(body, &geminiResp); err != nil {
		return batchGeminiResponse{}, fmt.Errorf("unmarshal response: %w", err)
	}
	return geminiResp, nil
}

// sendRequest is the legacy version that uses static struct fields.
// Kept for backward compatibility with tests that don't wire a configProvider.
func (c *BatchClient) sendRequest(ctx context.Context, reqBody batchGeminiRequest) (batchGeminiResponse, error) {
	buf, err := json.Marshal(reqBody)
	if err != nil {
		return batchGeminiResponse{}, fmt.Errorf("marshal request: %w", err)
	}

	// P1-8: API key sent via x-goog-api-key header only — never in URL.
	url := fmt.Sprintf("%s/v1beta/models/%s:generateContent", c.baseURL, c.model)

	reqCtx, cancel := context.WithTimeout(ctx, c.requestTimeout)
	defer cancel()

	httpReq, err := http.NewRequestWithContext(reqCtx, http.MethodPost, url, bytes.NewReader(buf))
	if err != nil {
		return batchGeminiResponse{}, fmt.Errorf("build request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-goog-api-key", c.apiKey)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return batchGeminiResponse{}, fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return batchGeminiResponse{}, fmt.Errorf("read response: %w", err)
	}
	if resp.StatusCode >= 400 {
		return batchGeminiResponse{}, fmt.Errorf("gemini http %d: %s", resp.StatusCode, string(body))
	}

	var out batchGeminiResponse
	if err := json.Unmarshal(body, &out); err != nil {
		return batchGeminiResponse{}, fmt.Errorf("unmarshal response: %w", err)
	}
	return out, nil
}

// parseBatchCandidates extracts the candidates array from the structured
// output per contract ② §5.
func parseBatchCandidates(resp batchGeminiResponse) ([]ports.CatalogBatchCandidate, error) {
	if len(resp.Candidates) == 0 {
		return nil, errors.New("no candidates in gemini response per contract ② §5")
	}
	candidate := resp.Candidates[0]
	if len(candidate.Content.Parts) == 0 {
		return nil, errors.New("no content parts in gemini response per contract ② §5")
	}
	raw := candidate.Content.Parts[0].Text
	if strings.TrimSpace(raw) == "" {
		// Empty response = no candidates in this batch (valid per contract ② §5).
		return nil, nil
	}
	// The structured output is a JSON object with a "candidates" array.
	var wrapper struct {
		Candidates []ports.CatalogBatchCandidate `json:"candidates"`
	}
	if err := decodeStrictStructuredJSON([]byte(raw), &wrapper); err != nil {
		return nil, fmt.Errorf("decode candidates wrapper: %w", err)
	}
	return wrapper.Candidates, nil
}

// parseFinalProposal extracts the CustomerSalesProposal from the structured
// output per contract ④ §4.
func parseFinalProposal(resp batchGeminiResponse) (ports.CustomerSalesProposal, error) {
	if len(resp.Candidates) == 0 {
		return ports.CustomerSalesProposal{}, errors.New("no candidates in gemini response per contract ④ §4")
	}
	candidate := resp.Candidates[0]
	if len(candidate.Content.Parts) == 0 {
		return ports.CustomerSalesProposal{}, errors.New("no content parts in gemini response per contract ④ §4")
	}
	raw := candidate.Content.Parts[0].Text
	if strings.TrimSpace(raw) == "" {
		return ports.CustomerSalesProposal{}, errors.New("empty structured output text per contract ④ §4")
	}
	var proposal ports.CustomerSalesProposal
	if err := decodeStrictStructuredJSON([]byte(raw), &proposal); err != nil {
		return ports.CustomerSalesProposal{}, fmt.Errorf("decode final proposal: %w", err)
	}
	return proposal, nil
}

// batchCandidateResponseSchema is the JSON Schema that enforces the contract
// ② §5 batch evaluation output shape via Gemini's responseSchema field.
//
// Per contract ② §5, the output is an object with a "candidates" array.
// Each candidate has: item_id (string), variant_ids (array of string),
// offer_ids (array of string), reason (string).
func batchCandidateResponseSchema() map[string]any {
	return map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"properties": map[string]any{
			"candidates": map[string]any{
				"type": "array",
				"items": map[string]any{
					"type":                 "object",
					"additionalProperties": false,
					"properties": map[string]any{
						"item_id":     map[string]any{"type": "string"},
						"variant_ids": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
						"offer_ids":   map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
						"reason":      map[string]any{"type": "string"},
					},
					"required": []string{"item_id"},
				},
			},
		},
		"required": []string{"candidates"},
	}
}

// finalProposalResponseSchema is the JSON Schema that enforces the contract
// ④ §4 final proposal output shape (same as the regular proposal schema).
func finalProposalResponseSchema() map[string]any {
	return contractProposalResponseSchema()
}

// batchGeminiRequest is the generateContent request body.
type batchGeminiRequest struct {
	SystemInstruction *batchContent         `json:"systemInstruction,omitempty"`
	Contents          []batchContent        `json:"contents"`
	GenerationConfig  batchGenerationConfig `json:"generationConfig"`
}

type batchGenerationConfig struct {
	ResponseMimeType string         `json:"responseMimeType,omitempty"`
	ResponseSchema   map[string]any `json:"responseSchema,omitempty"`
	MaxOutputTokens  int            `json:"maxOutputTokens,omitempty"`
}

type batchContent struct {
	Role  string      `json:"role,omitempty"`
	Parts []batchPart `json:"parts"`
}

type batchPart struct {
	Text string `json:"text,omitempty"`
}

type batchGeminiResponse struct {
	Candidates    []batchCandidate   `json:"candidates"`
	UsageMetadata batchUsageMetadata `json:"usageMetadata"`
}

type batchCandidate struct {
	Content batchContent `json:"content"`
}

type batchUsageMetadata struct {
	PromptTokenCount        int `json:"promptTokenCount,omitempty"`
	CandidatesTokenCount    int `json:"candidatesTokenCount,omitempty"`
	CachedContentTokenCount int `json:"cachedContentTokenCount,omitempty"`
	TotalTokenCount         int `json:"totalTokenCount,omitempty"`
}

// Compile-time assertion: BatchClient implements services.BatchGeminiClient.
var _ services.BatchGeminiClient = (*BatchClient)(nil)
