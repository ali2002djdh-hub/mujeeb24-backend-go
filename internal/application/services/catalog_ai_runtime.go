package services

import (
	"sort"
	"time"

	"github.com/Ammar777782439/mujeeb24-backend-go/internal/application/ports"
)

// ProjectionFromBundles converts bulk repository rows into the provider-neutral
// Catalog AI Projection. Shared mapping helpers live in catalog_batch_controller.go.
func ProjectionFromBundles(bundles []ports.CatalogAIProjectionBundle) CatalogAIProjection {
	projection := CatalogAIProjection{}
	schemas := map[string]CatalogAIAttributeSchema{}
	for _, bundle := range bundles {
		item := mapCatalogItemRecordToProjection(bundle.Item)
		for _, variant := range bundle.Variants {
			item.Variants = append(item.Variants, CatalogAIVariant{
				ID: variant.ID, CatalogItemID: variant.CatalogItemID, Name: variant.Name,
				Attributes: parseJSONAttributes(variant.Attributes), Status: variant.Status,
			})
		}
		for _, offer := range bundle.Offers {
			item.Offers = append(item.Offers, CatalogAIOffer{
				ID: offer.ID, CatalogItemID: offer.CatalogItemID, VariantID: offer.VariantID, Name: offer.Name,
				PricingMode: offer.PricingMode, Amount: offer.Amount, Currency: offer.Currency,
				PricingUnit: offer.PricingUnit, PriceSource: offer.PriceSource,
				PriceVerificationStatus: stringPtrOrNil(offer.PriceVerificationStatus),
				PriceCheckedAt: formatTimePtr(offer.PriceCheckedAt),
				AvailabilityMode: stringPtrOrNil(offer.AvailabilityMode),
				AvailabilityStatus: stringPtrOrNil(offer.AvailabilityStatus),
				AvailabilitySource: offer.AvailabilitySource,
				AvailabilityCheckedAt: formatTimePtr(offer.AvailabilityCheckedAt),
				AvailabilityValidUntil: formatTimePtr(offer.AvailabilityValidUntil),
				AvailabilityEvidenceRef: offer.AvailabilityEvidenceRef,
				FulfillmentMode: stringPtrOrNil(offer.FulfillmentMode),
				ValidityFrom: formatTimePtr(offer.ValidityFrom), ValidityUntil: formatTimePtr(offer.ValidityUntil),
				Status: offer.Status,
			})
		}
		projection.Items = append(projection.Items, item)
		if bundle.AttributeSchema != nil {
			schemas[bundle.AttributeSchema.ID] = mapAttributeSchemaRecordToProjection(*bundle.AttributeSchema)
		}
	}
	ids := make([]string, 0, len(schemas))
	for id := range schemas { ids = append(ids, id) }
	sort.Strings(ids)
	for _, id := range ids { projection.AttributeSchemas = append(projection.AttributeSchemas, schemas[id]) }
	return projection
}

func EvidenceFromBundles(bundles []ports.CatalogAIProjectionBundle) ports.CatalogAIEvidenceSet {
	evidence := ports.NewCatalogAIEvidenceSet()
	for _, bundle := range bundles { evidence.AddBundle(bundle) }
	return evidence
}

func EvidenceFromProjection(projection CatalogAIProjection) ports.CatalogAIEvidenceSet {
	evidence := ports.NewCatalogAIEvidenceSet()
	for _, item := range projection.Items {
		entry := ports.CatalogAIEvidenceItem{
			Variants: make(map[string]struct{}),
			Offers: make(map[string]ports.CatalogAIEvidenceOffer),
		}
		for _, variant := range item.Variants { entry.Variants[variant.ID] = struct{}{} }
		for _, offer := range item.Offers {
			var variantID string
			if offer.VariantID != nil { variantID = *offer.VariantID }
			entry.Offers[offer.ID] = ports.CatalogAIEvidenceOffer{VariantID: variantID}
		}
		evidence.Items[item.ID] = entry
	}
	return evidence
}

func EvidenceFromCustomerSalesContext(ctx *ports.CustomerSalesContext) ports.CatalogAIEvidenceSet {
	evidence := ports.NewCatalogAIEvidenceSet()
	if ctx == nil { return evidence }
	for _, item := range ctx.CatalogEvidence {
		evidence.Items[item.Reference] = ports.CatalogAIEvidenceItem{
			Variants: map[string]struct{}{}, Offers: map[string]ports.CatalogAIEvidenceOffer{},
		}
	}
	for _, variant := range ctx.VariantEvidence {
		item, ok := evidence.Items[variant.CatalogItemReference]; if !ok { continue }
		item.Variants[variant.Reference] = struct{}{}
		evidence.Items[variant.CatalogItemReference] = item
	}
	for _, offer := range ctx.OfferEvidence {
		item, ok := evidence.Items[offer.CatalogItemReference]; if !ok { continue }
		item.Offers[offer.Reference] = ports.CatalogAIEvidenceOffer{VariantID: offer.VariantReference}
		evidence.Items[offer.CatalogItemReference] = item
	}
	return evidence
}

func appendCatalogAIBundlesToContext(ctx *ports.CustomerSalesContext, bundles []ports.CatalogAIProjectionBundle, now time.Time) {
	if ctx == nil { return }
	for _, bundle := range bundles {
		ctx.CatalogEvidence = append(ctx.CatalogEvidence, catalogItemEvidence(bundle.Item, now))
		for _, offer := range bundle.Offers { ctx.OfferEvidence = append(ctx.OfferEvidence, toOfferEvidence(offer, now)) }
		for _, variant := range bundle.Variants {
			ctx.VariantEvidence = append(ctx.VariantEvidence, ports.CustomerSalesVariantEvidence{
				Reference: variant.ID, CatalogItemReference: variant.CatalogItemID,
				Name: variant.Name, Status: variant.Status, Attributes: safeJSONObject(variant.Attributes),
				EvidenceState: CustomerSalesContextFresh, RetrievedAt: now, SchemaVersion: AIEvidenceSchemaVersion,
			})
		}
	}
}

func catalogItemEvidence(item ports.CatalogItemRecord, now time.Time) ports.CustomerSalesCatalogEvidence {
	return ports.CustomerSalesCatalogEvidence{
		Reference: item.ID, CatalogReference: item.CatalogID,
		AttributeSchemaReference: item.AttributeSchemaID,
		AttributeSchemaVersion: item.AttributeSchemaVersion,
		ItemType: item.ItemType,
		Name: item.Name, Status: item.Status, Attributes: safeJSONObject(item.Attributes),
		EvidenceState: CustomerSalesContextFresh, RetrievedAt: now, SchemaVersion: AIEvidenceSchemaVersion,
		ShortDescription: item.ShortDescription, LongDescription: item.LongDescription,
		PricingMode: item.PricingMode, AvailabilityMode: item.AvailabilityMode,
		FulfillmentMode: item.FulfillmentMode, RequiresConfirmation: item.RequiresConfirmation,
	}
}


func appendCatalogRecordsToProjection(projection *CatalogAIProjection, catalogs []ports.CatalogRecord) {
	if projection == nil || len(catalogs) == 0 {
		return
	}
	seen := make(map[string]struct{}, len(projection.Catalogs))
	for _, catalog := range projection.Catalogs {
		seen[catalog.ID] = struct{}{}
	}
	for _, catalog := range catalogs {
		if _, ok := seen[catalog.ID]; ok {
			continue
		}
		projection.Catalogs = append(projection.Catalogs, CatalogAICatalog{
			ID: catalog.ID, Name: catalog.Name, Description: catalog.Description, Status: catalog.Status,
		})
		seen[catalog.ID] = struct{}{}
	}
}
