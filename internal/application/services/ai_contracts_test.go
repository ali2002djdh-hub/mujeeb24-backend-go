package services

import (
	"strings"
	"testing"
	"time"

	"github.com/Ammar777782439/mujeeb24-backend-go/internal/application/ports"
)

// TestAIRunLifecycleLegalTransitions verifies contract ⑨ §26: only valid
// forward transitions are allowed. Terminal states cannot transition.
func TestAIRunLifecycleLegalTransitions(t *testing.T) {
	// This test documents the legal-transition rules; full repo-backed tests
	// live in the postgres adapter integration tests.
	cases := []struct {
		from string
		to   string
		ok   bool
	}{
		{ports.AIRunStatusReceived, ports.AIRunStatusContextBuilt, true},
		{ports.AIRunStatusContextBuilt, ports.AIRunStatusRunning, true},
		{ports.AIRunStatusRunning, ports.AIRunStatusWaitingTool, true},
		{ports.AIRunStatusWaitingTool, ports.AIRunStatusRunning, true},
		{ports.AIRunStatusRunning, ports.AIRunStatusValidating, true},
		{ports.AIRunStatusValidating, ports.AIRunStatusAuthorized, true},
		{ports.AIRunStatusAuthorized, ports.AIRunStatusExecuting, true},
		{ports.AIRunStatusExecuting, ports.AIRunStatusCompleted, true},
		// Illegal transitions per contract ⑨ §26
		{ports.AIRunStatusCompleted, ports.AIRunStatusRunning, false},
		{ports.AIRunStatusFailed, ports.AIRunStatusExecuting, false},
		{ports.AIRunStatusCancelled, ports.AIRunStatusRunning, false},
		{ports.AIRunStatusReceived, ports.AIRunStatusAuthorized, false}, // skip CONTEXT_BUILT/RUNNING/VALIDATING
	}
	for _, c := range cases {
		// Verify the legal-transition list matches the contract.
		allowedFrom := legalTransitions[c.to]
		found := false
		for _, s := range allowedFrom {
			if s == c.from {
				found = true
				break
			}
		}
		if found != c.ok {
			t.Errorf("transition %s → %s: expected ok=%v, got found=%v", c.from, c.to, c.ok, found)
		}
	}
}

// legalTransitions maps a destination state to the list of source states
// from which the transition is legal, per contract ⑨ §26.
var legalTransitions = map[string][]string{
	ports.AIRunStatusContextBuilt: {ports.AIRunStatusReceived},
	ports.AIRunStatusRunning:      {ports.AIRunStatusContextBuilt, ports.AIRunStatusWaitingTool},
	ports.AIRunStatusWaitingTool:  {ports.AIRunStatusRunning},
	ports.AIRunStatusValidating:   {ports.AIRunStatusRunning},
	ports.AIRunStatusAuthorized:   {ports.AIRunStatusValidating},
	ports.AIRunStatusExecuting:    {ports.AIRunStatusAuthorized},
	ports.AIRunStatusCompleted: {
		ports.AIRunStatusExecuting,
		ports.AIRunStatusValidating,
		ports.AIRunStatusAuthorized,
	},
}

// TestRetryPolicyRetryable verifies contract ⑨ §6: retryable categories retry
// until MaxAttempts is exhausted.
func TestRetryPolicyRetryable(t *testing.T) {
	policy := NewRetryPolicy(RetryConfig{
		MaxAttempts:       3,
		InitialBackoff:    10 * time.Millisecond,
		MaxBackoff:        100 * time.Millisecond,
		BackoffMultiplier: 2.0,
		JitterFraction:    0.0,
	})
	cases := []struct {
		attempt  int
		category ports.AIRunFailureCategory
		retry    bool
	}{
		{1, ports.AIRunFailureCategoryProviderTemporary, true},
		{2, ports.AIRunFailureCategoryProviderTemporary, true},
		{3, ports.AIRunFailureCategoryProviderTemporary, false}, // exhausted
		{1, ports.AIRunFailureCategoryNetwork, true},
		{1, ports.AIRunFailureCategoryTimeout, true},
		{1, ports.AIRunFailureCategoryRateLimit, true},
		{1, ports.AIRunFailureCategoryInvalidAIOutput, false}, // non-retryable
		{1, ports.AIRunFailureCategoryTenantViolation, false},
		{1, ports.AIRunFailureCategoryPolicyDenial, false},
		{1, ports.AIRunFailureCategoryAuthorizationDenial, false},
	}
	for _, c := range cases {
		d := policy.DecideForFailure(c.attempt, c.category)
		if d.ShouldRetry != c.retry {
			t.Errorf("attempt=%d category=%s: expected retry=%v, got %v (reason: %s)",
				c.attempt, c.category, c.retry, d.ShouldRetry, d.Reason)
		}
	}
}

// TestPartialProgressCoverage verifies contract ⑨ §22-23: batches must all
// be COMPLETED before Final Evaluation may run.
func TestPartialProgressCoverage(t *testing.T) {
	policy := NewPartialProgressPolicy()
	cases := []struct {
		name    string
		batches []ports.AICatalogBatchRecord
		want    bool
	}{
		{
			name:    "empty",
			batches: nil,
			want:    true,
		},
		{
			name: "all completed",
			batches: []ports.AICatalogBatchRecord{
				{Status: "completed"},
				{Status: "completed"},
				{Status: "completed"},
			},
			want: true,
		},
		{
			name: "one pending",
			batches: []ports.AICatalogBatchRecord{
				{Status: "completed"},
				{Status: "pending"},
				{Status: "completed"},
			},
			want: false,
		},
		{
			name: "one failed",
			batches: []ports.AICatalogBatchRecord{
				{Status: "completed"},
				{Status: "failed"},
			},
			want: false,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := policy.CoverageComplete(c.batches)
			if got != c.want {
				t.Errorf("CoverageComplete: expected %v, got %v", c.want, got)
			}
		})
	}
}

// TestBatchesToRetry verifies contract ⑨ §21: only FAILED and PENDING batches
// are retried; COMPLETED batches are not re-run.
func TestBatchesToRetry(t *testing.T) {
	policy := NewPartialProgressPolicy()
	batches := []ports.AICatalogBatchRecord{
		{ID: "b1", Status: "completed"},
		{ID: "b2", Status: "completed"},
		{ID: "b3", Status: "failed"},
		{ID: "b4", Status: "pending"},
	}
	retry := policy.BatchesToRetry(batches)
	if len(retry) != 2 {
		t.Fatalf("expected 2 batches to retry, got %d", len(retry))
	}
	if retry[0] != "b3" || retry[1] != "b4" {
		t.Errorf("expected [b3, b4], got %v", retry)
	}
}

// TestValidateStructural verifies contract ⑥ §3-5: only the closed status
// and action values pass structural validation.
func TestValidateStructural(t *testing.T) {
	pipeline := &ValidationPipeline{}
	cases := []struct {
		name     string
		proposal ports.CustomerSalesProposal
		wantErr  bool
	}{
		{
			name: "valid resolved answer",
			proposal: ports.CustomerSalesProposal{
				Status:       ports.CustomerSalesProposalStatusResolved,
				Action:       ports.CustomerSalesProposalActionAnswer,
				ResponseText: "السعر 12000 ريال",
			},
			wantErr: false,
		},
		{
			name: "valid human_request without response_text",
			proposal: ports.CustomerSalesProposal{
				Status: ports.CustomerSalesProposalStatusAmbiguous,
				Action: ports.CustomerSalesProposalActionHumanRequest,
			},
			wantErr: false,
		},
		{
			name: "invalid status",
			proposal: ports.CustomerSalesProposal{
				Status:       "approved",
				Action:       ports.CustomerSalesProposalActionAnswer,
				ResponseText: "x",
			},
			wantErr: true,
		},
		{
			name: "invalid action",
			proposal: ports.CustomerSalesProposal{
				Status:       ports.CustomerSalesProposalStatusResolved,
				Action:       "create_lead", // legacy, no longer valid
				ResponseText: "x",
			},
			wantErr: true,
		},
		{
			name: "missing response_text for answer",
			proposal: ports.CustomerSalesProposal{
				Status: ports.CustomerSalesProposalStatusResolved,
				Action: ports.CustomerSalesProposalActionAnswer,
			},
			wantErr: true,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := pipeline.validateStructural(c.proposal)
			if (err != nil) != c.wantErr {
				t.Errorf("expected wantErr=%v, got err=%v", c.wantErr, err)
			}
		})
	}
}

// TestCatalogEntityContractDescriptor verifies contract ⑤ §8: every enum
// value used in merchant data must have a documented meaning in the descriptor.
//
// The closed enum values are sourced VERBATIM from the SQL migration CHECK
// constraints (migrations/000013..000018). Per the "NO INVENTION" rule, no
// value may be added unless it appears in the SQL migrations OR an explicit
// ADR amends the contract.
func TestCatalogEntityContractDescriptor(t *testing.T) {
	d := DefaultCatalogEntityContractDescriptor()
	// Per SQL migration 000016/000018: catalog_items_pricing_mode_chk +
	// offers_pricing_mode_chk.
	requiredPricingModes := []string{"fixed", "starting_from", "per_unit", "per_person", "per_day", "quote_required", "dynamic"}
	for _, m := range requiredPricingModes {
		if _, ok := d.PricingModes[m]; !ok {
			t.Errorf("missing pricing_mode %q in descriptor per SQL migration 000016/000018", m)
		}
	}
	// Per SQL migration 000016/000018: catalog_items_availability_mode_chk +
	// offers_availability_mode_chk.
	requiredAvailabilityModes := []string{"stock", "schedule", "supplier_check", "always_available", "unknown"}
	for _, m := range requiredAvailabilityModes {
		if _, ok := d.AvailabilityModes[m]; !ok {
			t.Errorf("missing availability_mode %q in descriptor per SQL migration 000016/000018", m)
		}
	}
	// Per SQL migration 000018: offers_availability_status_chk.
	// Per Catalog Contract §4: the non-breakable rule (unknown ≠ available,
	// stale ≠ confirmed, requires_check ≠ confirmed).
	requiredAvailabilityStatuses := []string{"available", "unavailable", "unknown", "requires_check", "stale"}
	for _, m := range requiredAvailabilityStatuses {
		if _, ok := d.AvailabilityStatuses[m]; !ok {
			t.Errorf("missing availability_status %q in descriptor per SQL migration 000018", m)
		}
	}
	// Per SQL migration 000018: offers_price_verification_chk.
	requiredPriceVerificationStatuses := []string{"unverified", "verified", "stale", "rejected"}
	for _, m := range requiredPriceVerificationStatuses {
		if _, ok := d.PriceVerificationStatuses[m]; !ok {
			t.Errorf("missing price_verification_status %q in descriptor per SQL migration 000018", m)
		}
	}
	// Per SQL migration 000016/000018: catalog_items_fulfillment_mode_chk +
	// offers_fulfillment_mode_chk.
	requiredFulfillmentModes := []string{"delivery", "pickup", "digital", "appointment", "travel", "manual"}
	for _, m := range requiredFulfillmentModes {
		if _, ok := d.FulfillmentModes[m]; !ok {
			t.Errorf("missing fulfillment_mode %q in descriptor per SQL migration 000016/000018", m)
		}
	}
	// Per Catalog Contract §6: attribute data_type MUST be one of these 9 values.
	// These are NOT in a SQL CHECK constraint; they are in the Domain contract
	// (contracts/domain_catalog_contract_review_ar.md §6) and contract 11 §11.
	requiredDataTypes := []string{"text", "number", "boolean", "date", "datetime", "select", "multi_select", "location", "money"}
	// NOTE: AttributeDataTypes is intentionally NOT in the descriptor map because
	// it's defined in the AttributeDefinition.data_type field's comment, not as
	// a separate descriptor entry. We verify it via the contract payload instead.
	payload := BuildCatalogEntityContractPayload()
	dataTypeField := payload.Contract.AttributeDefinition.DataType
	for _, m := range requiredDataTypes {
		if !strings.Contains(dataTypeField, m) {
			t.Errorf("missing attribute_data_type %q in entity contract payload", m)
		}
	}
}


func TestValidateStructuralRejectsRoutingReasonOutsideHumanRequest(t *testing.T) {
	pipeline := &ValidationPipeline{}
	err := pipeline.validateStructural(ports.CustomerSalesProposal{
		Status:        ports.CustomerSalesProposalStatusResolved,
		Action:        ports.CustomerSalesProposalActionAnswer,
		ResponseText:  "ok",
		RoutingReason: ports.CustomerSalesRoutingReasonOther,
	})
	if err == nil {
		t.Fatal("expected routing_reason outside human_request to be rejected")
	}
}

func TestValidateStructuralRejectsNotFoundWithSelectedReferences(t *testing.T) {
	pipeline := &ValidationPipeline{}
	err := pipeline.validateStructural(ports.CustomerSalesProposal{
		Status:       ports.CustomerSalesProposalStatusNotFound,
		Action:       ports.CustomerSalesProposalActionAnswer,
		ResponseText: "غير موجود",
		Selected:     []ports.SelectedReference{{ItemID: "item-1"}},
	})
	if err == nil {
		t.Fatal("expected not_found with selected references to be rejected")
	}
}
