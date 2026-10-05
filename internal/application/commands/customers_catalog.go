package commands

type CreateCustomerCommand struct {
	Meta             CommandMeta
	Profile          map[string]any
	ContactPoints    []ContactPoint
	LocalePreference string
}
type UpdateCustomerCommand struct {
	Meta             CommandMeta
	CustomerID       CustomerID
	Profile          map[string]any
	ContactPoints    []ContactPoint
	LocalePreference string
}
type MergeCustomerCommand struct {
	Meta             CommandMeta
	CustomerID       CustomerID
	TargetCustomerID CustomerID
	Reason           string
}
type ContactPoint struct {
	Kind  string
	Value string
}
type CustomerResult struct {
	MutationResult
	Customer CustomerView
}
type CreateCustomerHandler = CommandHandler[CreateCustomerCommand, CustomerResult]
type UpdateCustomerHandler = CommandHandler[UpdateCustomerCommand, CustomerResult]
type MergeCustomerHandler = CommandHandler[MergeCustomerCommand, CustomerResult]

type CreateCatalogCommand struct {
	Meta        CommandMeta
	Name        string
	Description string
}
type UpdateCatalogCommand struct {
	Meta        CommandMeta
	CatalogID   CatalogID
	Name        *string
	Description *string
	Status      *string
}
type CreateAttributeSchemaVersionCommand struct {
	Meta        CommandMeta
	Name        string
	Definitions []AttributeDefinition
}
type AttributeDefinition struct {
	Key             string
	Label           string
	DataType        string
	Required        bool
	ValidationRules map[string]any
	DisplayOrder    int
}
type CreateCatalogItemCommand struct {
	Meta                 CommandMeta
	CatalogID            CatalogID
	AttributeSchemaID    *AttributeSchemaID
	ItemType             string
	Name                 string
	ShortDescription     *string
	LongDescription      *string
	PricingMode          string
	AvailabilityMode     string
	FulfillmentMode      string
	RequiresConfirmation bool
	Attributes           map[string]any
}
type UpdateCatalogItemCommand struct {
	Meta                 CommandMeta
	CatalogItemID        CatalogItemID
	Name                 *string
	Status               *string
	ItemType             *string
	ShortDescription     *string
	LongDescription      *string
	PricingMode          *string
	AvailabilityMode     *string
	FulfillmentMode      *string
	RequiresConfirmation *bool
	Attributes           map[string]any
}
type CreateOfferCommand struct {
	Meta               CommandMeta
	CatalogItemID      CatalogItemID
	VariantID          *VariantID
	Name               string
	PricingMode        string
	AmountMinor        *int64
	Currency           *string
	PricingUnit        *string
	AvailabilityMode   string
	AvailabilityStatus string
	FulfillmentMode    string
	Status             string
}
type UpdateOfferCommand struct {
	Meta               CommandMeta
	OfferID            OfferID
	Name               *string
	AmountMinor        *int64
	AvailabilityStatus *string
	Status             *string
}
type CreateVariantCommand struct {
	Meta          CommandMeta
	CatalogItemID CatalogItemID
	Name          string
	Attributes    map[string]any
}
type UpdateVariantCommand struct {
	Meta       CommandMeta
	VariantID  VariantID
	Name       *string
	Attributes map[string]any
	Status     *string
}
type CatalogResult struct {
	MutationResult
	Catalog CatalogView
}
type CatalogItemResult struct {
	MutationResult
	Item CatalogItemView
}
type OfferResult struct {
	MutationResult
	Offer OfferView
}
type VariantResult struct {
	MutationResult
	Variant VariantView
}
type AttributeSchemaResult struct {
	MutationResult
	Schema AttributeSchemaView
}
type CreateCatalogHandler = CommandHandler[CreateCatalogCommand, CatalogResult]
type UpdateCatalogHandler = CommandHandler[UpdateCatalogCommand, CatalogResult]
type CreateAttributeSchemaVersionHandler = CommandHandler[CreateAttributeSchemaVersionCommand, AttributeSchemaResult]
type CreateCatalogItemHandler = CommandHandler[CreateCatalogItemCommand, CatalogItemResult]
type UpdateCatalogItemHandler = CommandHandler[UpdateCatalogItemCommand, CatalogItemResult]
type CreateOfferHandler = CommandHandler[CreateOfferCommand, OfferResult]
type UpdateOfferHandler = CommandHandler[UpdateOfferCommand, OfferResult]
type CreateVariantHandler = CommandHandler[CreateVariantCommand, VariantResult]
type UpdateVariantHandler = CommandHandler[UpdateVariantCommand, VariantResult]

type AuthorCatalogItemCommand struct {
	Meta                 CommandMeta
	CatalogID            CatalogID
	AttributeSchemaID    *AttributeSchemaID
	ItemType             string
	Name                 string
	PricingMode          string
	AvailabilityMode     string
	FulfillmentMode      string
	RequiresConfirmation bool
	Attributes           map[string]any
	Variants             []AuthorVariantInput
	Offers               []AuthorOfferInput
}

type AuthorVariantInput struct {
	Name       string
	Attributes map[string]any
}

type AuthorOfferInput struct {
	Name               string
	PricingMode        string
	AmountMinor        *int64
	Currency           *string
	PricingUnit        *string
	VariantName        *string
	VariantIndex       *int
	AvailabilityMode   string
	AvailabilityStatus string
	FulfillmentMode    string
	Status             string
}

type AuthorCatalogItemResult struct {
	MutationResult
	Item     CatalogItemView
	Variants []VariantView
	Offers   []OfferView
}

type AuthorCatalogItemHandler = CommandHandler[AuthorCatalogItemCommand, AuthorCatalogItemResult]
