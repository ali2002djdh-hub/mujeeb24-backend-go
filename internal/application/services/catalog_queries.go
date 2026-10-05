package services

import (
	"context"
	"strconv"

	"github.com/Ammar777782439/mujeeb24-backend-go/internal/application/commands"
	appErrors "github.com/Ammar777782439/mujeeb24-backend-go/internal/application/errors"
	"github.com/Ammar777782439/mujeeb24-backend-go/internal/application/ports"
	"github.com/Ammar777782439/mujeeb24-backend-go/internal/application/queries"
)

type ListCatalogsQueryService struct{ Repository ports.CatalogRepository }

func (s ListCatalogsQueryService) Handle(ctx context.Context, query queries.ListCatalogsQuery) (commands.ListResult[commands.CatalogView], error) {
	if s.Repository == nil {
		return commands.ListResult[commands.CatalogView]{}, appErrors.NotImplemented()
	}
	page, err := s.Repository.ListCatalogs(ctx, string(query.Meta.Actor.BusinessID), query.Status, query.Limit, query.Cursor)
	if err != nil {
		return commands.ListResult[commands.CatalogView]{}, err
	}
	items := make([]commands.CatalogView, 0, len(page.Items))
	for _, record := range page.Items {
		items = append(items, catalogView(record))
	}
	return commands.ListResult[commands.CatalogView]{Items: items, NextCursor: page.NextCursor, HasMore: page.HasMore}, nil
}

type GetCatalogQueryService struct{ Repository ports.CatalogRepository }

func (s GetCatalogQueryService) Handle(ctx context.Context, query queries.GetCatalogQuery) (commands.CatalogView, error) {
	if s.Repository == nil {
		return commands.CatalogView{}, appErrors.NotImplemented()
	}
	record, err := s.Repository.GetCatalog(ctx, string(query.Meta.Actor.BusinessID), string(query.CatalogID))
	if err != nil {
		return commands.CatalogView{}, err
	}
	return catalogView(record), nil
}

type ListCatalogItemsQueryService struct{ Repository ports.CatalogRepository }

func (s ListCatalogItemsQueryService) Handle(ctx context.Context, query queries.ListCatalogItemsQuery) (commands.ListResult[commands.CatalogItemView], error) {
	if s.Repository == nil {
		return commands.ListResult[commands.CatalogItemView]{}, appErrors.NotImplemented()
	}
	page, err := s.Repository.ListCatalogItems(ctx, string(query.Meta.Actor.BusinessID), string(query.CatalogID), query.Search, query.Status, query.Limit, query.Cursor)
	if err != nil {
		return commands.ListResult[commands.CatalogItemView]{}, err
	}
	items := make([]commands.CatalogItemView, 0, len(page.Items))
	for _, record := range page.Items {
		items = append(items, catalogItemView(record))
	}
	return commands.ListResult[commands.CatalogItemView]{Items: items, NextCursor: page.NextCursor, HasMore: page.HasMore}, nil
}

type GetCatalogItemQueryService struct{ Repository ports.CatalogRepository }

func (s GetCatalogItemQueryService) Handle(ctx context.Context, query queries.GetCatalogItemQuery) (commands.CatalogItemView, error) {
	if s.Repository == nil {
		return commands.CatalogItemView{}, appErrors.NotImplemented()
	}
	record, err := s.Repository.GetCatalogItem(ctx, string(query.Meta.Actor.BusinessID), string(query.CatalogID), string(query.ItemID))
	if err != nil {
		return commands.CatalogItemView{}, err
	}
	return catalogItemView(record), nil
}

type ListOffersQueryService struct{ Repository ports.CatalogRepository }

func (s ListOffersQueryService) Handle(ctx context.Context, query queries.ListOffersQuery) (commands.ListResult[commands.OfferView], error) {
	if s.Repository == nil {
		return commands.ListResult[commands.OfferView]{}, appErrors.NotImplemented()
	}
	page, err := s.Repository.ListOffers(ctx, string(query.Meta.Actor.BusinessID), string(query.ItemID), query.Status, query.Limit, query.Cursor)
	if err != nil {
		return commands.ListResult[commands.OfferView]{}, err
	}
	items := make([]commands.OfferView, 0, len(page.Items))
	for _, record := range page.Items {
		items = append(items, offerView(record))
	}
	return commands.ListResult[commands.OfferView]{Items: items, NextCursor: page.NextCursor, HasMore: page.HasMore}, nil
}

type ListVariantsQueryService struct{ Repository ports.CatalogRepository }

func (s ListVariantsQueryService) Handle(ctx context.Context, query queries.ListVariantsQuery) (commands.ListResult[commands.VariantView], error) {
	if s.Repository == nil {
		return commands.ListResult[commands.VariantView]{}, appErrors.NotImplemented()
	}
	page, err := s.Repository.ListVariants(ctx, string(query.Meta.Actor.BusinessID), string(query.ItemID), query.Status, query.Limit, query.Cursor)
	if err != nil {
		return commands.ListResult[commands.VariantView]{}, err
	}
	items := make([]commands.VariantView, 0, len(page.Items))
	for _, record := range page.Items {
		items = append(items, variantView(record))
	}
	return commands.ListResult[commands.VariantView]{Items: items, NextCursor: page.NextCursor, HasMore: page.HasMore}, nil
}

type ListAttributeSchemasQueryService struct{ Repository ports.CatalogRepository }

func (s ListAttributeSchemasQueryService) Handle(ctx context.Context, query queries.ListAttributeSchemasQuery) (commands.ListResult[commands.AttributeSchemaView], error) {
	if s.Repository == nil {
		return commands.ListResult[commands.AttributeSchemaView]{}, appErrors.NotImplemented()
	}
	page, err := s.Repository.ListAttributeSchemas(ctx, string(query.Meta.Actor.BusinessID), query.Name, query.Version, query.Limit, query.Cursor)
	if err != nil {
		return commands.ListResult[commands.AttributeSchemaView]{}, err
	}
	items := make([]commands.AttributeSchemaView, 0, len(page.Items))
	for _, record := range page.Items {
		items = append(items, attributeSchemaView(record))
	}
	return commands.ListResult[commands.AttributeSchemaView]{Items: items, NextCursor: page.NextCursor, HasMore: page.HasMore}, nil
}

type GetAttributeSchemaQueryService struct{ Repository ports.CatalogRepository }

func (s GetAttributeSchemaQueryService) Handle(ctx context.Context, query queries.GetAttributeSchemaQuery) (commands.AttributeSchemaView, error) {
	if s.Repository == nil {
		return commands.AttributeSchemaView{}, appErrors.NotImplemented()
	}
	record, err := s.Repository.GetAttributeSchema(ctx, string(query.Meta.Actor.BusinessID), string(query.SchemaID))
	if err != nil {
		return commands.AttributeSchemaView{}, err
	}
	return attributeSchemaView(record), nil
}

func catalogView(record ports.CatalogRecord) commands.CatalogView {
	return commands.CatalogView{ID: commands.CatalogID(record.ID), BusinessID: commands.BusinessID(record.BusinessID), Name: record.Name, Description: record.Description, Status: record.Status, ResourceVersion: commands.ResourceVersion(strconv.FormatInt(record.ResourceVersion, 10)), CreatedAt: record.CreatedAt, UpdatedAt: record.UpdatedAt}
}

func catalogItemView(record ports.CatalogItemRecord) commands.CatalogItemView {
	var schemaID *commands.AttributeSchemaID
	if record.AttributeSchemaID != nil {
		value := commands.AttributeSchemaID(*record.AttributeSchemaID)
		schemaID = &value
	}
	return commands.CatalogItemView{ID: commands.CatalogItemID(record.ID), BusinessID: commands.BusinessID(record.BusinessID), CatalogID: commands.CatalogID(record.CatalogID), AttributeSchemaID: schemaID, AttributeSchemaVersion: record.AttributeSchemaVersion, ItemType: record.ItemType, Name: record.Name, ShortDescription: record.ShortDescription, LongDescription: record.LongDescription, Status: record.Status, PricingMode: record.PricingMode, AvailabilityMode: record.AvailabilityMode, FulfillmentMode: record.FulfillmentMode, RequiresConfirmation: record.RequiresConfirmation, Attributes: append([]byte(nil), record.Attributes...), ResourceVersion: commands.ResourceVersion(strconv.FormatInt(record.ResourceVersion, 10)), CreatedAt: record.CreatedAt, UpdatedAt: record.UpdatedAt}
}

func offerView(record ports.OfferRecord) commands.OfferView {
	variantID := commands.VariantID("")
	if record.VariantID != nil {
		variantID = commands.VariantID(*record.VariantID)
	}
	return commands.OfferView{ID: commands.OfferID(record.ID), BusinessID: commands.BusinessID(record.BusinessID), CatalogItemID: commands.CatalogItemID(record.CatalogItemID), VariantID: variantID, Name: record.Name, PricingMode: record.PricingMode, Amount: record.Amount, Currency: record.Currency, PricingUnit: record.PricingUnit, PriceSource: record.PriceSource, PriceVerificationStatus: record.PriceVerificationStatus, PriceCheckedAt: record.PriceCheckedAt, AvailabilityMode: record.AvailabilityMode, AvailabilitySource: record.AvailabilitySource, AvailabilityCheckedAt: record.AvailabilityCheckedAt, AvailabilityValidUntil: record.AvailabilityValidUntil, AvailabilityEvidenceRef: record.AvailabilityEvidenceRef, FulfillmentMode: record.FulfillmentMode, ValidityFrom: record.ValidityFrom, ValidityUntil: record.ValidityUntil, AvailabilityStatus: record.AvailabilityStatus, Status: record.Status, ResourceVersion: commands.ResourceVersion(strconv.FormatInt(record.ResourceVersion, 10)), CreatedAt: record.CreatedAt, UpdatedAt: record.UpdatedAt}
}

func variantView(record ports.VariantRecord) commands.VariantView {
	return commands.VariantView{ID: commands.VariantID(record.ID), BusinessID: commands.BusinessID(record.BusinessID), CatalogItemID: commands.CatalogItemID(record.CatalogItemID), Name: record.Name, Attributes: append([]byte(nil), record.Attributes...), Status: record.Status, ResourceVersion: commands.ResourceVersion(strconv.FormatInt(record.ResourceVersion, 10)), CreatedAt: record.CreatedAt, UpdatedAt: record.UpdatedAt}
}

func attributeSchemaView(record ports.AttributeSchemaRecord) commands.AttributeSchemaView {
	definitions := make([]commands.AttributeDefinitionView, 0, len(record.Definitions))
	for _, definition := range record.Definitions {
		definitions = append(definitions, commands.AttributeDefinitionView{
			ID: commands.ID(definition.ID), Key: definition.Key, Label: definition.Label,
			DataType: definition.DataType, Required: definition.Required,
			ValidationRules: parseJSONAttributes(definition.ValidationRules),
			DisplayOrder: definition.DisplayOrder,
		})
	}
	return commands.AttributeSchemaView{ID: commands.AttributeSchemaID(record.ID), BusinessID: commands.BusinessID(record.BusinessID), Name: record.Name, Version: record.Version, Definitions: definitions}
}

var _ queries.ListCatalogsHandler = ListCatalogsQueryService{}
var _ queries.GetCatalogHandler = GetCatalogQueryService{}
var _ queries.ListCatalogItemsHandler = ListCatalogItemsQueryService{}
var _ queries.GetCatalogItemHandler = GetCatalogItemQueryService{}
var _ queries.ListOffersHandler = ListOffersQueryService{}
var _ queries.ListVariantsHandler = ListVariantsQueryService{}
var _ queries.ListAttributeSchemasHandler = ListAttributeSchemasQueryService{}
var _ queries.GetAttributeSchemaHandler = GetAttributeSchemaQueryService{}

// GetCatalogEntityContractQueryService returns the canonical Catalog Entity
// Contract descriptor. Per contract ⑤ §17, the descriptor is global
// knowledge (same for every business); the BusinessID in the query is used
// only for auth scope validation, NOT for selecting which contract to return.
//
// The service does NOT touch the database — it returns
// services.DefaultCatalogEntityContractDescriptor() directly. This is by
// design: the contract is canonical in-memory knowledge sourced from the
// SQL migration CHECK constraints; it is NOT merchant-specific data.
//
// Per ADR-045, this endpoint is the authoritative source the frontend uses
// to translate backend enum values to Arabic labels — eliminating the need
// to hardcode the translations in the client.
type GetCatalogEntityContractQueryService struct{}

// Handle returns the Catalog Entity Contract descriptor. The descriptor
// now carries all six enum dimensions (PricingModes, AvailabilityModes,
// FulfillmentModes, AvailabilityStatuses, PriceVerificationStatuses,
// ItemStatuses) per contract ⑤ §8 + migration 000016. Offer status is a separate
// CatalogEntityContract field at the entity level.
func (s GetCatalogEntityContractQueryService) Handle(ctx context.Context, query queries.GetCatalogEntityContractQuery) (commands.CatalogEntityContractView, error) {
	d := DefaultCatalogEntityContractDescriptor()
	return commands.CatalogEntityContractView{
		PricingModes:              d.PricingModes,
		AvailabilityModes:         d.AvailabilityModes,
		AvailabilityStatuses:      d.AvailabilityStatuses,
		PriceVerificationStatuses: d.PriceVerificationStatuses,
		FulfillmentModes:          d.FulfillmentModes,
		ItemStatuses:              d.ItemStatuses,
	}, nil
}

var _ queries.GetCatalogEntityContractHandler = GetCatalogEntityContractQueryService{}
