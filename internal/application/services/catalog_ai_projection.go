// Package services — Catalog AI Projection v1 (contract ①)
//
// Implements contract ① Catalog AI Projection v1 — CLOSED.
//
// Per contract ① §1, the Projection is a Read Model for AI ONLY, built 100%
// on the Catalog Contract (⑤). It is NOT a Domain Entity, NOT a Catalog
// Contract replacement. It is only a provider-facing read model.
//
// Per contract ① §2, only the AttributeSchemas USED by the Items in the
// Projection are sent. We do NOT send unused schemas.
//
// Per contract ① §3, the Schema is sent ONCE in attribute_schemas[] and
// items[] reference it by (attribute_schema_id, attribute_schema_version).
//
// Per contract ① §5, the Projection does NOT contain: business_id, SQL,
// database metadata, created_at, or updated_at. The projection contains only
// catalog facts and relationships needed by the model.
//
// Per contract ① §6:
//   Mujeeb reads the real Catalog → builds Projection → enforces Tenant
//   Isolation → sends to Gemini.
//   Gemini understands the data → understands the customer → infers →
//   compares → decides what's relevant → formulates the response.

package services

// CatalogAIProjection is the contract ① §1 Read Model sent to Gemini.
//
// The Projection contains three top-level arrays:
//
//	catalogs[]           — catalog identity/meaning for catalog_id references
//	attribute_schemas[]  — schemas actually used by items[]
//	items[]              — actual catalog items with nested variants + offers
//
// The Projection is NOT a Domain Entity. It is a transport shape.
type CatalogAIProjection struct {
	Catalogs         []CatalogAICatalog         `json:"catalogs,omitempty"`
	AttributeSchemas []CatalogAIAttributeSchema `json:"attribute_schemas,omitempty"`
	Items            []CatalogAIItem            `json:"items,omitempty"`
}

type CatalogAICatalog struct {
	ID          string  `json:"id"`
	Name        string  `json:"name"`
	Description *string `json:"description,omitempty"`
	Status      string  `json:"status"`
}

// CatalogAIAttributeSchema is contract ① §1 — one schema with its definitions.
type CatalogAIAttributeSchema struct {
	ID          string                         `json:"id"`
	Name        string                         `json:"name"`
	Version     int                            `json:"version"`
	Definitions []CatalogAIAttributeDefinition `json:"definitions"`
}

// CatalogAIAttributeDefinition is contract ① §1 — one attribute definition.
type CatalogAIAttributeDefinition struct {
	ID              string         `json:"id"`
	SchemaID        string         `json:"schema_id"`
	AttributeKey    string         `json:"attribute_key"`
	Label           string         `json:"label"`
	DataType        string         `json:"data_type"`
	IsRequired      bool           `json:"is_required"`
	ValidationRules map[string]any `json:"validation_rules,omitempty"`
	DisplayOrder    int            `json:"display_order"`
}

// CatalogAIItem is contract ① §1 — one catalog item with its variants and offers.
type CatalogAIItem struct {
	ID                     string             `json:"id"`
	CatalogID              string             `json:"catalog_id"`
	AttributeSchemaID      *string            `json:"attribute_schema_id,omitempty"`
	AttributeSchemaVersion *int               `json:"attribute_schema_version,omitempty"`
	ItemType               string             `json:"item_type"`
	Name                   string             `json:"name"`
	ShortDescription       *string            `json:"short_description,omitempty"`
	LongDescription        *string            `json:"long_description,omitempty"`
	Status                 string             `json:"status"`
	PricingMode            string             `json:"pricing_mode"`
	AvailabilityMode       string             `json:"availability_mode"`
	FulfillmentMode        string             `json:"fulfillment_mode"`
	RequiresConfirmation   bool               `json:"requires_confirmation"`
	Attributes             map[string]any     `json:"attributes,omitempty"`
	Variants               []CatalogAIVariant `json:"variants,omitempty"`
	Offers                 []CatalogAIOffer   `json:"offers,omitempty"`
}

// CatalogAIVariant is contract ① §1 — one variant under a CatalogAIItem.
type CatalogAIVariant struct {
	ID            string         `json:"id"`
	CatalogItemID string         `json:"catalog_item_id"`
	Name          string         `json:"name"`
	Attributes map[string]any `json:"attributes,omitempty"`
	Status     string         `json:"status"`
}

// CatalogAIOffer is contract ① §1 — one offer under a CatalogAIItem.
//
// Per contract ① §1, the offer has all the pricing/availability/fulfillment
// fields so Gemini can understand the commercial truth without guessing.
type CatalogAIOffer struct {
	ID                      string  `json:"id"`
	CatalogItemID           string  `json:"catalog_item_id"`
	VariantID               *string `json:"variant_id,omitempty"`
	Name                    string  `json:"name"`
	PricingMode             string  `json:"pricing_mode"`
	Amount                  *string `json:"amount,omitempty"`
	Currency                *string `json:"currency,omitempty"`
	PricingUnit             *string `json:"pricing_unit,omitempty"`
	PriceSource             *string `json:"price_source,omitempty"`
	PriceVerificationStatus *string `json:"price_verification_status,omitempty"`
	PriceCheckedAt          *string `json:"price_checked_at,omitempty"`
	AvailabilityMode        *string `json:"availability_mode,omitempty"`
	AvailabilityStatus      *string `json:"availability_status,omitempty"`
	AvailabilitySource      *string `json:"availability_source,omitempty"`
	AvailabilityCheckedAt   *string `json:"availability_checked_at,omitempty"`
	AvailabilityValidUntil  *string `json:"availability_valid_until,omitempty"`
	AvailabilityEvidenceRef *string `json:"availability_evidence_ref,omitempty"`
	FulfillmentMode         *string `json:"fulfillment_mode,omitempty"`
	ValidityFrom            *string `json:"validity_from,omitempty"`
	ValidityUntil           *string `json:"validity_until,omitempty"`
	Status                  string  `json:"status"`
}

// CatalogAIProjectionBuilder builds a CatalogAIProjection from the merchant's
// actual catalog data in PostgreSQL. Per contract ① §6, Mujeeb is the sole
// builder of the Projection.
//
// Per contract ⑤ §16, the Projection is NOT raw PostgreSQL rows — it is a
// structured representation chosen for Gemini's consumption.
//
// Per contract ① §2, only the schemas used by the included items are emitted.
// Schemas referenced by items not in the scope are omitted.
type CatalogAIProjectionBuilder struct {
	Catalogs    CatalogProjectionDataSource
	Items       CatalogProjectionDataSource
	Variants    CatalogProjectionDataSource
	Offers      CatalogProjectionDataSource
	Schemas     CatalogProjectionDataSource
	Definitions CatalogProjectionDataSource
}

// CatalogProjectionDataSource is the interface for reading catalog data per
// contract ⑤ §13 (Catalog Data Access Boundary: Read Only, Tenant Scoped,
// Structured, No SQL).
//
// Implementations must enforce:
//   - Read only — no writes via this interface
//   - Tenant scoped — all reads filter by business_id
//   - Structured — returns Projection shapes, not raw rows
//   - No SQL — the AI never sees query strings
type CatalogProjectionDataSource interface {
	// Read returns a list of catalog entities for the given business scope.
	// Per contract ⑤ §15, business_id is determined by the Authenticated
	// Context, never by Gemini.
	Read(ctx ReadContext) (any, error)
}

// ReadContext is the tenant-scoped read request.
type ReadContext struct {
	BusinessID string
	CatalogID  string
	ItemIDs    []string
	SchemaIDs  []string
	Limit      int
	Cursor     string
}
