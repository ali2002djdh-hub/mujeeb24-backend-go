package services

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/Ammar777782439/mujeeb24-backend-go/internal/application/ports"
)

func TestCatalogItemEvidencePreservesSchemaLinkage(t *testing.T) {
	schemaID := "schema-1"
	version := 3
	item := ports.CatalogItemRecord{
		ID:                     "item-1",
		CatalogID:              "catalog-1",
		AttributeSchemaID:      &schemaID,
		AttributeSchemaVersion: &version,
		ItemType:               "custom_vertical_item",
		Name:                   "Example",
		Status:                 "active",
		PricingMode:            "fixed",
		AvailabilityMode:       "always_available",
		FulfillmentMode:        "manual",
		Attributes:             json.RawMessage(`{"custom_metric":12}`),
	}
	evidence := catalogItemEvidence(item, time.Unix(0, 0).UTC())
	if evidence.AttributeSchemaReference == nil || *evidence.AttributeSchemaReference != schemaID {
		t.Fatalf("schema reference not preserved: %+v", evidence.AttributeSchemaReference)
	}
	if evidence.AttributeSchemaVersion == nil || *evidence.AttributeSchemaVersion != version {
		t.Fatalf("schema version not preserved: %+v", evidence.AttributeSchemaVersion)
	}
}

func TestCustomerSalesSchemaEvidencePreservesDefinitionsAndRules(t *testing.T) {
	schema := ports.AttributeSchemaRecord{
		ID:      "schema-1",
		Name:    "Universal Example",
		Version: 2,
		Definitions: []ports.AttributeDefinitionRecord{{
			ID:              "definition-1",
			Key:             "custom_metric",
			Label:           "Custom Metric",
			DataType:        "number",
			Required:        true,
			ValidationRules: json.RawMessage(`{"min":1,"max":100}`),
			DisplayOrder:    1,
		}},
	}
	evidence := customerSalesSchemaEvidence(schema)
	if len(evidence.Definitions) != 1 {
		t.Fatalf("expected one definition, got %d", len(evidence.Definitions))
	}
	def := evidence.Definitions[0]
	if def.SchemaID != schema.ID || def.AttributeKey != "custom_metric" || def.DataType != "number" || !def.IsRequired {
		t.Fatalf("definition mapping mismatch: %+v", def)
	}
	if def.ValidationRules["min"] != float64(1) || def.ValidationRules["max"] != float64(100) {
		t.Fatalf("validation rules not preserved: %+v", def.ValidationRules)
	}
}

func TestOfferEvidenceMarksUnverifiedOrExpiredFactsStale(t *testing.T) {
	now := time.Unix(1_700_000_000, 0).UTC()
	past := now.Add(-time.Minute)

	unverified := ports.OfferRecord{
		ID: "offer-1", BusinessID: "business-1", CatalogItemID: "item-1",
		Name: "Offer", PricingMode: "fixed", PriceVerificationStatus: "unverified",
		AvailabilityMode: "always_available", AvailabilityStatus: "available",
		FulfillmentMode: "manual", Status: "active",
	}
	if got := toOfferEvidence(unverified, now); got.EvidenceState != CustomerSalesContextStale {
		t.Fatalf("unverified price must be stale evidence, got %q", got.EvidenceState)
	}

	expiredAvailability := unverified
	expiredAvailability.ID = "offer-2"
	expiredAvailability.PriceVerificationStatus = "verified"
	expiredAvailability.AvailabilityValidUntil = &past
	if got := toOfferEvidence(expiredAvailability, now); got.EvidenceState != CustomerSalesContextStale {
		t.Fatalf("expired availability validity must be stale evidence, got %q", got.EvidenceState)
	}

	future := now.Add(time.Hour)
	notStarted := unverified
	notStarted.ID = "offer-3"
	notStarted.PriceVerificationStatus = "verified"
	notStarted.ValidityFrom = &future
	if got := toOfferEvidence(notStarted, now); got.EvidenceState != CustomerSalesContextStale {
		t.Fatalf("future offer validity must not be fresh evidence, got %q", got.EvidenceState)
	}
}
