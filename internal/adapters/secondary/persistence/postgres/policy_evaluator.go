// Package postgres — Postgres-backed Policy Evaluator (contract ⑥ §12-13).
//
// Implements the services.PolicyEvaluatorPort (which is ports.CustomerSalesPolicyPort)
// against the business_policies table per migration 000002.
//
// Per contract ⑥ §12, the PolicyEvaluator applies the merchant's business
// policies to the validated proposal. Per contract ⑥ §13, requires_approval
// comes ONLY from Mujeeb's PolicyEvaluator, never from Gemini.
//
// Per migration 000002, business_policies has these columns (all NOT NULL):
//   - ai_mode: 'disabled' | 'assist' | 'approval' | 'restricted_auto'
//   - default_human_review: bool
//   - allow_auto_reply: bool
//   - allow_auto_lead_creation: bool
//   - allow_auto_transaction_draft: bool
//   - allow_auto_confirmation: bool
//
// Evaluation rules (strictly derived from the migration columns — NO INVENTION):
//
//   1. ai_mode = 'disabled':
//      → PolicyDecision = 'denied' for ALL actions. AI is fully off.
//
//   2. ai_mode = 'assist':
//      → PolicyDecision = 'requires_approval' for ALL actions.
//      The AI proposes but every action needs human review.
//
//   3. ai_mode = 'approval':
//      → PolicyDecision = 'requires_approval' for ALL actions.
//      Same as 'assist' but signals a different workflow stage.
//
//   4. ai_mode = 'restricted_auto':
//      → action=answer: allowed IF allow_auto_reply=true
//      → action=clarification: allowed IF allow_auto_reply=true
//      → action=human_request: always allowed (handoff is always permitted)
//      → action=lead_draft: requires_approval IF allow_auto_lead_creation=false
//      → action=order_draft: requires_approval IF allow_auto_transaction_draft=false
//      → default_human_review=true overrides: requires_approval for all
//
// Per contract ⑥ §13, when Gemini proposes 'answer' but policy requires
// approval, the EffectiveDecision has PolicyDecision='requires_approval'.
// Per contract ⑥ §17, this blocks Execution until human approval arrives.

package postgres

import (
	"context"
	"strings"

	"github.com/Ammar777782439/mujeeb24-backend-go/internal/application/ports"
)

// PostgresCustomerSalesPolicyEvaluator implements ports.CustomerSalesPolicyPort against the
// business_policies table per migration 000002.
//
// Per contract ⑥ §12, this is the Mujeeb-side authority that decides
// requires_approval — Gemini does NOT decide this per contract ④ §5.
type PostgresCustomerSalesPolicyEvaluator struct {
	// Management is the BusinessManagementRepository that provides
	// GetRuntimePolicy per ports.BusinessManagementRepository interface.
	// Per contract ⑥ §9, business_id comes from the Authenticated Context,
	// never from Gemini.
	Management ports.BusinessManagementRepository
}

// NewPostgresCustomerSalesPolicyEvaluator wires the evaluator with the management repository.
func NewPostgresCustomerSalesPolicyEvaluator(management ports.BusinessManagementRepository) *PostgresCustomerSalesPolicyEvaluator {
	return &PostgresCustomerSalesPolicyEvaluator{Management: management}
}

// Evaluate implements ports.CustomerSalesPolicyPort.Evaluate per contract ⑥ §12-13.
//
// Per contract ⑥ §12, the evaluator applies the merchant's business policies
// to the validated proposal. Per contract ⑥ §13, requires_approval is decided
// here — NOT by Gemini.
//
// The input proposal is the legacy AIDecisionProposal shape (the bridge from
// contract ④ §4 CustomerSalesProposal is done by the ValidationPipeline's
// toLegacyProposal helper). The returned proposal has PolicyDecision set
// to 'allowed', 'requires_approval', or 'denied' per the business policy.
//
// Per contract ⑥ §20, validation is deterministic — no second LLM is used.
func (e *PostgresCustomerSalesPolicyEvaluator) Evaluate(ctx context.Context, proposal ports.CustomerSalesProposal, contextValue *ports.CustomerSalesContext) ports.CustomerSalesPolicyDecision {
	if e == nil || e.Management == nil {
		return ports.CustomerSalesPolicyDecision{
			Decision:      "requires_approval",
			RequiresHuman: true,
			Reason:        "customer sales policy repository is unavailable; requiring human approval",
		}
	}

	// Extract business_id from the context. Per contract ⑥ §9, business_id
	// comes from the Authenticated Context, NOT from Gemini.
	businessID := ""
	if contextValue != nil {
		businessID = contextValue.Business.Reference
	}
	if strings.TrimSpace(businessID) == "" {
		// No business context — cannot evaluate policy. Default to
		// requires_approval per contract ⑥ §13 (safe default).
		return ports.CustomerSalesPolicyDecision{
			Decision:      "requires_approval",
			RequiresHuman: true,
			Reason:        "customer sales business context is missing; requiring human approval per contract ⑥ §13",
		}
	}

	// Per contract ⑥ §12, fetch the actual business policy from PostgreSQL.
	// The GetRuntimePolicy call is tenant-scoped via business_id per
	// contract ⑧ §17.
	policy, err := e.Management.GetRuntimePolicy(ctx, businessID)
	if err != nil {
		// Policy fetch failed — default to requires_approval per contract ⑥ §13.
		// This is the safe default: when in doubt, require human review.
		return ports.CustomerSalesPolicyDecision{
			Decision:      "requires_approval",
			RequiresHuman: true,
			Reason:        "customer sales business context is missing; requiring human approval per contract ⑥ §13",
		}
	}

	// Evaluate per the migration 000002 columns. NO INVENTION — every
	// rule below maps directly to a column in business_policies.
	decision := evaluatePolicyAgainstAction(policy, string(proposal.Action))
	return ports.CustomerSalesPolicyDecision{
		Decision:      decision,
		RequiresHuman: decision == "requires_approval",
		Reason:        "customer sales policy evaluation completed per contract ⑥ §12",
	}
}

// evaluatePolicyAgainstAction applies the business_policies columns to the
// proposed action. Returns 'allowed', 'requires_approval', or 'denied'.
//
// Per migration 000002 business_policies_ai_mode_chk:
//
//	ai_mode IN ('disabled', 'assist', 'approval', 'restricted_auto')
func evaluatePolicyAgainstAction(policy ports.BusinessRuntimePolicyRecord, action string) string {
	switch policy.AIMode {
	case "disabled":
		// Per migration 000002: ai_mode='disabled' means AI is fully off.
		// All actions are denied.
		return "denied"

	case "assist", "approval":
		// Per migration 000002: ai_mode='assist' or 'approval' means every
		// AI action requires human review.
		return "requires_approval"

	case "restricted_auto":
		// Per migration 000002: ai_mode='restricted_auto' allows certain
		// actions automatically IF the corresponding allow_* flag is true.
		//
		// Per migration 000002: default_human_review=true overrides to
		// requires_approval for all actions.
		if policy.DefaultHumanReview {
			return "requires_approval"
		}

		switch action {
		case "answer", "clarification":
			// Per migration 000002: allow_auto_reply controls whether
			// the AI can auto-send answers and clarifications.
			if policy.AllowAutoReply {
				return "allowed"
			}
			return "requires_approval"

		case "human_request":
			// Handoff is always permitted — it routes to a human, which is
			// inherently safe per contract ⑥ §14.
			return "allowed"

		case "lead_draft":
			// Per migration 000002: allow_auto_lead_creation controls whether
			// the AI can create lead drafts.
			if policy.AllowAutoLeadCreation {
				return "allowed"
			}
			return "requires_approval"

		case "order_draft":
			// Per migration 000002: allow_auto_transaction_draft controls
			// whether the AI can create transaction (order) drafts.
			if policy.AllowAutoTransactionDraft {
				return "allowed"
			}
			return "requires_approval"

		default:
			// Unknown action — require approval (safe default).
			return "requires_approval"
		}

	default:
		// Unknown ai_mode — require approval (safe default).
		return "requires_approval"
	}
}

// Compile-time assertion: PostgresCustomerSalesPolicyEvaluator implements ports.CustomerSalesPolicyPort.
var _ ports.CustomerSalesPolicyPort = (*PostgresCustomerSalesPolicyEvaluator)(nil)
