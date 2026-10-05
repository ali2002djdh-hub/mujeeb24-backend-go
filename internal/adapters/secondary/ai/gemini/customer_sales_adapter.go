// Package gemini — Contract-aligned Gemini Client.
//
// Implements ports.CustomerSalesDecisionPort, the contract ④ §8 mapping of Mujeeb
// Contract to Gemini API:
//   - Mujeeb System Contract → system_instruction
//   - Mujeeb Input Context → input (contents)
//   - Catalog boundary → Structured Output (responseSchema)
//   - Mujeeb Output Contract → CustomerSalesProposal
//
// Per contract ③ §4, supports Gemini Interactions API with previous_interaction_id
// chaining (store=true per contract ③ §9). Per contract ⑤ §7, includes the
// Catalog Entity Contract in system_instruction. Per contract ④ §4, enforces
// Structured Output via responseSchema. Per contract ⑧ §8, captures usage
// telemetry. Per contract ⑧ §9, captures latency.
//
// This adapter is the sole Gemini implementation of the customer-sales
// decision capability. No generic AI execution path is used.

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
	"github.com/Ammar777782439/mujeeb24-backend-go/internal/domain/ai/prompts"
)

// GeminiCustomerSalesAdapter is the contract ④ §8 implementation of ports.CustomerSalesDecisionPort.
//
// It wraps an existing Client to reuse HTTP machinery (base URL, API key,
// model, system prompt) but adds:
//   - previous_interaction_id chaining per contract ③ §4
//   - Catalog Entity Contract in system_instruction per contract ⑤ §7
//   - Structured Output enforcement for CustomerSalesProposal per contract ④ §4
//   - Usage telemetry capture for AI Trace per contract ⑧ §8
type GeminiCustomerSalesAdapter struct {
	base           *GeminiHTTPClient
	httpClient     *http.Client
	baseURL        string
	apiKey         string
	model          string
	requestTimeout time.Duration
	capabilities   ports.CustomerSalesToolPort
	// configProvider, when set, is called at the start of every Decide
	// call to get the ACTIVE runtime configuration (API key, model, limits).
	// Per §2: the runtime gets the active config from Configuration abstraction,
	// not from static env vars. If nil, falls back to static fields (bootstrap/tests).
	configProvider ports.AIConfigurationProvider
	// runRepo (optional) persists tool call records during the Function
	// Calling Tool Loop. Reuses the SAME AIRunRepository instance wired
	// into AutoReplyService — no second repository.
	runRepo ports.AIRunRepository
	// newID generates UUIDs for tool call records.
	newID func() string
	// lifecycle (optional) transitions RUNNING → WAITING_TOOL → RUNNING
	// during the Tool Loop. Per fix #2: uses the existing AIRunLifecycle
	// via the AIRunLifecyclePort abstraction (no services import).
	lifecycle ports.AIRunLifecyclePort
}

// resolvedAIConfig holds the effective values for one Gemini call.
type resolvedAIConfig struct {
	apiKey             string
	model              string
	baseURL            string
	maxOutputTokens    int
	maxInputCharacters int
}

// SetConfigurationProvider wires the dynamic AIConfigurationProvider.
// After this call, every Decide resolves the active config from
// the provider (cache/DB) instead of static struct fields. Per §9:
// cache invalidation makes new config active without restart.
func (c *GeminiCustomerSalesAdapter) SetConfigurationProvider(provider ports.AIConfigurationProvider) {
	c.configProvider = provider
}

// resolveConfig returns the effective AI configuration for this call.
// Per §1-2: if configProvider is wired, reads from cache/DB. Otherwise
// falls back to static fields (env bootstrap, tests).
func (c *GeminiCustomerSalesAdapter) resolveConfig(ctx context.Context) (*resolvedAIConfig, error) {
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
		apiKey:             c.apiKey,
		model:              c.model,
		baseURL:            c.baseURL,
		maxOutputTokens:    c.base.maxOutputTokens,
		maxInputCharacters: c.base.maxInputCharacters,
	}, nil
}

// NewGeminiCustomerSalesAdapter wraps an existing Client with contract-aligned methods.
//
// Per contract ⑤ §7, the Catalog Entity Contract is built once at startup
// and reused for every call; callers pass it via CustomerSalesDecisionInput.
func NewGeminiCustomerSalesAdapter(base *GeminiHTTPClient, capabilities ports.CustomerSalesToolPort) (*GeminiCustomerSalesAdapter, error) {
	if base == nil {
		return nil, errors.New("base client is required")
	}
	return &GeminiCustomerSalesAdapter{
		base:           base,
		httpClient:     base.httpClient,
		baseURL:        base.baseURL,
		apiKey:         base.apiKey,
		model:          base.model,
		requestTimeout: base.requestTimeout,
		capabilities:   capabilities,
	}, nil
}

// Decide is the contract ④ §8 method implementing ports.CustomerSalesDecisionPort.
//
// Per contract ③ §4, it carries previous_interaction_id chaining via input.GeminiInteraction.
// Per contract ⑤ §7, the Entity Contract is sent as part of system_instruction.
// Per contract ④ §4, Structured Output enforces the CustomerSalesProposal shape.
// Per contract ⑧ §8, usage telemetry is captured for AI Trace.
//
// Production Customer Sales uses the Interactions API without catalog tools.
// A generateContent function-calling compatibility path remains only for
// isolated legacy/tool-loop tests and non-production callers that explicitly
// inject a capability dispatcher.
//
// This method is the Customer Sales decision adapter; no generic AI runtime is used.
func (c *GeminiCustomerSalesAdapter) Decide(ctx context.Context, input ports.CustomerSalesDecisionInput) (ports.CustomerSalesDecisionOutput, error) {
	if err := ctx.Err(); err != nil {
		return ports.CustomerSalesDecisionOutput{}, err
	}
	if strings.TrimSpace(input.Request.Text) == "" {
		return ports.CustomerSalesDecisionOutput{}, errors.New("AI input text is required")
	}

	rc, err := c.resolveConfig(ctx)
	if err != nil {
		return ports.CustomerSalesDecisionOutput{}, err
	}
	if rc.maxInputCharacters > 0 && len([]rune(input.Request.Text)) > rc.maxInputCharacters {
		return ports.CustomerSalesDecisionOutput{}, fmt.Errorf("AI input text exceeds %d characters", rc.maxInputCharacters)
	}

	startedAt := time.Now().UTC()
	toolDecls := c.buildToolDeclarations()

	// Compatibility-only function-calling path. Customer Sales production does
	// not wire catalog tools; its catalog flow is manifest -> full catalog batch.
	// Keeping this path isolated avoids mixing Interactions and generateContent
	// request/response schemas.
	if len(toolDecls) > 0 {
		reqBody := contractGeminiRequest{
			Store:             input.GeminiInteraction.Store,
			SystemInstruction: c.buildContractSystemInstruction(input.EntityContractPayload),
			Contents:          c.buildContractContents(input.Request),
			GenerationConfig: contractGenerationConfig{
				ResponseMimeType: "application/json",
				ResponseSchema:   contractProposalResponseSchema(),
				MaxOutputTokens:  rc.maxOutputTokens,
			},
			Tools: []contractTools{{FunctionDeclarations: toolDecls}},
		}
		return c.runToolLoop(
			ctx,
			reqBody,
			rc,
			input,
			input.Request.BusinessID,
			input.Request.ConversationID,
			input.AIRunID,
			startedAt,
		)
	}

	return c.decideInteraction(ctx, input, rc, startedAt)
}

func (c *GeminiCustomerSalesAdapter) decideInteraction(
	ctx context.Context,
	input ports.CustomerSalesDecisionInput,
	rc *resolvedAIConfig,
	startedAt time.Time,
) (ports.CustomerSalesDecisionOutput, error) {
	previousID := strings.TrimSpace(input.GeminiInteraction.PreviousInteractionID)
	if !input.GeminiInteraction.Store {
		previousID = ""
	}

	interactionInput := buildUserPrompt(input.Request)
	systemInstruction := c.buildContractSystemInstructionText(input.EntityContractPayload)
	if rc.maxInputCharacters > 0 {
		totalChars := len([]rune(interactionInput)) + len([]rune(systemInstruction))
		if totalChars > rc.maxInputCharacters {
			return ports.CustomerSalesDecisionOutput{}, fmt.Errorf(
				"customer sales interaction input exceeds %d characters after context and system instruction serialization: %d",
				rc.maxInputCharacters,
				totalChars,
			)
		}
	}

	reqBody := interactionRequest{
		Model:                 rc.model,
		Input:                 interactionInput,
		SystemInstruction:     systemInstruction,
		PreviousInteractionID: previousID,
		Store:                 input.GeminiInteraction.Store,
		ResponseFormat: interactionResponseFormat{
			Type:     "text",
			MimeType: "application/json",
			Schema:   contractProposalResponseSchema(),
		},
		GenerationConfig: interactionGenerationConfig{
			MaxOutputTokens: rc.maxOutputTokens,
		},
	}

	resp, err := c.sendInteractionRequest(ctx, reqBody, rc)
	if err != nil {
		return ports.CustomerSalesDecisionOutput{}, err
	}
	if resp.Status != "completed" {
		reason := resp.Status
		if len(resp.Errors) > 0 && strings.TrimSpace(resp.Errors[0].Message) != "" {
			reason += ": " + resp.Errors[0].Message
		}
		return ports.CustomerSalesDecisionOutput{}, fmt.Errorf("gemini interaction did not complete: %s", reason)
	}

	raw, err := interactionOutputText(resp)
	if err != nil {
		return ports.CustomerSalesDecisionOutput{}, err
	}
	var proposal ports.CustomerSalesProposal
	if err := decodeStrictStructuredJSON([]byte(raw), &proposal); err != nil {
		return ports.CustomerSalesDecisionOutput{}, fmt.Errorf("decode interaction structured output: %w", err)
	}

	latencyMs := time.Since(startedAt).Milliseconds()
	return ports.CustomerSalesDecisionOutput{
		Proposal: proposal,
		GeminiInteraction: ports.GeminiInteractionContext{
			PreviousInteractionID:  previousID,
			ResultingInteractionID: resp.ID,
			Store:                  input.GeminiInteraction.Store,
		},
		Usage: ports.CustomerSalesUsageTelemetry{
			InputTokens:         resp.Usage.TotalInputTokens,
			CachedTokens:        resp.Usage.TotalCachedTokens,
			OutputTokens:        resp.Usage.TotalOutputTokens,
			Model:               rc.model,
			EstimatedCostMicros: 0,
			LatencyMs:           latencyMs,
			ModelRequests:       1,
		},
		LatencyMs: latencyMs,
	}, nil
}

func (c *GeminiCustomerSalesAdapter) buildContractSystemInstructionText(entityContractJSON []byte) string {
	text := prompts.CustomerSalesSystemPrompt
	if len(entityContractJSON) > 0 {
		text += "\n\n# Catalog Entity Contract\n" + string(entityContractJSON)
	}
	return text
}

func (c *GeminiCustomerSalesAdapter) sendInteractionRequest(
	ctx context.Context,
	reqBody interactionRequest,
	rc *resolvedAIConfig,
) (interactionResponse, error) {
	buf, err := json.Marshal(reqBody)
	if err != nil {
		return interactionResponse{}, fmt.Errorf("marshal interaction request: %w", err)
	}

	reqCtx, cancel := context.WithTimeout(ctx, c.requestTimeout)
	defer cancel()

	url := strings.TrimRight(rc.baseURL, "/") + "/v1beta/interactions"
	httpReq, err := http.NewRequestWithContext(reqCtx, http.MethodPost, url, bytes.NewReader(buf))
	if err != nil {
		return interactionResponse{}, fmt.Errorf("build interaction request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-goog-api-key", rc.apiKey)

	httpResp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return interactionResponse{}, fmt.Errorf("send interaction request: %w", err)
	}
	defer httpResp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(httpResp.Body, 1<<20))
	if err != nil {
		return interactionResponse{}, fmt.Errorf("read interaction response: %w", err)
	}
	if httpResp.StatusCode >= 400 {
		return interactionResponse{}, fmt.Errorf("gemini interactions http %d: %s", httpResp.StatusCode, string(body))
	}

	var out interactionResponse
	if err := json.Unmarshal(body, &out); err != nil {
		return interactionResponse{}, fmt.Errorf("unmarshal interaction response: %w", err)
	}
	return out, nil
}

func decodeStrictStructuredJSON(raw []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return errors.New("structured output contains multiple JSON values")
		}
		return fmt.Errorf("decode trailing structured output: %w", err)
	}
	return nil
}

func interactionOutputText(resp interactionResponse) (string, error) {
	for i := len(resp.Steps) - 1; i >= 0; i-- {
		step := resp.Steps[i]
		if step.Type != "model_output" {
			continue
		}
		for _, content := range step.Content {
			if content.Type == "text" && strings.TrimSpace(content.Text) != "" {
				return content.Text, nil
			}
		}
	}
	return "", errors.New("gemini interaction completed without model text output")
}

// buildContractSystemInstruction builds the system_instruction content combining
// the base system prompt + the Catalog Entity Contract per contract ⑤ §7.
//
// Per contract ⑤ §8, this tells Gemini the meaning of every enum value so it
// never has to guess.
func (c *GeminiCustomerSalesAdapter) buildContractSystemInstruction(entityContractJSON []byte) *contractContent {
	parts := []contractPart{
		{Text: prompts.CustomerSalesSystemPrompt},
	}
	if len(entityContractJSON) > 0 {
		parts = append(parts, contractPart{
			Text: "\n\n# Catalog Entity Contract (contract ⑤ §7)\n\n" + string(entityContractJSON),
		})
	}
	return &contractContent{
		Role:  "system",
		Parts: parts,
	}
}

// buildContractContents builds the input contents (the conversation context).
//
// Per contract ④ §3, the input includes: business_context, conversation_context,
// conversation_state, catalog_evidence, user_message.
func (c *GeminiCustomerSalesAdapter) buildContractContents(input ports.CustomerSalesDecisionRequest) []contractContent {
	// The existing client.go has buildUserPrompt(input) which encodes the
	// AIContext (Business, Conversation, Customer, CatalogEvidence, etc.) into
	// the user-facing prompt text. We reuse it for the contract-aligned path.
	return []contractContent{
		{
			Role:  "user",
			Parts: []contractPart{{Text: buildUserPrompt(input)}},
		},
	}
}

// sendContractRequest is the HTTP call to the Gemini Interactions API.
//
// Per contract ③ §9, Mujeeb uses store=true to enable previous_interaction_id.
// Per contract ⑨ §11, every external operation has a Timeout.
func (c *GeminiCustomerSalesAdapter) sendContractRequest(ctx context.Context, reqBody contractGeminiRequest, rc *resolvedAIConfig) (contractGeminiResponse, error) {
	buf, err := json.Marshal(reqBody)
	if err != nil {
		return contractGeminiResponse{}, fmt.Errorf("marshal request: %w", err)
	}

	// P1-8: API key sent via x-goog-api-key header only — never in URL.
	url := fmt.Sprintf("%s/v1beta/models/%s:generateContent", rc.baseURL, rc.model)

	reqCtx, cancel := context.WithTimeout(ctx, c.requestTimeout)
	defer cancel()

	httpReq, err := http.NewRequestWithContext(reqCtx, http.MethodPost, url, bytes.NewReader(buf))
	if err != nil {
		return contractGeminiResponse{}, fmt.Errorf("build request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-goog-api-key", rc.apiKey)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return contractGeminiResponse{}, fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return contractGeminiResponse{}, fmt.Errorf("read response: %w", err)
	}
	if resp.StatusCode >= 400 {
		return contractGeminiResponse{}, fmt.Errorf("gemini http %d: %s", resp.StatusCode, string(body))
	}

	var out contractGeminiResponse
	if err := json.Unmarshal(body, &out); err != nil {
		return contractGeminiResponse{}, fmt.Errorf("unmarshal response: %w", err)
	}
	return out, nil
}

// parseContractProposal extracts the CustomerSalesProposal from the structured
// output per contract ④ §4.
//
// Per contract ④ §8, Structured Outputs enforces the JSON shape; Mujeeb
// additionally validates the values (per contract ⑥ §3).
func parseContractProposal(resp contractGeminiResponse) (ports.CustomerSalesProposal, error) {
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
		return ports.CustomerSalesProposal{}, fmt.Errorf("decode structured output: %w", err)
	}
	return proposal, nil
}

// contractProposalResponseSchema is the JSON Schema that enforces the contract
// ④ §4 output shape via Gemini's responseSchema field.
//
// The contract response schema is shared by contract-aligned AI calls.
func contractProposalResponseSchema() map[string]any {
	return map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"properties": map[string]any{
			"status": map[string]any{
				"type": "string",
				"enum": []string{
					string(ports.CustomerSalesProposalStatusResolved),
					string(ports.CustomerSalesProposalStatusAmbiguous),
					string(ports.CustomerSalesProposalStatusNotFound),
					string(ports.CustomerSalesProposalStatusNeedsMoreData),
				},
			},
			"action": map[string]any{
				"type": "string",
				"enum": []string{
					string(ports.CustomerSalesProposalActionAnswer),
					string(ports.CustomerSalesProposalActionClarification),
					string(ports.CustomerSalesProposalActionHumanRequest),
					string(ports.CustomerSalesProposalActionLeadDraft),
					string(ports.CustomerSalesProposalActionOrderDraft),
				},
			},
			"response_text": map[string]any{"type": "string"},
			"routing_reason": map[string]any{
				"type": "string",
				"enum": []string{
					string(ports.CustomerSalesRoutingReasonSubscriptionActivation),
					string(ports.CustomerSalesRoutingReasonCustomerRequestedHuman),
					string(ports.CustomerSalesRoutingReasonOther),
				},
			},
			"selected": map[string]any{
				"type": "array",
				"items": map[string]any{
					"type":                 "object",
					"additionalProperties": false,
					"properties": map[string]any{
						"item_id":    map[string]any{"type": "string"},
						"variant_id": map[string]any{"type": "string"},
						"offer_id":   map[string]any{"type": "string"},
					},
					"required": []string{"item_id"},
				},
			},
		},
		"required": []string{"status", "action", "response_text"},
	}
}

// contractGeminiRequest is the Interactions API request body.
type contractGeminiRequest struct {
	PreviousInteractionID string                   `json:"-"`
	Store                 bool                     `json:"store,omitempty"`
	SystemInstruction     *contractContent         `json:"systemInstruction,omitempty"`
	Contents              []contractContent        `json:"contents"`
	GenerationConfig      contractGenerationConfig `json:"generationConfig"`
	// Tools is an array of tool objects per Gemini generateContent API.
	// Each element has a "functionDeclarations" key (camelCase per
	// Gemini REST API spec). Per fix #1: must be a slice, not a
	// pointer to a single object.
	Tools []contractTools `json:"tools,omitempty"`
}

// contractTools wraps function declarations for one tools entry.
// Per fix #1: JSON field is "functionDeclarations" (camelCase) to
// match the Gemini generateContent REST API.
type contractTools struct {
	FunctionDeclarations []contractFunctionDeclaration `json:"functionDeclarations"`
}

type contractGenerationConfig struct {
	ResponseMimeType string         `json:"responseMimeType,omitempty"`
	ResponseSchema   map[string]any `json:"responseSchema,omitempty"`
	// MaxOutputTokens enforces the LLMMaxOutputTokens config limit.
	// Per contract ④ §6: the output token limit MUST be sent to Gemini
	// so the model respects the platform's operational boundary.
	MaxOutputTokens int `json:"maxOutputTokens,omitempty"`
}

type contractContent struct {
	Role  string         `json:"role,omitempty"`
	Parts []contractPart `json:"parts"`
}

// contractPart carries one part of a content block. Gemini responses
// can include Text (structured output), FunctionCall (tool invocation),
// or FunctionResponse (tool result sent back).
type contractPart struct {
	Text             string                    `json:"text,omitempty"`
	FunctionCall     *contractFunctionCall     `json:"functionCall,omitempty"`
	FunctionResponse *contractFunctionResponse `json:"functionResponse,omitempty"`
	// Per fix #3: preserve thoughtSignature from Gemini's model
	// response so it can be sent back unchanged in follow-up requests.
	// Gemini uses this for internal reasoning continuity — if we drop
	// it, the model may produce different/worse results on follow-up.
	ThoughtSignature string `json:"thoughtSignature,omitempty"`
}

type contractGeminiResponse struct {
	InteractionID string                `json:"interactionId,omitempty"`
	Candidates    []contractCandidate   `json:"candidates"`
	UsageMetadata contractUsageMetadata `json:"usageMetadata"`
}

type contractCandidate struct {
	Content contractContent `json:"content"`
}

type contractUsageMetadata struct {
	PromptTokenCount        int `json:"promptTokenCount,omitempty"`
	CandidatesTokenCount    int `json:"candidatesTokenCount,omitempty"`
	CachedContentTokenCount int `json:"cachedContentTokenCount,omitempty"`
	TotalTokenCount         int `json:"totalTokenCount,omitempty"`
}

type interactionRequest struct {
	Model                 string                      `json:"model"`
	Input                 string                      `json:"input"`
	SystemInstruction     string                      `json:"system_instruction,omitempty"`
	PreviousInteractionID string                      `json:"previous_interaction_id,omitempty"`
	Store                 bool                        `json:"store"`
	ResponseFormat        interactionResponseFormat   `json:"response_format"`
	GenerationConfig      interactionGenerationConfig `json:"generation_config,omitempty"`
}

type interactionResponseFormat struct {
	Type     string         `json:"type"`
	MimeType string         `json:"mime_type,omitempty"`
	Schema   map[string]any `json:"schema,omitempty"`
}

type interactionGenerationConfig struct {
	MaxOutputTokens int `json:"max_output_tokens,omitempty"`
}

type interactionResponse struct {
	ID     string            `json:"id"`
	Status string            `json:"status"`
	Steps  []interactionStep `json:"steps"`
	Usage  interactionUsage  `json:"usage"`
	Errors []interactionError `json:"errors,omitempty"`
}

type interactionStep struct {
	Type    string               `json:"type"`
	Content []interactionContent `json:"content,omitempty"`
}

type interactionContent struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

type interactionUsage struct {
	TotalInputTokens  int `json:"total_input_tokens,omitempty"`
	TotalCachedTokens int `json:"total_cached_tokens,omitempty"`
	TotalOutputTokens int `json:"total_output_tokens,omitempty"`
	TotalTokens       int `json:"total_tokens,omitempty"`
}

type interactionError struct {
	Code    string `json:"code,omitempty"`
	Message string `json:"message,omitempty"`
}

// Compile-time assertion: GeminiCustomerSalesAdapter implements ports.CustomerSalesDecisionPort.
var _ ports.CustomerSalesDecisionPort = (*GeminiCustomerSalesAdapter)(nil)
