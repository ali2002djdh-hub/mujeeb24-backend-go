package ports

import (
	"context"
	"time"
)

type CatalogRecord struct {
	ID              string
	BusinessID      string
	Name            string
	Description     *string
	Status          string
	ResourceVersion int64
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

type CatalogItemRecord struct {
	ID                     string
	BusinessID             string
	CatalogID              string
	AttributeSchemaID      *string
	AttributeSchemaVersion *int
	ItemType               string
	Name                   string
	// Per migration 000016: short_description, long_description,
	// pricing_mode, availability_mode, fulfillment_mode, requires_confirmation
	// are NOT NULL columns. Added here so the Catalog AI Projection (contract ① §1)
	// can carry the full commercial truth.
	ShortDescription     *string
	LongDescription      *string
	Status               string
	PricingMode          string
	AvailabilityMode     string
	FulfillmentMode      string
	RequiresConfirmation bool
	Attributes           []byte
	ResourceVersion      int64
	CreatedAt            time.Time
	UpdatedAt            time.Time
}

type OfferRecord struct {
	ID            string
	BusinessID    string
	CatalogItemID string
	VariantID     *string
	Name          string
	PricingMode   string
	Amount        *string
	Currency      *string
	// Per migration 000018: added PricingUnit, PriceSource,
	// PriceVerificationStatus, AvailabilityMode, FulfillmentMode,
	// ValidityFrom, ValidityUntil so the projection carries the full
	// commercial truth per contract ① §1.
	PricingUnit             *string
	PriceSource             *string
	PriceVerificationStatus string
	PriceCheckedAt          *time.Time
	AvailabilityMode        string
	AvailabilitySource      *string
	AvailabilityCheckedAt   *time.Time
	AvailabilityValidUntil  *time.Time
	AvailabilityEvidenceRef *string
	FulfillmentMode         string
	ValidityFrom            *time.Time
	ValidityUntil           *time.Time
	AvailabilityStatus      string
	Status                  string
	ResourceVersion         int64
	CreatedAt               time.Time
	UpdatedAt               time.Time
}

type VariantRecord struct {
	ID              string
	BusinessID      string
	CatalogItemID   string
	Name            string
	Attributes      []byte
	Status          string
	ResourceVersion int64
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

type AttributeDefinitionRecord struct {
	ID              string
	Key             string
	Label           string
	DataType        string
	Required        bool
	ValidationRules []byte
	DisplayOrder    int
}

type AttributeSchemaRecord struct {
	ID          string
	BusinessID  string
	Name        string
	Version     int
	Definitions []AttributeDefinitionRecord
}

type CatalogPage struct {
	Items      []CatalogRecord
	NextCursor string
	HasMore    bool
}

type CatalogItemPage struct {
	Items      []CatalogItemRecord
	NextCursor string
	HasMore    bool
}

type OfferPage struct {
	Items      []OfferRecord
	NextCursor string
	HasMore    bool
}

type VariantPage struct {
	Items      []VariantRecord
	NextCursor string
	HasMore    bool
}

type AttributeSchemaPage struct {
	Items      []AttributeSchemaRecord
	NextCursor string
	HasMore    bool
}

type CatalogDraft struct {
	ID          string
	BusinessID  string
	Name        string
	Description *string
	Status      string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

type CatalogPatch struct {
	ID              string
	BusinessID      string
	Name            *string
	Description     *string
	Status          *string
	ExpectedVersion int64
	UpdatedAt       time.Time
}

type AttributeSchemaDraft struct {
	ID          string
	BusinessID  string
	Name        string
	Version     int
	Definitions []AttributeDefinitionDraft
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

type AttributeDefinitionDraft struct {
	ID              string
	Key             string
	Label           string
	DataType        string
	Required        bool
	ValidationRules []byte
	DisplayOrder    int
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

type CatalogItemDraft struct {
	ID                     string
	BusinessID             string
	CatalogID              string
	AttributeSchemaID      *string
	AttributeSchemaVersion *int
	ItemType               string
	Name                   string
	ShortDescription       *string
	LongDescription        *string
	PricingMode            string
	AvailabilityMode       string
	FulfillmentMode        string
	RequiresConfirmation   bool
	Attributes             []byte
	CreatedAt              time.Time
	UpdatedAt              time.Time
}

type CatalogItemPatch struct {
	ID                   string
	BusinessID           string
	Name                 *string
	Status               *string
	ItemType             *string
	ShortDescription     *string
	LongDescription      *string
	PricingMode          *string
	AvailabilityMode     *string
	FulfillmentMode      *string
	RequiresConfirmation *bool
	Attributes           []byte
	ExpectedVersion      int64
	UpdatedAt            time.Time
}

type OfferDraft struct {
	ID                 string
	BusinessID         string
	CatalogItemID      string
	VariantID          *string
	Name               string
	PricingMode        string
	AmountMinor        *int64
	Currency           *string
	PricingUnit        *string
	AvailabilityMode   string
	AvailabilityStatus string
	FulfillmentMode    string
	Status             string
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

type OfferPatch struct {
	ID                 string
	BusinessID         string
	Name               *string
	AmountMinor        *int64
	AvailabilityStatus *string
	Status             *string
	ExpectedVersion    int64
	UpdatedAt          time.Time
}

type VariantDraft struct {
	ID            string
	BusinessID    string
	CatalogItemID string
	Name          string
	Attributes    []byte
	Status        string
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

type VariantPatch struct {
	ID              string
	BusinessID      string
	Name            *string
	Attributes      []byte
	Status          *string
	ExpectedVersion int64
	UpdatedAt       time.Time
}

type CatalogRepository interface {
	ListCatalogs(ctx context.Context, businessID, status string, limit int, cursor string) (CatalogPage, error)
	GetCatalog(ctx context.Context, businessID, catalogID string) (CatalogRecord, error)
	CreateCatalog(ctx context.Context, draft CatalogDraft) (CatalogRecord, error)
	UpdateCatalog(ctx context.Context, patch CatalogPatch) (CatalogRecord, error)
	ListCatalogItems(ctx context.Context, businessID, catalogID, search, status string, limit int, cursor string) (CatalogItemPage, error)
	GetCatalogItem(ctx context.Context, businessID, catalogID, itemID string) (CatalogItemRecord, error)
	CreateCatalogItem(ctx context.Context, draft CatalogItemDraft) (CatalogItemRecord, error)
	UpdateCatalogItem(ctx context.Context, patch CatalogItemPatch) (CatalogItemRecord, error)
	ListOffers(ctx context.Context, businessID, itemID, status string, limit int, cursor string) (OfferPage, error)
	CreateOffer(ctx context.Context, draft OfferDraft) (OfferRecord, error)
	UpdateOffer(ctx context.Context, patch OfferPatch) (OfferRecord, error)
	ListVariants(ctx context.Context, businessID, itemID, status string, limit int, cursor string) (VariantPage, error)
	CreateVariant(ctx context.Context, draft VariantDraft) (VariantRecord, error)
	UpdateVariant(ctx context.Context, patch VariantPatch) (VariantRecord, error)
	ListAttributeSchemas(ctx context.Context, businessID, name string, version *int, limit int, cursor string) (AttributeSchemaPage, error)
	GetAttributeSchema(ctx context.Context, businessID, schemaID string) (AttributeSchemaRecord, error)
	NextAttributeSchemaVersion(ctx context.Context, businessID, name string) (int, error)
	CreateAttributeSchemaVersion(ctx context.Context, draft AttributeSchemaDraft) (AttributeSchemaRecord, error)
}
