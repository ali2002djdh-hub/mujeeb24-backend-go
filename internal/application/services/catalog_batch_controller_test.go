package services

import (
	"context"
	"strings"
	"testing"

	"github.com/Ammar777782439/mujeeb24-backend-go/internal/application/ports"
)

type exactTokenBatchGeminiStub struct {
	countFn       func(BatchEvaluationInput) (int, error)
	seen          []BatchEvaluationInput
	finalProposal ports.CustomerSalesProposal
}

func (s *exactTokenBatchGeminiStub) CountBatchTokens(_ context.Context, input BatchEvaluationInput) (int, error) {
	s.seen = append(s.seen, input)
	if s.countFn != nil {
		return s.countFn(input)
	}
	return 0, nil
}

func (s *exactTokenBatchGeminiStub) EvaluateBatch(_ context.Context, input BatchEvaluationInput) (ports.CatalogBatchResult, error) {
	return ports.CatalogBatchResult{BatchNumber: input.BatchNumber}, nil
}

func (s *exactTokenBatchGeminiStub) FinalEvaluateWithDetails(_ context.Context, _ FinalEvaluationInput, _ string) (ports.CustomerSalesProposal, ports.CustomerSalesUsageTelemetry, error) {
	return s.finalProposal, ports.CustomerSalesUsageTelemetry{}, nil
}

func TestSplitIntoBatchesUsesExactRequestTokensAndSequentialNumbers(t *testing.T) {
	gemini := &exactTokenBatchGeminiStub{
		countFn: func(input BatchEvaluationInput) (int, error) {
			// Simulate fixed prompt/contract overhead + per-item payload.
			return 100 + 100*len(input.Batch.Items), nil
		},
	}
	controller := &CatalogBatchController{
		Gemini:      gemini,
		TokenBudget: 250,
	}
	schemaID := "schema-1"
	projection := CatalogAIProjection{
		Catalogs: []CatalogAICatalog{{ID: "catalog-1", Name: "Main", Status: "active"}},
		AttributeSchemas: []CatalogAIAttributeSchema{{
			ID: schemaID, Name: "Common", Version: 1,
		}},
		Items: []CatalogAIItem{
			{ID: "item-1", CatalogID: "catalog-1", AttributeSchemaID: &schemaID},
			{ID: "item-2", CatalogID: "catalog-1", AttributeSchemaID: &schemaID},
			{ID: "item-3", CatalogID: "catalog-1", AttributeSchemaID: &schemaID},
		},
	}
	input := CatalogEvaluationInput{
		AIRunID:         "run-1",
		BusinessID:      "business-1",
		ConversationID:  "conversation-1",
		CustomerMessage: "اريد منتج مناسب",
	}

	batches, err := controller.splitIntoBatches(context.Background(), projection, input, 7)
	if err != nil {
		t.Fatalf("splitIntoBatches: %v", err)
	}
	if len(batches) != 3 {
		t.Fatalf("expected 3 exact-token batches, got %d", len(batches))
	}
	for i, batch := range batches {
		wantNumber := 7 + i
		if batch.BatchNumber != wantNumber {
			t.Fatalf("batch[%d].BatchNumber=%d, want %d", i, batch.BatchNumber, wantNumber)
		}
		if len(batch.Items) != 1 {
			t.Fatalf("batch[%d] contains %d items; exact token split should keep one", i, len(batch.Items))
		}
		if len(batch.Catalogs) != 1 || batch.Catalogs[0].ID != "catalog-1" {
			t.Fatalf("batch[%d] lost catalog relationship: %+v", i, batch.Catalogs)
		}
		if len(batch.AttributeSchemas) != 1 || batch.AttributeSchemas[0].ID != schemaID {
			t.Fatalf("batch[%d] lost used schema: %+v", i, batch.AttributeSchemas)
		}
	}
	if len(gemini.seen) == 0 || gemini.seen[0].CustomerMessage != input.CustomerMessage {
		t.Fatal("exact token counter did not receive the real BatchEvaluationInput")
	}
}

func TestSplitIntoBatchesRejectsSingleOversizedItem(t *testing.T) {
	gemini := &exactTokenBatchGeminiStub{
		countFn: func(input BatchEvaluationInput) (int, error) {
			return 9999, nil
		},
	}
	controller := &CatalogBatchController{Gemini: gemini, TokenBudget: 500}
	projection := CatalogAIProjection{
		Items: []CatalogAIItem{{ID: "huge-item", CatalogID: "catalog-1"}},
	}
	_, err := controller.splitIntoBatches(context.Background(), projection, CatalogEvaluationInput{
		AIRunID: "run-1", BusinessID: "business-1", CustomerMessage: "test",
	}, 1)
	if err == nil {
		t.Fatal("expected oversized single item to fail closed")
	}
	if !strings.Contains(err.Error(), "huge-item") || !strings.Contains(err.Error(), "exceeds token budget") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateBatchCandidatesEnforcesItemVariantOfferRelationships(t *testing.T) {
	batch := CatalogAIBatchPayload{
		Items: []CatalogAIItem{
			{
				ID: "item-1",
				Variants: []CatalogAIVariant{{ID: "variant-1", CatalogItemID: "item-1"}},
				Offers: []CatalogAIOffer{{ID: "offer-1", CatalogItemID: "item-1"}},
			},
			{
				ID: "item-2",
				Variants: []CatalogAIVariant{{ID: "variant-2", CatalogItemID: "item-2"}},
				Offers: []CatalogAIOffer{{ID: "offer-2", CatalogItemID: "item-2"}},
			},
		},
	}

	if err := validateBatchCandidates(batch, []ports.CatalogBatchCandidate{{
		ItemID: "item-1", VariantIDs: []string{"variant-1"}, OfferIDs: []string{"offer-1"},
	}}); err != nil {
		t.Fatalf("valid relational candidate rejected: %v", err)
	}

	if err := validateBatchCandidates(batch, []ports.CatalogBatchCandidate{{
		ItemID: "item-1", VariantIDs: []string{"variant-2"},
	}}); err == nil {
		t.Fatal("expected cross-item variant candidate to be rejected")
	}

	if err := validateBatchCandidates(batch, []ports.CatalogBatchCandidate{{
		ItemID: "item-1", OfferIDs: []string{"offer-2"},
	}}); err == nil {
		t.Fatal("expected cross-item offer candidate to be rejected")
	}

	linkedVariantA := "variant-a"
	linkedVariantB := "variant-b"
	sameItemBatch := CatalogAIBatchPayload{
		Items: []CatalogAIItem{{
			ID: "item-3",
			Variants: []CatalogAIVariant{
				{ID: linkedVariantA, CatalogItemID: "item-3"},
				{ID: linkedVariantB, CatalogItemID: "item-3"},
			},
			Offers: []CatalogAIOffer{{
				ID: "offer-3", CatalogItemID: "item-3", VariantID: &linkedVariantB,
			}},
		}},
	}
	if err := validateBatchCandidates(sameItemBatch, []ports.CatalogBatchCandidate{{
		ItemID: "item-3", VariantIDs: []string{linkedVariantA}, OfferIDs: []string{"offer-3"},
	}}); err == nil {
		t.Fatal("expected offer linked to a different selected variant to be rejected")
	}
}

func TestReductionSplitUsesReductionRequestShape(t *testing.T) {
	gemini := &exactTokenBatchGeminiStub{
		countFn: func(input BatchEvaluationInput) (int, error) {
			return 100, nil
		},
	}
	controller := &CatalogBatchController{Gemini: gemini, TokenBudget: 500}
	projection := CatalogAIProjection{
		Items: []CatalogAIItem{{ID: "item-1", CatalogID: "catalog-1"}},
	}
	_, err := controller.splitIntoBatchesMode(context.Background(), projection, CatalogEvaluationInput{
		AIRunID: "run-1", BusinessID: "business-1", CustomerMessage: "test",
	}, 4, true)
	if err != nil {
		t.Fatalf("splitIntoBatchesMode: %v", err)
	}
	if len(gemini.seen) == 0 || !gemini.seen[0].Reduction {
		t.Fatal("exact token counter must count the reduction request shape, not the normal batch prompt")
	}
	if gemini.seen[0].BatchNumber != 4 {
		t.Fatalf("reduction batch number=%d, want 4", gemini.seen[0].BatchNumber)
	}
}

func TestAppendCandidateProjectionPrunesUnselectedNestedEvidence(t *testing.T) {
	variantA := "variant-a"
	variantB := "variant-b"
	offerA := "offer-a"
	offerB := "offer-b"
	batch := CatalogAIBatchPayload{
		Items: []CatalogAIItem{{
			ID: "item-1", CatalogID: "catalog-1",
			Variants: []CatalogAIVariant{
				{ID: variantA, CatalogItemID: "item-1"},
				{ID: variantB, CatalogItemID: "item-1"},
			},
			Offers: []CatalogAIOffer{
				{ID: offerA, CatalogItemID: "item-1", VariantID: &variantA},
				{ID: offerB, CatalogItemID: "item-1", VariantID: &variantB},
			},
		}},
	}
	target := CatalogAIProjection{}
	appendCandidateProjection(&target, batch, []ports.CatalogBatchCandidate{{
		ItemID: "item-1", VariantIDs: []string{variantA}, OfferIDs: []string{offerA},
	}}, map[string]struct{}{})

	if len(target.Items) != 1 {
		t.Fatalf("expected one candidate item, got %d", len(target.Items))
	}
	item := target.Items[0]
	if len(item.Variants) != 1 || item.Variants[0].ID != variantA {
		t.Fatalf("unexpected final variants: %+v", item.Variants)
	}
	if len(item.Offers) != 1 || item.Offers[0].ID != offerA {
		t.Fatalf("unexpected final offers: %+v", item.Offers)
	}
}


func TestSplitIntoBatchesFragmentsOversizedItemWithoutLosingNestedEvidence(t *testing.T) {
	v1, v2 := "variant-1", "variant-2"
	gemini := &exactTokenBatchGeminiStub{
		countFn: func(input BatchEvaluationInput) (int, error) {
			if len(input.Batch.Items) == 0 {
				return 0, nil
			}
			item := input.Batch.Items[0]
			return 200 + 100*(len(item.Variants)+len(item.Offers)), nil
		},
	}
	controller := &CatalogBatchController{Gemini: gemini, TokenBudget: 450}
	projection := CatalogAIProjection{
		Catalogs: []CatalogAICatalog{{ID: "catalog-1", Name: "Main", Status: "active"}},
		Items: []CatalogAIItem{{
			ID: "item-1", CatalogID: "catalog-1",
			Variants: []CatalogAIVariant{
				{ID: v1, CatalogItemID: "item-1"},
				{ID: v2, CatalogItemID: "item-1"},
			},
			Offers: []CatalogAIOffer{
				{ID: "offer-1", CatalogItemID: "item-1", VariantID: &v1},
				{ID: "offer-2", CatalogItemID: "item-1", VariantID: &v2},
			},
		}},
	}

	batches, err := controller.splitIntoBatches(context.Background(), projection, CatalogEvaluationInput{
		AIRunID: "run-1", BusinessID: "business-1", CustomerMessage: "test",
	}, 1)
	if err != nil {
		t.Fatalf("split oversized item: %v", err)
	}
	if len(batches) != 2 {
		t.Fatalf("expected 2 item fragments, got %d", len(batches))
	}
	variantSeen := map[string]bool{}
	offerSeen := map[string]bool{}
	for _, batch := range batches {
		if len(batch.Items) != 1 || batch.Items[0].ID != "item-1" {
			t.Fatalf("fragment lost item identity: %+v", batch.Items)
		}
		for _, variant := range batch.Items[0].Variants {
			variantSeen[variant.ID] = true
		}
		for _, offer := range batch.Items[0].Offers {
			offerSeen[offer.ID] = true
		}
	}
	for _, id := range []string{v1, v2} {
		if !variantSeen[id] {
			t.Fatalf("variant %s was not covered by fragments", id)
		}
	}
	for _, id := range []string{"offer-1", "offer-2"} {
		if !offerSeen[id] {
			t.Fatalf("offer %s was not covered by fragments", id)
		}
	}
}

func TestAppendCandidateProjectionMergesSameItemFragments(t *testing.T) {
	v1, v2 := "variant-1", "variant-2"
	target := CatalogAIProjection{}
	seen := map[string]struct{}{}

	batch1 := CatalogAIBatchPayload{Items: []CatalogAIItem{{
		ID: "item-1", CatalogID: "catalog-1",
		Variants: []CatalogAIVariant{{ID: v1, CatalogItemID: "item-1"}},
		Offers: []CatalogAIOffer{{ID: "offer-1", CatalogItemID: "item-1", VariantID: &v1}},
	}}}
	batch2 := CatalogAIBatchPayload{Items: []CatalogAIItem{{
		ID: "item-1", CatalogID: "catalog-1",
		Variants: []CatalogAIVariant{{ID: v2, CatalogItemID: "item-1"}},
		Offers: []CatalogAIOffer{{ID: "offer-2", CatalogItemID: "item-1", VariantID: &v2}},
	}}}

	appendCandidateProjection(&target, batch1, []ports.CatalogBatchCandidate{{
		ItemID: "item-1", VariantIDs: []string{v1}, OfferIDs: []string{"offer-1"},
	}}, seen)
	appendCandidateProjection(&target, batch2, []ports.CatalogBatchCandidate{{
		ItemID: "item-1", VariantIDs: []string{v2}, OfferIDs: []string{"offer-2"},
	}}, seen)

	if len(target.Items) != 1 {
		t.Fatalf("expected one merged item, got %d", len(target.Items))
	}
	if len(target.Items[0].Variants) != 2 || len(target.Items[0].Offers) != 2 {
		t.Fatalf("fragment evidence was not merged: variants=%d offers=%d", len(target.Items[0].Variants), len(target.Items[0].Offers))
	}
}

func TestFinalEvaluationRejectsNeedsMoreDataAfterCoverage(t *testing.T) {
	gemini := &exactTokenBatchGeminiStub{
		finalProposal: ports.CustomerSalesProposal{
			Status: ports.CustomerSalesProposalStatusNeedsMoreData,
			Action: ports.CustomerSalesProposalActionClarification,
			ResponseText: "أحتاج بيانات إضافية",
		},
	}
	controller := &CatalogBatchController{Gemini: gemini, TokenBudget: 8000}
	_, _, err := controller.runFinalEvaluation(context.Background(), CatalogEvaluationInput{
		AIRunID: "run-1", BusinessID: "business-1", CustomerMessage: "test",
	}, nil, CatalogAIProjection{}, 1)
	if err == nil {
		t.Fatal("expected final needs_more_data to fail after complete catalog coverage")
	}
	if !strings.Contains(err.Error(), "complete catalog coverage") {
		t.Fatalf("unexpected error: %v", err)
	}
}
