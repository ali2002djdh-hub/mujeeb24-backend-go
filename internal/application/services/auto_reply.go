// Package services — AutoReplyService refactored to contract-aligned flow.
//
// Implements contracts ③ §1 (Mujeeb = canonical conversation state),
// ④ §4 (Gemini output is CustomerSalesProposal only),
// ⑥ §2 (Structural→Reference→Tenant→Ownership→Policy→Authorization→EffectiveDecision),
// ⑨ §2 (AI Run lifecycle),
// ⑧ §5 (operational trace via ai_runs + ai_run_attempts + ai_tool_calls).
//
// Per contract ④ §5, Gemini does NOT decide requires_approval; that's
// PolicyEvaluator's job (now part of ValidationPipeline).
//
// Per contract ⑥ §11, Mujeeb does NOT re-interpret customer intent during
// validation; that is Gemini's role.
//
// Per contract ⑥ §19, Execution only happens after Authorization succeeds;
// the AI never has a direct execution channel.

package services

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/Ammar777782439/mujeeb24-backend-go/internal/application/commands"
	appErrors "github.com/Ammar777782439/mujeeb24-backend-go/internal/application/errors"
	"github.com/Ammar777782439/mujeeb24-backend-go/internal/application/ports"
)

const (
	AutoReplyModeRestrictedAuto = "restricted_auto"
	// Per contract ④ §4, the legacy "ask_clarification" value has been replaced
	// by "clarification" to align with the closed action enum. Migration 000056
	// re-maps existing rows.
	AutoReplyActionAnswer        = "answer"
	AutoReplyActionClarification = "clarification"
)

// HandoffFarewellMessage is fixed Mujeeb-owned farewell content for
// subscription/activation requests. It is never model output, so sending it
// cannot hallucinate prices or terms. The persisted decision keeps
// RequiresHuman=true so the dashboard hands the conversation to staff.
const HandoffFarewellMessage = "يسعدنا اختيارك! تم استلام طلبك، وسيقوم أحد ممثلي المبيعات بالتواصل معك فوراً لإتمام خطوات التفعيل والربط."

// AutoReplyService drives the contract ⑥ post-Gemini flow for one customer turn.
//
// Per contract ⑨ §1, each Handle() call is one AI Run.
// Per contract ⑨ §2, the Run progresses: RECEIVED → CONTEXT_BUILT → RUNNING
// → VALIDATING → AUTHORIZED → EXECUTING → COMPLETED (or FAILED/CANCELLED).
// Per contract ⑥ §2, after Gemini produces CustomerSalesProposal, the ValidationPipeline
// runs Structural→Reference→Tenant→Ownership→Policy→Authorization.
// Per contract ⑥ §19, Execution happens only after Authorization succeeds.
//
// Per contract ④ §5, requires_approval is decided by Mujeeb only; the AI
// proposal's status/action are validated but never overridden by Mujeeb
// (per contract ⑥ §11 we do NOT re-interpret intent).
type AutoReplyService struct {
	// CustomerSalesDecision is the contract ④ §8 customer-sales application port.
	// Its Gemini implementation is GeminiCustomerSalesAdapter.
	// Per contract ④ §8, this is the only way to call Gemini.
	CustomerSalesDecision ports.CustomerSalesDecisionPort

	// ContextBuilder per contract ③ §2 builds the CustomerSalesContext.
	CustomerSalesContextBuilder ports.CustomerSalesContextBuilder

	// Validation is the contract ⑥ §2 pipeline. It is mandatory for any
	// executable AI proposal; nil fails closed before persistence/execution.
	Validation *ValidationPipeline

	// RunRepository persists AI Run trace per contract ⑧ §5. If nil, the
	// lifecycle is run in-memory only (useful for tests).
	RunRepository ports.AIRunRepository

	// CatalogBatch *CatalogBatchController drives the contract ② token-aware batching
	// when Gemini's first response indicates catalog data is needed.
	// Per contract ② §9, the flow is: Gemini → needs_catalog → build
	// projection → token-count → batch → evaluate → aggregate candidates
	// → final evaluate → CustomerSalesProposal.
	// If nil, catalog evaluation is skipped (the AI replies with whatever
	// it can infer from the context alone).
	CatalogBatch *CatalogBatchController

	// SummaryService per ADR-039: maintains the running conversation
	// summary. MaybeSummarize is called after each successful AutoReply
	// to refresh the summary if the turn threshold has been reached.
	// If nil, summarization is skipped.
	SummaryService *ConversationSummaryService

	// EntityContractPayload is the JSON-encoded Catalog Entity Contract per
	// contract ⑤ §7. Built once at bootstrap and reused for every call.
	EntityContractPayload []byte

	// DecisionRepository persists the business ai_decisions row.
	DecisionRepository ports.AIDecisionRepository
	// ReferenceRepository resolves conversation provider references.
	ReferenceRepository ports.ConversationReferenceRepository
	// OutboundRepository creates outbound messages.
	OutboundRepository ports.OutboundMessageRepository
	// Outbox enqueues the actual send.
	Outbox ports.OutboxStore
	// MessageRepository records the communication message row.
	MessageRepository ports.MessageRepository
	// Transactions wraps multi-step DB writes.
	Transactions ports.TransactionManager
	// StateRepository persists ConversationState per contract ③ §1.
	StateRepository ports.ConversationStateRepository
	// Conversations for state machine transitions (waiting_human, etc.).
	Conversations ports.ConversationRuntimeRepository
	// Realtime publishes dashboard events.
	Realtime ports.RealtimePublisher

	// AIUsageRepository records per-execution telemetry to ai_usage_records.
	// Per AIUsageTokenTelemetry.md §6: every Gemini call's tokens + cost
	// must be persisted. If nil, telemetry is logged but not recorded.
	AIUsage ports.AIUsageRepository
	// AIProviderPricingRepository looks up the current pricing version for
	// computing provider_cost_yer. If nil, cost is recorded as 0.
	AIPricing ports.AIProviderPricingRepository
	// SubscriptionRepository looks up the business's active subscription ID
	// so usage records can be associated with the correct subscription.
	Subscriptions ports.SubscriptionRepository

	Mode          string
	PolicyVersion string
	Now           func() time.Time
	NewID         func() string
}

// NewAutoReplyService wires the required dependencies for the contract-aligned
// AutoReply flow. Optional dependencies (ContextBuilder, Validation,
// RunRepository, etc.) are set on the returned struct by the caller.
func NewAutoReplyService(customerSalesDecision ports.CustomerSalesDecisionPort, decisions ports.AIDecisionRepository, references ports.ConversationReferenceRepository, outbound ports.OutboundMessageRepository, outbox ports.OutboxStore, transactions ports.TransactionManager) AutoReplyService {
	return AutoReplyService{
		CustomerSalesDecision: customerSalesDecision,
		DecisionRepository:    decisions,
		ReferenceRepository:   references,
		OutboundRepository:    outbound,
		Outbox:                outbox,
		Transactions:          transactions,
		Mode:                  AutoReplyModeRestrictedAuto,
		PolicyVersion:         "auto-reply-v1",
		Now:                   func() time.Time { return time.Now().UTC() },
		NewID:                 uuid.NewString,
	}
}

// Handle processes one customer turn end-to-end per contracts ③④⑥⑧⑨.
//
// Flow:
//  1. Validate input command.
//  2. Start AI Run (RECEIVED) per contract ⑨ §1. Idempotent on
//     (business_id, source_message_reference).
//  3. Build context (CONTEXT_BUILT) per contract ③ §2.
//  4. Call Gemini via CustomerSalesDecision.Decide (RUNNING) per contract ④ §8.
//  5. Run ValidationPipeline (VALIDATING) per contract ⑥ §2.
//  6. If validation fails → Mark FAILED per contract ⑨ §18; no execution.
//  7. If policy denied → Mark FAILED; no execution.
//  8. If policy requires_approval → Mark AUTHORIZED, persist decision, return
//     (no execution; human approval needed first).
//  9. If policy allowed → Mark AUTHORIZED → EXECUTING.
//
// 10. Execute (outbound message + outbox + message row) per contract ⑥ §19.
// 11. Mark COMPLETED per contract ⑨ §31.
func (s AutoReplyService) Handle(ctx context.Context, command commands.AutoReplyCommand) (commands.AutoReplyResult, error) {
	businessID := string(command.Meta.Actor.BusinessID)
	conversationID := string(command.ConversationID)
	log.Printf("[AutoReply] START business=%s conversation=%s text=%q", businessID, conversationID, truncate(command.Text, 80))

	if err := s.validate(command); err != nil {
		log.Printf("[AutoReply] VALIDATE_FAILED business=%s err=%v", businessID, err)
		return commands.AutoReplyResult{}, err
	}
	if s.CustomerSalesDecision == nil || s.DecisionRepository == nil || s.ReferenceRepository == nil || s.OutboundRepository == nil || s.Outbox == nil || s.Transactions == nil {
		log.Printf("[AutoReply] NOT_WIRED business=%s customer_sales_decision=%v decisions=%v", businessID, s.CustomerSalesDecision != nil, s.DecisionRepository != nil)
		return commands.AutoReplyResult{}, appErrors.NotImplemented()
	}

	sourceMessageRef := command.SourceMessageReference
	policyVersion := s.policyVersionOr()

	// Per contract ⑨ §1, start an AI Run. Per ⑨ §16, the (business_id, key)
	// uniqueness prevents duplicate Runs for the same source event.
	var run ports.AIRunRecord
	if s.RunRepository != nil {
		lc := NewAIRunLifecycle(s.RunRepository)
		started, err := lc.StartRun(ctx, StartRunInput{
			BusinessID:     businessID,
			ConversationID: conversationID,
			MessageID:      sourceMessageRef,
			IdempotencyKey: "auto-reply:" + sourceMessageRef,
			AgentRole:      ports.AIRunAgentRoleCustomerSales,
			NewRunID:       s.NewID,
		})
		if err != nil {
			log.Printf("[AutoReply] START_RUN_FAILED business=%s err=%v", businessID, err)
			return commands.AutoReplyResult{}, err
		}
		run = started
	}

	// Per contract ③ §2, build the CustomerSalesContext.
	var loadedState *ports.ConversationStateRecord
	if s.StateRepository != nil {
		if st, err := s.StateRepository.Get(ctx, businessID, conversationID); err == nil {
			loadedState = &st
		}
	}
	var builtContext *ports.CustomerSalesContext
	if s.CustomerSalesContextBuilder != nil {
		bc, contextErr := s.CustomerSalesContextBuilder.Build(ctx, ports.CustomerSalesContextInput{
			BusinessID:             businessID,
			ConversationID:         conversationID,
			SourceMessageReference: sourceMessageRef,
			Text:                   command.Text,
			Channel:                command.Channel,
			PolicyVersion:          policyVersion,
			ConversationState:      loadedState,
		})
		if contextErr != nil {
			log.Printf("[AutoReply] CONTEXT_BUILD_FAILED business=%s err=%v", businessID, contextErr)
			s.markFailedSafe(ctx, run, ports.AIRunFailureStageContextBuild, string(ports.AIRunFailureCategoryInfrastructure), contextErr.Error())
			return commands.AutoReplyResult{}, contextErr
		}
		log.Printf("[AutoReply] CONTEXT_BUILT business=%s ownership=%s state=%s", businessID, bc.Conversation.Ownership, bc.Conversation.State)
		log.Printf("[AutoReply] CONTEXT_DEBUG catalog_manifest_items=%d catalog_evidence_count=%d", catalogManifestItemCount(bc.CatalogManifest), len(bc.CatalogEvidence))
		// Per contract ③ §1, if conversation is owned by human or waiting for human,
		// AI does not respond.
		if strings.EqualFold(bc.Conversation.Ownership, "human") || strings.EqualFold(bc.Conversation.State, "waiting_human") {
			log.Printf("[AutoReply] SKIPPED business=%s reason=human_owned_or_waiting_human ownership=%s state=%s", businessID, bc.Conversation.Ownership, bc.Conversation.State)
			s.markCompletedSafe(ctx, run)
			return commands.AutoReplyResult{Action: "no_action", Enqueued: false}, nil
		}
		builtContext = &bc
	}

	// Per contract ⑨ §3, mark CONTEXT_BUILT → RUNNING.
	s.markContextBuiltSafe(ctx, run)
	s.markRunningSafe(ctx, run)
	log.Printf("[AutoReply] STATE→RUNNING run=%s", run.ID)

	// Per contract ④ §8, call Gemini via the CustomerSalesDecisionPort.
	log.Printf("[AutoReply] GEMINI_CALL business=%s conversation=%s run=%s", businessID, conversationID, run.ID)
	// Per contract ③ §4 + Item 8: carry previous_interaction_id
	// from the conversation's last Gemini call. Empty for the
	// first turn (or after Gemini history expiry). The context
	// builder loaded this from the conversations table
	// (last_gemini_interaction_id column, migration 000057).
	var previousInteractionID string
	if builtContext != nil && builtContext.Conversation.LastGeminiInteractionID != nil {
		previousInteractionID = *builtContext.Conversation.LastGeminiInteractionID
	}
	out, err := s.CustomerSalesDecision.Decide(ctx, ports.CustomerSalesDecisionInput{
		Request: ports.CustomerSalesDecisionRequest{
			BusinessID:             businessID,
			ConversationID:         conversationID,
			SourceMessageReference: sourceMessageRef,
			Text:                   command.Text,
			Channel:                command.Channel,
			PolicyVersion:          policyVersion,
			Context:                builtContext,
		},
		GeminiInteraction: ports.GeminiInteractionContext{
			PreviousInteractionID: previousInteractionID,
			Store:                 true,
		},
		// Per contract ⑤ §7, pass the Catalog Entity Contract payload (may be nil).
		EntityContractPayload: s.EntityContractPayload,
		// Per fix #4: pass the AI Run ID so tool call records are
		// persisted with the correct run reference during the Tool Loop.
		AIRunID: run.ID,
	})
	if err != nil {
		log.Printf("[AutoReply] GEMINI_FAILED business=%s err=%v", businessID, err)
		s.markFailedSafe(ctx, run, ports.AIRunFailureStageGeminiRequest, string(ports.AIRunFailureCategoryProviderPermanent), err.Error())
		return commands.AutoReplyResult{}, err
	}
	proposal := out.Proposal
	interactionIDToPersist := strings.TrimSpace(out.GeminiInteraction.ResultingInteractionID)
	resetGeminiInteraction := false
	log.Printf("[AutoReply] GEMINI_OK business=%s status=%s action=%s tokens_in=%d tokens_out=%d latency=%dms response=%q",
		businessID, proposal.Status, proposal.Action, out.Usage.InputTokens, out.Usage.OutputTokens, out.LatencyMs, truncate(proposal.ResponseText, 200))

	// Gemini continuity is persisted only after the effective action is known.
	// An internal needs_more_data interaction is never stored as the previous
	// customer-facing turn; the full-catalog path below resets the chain.

	// Exact relational evidence exposed in the normal customer-sales context.
	// Batch evaluation evidence is merged only after complete batch coverage.
	catalogEvidence := EvidenceFromCustomerSalesContext(builtContext)

	// Per contract ② §9 — Catalog Evaluation flow.
	//
	// When Gemini's first response indicates it needs more catalog data
	// (status=needs_more_data),
	// invoke the CatalogBatchController to:
	//   1. Build the Catalog AI Projection from PostgreSQL (contract ① §6)
	//   2. Token-count and split into batches (contract ② §2)
	//   3. Evaluate each batch independently (contract ② §5-8)
	//   4. Aggregate candidates and run Final Evaluation (contract ② §6)
	//
	// The Final Evaluation produces a new CustomerSalesProposal that replaces
	// the initial one. This is the contract ② §9 flow:
	//   Customer Message → Gemini → needs_catalog? → Catalog Evaluation
	//   → Final Gemini → AI Proposal → Validation → Execution
	//
	// Per contract ② "ما أغلقناه": no semantic search, no product matching
	// inside Mujeeb. Mujeeb only builds the projection and counts tokens.
	fullCatalogRequired := proposal.Status == ports.CustomerSalesProposalStatusNeedsMoreData ||
		proposal.Status == ports.CustomerSalesProposalStatusNotFound ||
		proposalReferencesOutsideEvidence(proposal, catalogEvidence)
	if fullCatalogRequired && s.CatalogBatch == nil {
		err := errors.New("full catalog evaluation is required before needs_more_data/not_found can be finalized, but CatalogBatchController is not configured")
		s.markFailedSafe(ctx, run, ports.AIRunFailureStageGeminiRequest, string(ports.AIRunFailureCategoryProviderPermanent), err.Error())
		return commands.AutoReplyResult{Action: "no_action", Enqueued: false}, err
	}
	if s.CatalogBatch != nil && fullCatalogRequired {
		resetGeminiInteraction = true
		// Per contract ② §9, invoke catalog evaluation whenever Gemini
		// says it needs more data — regardless of whether some evidence
		// already exists. The fact that Gemini returned needs_more_data
		// means the current context was insufficient; the batch
		// evaluation will provide the FULL catalog for Gemini to reason over.
		//
		// Previous condition `len(builtContext.CatalogEvidence) == 0` was
		// wrong: the ContextBuilder always puts 5 items in the evidence,
		// so the condition was never true, and the batch evaluation never ran.
		log.Printf("[AutoReply] CATALOG_EVAL_TRIGGER run=%s reason=%s current_evidence=%d", run.ID, proposal.Status, len(builtContext.CatalogEvidence))
		// Per contract ⑨ §3, mark RUNNING again (back from VALIDATING
		// to RUNNING for the batch evaluation loop).
		s.markRunningSafe(ctx, run)

		// Per contract ② §9, run the full catalog evaluation pipeline.
		entityContract := CatalogEntityContractPayload{}
		if len(s.EntityContractPayload) == 0 {
			err := errors.New("catalog entity contract payload is required for full catalog evaluation")
			s.markFailedSafe(ctx, run, ports.AIRunFailureStageContextBuild, string(ports.AIRunFailureCategoryInfrastructure), err.Error())
			return commands.AutoReplyResult{Action: "no_action", Enqueued: false}, err
		}
		if err := json.Unmarshal(s.EntityContractPayload, &entityContract); err != nil {
			s.markFailedSafe(ctx, run, ports.AIRunFailureStageContextBuild, string(ports.AIRunFailureCategoryInfrastructure), "decode catalog entity contract: "+err.Error())
			return commands.AutoReplyResult{Action: "no_action", Enqueued: false}, fmt.Errorf("decode catalog entity contract: %w", err)
		}
		catalogResult, err := s.CatalogBatch.RunCatalogEvaluation(ctx, CatalogEvaluationInput{
			AIRunID:             run.ID,
			AttemptID:           "", // no separate attempt tracking in this path
			BusinessID:          businessID,
			ConversationID:      conversationID,
			CatalogScope:        "", // evaluate all active catalogs for the business
			CustomerMessage:     command.Text,
			ConversationContext: derefCustomerSalesContext(builtContext),
			EntityContract:      entityContract,
		})
		if err != nil {
			log.Printf("[AutoReply] CATALOG_EVAL_FAILED run=%s err=%v", run.ID, err)
			s.markFailedSafe(ctx, run, ports.AIRunFailureStageGeminiRequest, string(ports.AIRunFailureCategoryProviderPermanent), "catalog evaluation: "+err.Error())
			return commands.AutoReplyResult{Action: "no_action", Enqueued: false}, fmt.Errorf("catalog evaluation failed: %w", err)
		} else {
			log.Printf("[AutoReply] CATALOG_EVAL_OK run=%s final_status=%s final_action=%s response=%q", run.ID, catalogResult.Proposal.Status, catalogResult.Proposal.Action, truncate(catalogResult.Proposal.ResponseText, 200))
			proposal = catalogResult.Proposal
			catalogEvidence.Merge(catalogResult.Evidence)
		}
	}

	// needs_more_data is an internal retrieval signal, never an executable
	// customer-facing decision. By this point the catalog path has either run
	// to complete coverage or failed above; allowing this status to continue
	// could turn an unresolved model response into an outbound message.
	if proposal.Status == ports.CustomerSalesProposalStatusNeedsMoreData {
		err := errors.New("AI proposal remained needs_more_data after catalog evaluation")
		s.markFailedSafe(ctx, run, ports.AIRunFailureStageValidation, string(ports.AIRunFailureCategoryInvalidAIOutput), err.Error())
		return commands.AutoReplyResult{Action: "no_action", Enqueued: false}, err
	}

	// Per contract ⑨ §3, mark VALIDATING.
	s.markValidatingSafe(ctx, run)
	log.Printf("[AutoReply] STATE→VALIDATING run=%s", run.ID)

	// Per contract ⑥ §2, run the validation pipeline.
	// Per contract ⑥ §10, evidence IDs = what was actually sent to Gemini.
	// When the CatalogBatchController ran, it sent the FULL catalog projection
	// (items + variants + offers) to Gemini. The evidence set must include
	// ALL of those — not only the initial context evidence.
	var effective ports.EffectiveDecision
	if s.Validation != nil {
		// Universal Catalog AI v3: evidence contains only entities that Mujeeb
		// actually serialized into the normal context or completed catalog batches.
		// AI output never expands this trust boundary.

		ed, failure := s.Validation.Validate(ctx, ValidationInput{
			DecisionID:         "", // linked later when ai_decisions is created
			BusinessID:         businessID,
			ConversationID:     conversationID,
			Proposal:           proposal,
			Context:            builtContext,
			Evidence:           catalogEvidence,
		})
		if failure != nil {
			log.Printf("[AutoReply] VALIDATION_FAILED run=%s stage=%s category=%s reason=%s", run.ID, failure.Stage, failure.Category, failure.Reason)
			s.markFailedSafe(ctx, run, failure.Stage, string(failure.Category), failure.Reason)
			// Per contract ⑥ §21, no Execution when validation fails.
			// Persist a "blocked" decision for audit trail.
			now := s.now()
			blockedDraft := ports.AIDecisionDraft{
				ID:                     s.id(),
				BusinessID:             businessID,
				ConversationID:         &conversationID,
				SourceMessageReference: &sourceMessageRef,
				IntentBase:             string(proposal.Status),
				Entities:               []byte(`{}`),
				EvidenceReferences:     []byte(`[]`),
				RequestedAction:        string(proposal.Action),
				ConfidenceBand:         "unknown",
				RequiresHuman:          true,
				MissingInformation:     []byte(`["` + failure.Reason + `"]`),
				ReasonCodes:            []byte(`["validation_failed"]`),
				PolicyVersion:          policyVersion,
				SchemaVersion:          1,
				Lifecycle:              "expired",
				PolicyDecision:         stringPtr("denied"),
				ModelReference:         stringPtr(out.Usage.Model),
				CreatedAt:              now,
				UpdatedAt:              now,
			}
			if run.ID != "" {
				blockedDraft.AIRunID = &run.ID
			}
			// Per audit B-LOW-2: previously `_ = s.persistDecision(...)`.
			// If the persist fails, the validation failure has no audit
			// trail. Log so operators can investigate.
			if persistErr := s.persistDecision(ctx, blockedDraft); persistErr != nil {
				log.Printf("[AutoReply] BLOCKED_DECISION_PERSIST_FAILED business=%s conversation=%s run=%s err=%v — validation-failure audit trail lost",
					run.BusinessID, run.ConversationID, run.ID, persistErr)
			}
			return commands.AutoReplyResult{Action: "no_action", Enqueued: false}, nil
		}
		effective = ed
	} else {
		err := errors.New("validation pipeline is required for AutoReply")
		s.markFailedSafe(ctx, run, ports.AIRunFailureStageValidation, string(ports.AIRunFailureCategoryInvalidAIOutput), err.Error())
		return commands.AutoReplyResult{Action: "no_action", Enqueued: false}, err
	}

	// Per contract ⑥ §14, handoff for subscription/activation requests uses
	// fixed Mujeeb-owned farewell (never model text). This is product routing,
	// not reference resolution.
	//
	// Subscription/activation routing is structured model output. Mujeeb
	// never infers it from response_text, avoiding text-based false positives.
	farewellHandoff := false
	var farewellReasonCodes []string
	if proposal.Action == ports.CustomerSalesProposalActionHumanRequest &&
		effective.PolicyDecision == "allowed" &&
		proposal.RoutingReason == ports.CustomerSalesRoutingReasonSubscriptionActivation {
		farewellHandoff = true
		// Override the proposal's response text with the fixed farewell and
		// the action to "answer" so the sendable check enqueues the message.
		// Per contract ⑥ §14, handoff is governed by Mujeeb policy, not by
		// Gemini prompt text — Mujeeb decides what to send.
		proposal.ResponseText = HandoffFarewellMessage
		proposal.Action = ports.CustomerSalesProposalActionAnswer
		farewellReasonCodes = []string{"handoff_farewell_sent"}
	}

	now := s.now()
	decisionID := s.id()

	// Per contract ⑥ §17, persist the Effective Decision (or proposal if no
	// validation ran).
	decisionDraft := ports.AIDecisionDraft{
		ID:                     decisionID,
		BusinessID:             businessID,
		ConversationID:         &conversationID,
		SourceMessageReference: &sourceMessageRef,
		IntentBase:             string(proposal.Status),
		Entities:               []byte(`{}`),
		EvidenceReferences:     encodeProposalSelectedAsJSON(proposal.Selected),
		RequestedAction:        string(proposal.Action),
		ConfidenceBand:         "medium",
		RequiresHuman:          proposal.Action == ports.CustomerSalesProposalActionHumanRequest || farewellHandoff,
		MissingInformation:     []byte(`[]`),
		ReasonCodes:            encodeReasonCodes(farewellReasonCodes),
		PolicyVersion:          policyVersion,
		ModelReference:         stringPtr(out.Usage.Model),
		SchemaVersion:          1,
		Lifecycle:              "proposed",
		PolicyDecision:         stringPtr(effective.PolicyDecision),
		CorrelationID:          uuidStringPointer(command.Meta.CorrelationID),
		CausationID:            uuidStringPointer(sourceMessageRef),
		ExpiresAt:              pointerTo(now.Add(5 * time.Minute)),
		CreatedAt:              now,
		UpdatedAt:              now,
	}
	if run.ID != "" {
		decisionDraft.AIRunID = &run.ID
	}

	result := commands.AutoReplyResult{Action: string(proposal.Action)}
	err = s.Transactions.Within(ctx, func(txCtx context.Context) error {
		decision, createErr := s.DecisionRepository.CreateProposed(txCtx, decisionDraft)
		if createErr != nil {
			return mapAIRepositoryError(createErr)
		}
		result.Decision = aiDecisionView(decision)

		// Per contract ⑥ §17, if Effective Decision is "denied", no execution.
		if effective.PolicyDecision == "denied" {
			log.Printf("[AutoReply] POLICY_DENIED run=%s decision_id=%s", run.ID, decision.ID)
			return nil
		}
		// Per contract ⑥ §17, if Effective Decision is "requires_approval",
		// persist and wait for human approval — no execution.
		if effective.PolicyDecision == "requires_approval" && !farewellHandoff {
			log.Printf("[AutoReply] POLICY_REQUIRES_APPROVAL run=%s decision_id=%s", run.ID, decision.ID)
			// Transition conversation to waiting_human.
			if s.Conversations != nil {
				st := "waiting_human"
				if _, convErr := s.Conversations.TransitionLifecycle(txCtx, ports.ConversationLifecycleTransition{
					BusinessID:     businessID,
					ConversationID: conversationID,
					State:          &st,
					LastActivityAt: &now,
				}); convErr != nil {
					return mapAIRepositoryError(convErr)
				}
			}
			return nil
		}

		// Per contract ⑥ §19, Execution only after Authorization.
		// Mark EXECUTING per contract ⑨ §3.
		s.markExecutingSafe(ctx, run)
		log.Printf("[AutoReply] STATE→EXECUTING run=%s", run.ID)

		// Per contract ③ §1, Mujeeb owns the conversation state. After a
		// successful AI reply (answer or clarification), transition the
		// conversation to "waiting_customer" with ownership "ai" so the
		// dashboard reflects the current state.
		if s.Conversations != nil {
			var targetState *string
			var targetOwnership *string
			switch proposal.Action {
			case ports.CustomerSalesProposalActionAnswer, ports.CustomerSalesProposalActionClarification:
				st := "waiting_customer"
				targetState = &st
				own := "ai"
				targetOwnership = &own
			case ports.CustomerSalesProposalActionHumanRequest:
				st := "waiting_human"
				targetState = &st
			case ports.CustomerSalesProposalActionLeadDraft, ports.CustomerSalesProposalActionOrderDraft:
				st := "waiting_human"
				targetState = &st
			}
			if targetState != nil {
				if _, convErr := s.Conversations.TransitionLifecycle(txCtx, ports.ConversationLifecycleTransition{
					BusinessID:     businessID,
					ConversationID: conversationID,
					State:          targetState,
					Ownership:      targetOwnership,
					LastActivityAt: &now,
				}); convErr != nil {
					return mapAIRepositoryError(convErr)
				}
			}
		}

		// Per contract ⑥ §21, only answer/clarification are customer-facing
		// messaging actions. Lead/Order drafts follow their own flow.
		sendable := proposal.Action == ports.CustomerSalesProposalActionAnswer ||
			proposal.Action == ports.CustomerSalesProposalActionClarification
		if !sendable {
			return nil
		}

		if strings.TrimSpace(proposal.ResponseText) == "" {
			return fmt.Errorf("%w: reply action requires response text", appErrors.New(appErrors.CodeValidation, "auto reply"))
		}

		reference, referenceErr := s.ReferenceRepository.GetCurrentByConversation(txCtx, businessID, conversationID, "provider")
		if referenceErr != nil {
			return mapAIRepositoryError(referenceErr)
		}
		if reference.ProviderRef != command.ProviderRef {
			return appErrors.New(appErrors.CodeInvalidState, "conversation provider reference does not match requested provider")
		}
		if reference.ConnectionID == nil || strings.TrimSpace(*reference.ConnectionID) == "" || strings.TrimSpace(reference.ResourceID) == "" {
			return appErrors.New(appErrors.CodeInvalidState, "conversation provider reference is incomplete")
		}

		outboundID := s.id()
		contentReference := EncodeInlineTextContentReference(proposal.ResponseText)
		outbound, outboundErr := s.OutboundRepository.CreatePending(txCtx, ports.OutboundMessageDraft{
			ID:                      outboundID,
			BusinessID:              businessID,
			ConversationID:          conversationID,
			ConversationReferenceID: reference.ID,
			ConnectionID:            *reference.ConnectionID,
			ProviderRef:             command.ProviderRef,
			Channel:                 command.Channel,
			Origin:                  "ai",
			Transport:               "provider",
			ContentReference:        contentReference,
			ProviderIdempotencyKey:  "auto-reply:" + sourceMessageRef,
			CorrelationID:           uuidStringPointer(command.Meta.CorrelationID),
			CausationID:             uuidStringPointer(decision.ID),
		})
		if outboundErr != nil {
			return mapAIRepositoryError(outboundErr)
		}
		if s.MessageRepository != nil {
			msgDraft := ports.CommunicationMessageDraft{
				ID:                      s.id(),
				BusinessID:              businessID,
				ConversationReferenceID: reference.ID,
				OutboundMessageID:       &outbound.ID,
				Direction:               "outbound",
				Origin:                  "ai",
				Transport:               "provider",
				ContentType:             "text",
				TextContent:             &proposal.ResponseText,
				ContentReference:        contentReference,
				Visibility:              "public",
				OccurredAt:              s.now(),
				CreatedAt:               s.now(),
			}
			if _, msgErr := s.MessageRepository.Record(txCtx, msgDraft); msgErr != nil {
				return mapAIRepositoryError(msgErr)
			}
		}
		outboxEntry, outboxErr := s.Outbox.Enqueue(txCtx, ports.OutboxEntryDraft{
			ID:                s.id(),
			BusinessID:        businessID,
			OutboundMessageID: outbound.ID,
			CommandType:       OutboundSendCommandType,
			DedupeKey:         "auto-reply:" + sourceMessageRef,
			AvailableAt:       now,
			CreatedAt:         now,
			UpdatedAt:         now,
		})
		if outboxErr != nil {
			return mapAIRepositoryError(outboxErr)
		}
		result.OutboundMessageID = commands.MessageID(outbound.ID)
		result.OutboxEntryID = commands.ID(outboxEntry.ID)
		result.Enqueued = true
		return nil
	})
	if err != nil {
		log.Printf("[AutoReply] EXECUTE_FAILED business=%s err=%v", businessID, err)
		s.markFailedSafe(ctx, run, ports.AIRunFailureStageExecution, string(ports.AIRunFailureCategoryExecutionFailure), err.Error())
		return commands.AutoReplyResult{}, err
	}

	// Persist deterministic conversation focus from the validated proposal.
	// This is especially important after full-catalog evaluation, where the
	// provider interaction chain is intentionally reset. State is derived only
	// from validated selected[] IDs; no text inference or semantic rematching.
	if s.StateRepository != nil {
		if stateErr := s.persistValidatedProposalState(ctx, loadedState, businessID, conversationID, proposal, builtContext); stateErr != nil {
			log.Printf("[AutoReply] CONVERSATION_STATE_PERSIST_FAILED business=%s conversation=%s err=%v", businessID, conversationID, stateErr)
		}
	}

	// Persist Gemini server-side continuity only when it matches the canonical
	// customer-facing history. The full-catalog final response is generated by
	// a separate provider call, so its initial needs_more_data interaction must
	// not become the next turn's previous_interaction_id.
	if s.Conversations != nil {
		switch {
		case resetGeminiInteraction:
			if err := s.Conversations.UpdateLastGeminiInteractionID(ctx, businessID, conversationID, ""); err != nil {
				log.Printf("[AutoReply] GEMINI_INTERACTION_ID_RESET_FAILED business=%s conversation=%s err=%v", businessID, conversationID, err)
			}
		case result.Enqueued && !farewellHandoff && interactionIDToPersist != "":
			if err := s.Conversations.UpdateLastGeminiInteractionID(ctx, businessID, conversationID, interactionIDToPersist); err != nil {
				log.Printf("[AutoReply] GEMINI_INTERACTION_ID_PERSIST_FAILED business=%s conversation=%s err=%v", businessID, conversationID, err)
			}
		}
	}

	// Per contract ⑨ §31, Mark COMPLETED.
	s.markCompletedSafe(ctx, run)
	log.Printf("[AutoReply] COMPLETED business=%s action=%s enqueued=%v outbound=%s outbox=%s",
		businessID, result.Action, result.Enqueued, result.OutboundMessageID, result.OutboxEntryID)

	// Per Item 4: recordAIUsage now returns an error when AppendRecord
	// fails. The reply is already enqueued (customer received it), but
	// we MUST NOT silently swallow the telemetry failure — the
	// entitlement would drift (ai_replies_used not incremented even
	// though the reply was sent). The error is logged + returned to
	// the caller (worker pool / webhook) so the failure is visible
	// + the platform admin can investigate entitlement drift.
	if usageErr := s.recordAIUsage(ctx, businessID, out, run, result.Enqueued); usageErr != nil {
		log.Printf("[AutoReply] AI_USAGE_PERSISTENCE_ERROR business=%s conversation=%s err=%v (reply was enqueued but entitlement may have drifted — propagating per Item 4)", businessID, conversationID, usageErr)
		// The reply is already sent — we don't fail the Handle
		// (the customer received the response). But we return
		// the error so the caller can log + alert on the drift.
		// The run is already marked COMPLETED — the reply
		// succeeded, not the telemetry.
		return result, usageErr
	}

	// Per ADR-039: refresh the conversation summary if the turn
	// threshold has been reached. This runs in a separate goroutine with
	// a fresh context so it doesn't block the response to the customer.
	// Errors are logged but never fail the AutoReply — summary is a
	// background optimization, not a critical path.
	if s.SummaryService != nil {
		go func(bizID, convID string) {
			// Per audit B-CRIT-1: detached summarization goroutine —
			// panics here would crash the API worker process.
			defer func() {
				if r := recover(); r != nil {
					log.Printf("[SummaryService] PANIC business=%s conversation=%s recovered=%v", bizID, convID, r)
				}
			}()
			sumCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			summaryResult, sumErr := s.SummaryService.MaybeSummarize(sumCtx, bizID, convID)
			if sumErr != nil {
				log.Printf("[SummaryService] ERROR business=%s conversation=%s err=%v", bizID, convID, sumErr)
				return
			}
			if summaryResult.Summarized {
				log.Printf("[SummaryService] REFRESHED business=%s conversation=%s old_turns=%d new_turns=%d",
					bizID, convID, summaryResult.PreviousTurnCount, summaryResult.NewTurnCount)
			}
		}(businessID, conversationID)
	}

	// Per contract ⑧ §5, publish realtime events for the dashboard.
	if s.Realtime != nil && result.Enqueued {
		data, _ := json.Marshal(map[string]any{
			"decision_id":           result.Decision.ID,
			"outbound_message_id":   result.OutboundMessageID,
			"conversation_id":       conversationID,
			"text":                  proposal.ResponseText,
			"action":                result.Action,
			"ai_run_id":             run.ID,
			"gemini_interaction_id": out.GeminiInteraction.ResultingInteractionID,
		})
		var correlationID *string
		if command.Meta.CorrelationID != "" {
			correlationID = &command.Meta.CorrelationID
		}
		_ = s.Realtime.Publish(ctx, ports.RealtimeEvent{
			EventID:       uuid.NewString(),
			EventType:     "conversation.ai_replied",
			BusinessID:    businessID,
			ResourceType:  "conversation",
			ResourceID:    conversationID,
			OccurredAt:    s.now(),
			CorrelationID: correlationID,
			Data:          data,
		})
	}

	return result, nil
}

func (s AutoReplyService) validate(command commands.AutoReplyCommand) error {
	if s.Mode != AutoReplyModeRestrictedAuto {
		return appErrors.New(appErrors.CodeInvalidState, "auto reply is not enabled in restricted_auto mode")
	}
	if command.Meta.Actor.BusinessID == "" || command.ConversationID == "" || strings.TrimSpace(command.SourceMessageReference) == "" || strings.TrimSpace(command.Text) == "" || strings.TrimSpace(command.Channel) == "" || strings.TrimSpace(command.ProviderRef) == "" {
		return appErrors.New(appErrors.CodeValidation, "business, conversation, source message, text, channel, and provider are required")
	}
	if command.Channel != "facebook" && command.Channel != "instagram" && command.Channel != "whatsapp" {
		return appErrors.New(appErrors.CodeValidation, "unsupported auto reply channel")
	}
	return nil
}

// EncodeInlineTextContentReference encodes text as an inline content reference.
func EncodeInlineTextContentReference(text string) string {
	return "content://inline-text/v1/" + base64.RawURLEncoding.EncodeToString([]byte(text))
}

// DecodeInlineTextContentReference decodes an inline content reference.
func DecodeInlineTextContentReference(reference string) (string, error) {
	const prefix = "content://inline-text/v1/"
	if !strings.HasPrefix(reference, prefix) {
		return "", errors.New("unsupported content reference")
	}
	decoded, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(reference, prefix))
	if err != nil {
		return "", fmt.Errorf("decode inline text content: %w", err)
	}
	return string(decoded), nil
}

func (s AutoReplyService) now() time.Time {
	if s.Now == nil {
		return time.Now().UTC()
	}
	return s.Now().UTC()
}

func (s AutoReplyService) id() string {
	if s.NewID == nil {
		return uuid.NewString()
	}
	return s.NewID()
}

func (s AutoReplyService) policyVersionOr() string {
	if strings.TrimSpace(s.PolicyVersion) == "" {
		return "auto-reply-v1"
	}
	return s.PolicyVersion
}

// markContextBuiltSafe, markRunningSafe, etc. are no-ops when RunRepository
// is nil (e.g., unit tests that don't need the trace). They're safe to call
// on a zero-value AIRunRecord.
//
// Per audit B-HIGH-2: previously every call silently dropped the transition
// error with `_, _ = lc.MarkX(...)`. Now logged so a failed lifecycle
// transition (DB outage, race condition) is visible in production logs.
func (s AutoReplyService) markContextBuiltSafe(ctx context.Context, run ports.AIRunRecord) {
	if s.RunRepository == nil || run.ID == "" {
		return
	}
	lc := NewAIRunLifecycle(s.RunRepository)
	if _, err := lc.MarkContextBuilt(ctx, run.BusinessID, run.ID); err != nil {
		log.Printf("[AutoReply] LIFECYCLE_TRANSITION_FAILED business=%s run=%s transition=context_built err=%v",
			run.BusinessID, run.ID, err)
	}
}

func (s AutoReplyService) markRunningSafe(ctx context.Context, run ports.AIRunRecord) {
	if s.RunRepository == nil || run.ID == "" {
		return
	}
	lc := NewAIRunLifecycle(s.RunRepository)
	if _, err := lc.MarkRunning(ctx, run.BusinessID, run.ID); err != nil {
		log.Printf("[AutoReply] LIFECYCLE_TRANSITION_FAILED business=%s run=%s transition=running err=%v",
			run.BusinessID, run.ID, err)
	}
}

func (s AutoReplyService) markValidatingSafe(ctx context.Context, run ports.AIRunRecord) {
	if s.RunRepository == nil || run.ID == "" {
		return
	}
	lc := NewAIRunLifecycle(s.RunRepository)
	if _, err := lc.MarkValidating(ctx, run.BusinessID, run.ID); err != nil {
		log.Printf("[AutoReply] LIFECYCLE_TRANSITION_FAILED business=%s run=%s transition=validating err=%v",
			run.BusinessID, run.ID, err)
	}
}

func (s AutoReplyService) markExecutingSafe(ctx context.Context, run ports.AIRunRecord) {
	if s.RunRepository == nil || run.ID == "" {
		return
	}
	lc := NewAIRunLifecycle(s.RunRepository)
	if _, err := lc.MarkExecuting(ctx, run.BusinessID, run.ID); err != nil {
		log.Printf("[AutoReply] LIFECYCLE_TRANSITION_FAILED business=%s run=%s transition=executing err=%v",
			run.BusinessID, run.ID, err)
	}
}

func (s AutoReplyService) markCompletedSafe(ctx context.Context, run ports.AIRunRecord) {
	if s.RunRepository == nil || run.ID == "" {
		return
	}
	lc := NewAIRunLifecycle(s.RunRepository)
	if _, err := lc.MarkCompleted(ctx, run.BusinessID, run.ID); err != nil {
		log.Printf("[AutoReply] LIFECYCLE_TRANSITION_FAILED business=%s run=%s transition=completed err=%v",
			run.BusinessID, run.ID, err)
	}
}

func (s AutoReplyService) markFailedSafe(ctx context.Context, run ports.AIRunRecord, stage, category, reason string) {
	if s.RunRepository == nil || run.ID == "" {
		return
	}
	lc := NewAIRunLifecycle(s.RunRepository)
	if _, err := lc.MarkFailed(ctx, run.BusinessID, run.ID, stage, category, reason); err != nil {
		// CRITICAL: if MarkFailed itself fails, the run stays in its prior
		// state forever — the contract ⑧ §13-14 promise that every run has
		// a terminal status is broken. Log loudly so operators can manually
		// reconcile the run table.
		log.Printf("[AutoReply] LIFECYCLE_MARK_FAILED_FAILED business=%s run=%s stage=%s category=%s reason=%q err=%v — RUN STUCK, manual reconciliation needed",
			run.BusinessID, run.ID, stage, category, reason, err)
	}
}

// persistDecision is a non-transactional best-effort persist for the blocked
// decision path. Used when validation fails and we still want an audit row.
func (s AutoReplyService) persistDecision(ctx context.Context, draft ports.AIDecisionDraft) error {
	if s.DecisionRepository == nil {
		return nil
	}
	_, err := s.DecisionRepository.CreateProposed(ctx, draft)
	return err
}

// encodeReasonCodes serializes a list of reason codes as JSON for persistence.
func encodeReasonCodes(codes []string) []byte {
	if len(codes) == 0 {
		return []byte(`[]`)
	}
	var sb strings.Builder
	sb.WriteByte('[')
	for i, c := range codes {
		if i > 0 {
			sb.WriteByte(',')
		}
		sb.WriteByte('"')
		sb.WriteString(c)
		sb.WriteByte('"')
	}
	sb.WriteByte(']')
	return []byte(sb.String())
}

// encodeProposalSelectedAsJSON serializes the contract ④ §4 selected[] as
// the legacy ai_decisions.evidence_references JSON shape for persistence.
func encodeProposalSelectedAsJSON(selected []ports.SelectedReference) []byte {
	if len(selected) == 0 {
		return []byte(`[]`)
	}
	var sb strings.Builder
	sb.WriteByte('[')
	for i, ref := range selected {
		if i > 0 {
			sb.WriteByte(',')
		}
		sb.WriteString(`{"item_id":"`)
		sb.WriteString(ref.ItemID)
		sb.WriteByte('"')
		if ref.VariantID != nil && *ref.VariantID != "" {
			sb.WriteString(`,"variant_id":"`)
			sb.WriteString(*ref.VariantID)
			sb.WriteByte('"')
		}
		if ref.OfferID != nil && *ref.OfferID != "" {
			sb.WriteString(`,"offer_id":"`)
			sb.WriteString(*ref.OfferID)
			sb.WriteByte('"')
		}
		sb.WriteByte('}')
	}
	sb.WriteByte(']')
	return []byte(sb.String())
}

// derefCustomerSalesContext safely dereferences a *ports.CustomerSalesContext, returning a zero
// value if nil. Used when passing the context to CatalogBatchController
// which expects a value (not a pointer).
func proposalReferencesOutsideEvidence(proposal ports.CustomerSalesProposal, evidence ports.CatalogAIEvidenceSet) bool {
	for _, ref := range proposal.Selected {
		if !evidence.ContainsSelection(ref) {
			return true
		}
	}
	return false
}

func derefCustomerSalesContext(ctx *ports.CustomerSalesContext) ports.CustomerSalesContext {
	if ctx == nil {
		return ports.CustomerSalesContext{}
	}
	return *ctx
}

// extractItemIDs/extractVariantIDs/extractOfferIDs pull the evidence IDs from
// the CustomerSalesContext so the ValidationPipeline can verify per contract ⑥ §10.
func extractItemIDs(ctx *ports.CustomerSalesContext) []string {
	if ctx == nil {
		return nil
	}
	out := make([]string, 0, len(ctx.CatalogEvidence))
	for _, e := range ctx.CatalogEvidence {
		out = append(out, e.Reference)
	}
	return out
}
func extractVariantIDs(ctx *ports.CustomerSalesContext) []string {
	if ctx == nil {
		return nil
	}
	out := make([]string, 0, len(ctx.VariantEvidence))
	for _, e := range ctx.VariantEvidence {
		out = append(out, e.Reference)
	}
	return out
}
func extractOfferIDs(ctx *ports.CustomerSalesContext) []string {
	if ctx == nil {
		return nil
	}
	out := make([]string, 0, len(ctx.OfferEvidence))
	for _, e := range ctx.OfferEvidence {
		out = append(out, e.Reference)
	}
	return out
}

func uuidStringPointer(value string) *string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	if _, err := uuid.Parse(value); err != nil {
		return nil
	}
	return &value
}

func pointerTo(value time.Time) *time.Time { return &value }

// truncate shortens a string for logging, appending "..." if truncated.
func (s *AutoReplyService) persistValidatedProposalState(
	ctx context.Context,
	current *ports.ConversationStateRecord,
	businessID string,
	conversationID string,
	proposal ports.CustomerSalesProposal,
	context *ports.CustomerSalesContext,
) error {
	if s.StateRepository == nil || len(proposal.Selected) == 0 {
		return nil
	}

	next := ports.ConversationStateRecord{
		BusinessID:     businessID,
		ConversationID: conversationID,
	}
	if current != nil {
		next = *current
		next.BusinessID = businessID
		next.ConversationID = conversationID
	}

	itemName := func(itemID string) *string {
		if context == nil {
			return nil
		}
		for _, item := range context.CatalogEvidence {
			if item.Reference == itemID && strings.TrimSpace(item.Name) != "" {
				name := item.Name
				return &name
			}
		}
		return nil
	}

	if len(proposal.Selected) == 1 {
		ref := proposal.Selected[0]
		itemID := ref.ItemID
		focus := &ports.ConversationFocus{
			Type: "item",
			ID:   itemID,
			Name: itemName(itemID),
		}
		if ref.VariantID != nil && strings.TrimSpace(*ref.VariantID) != "" {
			variantID := strings.TrimSpace(*ref.VariantID)
			focus.Type = "variant"
			focus.ID = variantID
			focus.ItemID = &itemID
		}
		if ref.OfferID != nil && strings.TrimSpace(*ref.OfferID) != "" {
			offerID := strings.TrimSpace(*ref.OfferID)
			focus.Type = "offer"
			focus.ID = offerID
			focus.ItemID = &itemID
		}
		if next.Focus != nil && (next.Focus.Type != focus.Type || next.Focus.ID != focus.ID) {
			next.Previous = append(next.Previous, *next.Focus)
			if len(next.Previous) > 8 {
				next.Previous = append([]ports.ConversationFocus(nil), next.Previous[len(next.Previous)-8:]...)
			}
		}
		next.Focus = focus
		next.Comparison = nil
	} else {
		ids := make([]string, 0, len(proposal.Selected))
		seen := make(map[string]struct{}, len(proposal.Selected))
		for _, ref := range proposal.Selected {
			id := ref.ItemID
			if ref.OfferID != nil && strings.TrimSpace(*ref.OfferID) != "" {
				id = strings.TrimSpace(*ref.OfferID)
			}
			if _, ok := seen[id]; ok || strings.TrimSpace(id) == "" {
				continue
			}
			seen[id] = struct{}{}
			ids = append(ids, id)
		}
		if len(ids) >= 2 {
			next.Comparison = &ports.ConversationComparison{Type: "selected_set", IDs: ids}
			next.Focus = nil
		}
	}

	_, err := s.StateRepository.UpsertValidated(ctx, next)
	return err
}

func catalogManifestItemCount(manifest *ports.CatalogAIManifest) int {
	if manifest == nil {
		return 0
	}
	return manifest.TotalActiveItems
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}

var _ commands.AutoReplyHandler = AutoReplyService{}

// appendUniqueString adds s to slice if not already present.
func appendUniqueString(slice []string, s string) []string {
	if s == "" {
		return slice
	}
	for _, existing := range slice {
		if existing == s {
			return slice
		}
	}
	return append(slice, s)
}

// recordAIUsage persists per-execution telemetry to ai_usage_records.
//
// This is the END-TO-END wiring that connects:
//
//	Gemini API response (usageMetadata)
//	→ token extraction (CustomerSalesUsageTelemetry)
//	→ pricing lookup (AIProviderPricingRepository)
//	→ provider_cost computation
//	→ ai_usage_records INSERT (AppendRecord)
//	→ subscription_ai_usage aggregate refresh
//	→ Platform Admin AI Usage API
//
// Per AIUsageTokenTelemetry.md:
//
//	§3  — final_ai_replies counts ONLY when a Final AI Response is produced.
//	       If the reply was enqueued (sent to the customer), it counts as 1.
//	§6  — every AI execution records: tokens, model_requests, tool_calls,
//	       final_ai_replies, provider_cost, pricing_version.
//	§10 — provider_cost = input_tokens * input_rate + cached * cached_rate
//	       + output_tokens * output_rate (per the pricing version).
//	§31 — cost is computed from actual token consumption + pricing version,
//	       NOT from (AI Replies × fixed cost).
//
// Per Item 4: returns an error when AppendRecord fails — the caller
// (Handle) MUST propagate this so entitlement drift is NOT silent.
//
// Per Item 4 (revised): ALL silent-success paths are closed:
//   - AIUsage == nil + replyEnqueued → ERROR (cannot account without repo)
//   - Subscriptions == nil + replyEnqueued → ERROR (cannot resolve subscription)
//   - Subscriptions.List() error + replyEnqueued → ERROR (subscription lookup failure)
//   - subscriptionID == "" + replyEnqueued → ERROR (no active subscription)
//   - AppendRecord() error → ERROR (persistence failure)
//
// When replyEnqueued is false, telemetry is best-effort (the reply
// wasn't sent, so no entitlement was consumed — nil return is OK).
func (s AutoReplyService) recordAIUsage(ctx context.Context, businessID string, out ports.CustomerSalesDecisionOutput, run ports.AIRunRecord, replyEnqueued bool) error {
	if s.AIUsage == nil {
		if replyEnqueued {
			log.Printf("[AutoReply] AI_USAGE_ENTITLEMENT_DRIFT business=%s reason=AIUsage_repository_not_wired replyEnqueued=true (entitlement cannot be accounted — propagating error per Item 4)", businessID)
			return fmt.Errorf("ai usage repository not wired but reply was enqueued (entitlement drift — per Item 4)")
		}
		log.Printf("[AutoReply] AI_USAGE_SKIP business=%s reason=AIUsage_repository_not_wired replyEnqueued=false", businessID)
		return nil
	}
	now := s.now()
	// Find the business's active subscription ID for association.
	var subscriptionID string
	if s.Subscriptions == nil {
		if replyEnqueued {
			log.Printf("[AutoReply] AI_USAGE_ENTITLEMENT_DRIFT business=%s reason=Subscriptions_repository_not_wired replyEnqueued=true (cannot resolve subscription — propagating error per Item 4)", businessID)
			return fmt.Errorf("subscription repository not wired but reply was enqueued (entitlement drift — per Item 4)")
		}
		log.Printf("[AutoReply] AI_USAGE_SKIP business=%s reason=Subscriptions_repository_not_wired replyEnqueued=false", businessID)
		return nil
	}
	page, subErr := s.Subscriptions.List(ctx, ports.SubscriptionListFilter{
		BusinessID: businessID,
		Status:     "ACTIVE",
		Limit:      1,
	})
	if subErr != nil {
		if replyEnqueued {
			log.Printf("[AutoReply] AI_USAGE_ENTITLEMENT_DRIFT business=%s reason=subscription_list_failed replyEnqueued=true err=%v (propagating error per Item 4)", businessID, subErr)
			return fmt.Errorf("subscription lookup failed but reply was enqueued (entitlement drift — per Item 4): %w", subErr)
		}
		log.Printf("[AutoReply] AI_USAGE_SKIP business=%s reason=subscription_list_failed replyEnqueued=false err=%v", businessID, subErr)
		return nil
	}
	if len(page.Items) == 0 {
		if replyEnqueued {
			log.Printf("[AutoReply] AI_USAGE_ENTITLEMENT_DRIFT business=%s reason=no_active_subscription replyEnqueued=true (reply was sent but subscription is missing — propagating error per Item 4)", businessID)
			return fmt.Errorf("no active subscription found but reply was enqueued (entitlement drift — per Item 4)")
		}
		log.Printf("[AutoReply] AI_USAGE_SKIP business=%s reason=no_active_subscription replyEnqueued=false", businessID)
		return nil
	}
	subscriptionID = page.Items[0].ID
	// Compute provider_cost from the pricing table.
	// Per AIUsageTokenTelemetry.md §10 + §31: cost is computed from actual
	// token consumption + pricing version, NOT from (AI Replies × fixed cost).
	//
	// Gemini's UsageMetadata fields:
	//   promptTokenCount        = TOTAL input tokens (INCLUDING cached)
	//   cachedContentTokenCount = the subset served from cache
	//   candidatesTokenCount    = output tokens
	//
	// Billing: the non-cached input is billed at InputPerMillionYER, the
	// cached portion at CachedInputPerMillionYER (which is lower). We must
	// NOT bill cached tokens at both rates — that would double-count.
	// Therefore: non_cached_input = promptTokenCount - cachedContentTokenCount.
	//
	// P1-6 fix: when pricing lookup fails, we MUST NOT record the usage as
	// status="success" with cost=0 + pricing_version="unknown". That would
	// (a) hide the pricing failure from operators, (b) understate the
	// actual provider cost in the aggregate, (c) count the call as a
	// successful AI Reply in the entitlement counter.
	//
	// Instead, the record is persisted with status="pricing_failed" so:
	// - operators see the failure in the audit + usage logs
	// - the aggregate's final_ai_replies counter does NOT increment
	//   (only status="success" counts toward the commercial AI Reply
	//   entitlement per Contract §33)
	// - provider_cost_yer stays at 0 (honest — we couldn't compute it)
	// - pricing_version stays at "unknown" (honest — we couldn't resolve it)
	//
	// The frontend Platform Admin AI Usage view shows the failed pricing
	// records separately so operators can investigate pricing gaps.
	providerCostYER := 0
	pricingVersion := "unknown"
	pricingFailed := false
	// Also fix the stored token counts: input_tokens should be non-cached
	// input (promptTokenCount - cachedContentTokenCount), and cached_input_tokens
	// is the cached portion. This matches the contract's intent that the two
	// fields are mutually exclusive.
	totalInput := out.Usage.InputTokens
	cachedInput := out.Usage.CachedTokens
	nonCachedInput := totalInput - cachedInput
	if nonCachedInput < 0 {
		nonCachedInput = 0 // defensive — Gemini shouldn't return cached > total
	}
	if s.AIPricing != nil {
		pricing, err := s.AIPricing.GetCurrentForProvider(ctx, "google_gemini", out.Usage.Model)
		if err == nil {
			pricingVersion = pricing.PricingVersion
			// Cost per 1M tokens → scale to actual token count.
			// inputCost = nonCachedInput * InputPerMillionYER / 1M
			// cachedCost = cachedInput * CachedInputPerMillionYER / 1M
			// outputCost = outputTokens * OutputPerMillionYER / 1M
			inputCost := int64(nonCachedInput) * int64(pricing.InputPerMillionYER) / 1_000_000
			cachedCost := int64(cachedInput) * int64(pricing.CachedInputPerMillionYER) / 1_000_000
			outputCost := int64(out.Usage.OutputTokens) * int64(pricing.OutputPerMillionYER) / 1_000_000
			providerCostYER = int(inputCost + cachedCost + outputCost)
		} else {
			// P1-6: pricing lookup failed — mark the record so the
			// aggregate does NOT count this as a successful AI Reply.
			log.Printf("[AutoReply] AI_USAGE_PRICING_LOOKUP_FAILED business=%s model=%s err=%v", businessID, out.Usage.Model, err)
			pricingFailed = true
		}
	} else {
		// P1-6: pricing repository not wired — same as lookup failure.
		log.Printf("[AutoReply] AI_USAGE_PRICING_REPO_NOT_WIRED business=%s model=%s", businessID, out.Usage.Model)
		pricingFailed = true
	}
	// Per §3 + Item 3: final_ai_replies = 1 when a Final AI Response was
	// produced and enqueued (sent to the customer). This is the
	// COMMERCIAL metric — it counts toward the merchant's AI Reply
	// entitlement. Pricing failure is a COST-CALCULATION failure, NOT
	// a reply-failure: the customer received the response, so the
	// entitlement MUST be consumed.
	//
	// The previous logic conflated the two: when pricing failed, it set
	// final_ai_replies=0 — which silently erased the reply from the
	// entitlement counter. That let merchants send unlimited replies
	// when pricing was broken.
	//
	// Now: final_ai_replies=1 IF replyEnqueued (regardless of pricing
	// success). The record's `status` field carries the pricing
	// failure flag separately so operators can see "this reply's cost
	// is uncomputed" without erasing the entitlement consumption.
	//
	// The aggregate's `provider_cost_yer` will understate the real cost
	// (pricing_failed records contribute 0). That's honest — we don't
	// invent a cost. Operators see the gap via the status field.
	finalAIReplies := 0
	if replyEnqueued {
		finalAIReplies = 1
	}
	// recordStatus tracks COST calculation status, NOT reply delivery.
	// "success" = cost computed; "pricing_failed" = cost uncomputed
	// (provider_cost_yer=0 is honest — we don't know the real cost).
	// final_ai_replies is independent — it tracks reply delivery.
	recordStatus := "success"
	if pricingFailed {
		recordStatus = "pricing_failed"
	}
	// Per §6: record the per-execution row.
	recordID := s.NewID()
	correlationID := run.ID
	// Per fix #5: count actual tool calls from the run repository
	// instead of hardcoding 0. Uses the existing AIRunRepository +
	// ListToolCalls (no new repository or trace system).
	toolCallCount := 0
	if s.RunRepository != nil && run.ID != "" {
		if toolCalls, tcErr := s.RunRepository.ListToolCalls(ctx, run.ID); tcErr == nil {
			toolCallCount = len(toolCalls)
		}
	}
	_, err := s.AIUsage.AppendRecord(ctx, ports.AIUsageAppend{
		ID:                recordID,
		BusinessID:        businessID,
		SubscriptionID:    subscriptionID,
		Provider:          "google_gemini",
		Model:             out.Usage.Model,
		InputTokens:       int64(nonCachedInput),
		CachedInputTokens: int64(cachedInput),
		OutputTokens:      int64(out.Usage.OutputTokens),
		ModelRequests:     out.Usage.ModelRequests,
		ToolCalls:         toolCallCount,
		FinalAIReplies:    finalAIReplies,
		ProviderCostYER:   providerCostYER,
		PricingVersion:    pricingVersion,
		Status:            recordStatus,
		CorrelationID:     &correlationID,
		StartedAt:         now.Add(-time.Duration(out.LatencyMs) * time.Millisecond),
		CompletedAt:       now,
		Now:               now,
	})
	if err != nil {
		// Per Item 4: do NOT silently swallow this error. Return it
		// so the caller (Handle) propagates it — the reply was
		// already enqueued (customer received it), but the
		// entitlement MUST NOT drift silently. The error surfaces
		// to the worker pool / webhook caller as a signal that
		// entitlement accounting may be inconsistent.
		log.Printf("[AutoReply] AI_USAGE_RECORD_FAILED business=%s subscription=%s err=%v (entitlement may drift — propagating per Item 4)", businessID, subscriptionID, err)
		return fmt.Errorf("ai usage record failed (entitlement may drift): %w", err)
	}
	log.Printf("[AutoReply] AI_USAGE_RECORDED business=%s subscription=%s tokens_in=%d tokens_cached=%d tokens_out=%d cost_yer=%d final_replies=%d pricing=%s status=%s",
		businessID, subscriptionID, out.Usage.InputTokens, out.Usage.CachedTokens, out.Usage.OutputTokens, providerCostYER, finalAIReplies, pricingVersion, recordStatus)
	return nil
}
