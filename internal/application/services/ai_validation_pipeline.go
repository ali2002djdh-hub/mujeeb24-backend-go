// Package services — AI Validation + Authorization Pipeline
//
// Implements contract ⑥ AI Validation + Authorization Boundary.
//
// The pipeline is the contract ⑥ §2 post-Gemini flow:
//   AI Proposal
//     ↓
//   Structural Validation       (§3 — schema correctness)
//     ↓
//   Reference Validation         (§6 — selected IDs exist in Mujeeb data)
//     ↓
//   Tenant / Ownership Validation (§8 — references are within current Business scope)
//     ↓
//   Business Policy Evaluation   (§12 — PolicyEvaluator applies merchant policies)
//     ↓
//   Authorization               (§17 — final go/no-go for execution)
//     ↓
//   Effective Decision           (§17 — distinct from AI Proposal)
//     ↓
//   Execution                   (§19 — only after Authorization; AI has no direct channel)
//
// Per contract ⑥ §11, Mujeeb does NOT re-interpret customer intent here.
// Per contract ⑥ §20, Validation is deterministic; no second LLM is used.
// Per contract ⑥ §22, no new "invalid/rejected/unauthorized" statuses are
// added to the AI Contract; those are internal layer results.
// Per contract ⑥ §13, requires_approval is decided by Mujeeb's PolicyEvaluator
// only — never by Gemini.

package services

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/Ammar777782439/mujeeb24-backend-go/internal/application/ports"
)

// ValidationPipeline executes the contract ⑥ post-Gemini validation chain.
//
// Each stage returns a structured failure (StageFailure) which the pipeline
// converts into an EffectiveDecision with EffectiveAction "blocked" (per
// contract ⑥ §21 — no Execution when validation fails) and records the
// failure in ai_runs (via the lifecycle) for observability.
//
// On full success, the pipeline returns an EffectiveDecision with the
// EffectiveAction set per PolicyEvaluator's verdict and the PolicyDecision
// (allowed / requires_approval / denied).
type ValidationPipeline struct {
	ReferenceValidator   ReferenceValidator
	TenantValidator      TenantValidator
	CustomerSalesPolicy  ports.CustomerSalesPolicyPort
	AuthorizationService AuthorizationService
	Now                  func() time.Time
}

// NewValidationPipeline wires the pipeline dependencies. Each may be nil if
// Reference, tenant and policy stages are mandatory for Customer Sales.
// Missing mandatory stages fail closed. AuthorizationService may remain nil
// only when the merchant policy decision is intentionally the final authority.
func NewValidationPipeline(
	rv ReferenceValidator,
	tv TenantValidator,
	policy ports.CustomerSalesPolicyPort,
	as AuthorizationService,
) *ValidationPipeline {
	return &ValidationPipeline{
		ReferenceValidator:   rv,
		TenantValidator:      tv,
		CustomerSalesPolicy:  policy,
		AuthorizationService: as,
		Now:                  func() time.Time { return time.Now().UTC() },
	}
}

// StageFailure is the structured error returned by any validation stage.
// It carries the contract ⑥ §30 failure category for ai_runs.failure_*.
type StageFailure struct {
	Stage    string // one of ports.AIRunFailureStage* constants
	Category ports.AIRunFailureCategory
	Reason   string
}

func (f StageFailure) Error() string { return fmt.Sprintf("%s/%s: %s", f.Stage, f.Category, f.Reason) }

// Validate runs the full pipeline. It returns either a successful
// EffectiveDecision or a StageFailure.
//
// Per contract ⑥ §2, if any barrier fails, no downstream barrier runs.
// This is enforced via early return on each stage.
func (p *ValidationPipeline) Validate(ctx context.Context, input ValidationInput) (ports.EffectiveDecision, *StageFailure) {
	if err := p.validateStructural(input.Proposal); err != nil {
		return ports.EffectiveDecision{}, err
	}
	if err := p.validateReferences(ctx, input); err != nil {
		return ports.EffectiveDecision{}, err
	}
	if err := p.validateTenant(ctx, input); err != nil {
		return ports.EffectiveDecision{}, err
	}
	decision, err := p.evaluateCustomerSalesPolicy(ctx, input)
	if err != nil {
		return ports.EffectiveDecision{}, err
	}
	authorized, err := p.authorize(ctx, input, decision)
	if err != nil {
		return ports.EffectiveDecision{}, err
	}
	return authorized, nil
}

// validateStructural is contract ⑥ §3 — the AI Proposal must match the
// closed Output Contract per contract ④ §4.
//
// Allowed status: resolved/ambiguous/not_found/needs_more_data.
// Allowed action: answer/clarification/human_request/lead_draft/order_draft.
// response_text: non-empty when action != human_request.
// selected: each SelectedReference has a non-empty item_id (variant_id and
// offer_id may be nil per contract ④ §4).
func (p *ValidationPipeline) validateStructural(proposal ports.CustomerSalesProposal) *StageFailure {
	switch proposal.Status {
	case ports.CustomerSalesProposalStatusResolved,
		ports.CustomerSalesProposalStatusAmbiguous,
		ports.CustomerSalesProposalStatusNotFound,
		ports.CustomerSalesProposalStatusNeedsMoreData:
		// allowed
	default:
		return &StageFailure{
			Stage:    ports.AIRunFailureStageValidation,
			Category: ports.AIRunFailureCategoryInvalidAIOutput,
			Reason:   fmt.Sprintf("invalid status %q per contract ④ §4", proposal.Status),
		}
	}
	switch proposal.Action {
	case ports.CustomerSalesProposalActionAnswer,
		ports.CustomerSalesProposalActionClarification,
		ports.CustomerSalesProposalActionHumanRequest,
		ports.CustomerSalesProposalActionLeadDraft,
		ports.CustomerSalesProposalActionOrderDraft:
		// allowed
	default:
		return &StageFailure{
			Stage:    ports.AIRunFailureStageValidation,
			Category: ports.AIRunFailureCategoryInvalidAIOutput,
			Reason:   fmt.Sprintf("invalid action %q per contract ④ §4", proposal.Action),
		}
	}
	if proposal.RoutingReason != "" {
		switch proposal.RoutingReason {
		case ports.CustomerSalesRoutingReasonSubscriptionActivation,
			ports.CustomerSalesRoutingReasonCustomerRequestedHuman,
			ports.CustomerSalesRoutingReasonOther:
			// allowed
		default:
			return &StageFailure{
				Stage:    ports.AIRunFailureStageValidation,
				Category: ports.AIRunFailureCategoryInvalidAIOutput,
				Reason:   fmt.Sprintf("invalid routing_reason %q", proposal.RoutingReason),
			}
		}
	}
	if proposal.RoutingReason != "" && proposal.Action != ports.CustomerSalesProposalActionHumanRequest {
		return &StageFailure{
			Stage:    ports.AIRunFailureStageValidation,
			Category: ports.AIRunFailureCategoryInvalidAIOutput,
			Reason:   "routing_reason is only valid with action=human_request",
		}
	}
	if proposal.Status == ports.CustomerSalesProposalStatusNotFound && len(proposal.Selected) > 0 {
		return &StageFailure{
			Stage:    ports.AIRunFailureStageValidation,
			Category: ports.AIRunFailureCategoryInvalidAIOutput,
			Reason:   "not_found proposal must not contain selected catalog references",
		}
	}
	if proposal.Action != ports.CustomerSalesProposalActionHumanRequest && strings.TrimSpace(proposal.ResponseText) == "" {
		return &StageFailure{
			Stage:    ports.AIRunFailureStageValidation,
			Category: ports.AIRunFailureCategoryInvalidAIOutput,
			Reason:   "response_text must be non-empty unless action is human_request",
		}
	}
	for i, ref := range proposal.Selected {
		if strings.TrimSpace(ref.ItemID) == "" {
			return &StageFailure{
				Stage:    ports.AIRunFailureStageValidation,
				Category: ports.AIRunFailureCategoryInvalidAIOutput,
				Reason:   fmt.Sprintf("selected[%d].item_id is required per contract ④ §4", i),
			}
		}
	}
	return nil
}

// validateReferences is contract ⑥ §6-7 — every selected ID must exist in
// the actual catalog data Mujeeb provided to Gemini. Per contract ⑥ §7,
// Gemini cannot invent references; even a syntactically valid UUID fails
// if it wasn't in the evidence sent.
func (p *ValidationPipeline) validateReferences(ctx context.Context, input ValidationInput) *StageFailure {
	if p.ReferenceValidator == nil {
		return &StageFailure{
			Stage:    ports.AIRunFailureStageValidation,
			Category: ports.AIRunFailureCategoryInvalidReference,
			Reason:   "reference validator is required; validation fails closed",
		}
	}

	// Universal Catalog AI v3 validates the complete item -> variant -> offer
	// relationship against the exact evidence exposed to the model.
	if relational, ok := p.ReferenceValidator.(RelationalReferenceValidator); ok {
		for i, ref := range input.Proposal.Selected {
			if err := relational.ValidateSelection(ctx, input.BusinessID, ref, input.Evidence); err != nil {
				return &StageFailure{
					Stage:    ports.AIRunFailureStageValidation,
					Category: ports.AIRunFailureCategoryInvalidReference,
					Reason:   fmt.Sprintf("selected[%d]: %s", i, err.Error()),
				}
			}
		}
		return nil
	}

	// Compatibility path for older validators/tests. Production Postgres
	// implements RelationalReferenceValidator and never reaches this path.
	for i, ref := range input.Proposal.Selected {
		if err := p.ReferenceValidator.ValidateItemReference(ctx, input.BusinessID, ref.ItemID, input.EvidenceItemIDs); err != nil {
			return &StageFailure{Stage: ports.AIRunFailureStageValidation, Category: ports.AIRunFailureCategoryInvalidReference, Reason: fmt.Sprintf("selected[%d].item_id %s: %s", i, ref.ItemID, err.Error())}
		}
		if ref.VariantID != nil && *ref.VariantID != "" {
			if err := p.ReferenceValidator.ValidateVariantReference(ctx, input.BusinessID, *ref.VariantID, input.EvidenceVariantIDs); err != nil {
				return &StageFailure{Stage: ports.AIRunFailureStageValidation, Category: ports.AIRunFailureCategoryInvalidReference, Reason: fmt.Sprintf("selected[%d].variant_id %s: %s", i, *ref.VariantID, err.Error())}
			}
		}
		if ref.OfferID != nil && *ref.OfferID != "" {
			if err := p.ReferenceValidator.ValidateOfferReference(ctx, input.BusinessID, *ref.OfferID, input.EvidenceOfferIDs); err != nil {
				return &StageFailure{Stage: ports.AIRunFailureStageValidation, Category: ports.AIRunFailureCategoryInvalidReference, Reason: fmt.Sprintf("selected[%d].offer_id %s: %s", i, *ref.OfferID, err.Error())}
			}
		}
	}
	return nil
}

// validateTenant is contract ⑥ §8-9 — every selected reference must belong
// to the current Business. Per contract ⑥ §9, business_id and tenant_id
// cannot come from Gemini; they come from the Authenticated Context.
//
// Per contract ⑥ §8, references from a different Business are silently
// rejected (Do not expose the resource / Do not treat it as valid / Do not
// leak its existence).
func (p *ValidationPipeline) validateTenant(ctx context.Context, input ValidationInput) *StageFailure {
	if p.TenantValidator == nil {
		return &StageFailure{
			Stage:    ports.AIRunFailureStageValidation,
			Category: ports.AIRunFailureCategoryTenantViolation,
			Reason:   "tenant validator is required; validation fails closed",
		}
	}
	for i, ref := range input.Proposal.Selected {
		if err := p.TenantValidator.ValidateItemOwnership(ctx, input.BusinessID, ref.ItemID); err != nil {
			return &StageFailure{
				Stage:    ports.AIRunFailureStageValidation,
				Category: ports.AIRunFailureCategoryTenantViolation,
				Reason:   fmt.Sprintf("selected[%d].item_id %s not owned by business per contract ⑥ §8", i, ref.ItemID),
			}
		}
		if ref.VariantID != nil && *ref.VariantID != "" {
			if err := p.TenantValidator.ValidateVariantOwnership(ctx, input.BusinessID, *ref.VariantID); err != nil {
				return &StageFailure{
					Stage:    ports.AIRunFailureStageValidation,
					Category: ports.AIRunFailureCategoryTenantViolation,
					Reason:   fmt.Sprintf("selected[%d].variant_id %s not owned by business", i, *ref.VariantID),
				}
			}
		}
		if ref.OfferID != nil && *ref.OfferID != "" {
			if err := p.TenantValidator.ValidateOfferOwnership(ctx, input.BusinessID, *ref.OfferID); err != nil {
				return &StageFailure{
					Stage:    ports.AIRunFailureStageValidation,
					Category: ports.AIRunFailureCategoryTenantViolation,
					Reason:   fmt.Sprintf("selected[%d].offer_id %s not owned by business", i, *ref.OfferID),
				}
			}
		}
	}
	return nil
}

// evaluateCustomerSalesPolicy is contract ⑥ §12 — Mujeeb applies the
// merchant's business policy to the validated customer-sales proposal.
// Gemini only proposes; policy never re-interprets customer intent.
func (p *ValidationPipeline) evaluateCustomerSalesPolicy(ctx context.Context, input ValidationInput) (ports.EffectiveDecision, *StageFailure) {
	if p.CustomerSalesPolicy == nil {
		return ports.EffectiveDecision{}, &StageFailure{
			Stage:    ports.AIRunFailureStagePolicy,
			Category: ports.AIRunFailureCategoryPolicyDenial,
			Reason:   "customer sales policy evaluator is required; policy fails closed",
		}
	}

	result := p.CustomerSalesPolicy.Evaluate(ctx, input.Proposal, input.Context)
	policyDecision := strings.TrimSpace(result.Decision)
	switch policyDecision {
	case "allowed", "requires_approval", "denied":
		// closed policy decision set
	default:
		// A broken/missing policy decision must never become implicit execution.
		// Route to human review instead of failing open.
		policyDecision = "requires_approval"
		if strings.TrimSpace(result.Reason) == "" {
			result.Reason = "policy evaluator returned an empty or unsupported decision; requiring human approval"
		}
	}

	return ports.EffectiveDecision{
		DecisionID:      input.DecisionID,
		EffectiveAction: string(input.Proposal.Action),
		PolicyDecision:  policyDecision,
		Reason:          result.Reason,
	}, nil
}

// authorize is contract ⑥ §17 — the final go/no-go. Per contract ⑥ §19,
// Execution only happens after Authorization succeeds.
//
// Per contract ⑥ §14, when action == human_request, Mujeeb applies its own
// handoff policy; Gemini cannot force handoff via prompt text alone.
func (p *ValidationPipeline) authorize(ctx context.Context, input ValidationInput, decision ports.EffectiveDecision) (ports.EffectiveDecision, *StageFailure) {
	if p.AuthorizationService == nil {
		// No authorization service → use the policy decision as final.
		if decision.PolicyDecision == "allowed" {
			now := p.Now()
			decision.AuthorizedAt = &now
		}
		return decision, nil
	}
	authorized, err := p.AuthorizationService.Authorize(ctx, input, decision)
	if err != nil {
		return ports.EffectiveDecision{}, &StageFailure{
			Stage:    ports.AIRunFailureStageAuthorization,
			Category: ports.AIRunFailureCategoryAuthorizationDenial,
			Reason:   err.Error(),
		}
	}
	return authorized, nil
}

// ValidationInput is the input to the pipeline.
type ValidationInput struct {
	// DecisionID is the ai_decisions.id that this validation is for. The
	// resulting EffectiveDecision is linked to it.
	DecisionID string

	// BusinessID comes from the Authenticated Context — never from Gemini.
	BusinessID string

	// ConversationID is the conversation this decision belongs to.
	ConversationID string

	// Proposal is the contract ④ §4 Gemini output.
	Proposal ports.CustomerSalesProposal

	// Context is the CustomerSalesContext built by the ContextBuilder.
	Context *ports.CustomerSalesContext

	// Evidence is the exact relational catalog evidence exposed to Gemini.
	Evidence ports.CatalogAIEvidenceSet

	// EvidenceItemIDs is the set of item IDs that were actually sent to
	// Gemini as evidence. Used by ReferenceValidator per contract ⑥ §10.
	EvidenceItemIDs []string

	// EvidenceVariantIDs is the set of variant IDs sent.
	EvidenceVariantIDs []string

	// EvidenceOfferIDs is the set of offer IDs sent.
	EvidenceOfferIDs []string
}

// ReferenceValidator is the contract ⑥ §6-7 reference-existence check.
// Per contract ⑥ §10, references must be in the data Mujeeb actually provided.
type ReferenceValidator interface {
	ValidateItemReference(ctx context.Context, businessID, itemID string, evidenceItemIDs []string) error
	ValidateVariantReference(ctx context.Context, businessID, variantID string, evidenceVariantIDs []string) error
	ValidateOfferReference(ctx context.Context, businessID, offerID string, evidenceOfferIDs []string) error
}

type RelationalReferenceValidator interface {
	ValidateSelection(ctx context.Context, businessID string, selected ports.SelectedReference, evidence ports.CatalogAIEvidenceSet) error
}

// TenantValidator is the contract ⑥ §8 ownership check.
type TenantValidator interface {
	ValidateItemOwnership(ctx context.Context, businessID, itemID string) error
	ValidateVariantOwnership(ctx context.Context, businessID, variantID string) error
	ValidateOfferOwnership(ctx context.Context, businessID, offerID string) error
}

// AuthorizationService is the contract ⑥ §17 final go/no-go for execution.
// It may add additional checks (e.g., approval workflow state).
type AuthorizationService interface {
	Authorize(ctx context.Context, input ValidationInput, decision ports.EffectiveDecision) (ports.EffectiveDecision, error)
}
