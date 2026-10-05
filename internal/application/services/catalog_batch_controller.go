// Package services — Catalog Batch Controller (contracts ② and ⑨ §22)
//
// Implements contract ② Catalog Evaluation + Batching — CLOSED.
//
// Per contract ② §2, batch size is TOKEN-BASED, not item-count-based.
// Per contract ② §3, every item enters evaluation AT LEAST ONCE; the
// Controller guarantees coverage (Sent == Completed == Total).
// Per contract ② §4, each batch carries the AttributeSchemas its items use.
// Per contract ② §5, Gemini returns ONLY candidates (item_id, variant_ids,
// offer_ids, reason) — not the full items back.
// Per contract ② §6, after all batches complete, a Final Gemini Evaluation
// runs over the candidate set + customer message + context.
// Per contract ② §7, candidates are split by token budget, not by count.
// Per contract ② §8, batches are NOT chained via previous_interaction_id;
// they are independent stateless Gemini requests over the same verified Conversation Context.
//
// Per contract ⑨ §22, each batch has state PENDING/RUNNING/COMPLETED/FAILED
// so the Controller can resume from where it left off after partial failure.

package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/Ammar777782439/mujeeb24-backend-go/internal/application/ports"
)

// CatalogBatchController drives a contract ② §9 evaluation.
//
// It is invoked when Gemini's first Interaction indicates that catalog data
// is needed. The Controller:
//  1. Builds the Catalog AI Projection (per contract ①) for the business scope.
//  2. Token-counts the serialized Projection.
//  3. Splits into batches by token budget.
//  4. Sends each batch as an independent stateless Gemini request.
//  5. Collects candidates per batch.
//  6. Enforces coverage (Total == Sent == Completed).
//  7. Hands the candidate set to the Final Gemini Evaluation.
//
// Per contract ② §1, Mujeeb:
//   - Determines the Catalog scope allowed for the merchant
//   - Builds the Projection
//   - Splits into Batches by TOKENS
//   - Guarantees every Item in scope was sent to Gemini
//   - Tracks which Batches completed
//   - Aggregates evaluation results
//
// Gemini:
//   - Understands the customer message
//   - Understands each Batch's data
//   - Infers and compares
//   - Identifies candidates
//   - Explains why each candidate is in the candidate set
type CatalogBatchController struct {
	// Catalogs is the Postgres-backed catalog repository used to fetch the
	// raw items/variants/offers/schemas for the projection per contract ① §6.
	// Per contract ⑤ §13, the data access boundary is Read Only, Tenant
	// Scoped, Structured, No SQL.
	Catalogs          ports.CatalogRepository
	CatalogAI         ports.CatalogAIReadRepository
	ProjectionBuilder *CatalogAIProjectionBuilder
	TokenCounter      TokenCounter
	Gemini            BatchGeminiClient
	RunRepo           ports.AIRunRepository
	Now               func() time.Time
	NewID             func() string

	// TokenBudget is the per-batch token cap. Per contract ② §2, this is
	// token-based not item-count-based. Set via runtime config.
	TokenBudget int

	// AIUsageRepository records per-batch telemetry to ai_usage_records.
	// Per AIUsageTokenTelemetry.md §6: every Gemini call's tokens + cost
	// must be persisted. Batch calls record final_ai_replies=0.
	AIUsage       ports.AIUsageRepository
	AIPricing     ports.AIProviderPricingRepository
	Subscriptions ports.SubscriptionRepository
}

// BatchGeminiClient is the per-batch Gemini interaction contract.
// Each batch is one independent stateless Gemini request per contract ② §8 — no
// previous_interaction_id chaining between batches.
type BatchGeminiClient interface {
	// EvaluateBatch sends one batch to Gemini and returns the candidate set.
	EvaluateBatch(ctx context.Context, input BatchEvaluationInput) (ports.CatalogBatchResult, error)

	// FinalEvaluate runs the contract ② §6 final evaluation with a custom
	// user prompt that includes BOTH candidate IDs AND full product details.
	// This allows Gemini to compose a response with names, prices, descriptions.
	// Returns the proposal + usage telemetry for cost recording.
	FinalEvaluateWithDetails(ctx context.Context, input FinalEvaluationInput, userPrompt string) (ports.CustomerSalesProposal, ports.CustomerSalesUsageTelemetry, error)
}

type ExactBatchTokenCounter interface {
	CountBatchTokens(ctx context.Context, input BatchEvaluationInput) (int, error)
}

type ExactFinalTokenCounter interface {
	CountFinalTokens(ctx context.Context, input FinalEvaluationInput, userPrompt string) (int, error)
}


// BatchEvaluationInput is one batch's input to Gemini.
type BatchEvaluationInput struct {
	AIRunID             string
	AttemptID           string
	BatchNumber         int
	BusinessID          string
	ConversationID      string
	CustomerMessage     string
	ConversationContext ports.CustomerSalesContext
	EntityContract      CatalogEntityContractPayload
	Batch               CatalogAIBatchPayload
	// Reduction switches the provider prompt from full-catalog evaluation
	// to token-driven candidate reduction. It never changes tenant scope.
	Reduction           bool
}

// FinalEvaluationInput is the post-batch final Gemini call.
type FinalEvaluationInput struct {
	AIRunID             string
	AttemptID           string
	BusinessID          string
	ConversationID      string
	CustomerMessage     string
	ConversationContext ports.CustomerSalesContext
	EntityContract      CatalogEntityContractPayload
	CandidateResults    []ports.CatalogBatchCandidate
}

type CatalogEvaluationResult struct {
	Proposal ports.CustomerSalesProposal
	Evidence ports.CatalogAIEvidenceSet
}

// CatalogAIBatchPayload is one batch's content.
//
// Per contract ② §4, each batch carries the schemas its items use — schemas
// are NOT repeated globally across batches.
type CatalogAIBatchPayload struct {
	BatchNumber      int                        `json:"batch_number"`
	Catalogs         []CatalogAICatalog         `json:"catalogs,omitempty"`
	AttributeSchemas []CatalogAIAttributeSchema `json:"attribute_schemas,omitempty"`
	Items            []CatalogAIItem            `json:"items,omitempty"`
}

// TokenCounter returns the token count of a serialized payload.
//
// Per contract ② §2, Google provides count_tokens for exactly this purpose.
// Implementations wrap that API. The Contract is token-based, not character
// or byte based, so the model's own tokenizer is authoritative.
type TokenCounter interface {
	CountTokens(ctx context.Context, payload any) (int, error)
}

// RunCatalogEvaluation drives the full contract ② §9 evaluation pipeline.
//
// Returns the final CustomerSalesProposal (post-Final-Evaluation) on success.
// On failure, returns the failure stage + category for ai_runs.failure_*.
func (c *CatalogBatchController) RunCatalogEvaluation(ctx context.Context, input CatalogEvaluationInput) (CatalogEvaluationResult, error) {
	if c.Gemini == nil || c.RunRepo == nil || (c.CatalogAI == nil && c.Catalogs == nil) {
		return CatalogEvaluationResult{}, errors.New("CatalogBatchController is not fully wired per contract ② §1")
	}
	if _, ok := c.Gemini.(ExactBatchTokenCounter); !ok && c.TokenCounter == nil {
		return CatalogEvaluationResult{}, errors.New("CatalogBatchController requires an exact or compatibility token counter")
	}
	if strings.TrimSpace(input.BusinessID) == "" || strings.TrimSpace(input.AIRunID) == "" {
		return CatalogEvaluationResult{}, errors.New("business_id and ai_run_id are required for Catalog Evaluation per contract ② §1")
	}

	if c.CatalogAI != nil {
		return c.runPagedCatalogEvaluation(ctx, input)
	}

	// Step 1: Build the Projection. Per contract ① §6, Mujeeb builds it.
	projection, err := c.buildProjection(ctx, input.BusinessID, input.CatalogScope)
	if err != nil {
		return CatalogEvaluationResult{}, fmt.Errorf("build projection: %w", err)
	}

	// Step 2: Token-count and split. Per contract ② §2, token-based not item-count.
	batches, err := c.splitIntoBatches(ctx, projection, input, 1)
	if err != nil {
		return CatalogEvaluationResult{}, fmt.Errorf("split batches: %w", err)
	}
	if len(batches) == 0 {
		// No items in scope — Final Evaluation with empty candidate set.
		proposal, finalProjection, finalErr := c.runFinalEvaluation(ctx, input, nil, CatalogAIProjection{}, 1)
		return CatalogEvaluationResult{Proposal: proposal, Evidence: EvidenceFromProjection(finalProjection)}, finalErr
	}

	// Step 3: Register each batch in ai_catalog_batches per contract ⑨ §22.
	batchRecords := make([]ports.AICatalogBatchRecord, 0, len(batches))
	for _, b := range batches {
		rec, err := c.createBatchRecord(ctx, input.AIRunID, b)
		if err != nil {
			return CatalogEvaluationResult{}, fmt.Errorf("create batch %d record: %w", b.BatchNumber, err)
		}
		batchRecords = append(batchRecords, rec)
	}

	// Step 4: Evaluate each batch independently per contract ② §8.
	// Coverage tracking: Sent == len(batches); Completed == count of COMPLETED.
	candidateSet := make([]ports.CatalogBatchCandidate, 0)
	for i, b := range batches {
		// Per contract ⑨ §22, mark batch RUNNING.
		if err := c.markBatchRunning(ctx, batchRecords[i].ID); err != nil {
			return CatalogEvaluationResult{}, err
		}
		result, err := c.Gemini.EvaluateBatch(ctx, BatchEvaluationInput{
			AIRunID:             input.AIRunID,
			AttemptID:           input.AttemptID,
			BatchNumber:         b.BatchNumber,
			BusinessID:          input.BusinessID,
			ConversationID:      input.ConversationID,
			CustomerMessage:     input.CustomerMessage,
			ConversationContext: input.ConversationContext,
			EntityContract:      input.EntityContract,
			Batch:               b,
		})
		if err != nil {
			// Per contract ⑨ §22, mark batch FAILED; per ⑨ §21 do NOT re-run other batches.
			_ = c.markBatchFailed(ctx, batchRecords[i].ID, err.Error())
			return CatalogEvaluationResult{}, fmt.Errorf("batch %d evaluation: %w", b.BatchNumber, err)
		}
		if err := validateBatchCandidates(b, result.Candidates); err != nil {
			_ = c.markBatchFailed(ctx, batchRecords[i].ID, err.Error())
			return CatalogEvaluationResult{}, fmt.Errorf("batch %d returned invalid candidate evidence: %w", b.BatchNumber, err)
		}
		// Per contract ⑨ §22, mark batch COMPLETED — update the in-memory record too.
		if err := c.markBatchCompleted(ctx, batchRecords[i].ID, len(result.Candidates)); err != nil {
			return CatalogEvaluationResult{}, err
		}
		batchRecords[i].Status = "completed"
		candidateSet = append(candidateSet, result.Candidates...)
		// Record this batch call's usage telemetry (final_ai_replies=0).
		c.recordBatchUsage(ctx, input.BusinessID, input.AIRunID, result.Usage, fmt.Sprintf("batch_%d", b.BatchNumber))
		log.Printf("[CatalogBatch] BATCH_DONE batch=%d items=%d candidates=%d", b.BatchNumber, len(b.Items), len(result.Candidates))
	}

	// Step 5: Coverage check per contract ② §3.
	// Coverage is complete when all batches are COMPLETED.
	log.Printf("[CatalogBatch] COVERAGE total=%d completed=%d", len(batchRecords), countCompleted(batchRecords))
	if !c.coverageComplete(batchRecords) {
		return CatalogEvaluationResult{}, fmt.Errorf("coverage incomplete per contract ② §3 — %d/%d batches completed", countCompleted(batchRecords), len(batchRecords))
	}

	// Step 6: Final Gemini Evaluation per contract ② §6.
	// Per contract ② §6: "يرى: Customer Message + Conversation Context +
	// Candidate Results + الدليل التجاري المرتبط بالمرشحين"
	// The "الدليل التجاري المرتبط بالمرشحين" = full product details for
	// each candidate item. Without this, Gemini only sees IDs and can't
	// compose a proper response with names, prices, descriptions.
	proposal, finalProjection, finalErr := c.runFinalEvaluation(ctx, input, candidateSet, projection, len(batches)+1)
	if finalErr != nil {
		return CatalogEvaluationResult{}, finalErr
	}
	return CatalogEvaluationResult{
		Proposal: proposal,
		Evidence: EvidenceFromProjection(finalProjection),
	}, nil
}

func (c *CatalogBatchController) runPagedCatalogEvaluation(ctx context.Context, input CatalogEvaluationInput) (CatalogEvaluationResult, error) {
	revisionStart, err := c.CatalogAI.GetRevision(ctx, input.BusinessID)
	if err != nil {
		return CatalogEvaluationResult{}, fmt.Errorf("read catalog revision before evaluation: %w", err)
	}
	cursor := ""
	nextBatchNumber := 1
	batchRecords := make([]ports.AICatalogBatchRecord, 0)
	candidateSet := make([]ports.CatalogBatchCandidate, 0)
	candidateProjection := CatalogAIProjection{}
	candidateItemsSeen := make(map[string]struct{})

	for {
		page, err := c.CatalogAI.ListProjectionPage(ctx, ports.CatalogAIProjectionRequest{
			BusinessID: input.BusinessID,
			CatalogID:  input.CatalogScope,
			Limit:      200,
			Cursor:     cursor,
		})
		if err != nil {
			return CatalogEvaluationResult{}, fmt.Errorf("list catalog projection page: %w", err)
		}

		if len(page.Items) > 0 {
			pageProjection := ProjectionFromBundles(page.Items)
			appendCatalogRecordsToProjection(&pageProjection, page.Catalogs)
			batches, err := c.splitIntoBatches(ctx, pageProjection, input, nextBatchNumber)
			if err != nil {
				return CatalogEvaluationResult{}, fmt.Errorf("split catalog page into exact-token batches: %w", err)
			}
			pageCandidates := make([]ports.CatalogBatchCandidate, 0)

			for _, batch := range batches {
				rec, err := c.createBatchRecord(ctx, input.AIRunID, batch)
				if err != nil {
					return CatalogEvaluationResult{}, fmt.Errorf("create batch %d record: %w", batch.BatchNumber, err)
				}
				batchRecords = append(batchRecords, rec)
				recordIndex := len(batchRecords) - 1

				if err := c.markBatchRunning(ctx, rec.ID); err != nil {
					return CatalogEvaluationResult{}, err
				}
				result, err := c.Gemini.EvaluateBatch(ctx, BatchEvaluationInput{
					AIRunID:             input.AIRunID,
					AttemptID:           input.AttemptID,
					BatchNumber:         batch.BatchNumber,
					BusinessID:          input.BusinessID,
					ConversationID:      input.ConversationID,
					CustomerMessage:     input.CustomerMessage,
					ConversationContext: input.ConversationContext,
					EntityContract:      input.EntityContract,
					Batch:               batch,
				})
				if err != nil {
					_ = c.markBatchFailed(ctx, rec.ID, err.Error())
					return CatalogEvaluationResult{}, fmt.Errorf("batch %d evaluation: %w", batch.BatchNumber, err)
				}
				if err := validateBatchCandidates(batch, result.Candidates); err != nil {
					_ = c.markBatchFailed(ctx, rec.ID, err.Error())
					return CatalogEvaluationResult{}, fmt.Errorf("batch %d returned invalid candidate evidence: %w", batch.BatchNumber, err)
				}
				if err := c.markBatchCompleted(ctx, rec.ID, len(result.Candidates)); err != nil {
					return CatalogEvaluationResult{}, err
				}
				batchRecords[recordIndex].Status = "completed"
				c.recordBatchUsage(ctx, input.BusinessID, input.AIRunID, result.Usage, fmt.Sprintf("batch_%d", batch.BatchNumber))
				candidateSet = append(candidateSet, result.Candidates...)
				pageCandidates = append(pageCandidates, result.Candidates...)
				log.Printf("[CatalogBatch] BATCH_DONE batch=%d items=%d candidates=%d", batch.BatchNumber, len(batch.Items), len(result.Candidates))
			}
			if len(pageCandidates) > 0 {
				appendCandidateProjection(
					&candidateProjection,
					CatalogAIBatchPayload{
						Catalogs:         pageProjection.Catalogs,
						AttributeSchemas: pageProjection.AttributeSchemas,
						Items:            pageProjection.Items,
					},
					normalizeBatchCandidates(pageCandidates),
					candidateItemsSeen,
				)
			}
			nextBatchNumber += len(batches)
		}

		if !page.HasMore {
			break
		}
		if strings.TrimSpace(page.NextCursor) == "" || page.NextCursor == cursor {
			return CatalogEvaluationResult{}, errors.New("catalog projection cursor did not advance")
		}
		cursor = page.NextCursor
	}

	log.Printf("[CatalogBatch] COVERAGE total=%d completed=%d", len(batchRecords), countCompleted(batchRecords))
	if !c.coverageComplete(batchRecords) {
		return CatalogEvaluationResult{}, fmt.Errorf("coverage incomplete per contract ② §3 — %d/%d batches completed", countCompleted(batchRecords), len(batchRecords))
	}

	revisionEnd, err := c.CatalogAI.GetRevision(ctx, input.BusinessID)
	if err != nil {
		return CatalogEvaluationResult{}, fmt.Errorf("read catalog revision after evaluation: %w", err)
	}
	if revisionEnd != revisionStart {
		return CatalogEvaluationResult{}, errors.New("catalog changed during full evaluation; refusing mixed-snapshot AI result")
	}

	proposal, finalProjection, err := c.runFinalEvaluation(ctx, input, candidateSet, candidateProjection, nextBatchNumber)
	if err != nil {
		return CatalogEvaluationResult{}, err
	}
	return CatalogEvaluationResult{
		Proposal: proposal,
		// Validation trusts exactly the projection exposed to the final model
		// after any token-driven reduction, never discarded catalog pages.
		Evidence: EvidenceFromProjection(finalProjection),
	}, nil
}

func validateBatchCandidates(batch CatalogAIBatchPayload, candidates []ports.CatalogBatchCandidate) error {
	items := make(map[string]CatalogAIItem, len(batch.Items))
	for _, item := range batch.Items {
		items[item.ID] = item
	}
	for _, candidate := range candidates {
		item, ok := items[candidate.ItemID]
		if !ok {
			return fmt.Errorf("candidate item_id %s was not present in batch", candidate.ItemID)
		}
		variants := make(map[string]struct{}, len(item.Variants))
		for _, variant := range item.Variants {
			variants[variant.ID] = struct{}{}
		}
		offers := make(map[string]CatalogAIOffer, len(item.Offers))
		for _, offer := range item.Offers {
			offers[offer.ID] = offer
		}
		selectedVariants := make(map[string]struct{}, len(candidate.VariantIDs))
		for _, variantID := range candidate.VariantIDs {
			if _, ok := variants[variantID]; !ok {
				return fmt.Errorf("candidate variant_id %s was not present under item %s", variantID, candidate.ItemID)
			}
			selectedVariants[variantID] = struct{}{}
		}
		for _, offerID := range candidate.OfferIDs {
			offer, ok := offers[offerID]
			if !ok {
				return fmt.Errorf("candidate offer_id %s was not present under item %s", offerID, candidate.ItemID)
			}
			if len(selectedVariants) > 0 && offer.VariantID != nil && *offer.VariantID != "" {
				if _, ok := selectedVariants[*offer.VariantID]; !ok {
					return fmt.Errorf(
						"candidate offer_id %s belongs to variant %s, not one of the selected variants for item %s",
						offerID, *offer.VariantID, candidate.ItemID,
					)
				}
			}
		}
	}
	return nil
}

func appendCandidateProjection(target *CatalogAIProjection, batch CatalogAIBatchPayload, candidates []ports.CatalogBatchCandidate, seen map[string]struct{}) {
	if target == nil {
		return
	}

	candidateByItem := make(map[string]ports.CatalogBatchCandidate, len(candidates))
	for _, candidate := range normalizeBatchCandidates(candidates) {
		candidateByItem[candidate.ItemID] = candidate
	}

	for _, item := range batch.Items {
		candidate, ok := candidateByItem[item.ID]
		if !ok {
			continue
		}
		// If Gemini narrowed a candidate to specific variants/offers, expose
		// only those nested records to the next stage. Empty child lists mean
		// the whole item remains the candidate and its nested facts stay visible.
		filtered := item
		if len(candidate.VariantIDs) > 0 || len(candidate.OfferIDs) > 0 {
			variantIDs := make(map[string]struct{}, len(candidate.VariantIDs))
			for _, id := range candidate.VariantIDs {
				variantIDs[id] = struct{}{}
			}
			offerIDs := make(map[string]struct{}, len(candidate.OfferIDs))
			for _, id := range candidate.OfferIDs {
				offerIDs[id] = struct{}{}
			}

			filtered.Offers = nil
			for _, offer := range item.Offers {
				include := false
				if len(offerIDs) > 0 {
					_, include = offerIDs[offer.ID]
				} else if len(variantIDs) > 0 {
					include = offer.VariantID == nil
					if offer.VariantID != nil {
						_, include = variantIDs[*offer.VariantID]
					}
				}
				if !include {
					continue
				}
				filtered.Offers = append(filtered.Offers, offer)
				if offer.VariantID != nil && *offer.VariantID != "" {
					variantIDs[*offer.VariantID] = struct{}{}
				}
			}

			filtered.Variants = nil
			for _, variant := range item.Variants {
				if _, include := variantIDs[variant.ID]; include {
					filtered.Variants = append(filtered.Variants, variant)
				}
			}
		}

		merged := false
		for i := range target.Items {
			if target.Items[i].ID != filtered.ID {
				continue
			}
			mergeCatalogAIItemChildren(&target.Items[i], filtered)
			merged = true
			break
		}
		if !merged {
			target.Items = append(target.Items, filtered)
		}
		seen[item.ID] = struct{}{}
	}

	knownCatalogs := make(map[string]struct{}, len(target.Catalogs))
	for _, catalog := range target.Catalogs {
		knownCatalogs[catalog.ID] = struct{}{}
	}
	for _, catalog := range batch.Catalogs {
		if _, ok := knownCatalogs[catalog.ID]; ok {
			continue
		}
		for _, item := range target.Items {
			if item.CatalogID == catalog.ID {
				target.Catalogs = append(target.Catalogs, catalog)
				knownCatalogs[catalog.ID] = struct{}{}
				break
			}
		}
	}

	knownSchemas := make(map[string]struct{}, len(target.AttributeSchemas))
	for _, schema := range target.AttributeSchemas {
		knownSchemas[schema.ID] = struct{}{}
	}
	for _, schema := range batch.AttributeSchemas {
		if _, ok := knownSchemas[schema.ID]; ok {
			continue
		}
		for _, item := range target.Items {
			if item.AttributeSchemaID != nil && *item.AttributeSchemaID == schema.ID {
				target.AttributeSchemas = append(target.AttributeSchemas, schema)
				knownSchemas[schema.ID] = struct{}{}
				break
			}
		}
	}
}

func mergeCatalogAIItemChildren(target *CatalogAIItem, incoming CatalogAIItem) {
	if target == nil {
		return
	}
	variantSeen := make(map[string]struct{}, len(target.Variants))
	for _, variant := range target.Variants {
		variantSeen[variant.ID] = struct{}{}
	}
	for _, variant := range incoming.Variants {
		if _, ok := variantSeen[variant.ID]; ok {
			continue
		}
		target.Variants = append(target.Variants, variant)
		variantSeen[variant.ID] = struct{}{}
	}

	offerSeen := make(map[string]struct{}, len(target.Offers))
	for _, offer := range target.Offers {
		offerSeen[offer.ID] = struct{}{}
	}
	for _, offer := range incoming.Offers {
		if _, ok := offerSeen[offer.ID]; ok {
			continue
		}
		target.Offers = append(target.Offers, offer)
		offerSeen[offer.ID] = struct{}{}
	}
}

func normalizeBatchCandidates(candidates []ports.CatalogBatchCandidate) []ports.CatalogBatchCandidate {
	if len(candidates) == 0 {
		return nil
	}
	index := make(map[string]int, len(candidates))
	out := make([]ports.CatalogBatchCandidate, 0, len(candidates))
	variantSeen := make(map[string]map[string]struct{})
	offerSeen := make(map[string]map[string]struct{})

	for _, candidate := range candidates {
		pos, exists := index[candidate.ItemID]
		if !exists {
			pos = len(out)
			index[candidate.ItemID] = pos
			out = append(out, ports.CatalogBatchCandidate{ItemID: candidate.ItemID, Reason: candidate.Reason})
			variantSeen[candidate.ItemID] = make(map[string]struct{})
			offerSeen[candidate.ItemID] = make(map[string]struct{})
		} else if out[pos].Reason == "" && candidate.Reason != "" {
			out[pos].Reason = candidate.Reason
		}
		for _, variantID := range candidate.VariantIDs {
			if _, ok := variantSeen[candidate.ItemID][variantID]; ok {
				continue
			}
			variantSeen[candidate.ItemID][variantID] = struct{}{}
			out[pos].VariantIDs = append(out[pos].VariantIDs, variantID)
		}
		for _, offerID := range candidate.OfferIDs {
			if _, ok := offerSeen[candidate.ItemID][offerID]; ok {
				continue
			}
			offerSeen[candidate.ItemID][offerID] = struct{}{}
			out[pos].OfferIDs = append(out[pos].OfferIDs, offerID)
		}
	}
	return out
}


// buildProjection builds the contract ① Catalog AI Projection from the
// merchant's actual catalog data in PostgreSQL. Per contract ① §6, Mujeeb
// is the sole builder of the Projection.
//
// Per contract ⑤ §13, the data access boundary is:
//   - Read Only — no writes via this path
//   - Tenant Scoped — all reads filter by business_id
//   - Structured — returns Projection shapes, not raw rows
//   - No SQL — the AI never sees query strings
//
// Per contract ① §2, only the AttributeSchemas USED by the Items in the
// projection are sent. We do NOT send unused schemas.
//
// Per contract ① §5, the Projection does NOT contain: business_id, SQL,
// database metadata, created_at, or updated_at. The projection contains only
// catalog facts and relationships needed by the model.
//
// Flow:
//  1. List catalog_items for the given (businessID, catalogScope) with
//     status='active' — per contract ① §6, Mujeeb determines the scope.
//  2. For each item, list its variants and offers (also active only).
//  3. For each item's attribute_schema_id, fetch the schema + definitions.
//  4. Assemble the CatalogAIProjection with nested variants+offers per item.
//  5. Deduplicate schemas — per contract ① §2, each schema appears once.
//
// Per contract ⑧ §17, every read is tenant-scoped via business_id. A
// cross-tenant read returns empty (per contract ⑥ §8: do not leak existence).
func (c *CatalogBatchController) buildProjection(ctx context.Context, businessID, catalogScope string) (CatalogAIProjection, error) {
	if c.CatalogAI != nil {
		projection := CatalogAIProjection{}
		schemaByID := map[string]CatalogAIAttributeSchema{}
		cursor := ""
		for {
			page, err := c.CatalogAI.ListProjectionPage(ctx, ports.CatalogAIProjectionRequest{
				BusinessID: businessID,
				CatalogID:  catalogScope,
				Limit:      200,
				Cursor:     cursor,
			})
			if err != nil {
				return CatalogAIProjection{}, fmt.Errorf("list bulk catalog projection page: %w", err)
			}
			part := ProjectionFromBundles(page.Items)
			appendCatalogRecordsToProjection(&projection, page.Catalogs)
			projection.Items = append(projection.Items, part.Items...)
			for _, schema := range part.AttributeSchemas {
				schemaByID[schema.ID] = schema
			}
			if !page.HasMore {
				break
			}
			if strings.TrimSpace(page.NextCursor) == "" || page.NextCursor == cursor {
				return CatalogAIProjection{}, errors.New("catalog AI projection cursor did not advance")
			}
			cursor = page.NextCursor
		}
		for _, schema := range schemaByID {
			projection.AttributeSchemas = append(projection.AttributeSchemas, schema)
		}
		return projection, nil
	}

	if c.Catalogs == nil {
		return CatalogAIProjection{}, errors.New("catalog repository is not wired on CatalogBatchController per contract ① §6")
	}
	if strings.TrimSpace(businessID) == "" {
		return CatalogAIProjection{}, errors.New("business_id is required for projection build per contract ⑧ §17")
	}

	// Per contract ① §6, Mujeeb determines the catalog scope. catalogScope
	// is the catalog_id; if empty, we list all catalogs for the business and
	// iterate items across all of them.
	catalogIDs := make([]string, 0)
	projectionCatalogs := make([]CatalogAICatalog, 0)
	if strings.TrimSpace(catalogScope) != "" {
		catalogIDs = append(catalogIDs, catalogScope)
	} else {
		// List all active catalogs for the business.
		catPage, err := c.Catalogs.ListCatalogs(ctx, businessID, "active", 100, "")
		if err != nil {
			return CatalogAIProjection{}, fmt.Errorf("list catalogs: %w", err)
		}
		for _, cat := range catPage.Items {
			catalogIDs = append(catalogIDs, cat.ID)
			projectionCatalogs = append(projectionCatalogs, CatalogAICatalog{ID: cat.ID, Name: cat.Name, Description: cat.Description, Status: cat.Status})
		}
	}

	projection := CatalogAIProjection{
		Catalogs:         projectionCatalogs,
		Items:            make([]CatalogAIItem, 0),
		AttributeSchemas: make([]CatalogAIAttributeSchema, 0),
	}
	schemaCache := make(map[string]CatalogAIAttributeSchema) // dedup per contract ① §2

	for _, catalogID := range catalogIDs {
		// List active items for this catalog. Per contract ① §6, we send
		// only active items (drafts are not yet published).
		cursor := ""
		for {
			itemPage, err := c.Catalogs.ListCatalogItems(ctx, businessID, catalogID, "", "active", 200, cursor)
			if err != nil {
				return CatalogAIProjection{}, fmt.Errorf("list catalog items for catalog %s: %w", catalogID, err)
			}
			for _, itemRec := range itemPage.Items {
				item := mapCatalogItemRecordToProjection(itemRec)

				// Fetch variants for this item (active only).
				variants, err := c.fetchVariants(ctx, businessID, itemRec.ID)
				if err != nil {
					return CatalogAIProjection{}, fmt.Errorf("fetch variants for item %s: %w", itemRec.ID, err)
				}
				item.Variants = variants

				// Fetch offers for this item (active only).
				offers, err := c.fetchOffers(ctx, businessID, itemRec.ID)
				if err != nil {
					return CatalogAIProjection{}, fmt.Errorf("fetch offers for item %s: %w", itemRec.ID, err)
				}
				item.Offers = offers

				// Fetch the attribute schema if the item references one.
				if itemRec.AttributeSchemaID != nil && *itemRec.AttributeSchemaID != "" {
					schemaID := *itemRec.AttributeSchemaID
					if _, ok := schemaCache[schemaID]; !ok {
						schemaRec, err := c.Catalogs.GetAttributeSchema(ctx, businessID, schemaID)
						if err != nil {
							return CatalogAIProjection{}, fmt.Errorf("fetch attribute schema %s: %w", schemaID, err)
						}
						schema := mapAttributeSchemaRecordToProjection(schemaRec)
						schemaCache[schemaID] = schema
					}
				}

				projection.Items = append(projection.Items, item)
			}
			if !itemPage.HasMore {
				break
			}
			cursor = itemPage.NextCursor
		}
	}

	// Per contract ① §2: only the schemas actually used by items[] are sent.
	for _, schema := range schemaCache {
		projection.AttributeSchemas = append(projection.AttributeSchemas, schema)
	}

	return projection, nil
}

// fetchVariants reads active variants for a catalog item.
// Per contract ⑧ §17, the read is tenant-scoped via business_id.
func (c *CatalogBatchController) fetchVariants(ctx context.Context, businessID, itemID string) ([]CatalogAIVariant, error) {
	page, err := c.Catalogs.ListVariants(ctx, businessID, itemID, "active", 200, "")
	if err != nil {
		return nil, err
	}
	out := make([]CatalogAIVariant, 0, len(page.Items))
	for _, v := range page.Items {
		out = append(out, CatalogAIVariant{
			ID:            v.ID,
			CatalogItemID: v.CatalogItemID,
			Name:          v.Name,
			Attributes: parseJSONAttributes(v.Attributes),
			Status:     v.Status,
		})
	}
	return out, nil
}

// fetchOffers reads active offers for a catalog item.
// Per contract ⑧ §17, the read is tenant-scoped via business_id.
func (c *CatalogBatchController) fetchOffers(ctx context.Context, businessID, itemID string) ([]CatalogAIOffer, error) {
	page, err := c.Catalogs.ListOffers(ctx, businessID, itemID, "active", 200, "")
	if err != nil {
		return nil, err
	}
	out := make([]CatalogAIOffer, 0, len(page.Items))
	for _, o := range page.Items {
		out = append(out, CatalogAIOffer{
			ID:                      o.ID,
			CatalogItemID:           o.CatalogItemID,
			VariantID:               o.VariantID,
			Name:                    o.Name,
			PricingMode:             o.PricingMode,
			Amount:                  o.Amount,
			Currency:                o.Currency,
			PricingUnit:             o.PricingUnit,
			PriceSource:             o.PriceSource,
			PriceVerificationStatus: stringPtrOrNil(o.PriceVerificationStatus),
			AvailabilityMode:        stringPtrOrNil(o.AvailabilityMode),
			AvailabilityStatus:      stringPtrOrNil(o.AvailabilityStatus),
			FulfillmentMode:         stringPtrOrNil(o.FulfillmentMode),
			ValidityFrom:            formatTimePtr(o.ValidityFrom),
			ValidityUntil:           formatTimePtr(o.ValidityUntil),
			Status:                  o.Status,
		})
	}
	return out, nil
}

// mapCatalogItemRecordToProjection converts a ports.CatalogItemRecord to a
// CatalogAIItem per contract ① §1. Per contract ① §5, the projection does
// NOT carry business_id, SQL, database metadata, created_at, updated_at.
func mapCatalogItemRecordToProjection(r ports.CatalogItemRecord) CatalogAIItem {
	return CatalogAIItem{
		ID:                     r.ID,
		CatalogID:              r.CatalogID,
		AttributeSchemaID:      r.AttributeSchemaID,
		AttributeSchemaVersion: r.AttributeSchemaVersion,
		ItemType:               r.ItemType,
		Name:                   r.Name,
		ShortDescription:       r.ShortDescription,
		LongDescription:        r.LongDescription,
		Status:                 r.Status,
		PricingMode:            r.PricingMode,
		AvailabilityMode:       r.AvailabilityMode,
		FulfillmentMode:        r.FulfillmentMode,
		RequiresConfirmation:   r.RequiresConfirmation,
		Attributes:             parseJSONAttributes(r.Attributes),
	}
}

// mapAttributeSchemaRecordToProjection converts a ports.AttributeSchemaRecord
// to a CatalogAIAttributeSchema per contract ① §1.
func mapAttributeSchemaRecordToProjection(r ports.AttributeSchemaRecord) CatalogAIAttributeSchema {
	defs := make([]CatalogAIAttributeDefinition, 0, len(r.Definitions))
	for _, d := range r.Definitions {
		defs = append(defs, CatalogAIAttributeDefinition{
			ID:              d.ID,
			SchemaID:        r.ID,
			AttributeKey:    d.Key,
			Label:           d.Label,
			DataType:        d.DataType,
			IsRequired:      d.Required,
			ValidationRules: parseJSONAttributes(d.ValidationRules),
			DisplayOrder:    d.DisplayOrder,
		})
	}
	return CatalogAIAttributeSchema{
		ID:          r.ID,
		Name:        r.Name,
		Version:     r.Version,
		Definitions: defs,
	}
}

// parseJSONAttributes parses the JSONB attributes column into map[string]any.
// Per contract ① §5 + migration 000016 catalog_items_attributes_object_chk,
// attributes is always a JSON object. Returns empty map for nil/empty.
func parseJSONAttributes(raw []byte) map[string]any {
	if len(raw) == 0 {
		return nil
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// stringPtrOrNil converts a non-empty string to *string, nil for empty.
func stringPtrOrNil(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// formatTimePtr formats a *time.Time as ISO8601 string pointer for the
// projection (per contract ① §1, the projection uses ISO8601 strings for
// timestamps, not Go time.Time).
func formatTimePtr(t *time.Time) *string {
	if t == nil {
		return nil
	}
	s := t.UTC().Format(time.RFC3339)
	return &s
}

// splitIntoBatches implements contract ② §2 token-based splitting.
//
// Per contract ② §2, we do NOT say "100 products = batch" or "50 = batch".
// We serialize the projection, count tokens, and split when the running
// total exceeds TokenBudget.
func (c *CatalogBatchController) splitIntoBatches(ctx context.Context, projection CatalogAIProjection, input CatalogEvaluationInput, startBatchNumber int) ([]CatalogAIBatchPayload, error) {
	return c.splitIntoBatchesMode(ctx, projection, input, startBatchNumber, false)
}

func (c *CatalogBatchController) splitIntoBatchesMode(ctx context.Context, projection CatalogAIProjection, input CatalogEvaluationInput, startBatchNumber int, reduction bool) ([]CatalogAIBatchPayload, error) {
	if len(projection.Items) == 0 {
		return nil, nil
	}
	budget := c.TokenBudget
	if budget <= 0 {
		budget = 8000
	}

	exactCounter, hasExactCounter := c.Gemini.(ExactBatchTokenCounter)
	if !hasExactCounter && c.TokenCounter == nil {
		return nil, errors.New("batch token counter is not configured")
	}

	buildBatch := func(items []CatalogAIItem, batchNumber int) CatalogAIBatchPayload {
		schemaByID := make(map[string]CatalogAIAttributeSchema)
		for _, item := range items {
			if item.AttributeSchemaID == nil {
				continue
			}
			for _, schema := range projection.AttributeSchemas {
				if schema.ID == *item.AttributeSchemaID {
					schemaByID[schema.ID] = schema
					break
				}
			}
		}
		return CatalogAIBatchPayload{
			BatchNumber:      batchNumber,
			Catalogs:         collectCatalogsForItems(items, projection.Catalogs),
			AttributeSchemas: collectSchemas(schemaByID),
			Items:            append([]CatalogAIItem(nil), items...),
		}
	}

	nextBatchNumber := startBatchNumber
	if nextBatchNumber <= 0 {
		nextBatchNumber = 1
	}
	batches := make([]CatalogAIBatchPayload, 0)
	countBatch := func(batch CatalogAIBatchPayload) (int, error) {
		if hasExactCounter {
			return exactCounter.CountBatchTokens(ctx, BatchEvaluationInput{
				AIRunID:             input.AIRunID,
				AttemptID:           input.AttemptID,
				BatchNumber:         batch.BatchNumber,
				BusinessID:          input.BusinessID,
				ConversationID:      input.ConversationID,
				CustomerMessage:     input.CustomerMessage,
				ConversationContext: input.ConversationContext,
				EntityContract:      input.EntityContract,
				Batch:               batch,
				Reduction:           reduction,
			})
		}
		// Compatibility fallback for unit tests. Production BatchClient
		// implements ExactBatchTokenCounter.
		return c.TokenCounter.CountTokens(ctx, batch)
	}

	type itemFragmentUnit struct {
		variant *CatalogAIVariant
		offer   *CatalogAIOffer
	}

	buildFragmentItem := func(item CatalogAIItem, units []itemFragmentUnit) CatalogAIItem {
		fragment := item
		fragment.Variants = nil
		fragment.Offers = nil
		variantSeen := make(map[string]struct{})
		offerSeen := make(map[string]struct{})
		for _, unit := range units {
			if unit.variant != nil {
				if _, ok := variantSeen[unit.variant.ID]; !ok {
					fragment.Variants = append(fragment.Variants, *unit.variant)
					variantSeen[unit.variant.ID] = struct{}{}
				}
			}
			if unit.offer != nil {
				if _, ok := offerSeen[unit.offer.ID]; !ok {
					fragment.Offers = append(fragment.Offers, *unit.offer)
					offerSeen[unit.offer.ID] = struct{}{}
				}
			}
		}
		return fragment
	}

	var splitOversizedItem func(CatalogAIItem) error
	splitOversizedItem = func(item CatalogAIItem) error {
		variantByID := make(map[string]CatalogAIVariant, len(item.Variants))
		for _, variant := range item.Variants {
			variantByID[variant.ID] = variant
		}
		variantUsedByOffer := make(map[string]struct{})
		units := make([]itemFragmentUnit, 0, len(item.Offers)+len(item.Variants))
		for i := range item.Offers {
			offer := item.Offers[i]
			unit := itemFragmentUnit{offer: &offer}
			if offer.VariantID != nil && *offer.VariantID != "" {
				if variant, ok := variantByID[*offer.VariantID]; ok {
					variantCopy := variant
					unit.variant = &variantCopy
					variantUsedByOffer[variant.ID] = struct{}{}
				}
			}
			offerCopy := offer
			unit.offer = &offerCopy
			units = append(units, unit)
		}
		for i := range item.Variants {
			variant := item.Variants[i]
			if _, used := variantUsedByOffer[variant.ID]; used {
				continue
			}
			variantCopy := variant
			units = append(units, itemFragmentUnit{variant: &variantCopy})
		}
		if len(units) == 0 {
			return fmt.Errorf("catalog item %s exceeds token budget %d and has no splittable variants/offers", item.ID, budget)
		}

		var splitUnits func([]itemFragmentUnit) error
		splitUnits = func(part []itemFragmentUnit) error {
			fragment := buildFragmentItem(item, part)
			batch := buildBatch([]CatalogAIItem{fragment}, nextBatchNumber)
			tokens, err := countBatch(batch)
			if err != nil {
				return fmt.Errorf("count tokens for item %s fragment batch %d: %w", item.ID, nextBatchNumber, err)
			}
			if tokens <= budget {
				batches = append(batches, batch)
				nextBatchNumber++
				return nil
			}
			if len(part) == 1 {
				return fmt.Errorf("catalog item %s contains a single commercial fragment exceeding token budget %d with exact request size %d", item.ID, budget, tokens)
			}
			mid := len(part) / 2
			if err := splitUnits(part[:mid]); err != nil {
				return err
			}
			return splitUnits(part[mid:])
		}
		return splitUnits(units)
	}

	var split func([]CatalogAIItem) error
	split = func(items []CatalogAIItem) error {
		if len(items) == 0 {
			return nil
		}
		batch := buildBatch(items, nextBatchNumber)
		tokens, err := countBatch(batch)
		if err != nil {
			return fmt.Errorf("count tokens for batch %d: %w", nextBatchNumber, err)
		}
		if tokens <= budget {
			batches = append(batches, batch)
			nextBatchNumber++
			return nil
		}
		if len(items) == 1 {
			return splitOversizedItem(items[0])
		}
		mid := len(items) / 2
		if err := split(items[:mid]); err != nil {
			return err
		}
		return split(items[mid:])
	}

	if err := split(projection.Items); err != nil {
		return nil, err
	}
	return batches, nil
}

func collectCatalogsForItems(items []CatalogAIItem, catalogs []CatalogAICatalog) []CatalogAICatalog {
	if len(items) == 0 || len(catalogs) == 0 {
		return nil
	}
	needed := make(map[string]struct{}, len(items))
	for _, item := range items {
		needed[item.CatalogID] = struct{}{}
	}
	out := make([]CatalogAICatalog, 0, len(needed))
	for _, catalog := range catalogs {
		if _, ok := needed[catalog.ID]; ok {
			out = append(out, catalog)
		}
	}
	return out
}

// collectSchemas converts a schema map to a slice.
func collectSchemas(m map[string]CatalogAIAttributeSchema) []CatalogAIAttributeSchema {
	if len(m) == 0 {
		return nil
	}
	out := make([]CatalogAIAttributeSchema, 0, len(m))
	for _, s := range m {
		out = append(out, s)
	}
	return out
}

// createBatchRecord persists a batch state in ai_catalog_batches per contract ⑨ §22.
func (c *CatalogBatchController) createBatchRecord(ctx context.Context, runID string, batch CatalogAIBatchPayload) (ports.AICatalogBatchRecord, error) {
	now := c.Now()
	return c.RunRepo.CreateCatalogBatch(ctx, ports.AICatalogBatchRecord{
		ID:           c.NewID(),
		AIRunID:      runID,
		BatchNumber:  batch.BatchNumber,
		Status:       "pending",
		ItemsCount:   len(batch.Items),
		SchemasCount: len(batch.AttributeSchemas),
		CreatedAt:    now,
		UpdatedAt:    now,
	})
}

func (c *CatalogBatchController) markBatchRunning(ctx context.Context, batchID string) error {
	now := c.Now()
	_, err := c.RunRepo.UpdateCatalogBatch(ctx, batchID, ports.AICatalogBatchPatch{
		Status:    "running",
		StartedAt: &now,
	})
	return err
}

func (c *CatalogBatchController) markBatchCompleted(ctx context.Context, batchID string, candidateCount int) error {
	now := c.Now()
	_, err := c.RunRepo.UpdateCatalogBatch(ctx, batchID, ports.AICatalogBatchPatch{
		Status:         "completed",
		CandidateCount: &candidateCount,
		CompletedAt:    &now,
	})
	return err
}

func (c *CatalogBatchController) markBatchFailed(ctx context.Context, batchID, reason string) error {
	now := c.Now()
	_, err := c.RunRepo.UpdateCatalogBatch(ctx, batchID, ports.AICatalogBatchPatch{
		Status:        "failed",
		FailureReason: &reason,
		CompletedAt:   &now,
	})
	return err
}

// coverageComplete implements contract ② §3 — every batch must be COMPLETED.
func (c *CatalogBatchController) coverageComplete(records []ports.AICatalogBatchRecord) bool {
	for _, r := range records {
		if r.Status != "completed" {
			return false
		}
	}
	return true
}

// runFinalEvaluation calls the contract ② §6 final Gemini evaluation.
// Per contract ② §6, the Final Gemini sees:
//   - Customer Message
//   - Conversation Context
//   - Candidate Results (IDs + reason)
//   - الدليل التجاري المرتبط بالمرشحين (full product details for each candidate)
//
// Without the full product details, Gemini only sees IDs and cannot
// compose a response with names, prices, descriptions.
func (c *CatalogBatchController) runFinalEvaluation(
	ctx context.Context,
	input CatalogEvaluationInput,
	candidates []ports.CatalogBatchCandidate,
	projection CatalogAIProjection,
	startBatchNumber int,
) (ports.CustomerSalesProposal, CatalogAIProjection, error) {
	candidates = normalizeBatchCandidates(candidates)
	candidateProjection := CatalogAIProjection{}
	appendCandidateProjection(
		&candidateProjection,
		CatalogAIBatchPayload{
			Catalogs:         projection.Catalogs,
			AttributeSchemas: projection.AttributeSchemas,
			Items:            projection.Items,
		},
		candidates,
		make(map[string]struct{}),
	)

	nextBatchNumber := startBatchNumber
	if nextBatchNumber <= 0 {
		nextBatchNumber = 1
	}
	budget := c.TokenBudget
	if budget <= 0 {
		budget = 8000
	}
	lastOversizeTokens := 0

	for {
		if err := ctx.Err(); err != nil {
			return ports.CustomerSalesProposal{}, CatalogAIProjection{}, err
		}

		finalInput, userPrompt := buildFinalEvaluationPayload(input, candidates, candidateProjection)

		if exact, ok := c.Gemini.(ExactFinalTokenCounter); ok {
			tokens, err := exact.CountFinalTokens(ctx, finalInput, userPrompt)
			if err != nil {
				return ports.CustomerSalesProposal{}, CatalogAIProjection{}, fmt.Errorf("count final evaluation tokens: %w", err)
			}
			if tokens > budget {
				if len(candidateProjection.Items) == 0 {
					return ports.CustomerSalesProposal{}, CatalogAIProjection{}, fmt.Errorf(
						"final evaluation base context exceeds token budget %d with exact request size %d",
						budget, tokens,
					)
				}
				if lastOversizeTokens > 0 && tokens >= lastOversizeTokens {
					return ports.CustomerSalesProposal{}, CatalogAIProjection{}, fmt.Errorf(
						"candidate reduction made no token progress: previous=%d current=%d budget=%d",
						lastOversizeTokens, tokens, budget,
					)
				}
				lastOversizeTokens = tokens

				reducedCandidates, reducedProjection, nextNumber, err := c.reduceCandidateRound(
					ctx,
					input,
					candidateProjection,
					nextBatchNumber,
				)
				if err != nil {
					return ports.CustomerSalesProposal{}, CatalogAIProjection{}, err
				}
				candidates = normalizeBatchCandidates(reducedCandidates)
				candidateProjection = reducedProjection
				nextBatchNumber = nextNumber
				continue
			}
		}

		proposal, usage, err := c.Gemini.FinalEvaluateWithDetails(ctx, finalInput, userPrompt)
		if err != nil {
			return ports.CustomerSalesProposal{}, CatalogAIProjection{}, err
		}
		if proposal.Status == ports.CustomerSalesProposalStatusNeedsMoreData {
			return ports.CustomerSalesProposal{}, CatalogAIProjection{}, errors.New("final catalog evaluation returned needs_more_data after complete catalog coverage")
		}
		c.recordBatchUsage(ctx, input.BusinessID, input.AIRunID, usage, "final_evaluation")
		return proposal, candidateProjection, nil
	}
}

func buildFinalEvaluationPayload(
	input CatalogEvaluationInput,
	candidates []ports.CatalogBatchCandidate,
	candidateProjection CatalogAIProjection,
) (FinalEvaluationInput, string) {
	contextPayload := struct {
		Business               ports.CustomerSalesContextBusiness          `json:"business"`
		Conversation           ports.CustomerSalesContextConversation      `json:"conversation"`
		Customer               ports.CustomerSalesContextCustomer          `json:"customer"`
		KnowledgeEvidence      []ports.CustomerSalesKnowledgeEvidence      `json:"knowledge_evidence,omitempty"`
		BusinessPolicyEvidence []ports.CustomerSalesBusinessPolicyEvidence `json:"business_policy_evidence,omitempty"`
		RecentMessages         []ports.CustomerSalesRecentMessageEvidence  `json:"recent_messages,omitempty"`
		ConversationState      *ports.ConversationStateRecord              `json:"conversation_state,omitempty"`
		ConversationSummary    string                                      `json:"conversation_summary,omitempty"`
		GeneratedAt            time.Time                                   `json:"generated_at"`
		ExpiresAt              time.Time                                   `json:"expires_at"`
	}{
		Business:               input.ConversationContext.Business,
		Conversation:           input.ConversationContext.Conversation,
		Customer:               input.ConversationContext.Customer,
		KnowledgeEvidence:      input.ConversationContext.KnowledgeEvidence,
		BusinessPolicyEvidence: input.ConversationContext.BusinessPolicyEvidence,
		RecentMessages:         input.ConversationContext.RecentMessages,
		ConversationState:      input.ConversationContext.ConversationState,
		ConversationSummary:    input.ConversationContext.ConversationSummary,
		GeneratedAt:            input.ConversationContext.GeneratedAt,
		ExpiresAt:              input.ConversationContext.ExpiresAt,
	}

	userPrompt := fmt.Sprintf(
		"Customer message: %s\n\nVerified conversation context:\n%s\n\nAggregated candidates:\n%s\n\nCandidate catalog projection:\n%s",
		input.CustomerMessage,
		string(mustMarshal(contextPayload)),
		string(mustMarshal(candidates)),
		string(mustMarshal(candidateProjection)),
	)

	return FinalEvaluationInput{
		AIRunID:             input.AIRunID,
		AttemptID:           input.AttemptID,
		BusinessID:          input.BusinessID,
		ConversationID:      input.ConversationID,
		CustomerMessage:     input.CustomerMessage,
		ConversationContext: input.ConversationContext,
		EntityContract:      input.EntityContract,
		CandidateResults:    candidates,
	}, userPrompt
}

func (c *CatalogBatchController) reduceCandidateRound(
	ctx context.Context,
	input CatalogEvaluationInput,
	projection CatalogAIProjection,
	startBatchNumber int,
) ([]ports.CatalogBatchCandidate, CatalogAIProjection, int, error) {
	batches, err := c.splitIntoBatchesMode(ctx, projection, input, startBatchNumber, true)
	if err != nil {
		return nil, CatalogAIProjection{}, startBatchNumber, fmt.Errorf("split candidate reduction batches: %w", err)
	}

	reducedCandidates := make([]ports.CatalogBatchCandidate, 0)
	reducedProjection := CatalogAIProjection{}
	seenItems := make(map[string]struct{})

	for _, batch := range batches {
		rec, err := c.createBatchRecord(ctx, input.AIRunID, batch)
		if err != nil {
			return nil, CatalogAIProjection{}, startBatchNumber, fmt.Errorf("create reduction batch %d: %w", batch.BatchNumber, err)
		}
		if err := c.markBatchRunning(ctx, rec.ID); err != nil {
			return nil, CatalogAIProjection{}, startBatchNumber, err
		}

		result, err := c.Gemini.EvaluateBatch(ctx, BatchEvaluationInput{
			AIRunID:             input.AIRunID,
			AttemptID:           input.AttemptID,
			BatchNumber:         batch.BatchNumber,
			BusinessID:          input.BusinessID,
			ConversationID:      input.ConversationID,
			CustomerMessage:     input.CustomerMessage,
			ConversationContext: input.ConversationContext,
			EntityContract:      input.EntityContract,
			Batch:               batch,
			Reduction:           true,
		})
		if err != nil {
			_ = c.markBatchFailed(ctx, rec.ID, err.Error())
			return nil, CatalogAIProjection{}, startBatchNumber, fmt.Errorf("candidate reduction batch %d: %w", batch.BatchNumber, err)
		}
		if err := validateBatchCandidates(batch, result.Candidates); err != nil {
			_ = c.markBatchFailed(ctx, rec.ID, err.Error())
			return nil, CatalogAIProjection{}, startBatchNumber, fmt.Errorf("candidate reduction batch %d returned invalid evidence: %w", batch.BatchNumber, err)
		}
		if err := c.markBatchCompleted(ctx, rec.ID, len(result.Candidates)); err != nil {
			return nil, CatalogAIProjection{}, startBatchNumber, err
		}

		reducedCandidates = append(reducedCandidates, result.Candidates...)
		appendCandidateProjection(&reducedProjection, batch, result.Candidates, seenItems)
		c.recordBatchUsage(
			ctx,
			input.BusinessID,
			input.AIRunID,
			result.Usage,
			fmt.Sprintf("candidate_reduction_batch_%d", batch.BatchNumber),
		)
		log.Printf(
			"[CatalogBatch] REDUCTION_DONE batch=%d items=%d candidates=%d",
			batch.BatchNumber,
			len(batch.Items),
			len(result.Candidates),
		)
	}

	return normalizeBatchCandidates(reducedCandidates), reducedProjection, startBatchNumber + len(batches), nil
}

// mustMarshal marshals v to JSON, panicking on error (should never fail).
func mustMarshal(v any) []byte {
	buf, err := json.Marshal(v)
	if err != nil {
		return []byte(`[]`)
	}
	return buf
}

// CatalogEvaluationInput is the controller's input.
type CatalogEvaluationInput struct {
	AIRunID             string
	AttemptID           string
	BusinessID          string
	ConversationID      string
	CatalogScope        string // the catalog scope to evaluate (e.g., a specific catalog_id)
	CustomerMessage     string
	ConversationContext ports.CustomerSalesContext
	EntityContract      CatalogEntityContractPayload
}

// countCompleted returns the number of batches with status="completed".
func countCompleted(records []ports.AICatalogBatchRecord) int {
	count := 0
	for _, r := range records {
		if r.Status == "completed" {
			count++
		}
	}
	return count
}

// recordBatchUsage persists per-batch Gemini call telemetry to ai_usage_records.
//
// Per AIUsageTokenTelemetry.md §6: every Gemini call's tokens + cost must be
// recorded. Batch calls and the final evaluation call record:
//   - model_requests = 1
//   - final_ai_replies = 0 (the final reply is counted by AutoReply.recordAIUsage)
//   - tokens (input/cached/output) from Gemini's usageMetadata
//   - provider_cost_yer computed from the pricing table
//   - correlation_id = runID + "|" + phase (for dedup)
//
// Best-effort: errors are logged but never fail the catalog evaluation.
func (c *CatalogBatchController) recordBatchUsage(ctx context.Context, businessID, runID string, usage ports.CustomerSalesUsageTelemetry, phase string) {
	if c.AIUsage == nil {
		return
	}
	// Find the business's active subscription ID.
	var subscriptionID string
	if c.Subscriptions != nil {
		page, err := c.Subscriptions.List(ctx, ports.SubscriptionListFilter{
			BusinessID: businessID,
			Status:     "ACTIVE",
			Limit:      1,
		})
		if err == nil && len(page.Items) > 0 {
			subscriptionID = page.Items[0].ID
		}
	}
	if subscriptionID == "" {
		log.Printf("[CatalogBatch] AI_USAGE_SKIP business=%s reason=no_active_subscription phase=%s", businessID, phase)
		return
	}
	// Compute provider_cost from the pricing table.
	// Same anti-double-counting logic as auto_reply.go:
	// promptTokenCount INCLUDES cachedContentTokenCount, so we subtract.
	totalInput := usage.InputTokens
	cachedInput := usage.CachedTokens
	nonCachedInput := totalInput - cachedInput
	if nonCachedInput < 0 {
		nonCachedInput = 0
	}
	providerCostYER := 0
	pricingVersion := "unknown"
	// Per P1-6: when pricing lookup fails, mark the record with
	// status="pricing_failed" so the aggregate does NOT silently
	// understate provider cost. Same fail-safe applied to AutoReply
	// + ConversationSummary — the catalog batch path was the last
	// holdout still writing cost=0 as "success".
	pricingFailed := false
	if c.AIPricing != nil {
		pricing, err := c.AIPricing.GetCurrentForProvider(ctx, "google_gemini", usage.Model)
		if err == nil {
			pricingVersion = pricing.PricingVersion
			inputCost := int64(nonCachedInput) * int64(pricing.InputPerMillionYER) / 1_000_000
			cachedCost := int64(cachedInput) * int64(pricing.CachedInputPerMillionYER) / 1_000_000
			outputCost := int64(usage.OutputTokens) * int64(pricing.OutputPerMillionYER) / 1_000_000
			providerCostYER = int(inputCost + cachedCost + outputCost)
		} else {
			log.Printf("[CatalogBatch] AI_USAGE_PRICING_LOOKUP_FAILED business=%s model=%s phase=%s err=%v", businessID, usage.Model, phase, err)
			pricingFailed = true
		}
	} else {
		log.Printf("[CatalogBatch] AI_USAGE_PRICING_REPO_NOT_WIRED business=%s model=%s phase=%s", businessID, usage.Model, phase)
		pricingFailed = true
	}
	recordStatus := "success"
	if pricingFailed {
		recordStatus = "pricing_failed"
	}
	// Use correlation_id = runID + "|" + phase for dedup.
	// If the same phase is recorded twice for the same run, it's a retry —
	// the AppendRecord generates a new UUID so we rely on the caller not
	// calling recordBatchUsage twice for the same phase. The catalog batch
	// controller calls it exactly once per batch + once for final evaluation.
	correlationID := runID + "|" + phase
	recordID := c.NewID()
	now := c.Now()
	// Per P2-13: use the REAL latency from the Gemini call
	// (usage.LatencyMs) to compute StartedAt — NOT EstimatedCostMicros
	// (which was always 0, resulting in StartedAt == CompletedAt +
	// latency=0 for every batch record).
	startedAt := now.Add(-time.Duration(usage.LatencyMs) * time.Millisecond)
	_, err := c.AIUsage.AppendRecord(ctx, ports.AIUsageAppend{
		ID:                recordID,
		BusinessID:        businessID,
		SubscriptionID:    subscriptionID,
		Provider:          "google_gemini",
		Model:             usage.Model,
		InputTokens:       int64(nonCachedInput),
		CachedInputTokens: int64(cachedInput),
		OutputTokens:      int64(usage.OutputTokens),
		ModelRequests:     1,
		ToolCalls:         0,
		FinalAIReplies:    0, // batch calls never count as final replies
		ProviderCostYER:   providerCostYER,
		PricingVersion:    pricingVersion,
		Status:            recordStatus,
		CorrelationID:     &correlationID,
		StartedAt:         startedAt,
		CompletedAt:       now,
		Now:               now,
	})
	if err != nil {
		log.Printf("[CatalogBatch] AI_USAGE_RECORD_FAILED business=%s phase=%s err=%v", businessID, phase, err)
		return
	}
	log.Printf("[CatalogBatch] AI_USAGE_RECORDED business=%s phase=%s tokens_in=%d cached=%d tokens_out=%d cost_yer=%d",
		businessID, phase, nonCachedInput, cachedInput, usage.OutputTokens, providerCostYER)
}
