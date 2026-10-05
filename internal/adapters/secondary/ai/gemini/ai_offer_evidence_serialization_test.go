// Package gemini — JSON serialization contract tests for CustomerSalesOfferEvidence.
//
// Per ADR-046: verifies that the JSON sent to Gemini via customerSalesPromptContext
// uses "availability_status" (snake_case) as the field key — matching
// the DB column, Catalog Entity Contract, and the prompt references.
//
// Before ADR-046, CustomerSalesOfferEvidence had no JSON tags, so Go's
// encoding/json serialized the field as "AvailabilityState" (PascalCase)
// — mismatching the Contract's "availability_status" and the prompt
// references "availability_state". This test proves the fix and prevents
// regression.
package gemini

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/Ammar777782439/mujeeb24-backend-go/internal/application/ports"
)

// TestCustomerSalesOfferEvidenceJSONSerialization verifies that the CustomerSalesOfferEvidence
// struct serializes with the correct JSON key "availability_status"
// (snake_case), matching the Catalog Entity Contract and DB column.
//
// Per ADR-046, the chain is:
//
//	DB column: availability_status
//	Domain: AvailabilityStatus
//	Contract: AvailabilityStatus (json:"availability_status")
//	AI Evidence: AvailabilityStatus (json:"availability_status")
//	JSON to Gemini: "availability_status"
//	Prompt: availability_status
//	Gemini output: availability_status
//
// This test specifically proves the JSON serialization boundary —
// it uses the SAME encoding/json path as the production code
// (buildUserPrompt → customerSalesPromptContextFrom → json.Marshal).
func TestCustomerSalesOfferEvidenceJSONSerialization(t *testing.T) {
	// Build a customerSalesPromptContext with one offer evidence, exactly like
	// the production code does in customerSalesPromptContextFrom().
	ctx := customerSalesPromptContext{
		OfferEvidence: []ports.CustomerSalesOfferEvidence{{
			Reference:          "offer-001",
			Name:               "Test Offer",
			PricingMode:        "fixed",
			Amount:             "3500",
			Currency:           "SAR",
			AvailabilityStatus: "available",
			Status:             "active",
		}},
	}

	encoded, err := json.Marshal(ctx)
	if err != nil {
		t.Fatalf("json.Marshal failed: %v", err)
	}
	jsonStr := string(encoded)

	// MUST contain "availability_status" (snake_case) — the Contract key.
	if !strings.Contains(jsonStr, `"availability_status"`) {
		t.Fatalf("JSON does not contain \"availability_status\" key.\n"+
			"Got: %s", jsonStr)
	}

	// MUST NOT contain "AvailabilityState" (old PascalCase, pre-ADR-046).
	if strings.Contains(jsonStr, `"AvailabilityState"`) {
		t.Fatalf("JSON still contains old PascalCase \"AvailabilityState\" key.\n"+
			"Got: %s", jsonStr)
	}

	// MUST NOT contain "availability_state" (old snake_case used in prompts
	// pre-ADR-046, which never matched the actual JSON key).
	if strings.Contains(jsonStr, `"availability_state"`) {
		t.Fatalf("JSON contains \"availability_state\" which was never the "+
			"Contract field name.\nGot: %s", jsonStr)
	}

	// Verify the value is correctly serialized.
	var decoded map[string]any
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatalf("json.Unmarshal failed: %v", err)
	}
	offers, ok := decoded["offer_evidence"].([]any)
	if !ok || len(offers) != 1 {
		t.Fatalf("expected 1 offer in offer_evidence, got: %v", decoded["offer_evidence"])
	}
	offer, ok := offers[0].(map[string]any)
	if !ok {
		t.Fatalf("expected offer to be a map, got: %T", offers[0])
	}
	val, ok := offer["availability_status"]
	if !ok {
		t.Fatalf("availability_status key missing from serialized offer: %v", offer)
	}
	if val != "available" {
		t.Fatalf("expected availability_status=\"available\", got: %v", val)
	}
}


func TestCustomerSalesOfferEvidenceSerializesCommercialProvenanceWhenPresent(t *testing.T) {
	now := time.Unix(1_700_000_000, 0).UTC()
	ctx := customerSalesPromptContext{
		OfferEvidence: []ports.CustomerSalesOfferEvidence{{
			Reference:               "offer-001",
			Name:                    "Test Offer",
			PricingMode:             "fixed",
			Amount:                  "3500",
			Currency:                "SAR",
			PricingUnit:             "unit",
			PriceSource:             "merchant",
			PriceVerificationStatus: "verified",
			PriceCheckedAt:          &now,
			AvailabilityMode:        "stock",
			AvailabilityStatus:      "available",
			AvailabilityValidUntil:  &now,
			FulfillmentMode:         "delivery",
			Status:                  "active",
		}},
	}

	encoded, err := json.Marshal(ctx)
	if err != nil {
		t.Fatalf("json.Marshal failed: %v", err)
	}
	jsonStr := string(encoded)
	for _, key := range []string{
		`"price_verification_status"`,
		`"price_checked_at"`,
		`"availability_mode"`,
		`"availability_valid_until"`,
		`"fulfillment_mode"`,
	} {
		if !strings.Contains(jsonStr, key) {
			t.Fatalf("expected %s in serialized offer evidence: %s", key, jsonStr)
		}
	}
}
