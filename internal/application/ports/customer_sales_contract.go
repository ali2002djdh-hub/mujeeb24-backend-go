// Package ports contains the application contracts used by Mujeeb AI capabilities.

package ports

import "context"

// CustomerSales contract types are B2C-specific. They define the customer-facing
// proposal, catalog-evaluation results, Gemini conversation continuity, and the
// application port consumed by Customer Sales AI.

// ════════════════════════════════════════════════════════════════════════════
// Contract ④ §4 — Gemini → Mujeeb Output Contract (Final Proposal)
// ════════════════════════════════════════════════════════════════════════════

// CustomerSalesProposalStatus — the four closed status values per contract ④ §4.
//
// These are the ONLY allowed status values Gemini may return. Any other value
// fails Structural Validation per contract ⑥ §4.
type CustomerSalesProposalStatus string

const (
	CustomerSalesProposalStatusResolved      CustomerSalesProposalStatus = "resolved"
	CustomerSalesProposalStatusAmbiguous     CustomerSalesProposalStatus = "ambiguous"
	CustomerSalesProposalStatusNotFound      CustomerSalesProposalStatus = "not_found"
	CustomerSalesProposalStatusNeedsMoreData CustomerSalesProposalStatus = "needs_more_data"
)

// CustomerSalesProposalAction — the five closed action values per contract ④ §4.
//
// These are the ONLY allowed action values Gemini may return. Any other value
// fails Structural Validation per contract ⑥ §5.
//
// Note: contract ④ uses "human_request" (not "request_human"), and
// "clarification" (not "ask_clarification"), "lead_draft" (not "create_lead"),
// "order_draft" (not "create_transaction_draft"). Migration 000056 aligns the
// ai_decisions table accordingly.
type CustomerSalesRoutingReason string

const (
	CustomerSalesRoutingReasonSubscriptionActivation CustomerSalesRoutingReason = "subscription_activation"
	CustomerSalesRoutingReasonCustomerRequestedHuman CustomerSalesRoutingReason = "customer_requested_human"
	CustomerSalesRoutingReasonOther                  CustomerSalesRoutingReason = "other"
)

type CustomerSalesProposalAction string

const (
	CustomerSalesProposalActionAnswer        CustomerSalesProposalAction = "answer"
	CustomerSalesProposalActionClarification CustomerSalesProposalAction = "clarification"
	CustomerSalesProposalActionHumanRequest  CustomerSalesProposalAction = "human_request"
	CustomerSalesProposalActionLeadDraft     CustomerSalesProposalAction = "lead_draft"
	CustomerSalesProposalActionOrderDraft    CustomerSalesProposalAction = "order_draft"
)

// SelectedReference is a per-contract ④ §4 reference to a catalog entity that
// Gemini's proposal depends on. All three IDs are required to be UUIDs that
// were actually present in the evidence Mujeeb sent — never invented by Gemini.
//
// variant_id and offer_id may be nil when the relation does not apply or is
// not required (e.g., a service-type item with no variants).
type SelectedReference struct {
	ItemID    string  `json:"item_id"`
	VariantID *string `json:"variant_id,omitempty"`
	OfferID   *string `json:"offer_id,omitempty"`
}

// CatalogBatchCandidate is per-contract ② §5 — the per-batch evaluation result
// Gemini returns. Mujeeb already knows which items it sent, so Gemini only
// returns the IDs it considered candidates plus a short reason.
//
// variant_ids and offer_ids are plural because one item may produce multiple
// variants/offers that are all candidates.
type CatalogBatchCandidate struct {
	ItemID     string   `json:"item_id"`
	VariantIDs []string `json:"variant_ids,omitempty"`
	OfferIDs   []string `json:"offer_ids,omitempty"`
	Reason     string   `json:"reason,omitempty"`
}

// CustomerSalesProposal is the contract ④ §4 final output of one Gemini interaction
// for one AI Run. It is the ONLY shape Gemini is allowed to return for the
// customer-facing decision.
//
// This type is distinct from the per-batch CatalogBatchResult (contract ② §5).
//
// Contract ④ §5 explicitly forbids these fields from appearing in Gemini output:
//   - business_id, tenant_id
//   - requires_approval, authorized, executed, sent
//   - payment_confirmed, order_created
//
// Such fields are Mujeeb's responsibility and live in EffectiveDecision.
type CustomerSalesProposal struct {
	Status        CustomerSalesProposalStatus `json:"status"`
	Action        CustomerSalesProposalAction `json:"action"`
	ResponseText  string                      `json:"response_text"`
	RoutingReason CustomerSalesRoutingReason  `json:"routing_reason,omitempty"`
	Selected      []SelectedReference         `json:"selected,omitempty"`
}

// CatalogBatchResult is the contract ② §5 per-batch evaluation output.
//
// Per contract ② §7, Gemini does NOT decide whether a batch was "skipped" or
// "completed". Mujeeb (the Catalog Batch Controller) owns coverage; Gemini only
// returns the candidates it inferred from the items in this batch.
type CatalogBatchResult struct {
	BatchNumber int                     `json:"batch_number"`
	Candidates  []CatalogBatchCandidate `json:"candidates"`
	// Usage telemetry from the Gemini response for this batch call.
	// Per AIUsageTokenTelemetry.md §6: every Gemini call's tokens must be
	// recorded. This field carries the usageMetadata from the batch's
	// Gemini response so the CatalogBatchController can persist it.
	Usage CustomerSalesUsageTelemetry `json:"-"`
	// LatencyMs is the wall-clock duration of the Gemini batch call.
	// Per P2-13: this is the REAL latency from start-of-request to
	// end-of-response, NOT derived from EstimatedCostMicros (which is
	// always 0 — see client_contracts.go). The controller uses this to
	// compute StartedAt = CompletedAt - LatencyMs for the usage record.
	LatencyMs int64 `json:"-"`
}

// ════════════════════════════════════════════════════════════════════════════
// Contract ③ §4 — Gemini Interaction Continuity
// ════════════════════════════════════════════════════════════════════════════

// GeminiInteractionContext carries the previous_interaction_id (if any) for
// the current customer turn. Mujeeb stores it on the conversation row; the
// The customer-sales adapter passes it to Gemini for continuity.
//
// Per contract ③ §4, last_gemini_interaction_id is NOT the source of truth —
// Mujeeb's canonical conversation state is. This ID is only for Gemini's
// internal continuity; if Gemini is unavailable, Mujeeb does not lose any
// conversation, message, state, or business context.
type GeminiInteractionContext struct {
	// PreviousInteractionID is the gemini_interaction_id returned by the
	// previous successful customer-facing Gemini call for this conversation.
	// Empty for the first turn of a new conversation or after Gemini history
	// expiry (1 day free tier, 55 days paid tier per contract ③ §9).
	PreviousInteractionID string

	// ResultingInteractionID is populated by the customer-sales adapter after a successful
	// Gemini call. Mujeeb persists this as the new last_gemini_interaction_id
	// on the conversation row, to be used as PreviousInteractionID in the
	// next turn.
	ResultingInteractionID string

	// Store controls whether Gemini server-side history is used. Per contract
	// ③ §9, Mujeeb uses store=true to enable previous_interaction_id chaining.
	// When store=false, PreviousInteractionID MUST be empty and chaining is
	// disabled for that interaction.
	Store bool
}

// ════════════════════════════════════════════════════════════════════════════
// Contract ④ §8 — CustomerSalesDecisionPort
// ════════════════════════════════════════════════════════════════════════════

// CustomerSalesDecisionPort is the contract ④ §8 mapping of Mujeeb Contract to Gemini API.
//
// Per contract ④ §8:
//
//	Mujeeb System Contract → system_instruction
//	Mujeeb Input Context → input (contents)
//	Catalog boundary → Function Calling / tool
//	Mujeeb Output Contract → Structured Output (responseSchema)
//
// CustomerSalesDecisionPort is the application port for customer-facing sales decisions.
//
// Per contract ③ §4, this interface carries GeminiInteractionContext with
// previous_interaction_id chaining.
//
// Per contract ⑤ §7, the Catalog Entity Contract is sent as part of system
// instruction; it is passed through as opaque JSON.
type CustomerSalesDecisionPort interface {
	Decide(ctx context.Context, input CustomerSalesDecisionInput) (CustomerSalesDecisionOutput, error)
}

// CustomerSalesDecisionInput is the complete request envelope for CustomerSalesDecisionPort.Decide.
type CustomerSalesDecisionInput struct {
	// Request is the customer-facing business request and grounded Mujeeb context.
	Request CustomerSalesDecisionRequest

	// GeminiInteraction carries the previous interaction identifier for continuity.
	GeminiInteraction GeminiInteractionContext

	// EntityContractPayload is the Catalog Entity Contract sent to Gemini.
	EntityContractPayload []byte

	// AIRunID links customer-sales tool calls to the current AI Run trace.
	AIRunID string
}

// CustomerSalesDecisionOutput is the output of CustomerSalesDecisionPort.Decide.
type CustomerSalesDecisionOutput struct {
	// Proposal is the contract ④ §4 structured Gemini output.
	Proposal CustomerSalesProposal

	// GeminiInteraction echoes the input and is populated with the
	// ResultingInteractionID returned by Gemini. Caller persists this as the
	// new last_gemini_interaction_id on the conversation row per contract ③ §4.
	GeminiInteraction GeminiInteractionContext

	// Usage per contract ⑧ §8 (token counts + estimated cost).
	Usage CustomerSalesUsageTelemetry

	// LatencyMs per contract ⑧ §9 (the Gemini API call latency).
	LatencyMs int64
}

// CustomerSalesUsageTelemetry is the per-call usage data per contract ⑧ §8.
type CustomerSalesUsageTelemetry struct {
	InputTokens         int
	CachedTokens        int
	OutputTokens        int
	Model               string
	EstimatedCostMicros int64
	// LatencyMs is the wall-clock duration of the Gemini call.
	// Per P2-13: the controller uses this to compute StartedAt =
	// CompletedAt - LatencyMs for the usage record. Before this field,
	// the controller derived latency from EstimatedCostMicros (always 0)
	// — resulting in StartedAt == CompletedAt + latency=0.
	LatencyMs int64
	// ModelRequests is the actual count of Gemini API calls made during
	// this customer-sales Decide invocation. Per fix #1: 1 for non-tool path,
	// N for tool loop (one per sendContractRequest call).
	// The caller (recordAIUsage) uses this instead of hardcoding 1.
	ModelRequests int
}
