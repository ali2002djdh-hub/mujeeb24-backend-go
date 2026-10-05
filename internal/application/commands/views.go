package commands

import "time"

type BusinessView struct {
	ID              BusinessID
	Name            string
	Slug            string
	Status          string
	VerticalType    string
	Timezone        string
	DefaultCurrency string
	Locale          string
	ResourceVersion ResourceVersion
	CreatedAt       time.Time
	UpdatedAt       time.Time
}
type BusinessPolicyView struct {
	BusinessID                BusinessID
	AIMode                    string
	DefaultHumanReview        bool
	AllowAutoReply            bool
	AllowAutoLeadCreation     bool
	AllowAutoTransactionDraft bool
	AllowAutoConfirmation     bool
	ResourceVersion           ResourceVersion
}
type ChannelConnectionView struct {
	ID                       ConnectionID
	BusinessID               BusinessID
	Provider                 string
	Channel                  string
	Status                   string
	ExternalAccountReference string
	ResourceVersion          ResourceVersion
}
type ChannelProvisioningView struct {
	ID               ID
	BusinessID       BusinessID
	Provider         string
	Channel          string
	Status           string
	AuthorizationURL string
}
type ConversationView struct {
	ID                  ConversationID
	BusinessID          BusinessID
	CustomerID          CustomerID
	CustomerDisplayName *string
	State               string
	Ownership           string
	AIMode              string
	Priority            string
	Labels              []string
	LastActivityAt      time.Time
	ResourceVersion     ResourceVersion
}
type MessageView struct {
	ID                       MessageID
	ConversationID           ConversationID
	Direction                string
	Origin                   string
	Status                   string
	Text                     string
	ProviderMessageReference *string
	OccurredAt               time.Time
	CreatedAt                time.Time
	ResourceVersion          ResourceVersion
	Private                  bool
}
type CannedReplyView struct {
	ID              CannedReplyID
	BusinessID      BusinessID
	Title           string
	Shortcut        string
	Body            string
	Status          string
	ResourceVersion ResourceVersion
	CreatedAt       time.Time
	UpdatedAt       time.Time
}
type CustomerView struct {
	ID              CustomerID
	BusinessID      BusinessID
	DisplayName     string
	Status          string
	ResourceVersion ResourceVersion
}
type AttributeDefinitionView struct {
	ID              ID
	Key             string
	Label           string
	DataType        string
	Required        bool
	ValidationRules map[string]any
	DisplayOrder    int
}
type AttributeSchemaView struct {
	ID          AttributeSchemaID
	BusinessID  BusinessID
	Name        string
	Version     int
	Definitions []AttributeDefinitionView
}
type CatalogView struct {
	ID              CatalogID
	BusinessID      BusinessID
	Name            string
	Description     *string
	Status          string
	CreatedAt       time.Time
	UpdatedAt       time.Time
	ResourceVersion ResourceVersion
}
type CatalogItemView struct {
	ID                     CatalogItemID
	BusinessID             BusinessID
	CatalogID              CatalogID
	AttributeSchemaID      *AttributeSchemaID
	AttributeSchemaVersion *int
	Name                   string
	ItemType               string
	ShortDescription       *string
	LongDescription        *string
	Status                 string
	PricingMode            string
	AvailabilityMode       string
	FulfillmentMode        string
	RequiresConfirmation   bool
	Attributes             []byte
	CreatedAt              time.Time
	UpdatedAt              time.Time
	ResourceVersion        ResourceVersion
}
type OfferView struct {
	ID                      OfferID
	BusinessID              BusinessID
	CatalogItemID           CatalogItemID
	VariantID               VariantID
	Name                    string
	PricingMode             string
	Amount                  *string
	Currency                *string
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
	CreatedAt               time.Time
	UpdatedAt               time.Time
	ResourceVersion         ResourceVersion
}
type VariantView struct {
	ID              VariantID
	BusinessID      BusinessID
	CatalogItemID   CatalogItemID
	Name            string
	Attributes      []byte
	Status          string
	CreatedAt       time.Time
	UpdatedAt       time.Time
	ResourceVersion ResourceVersion
}
type LeadView struct {
	ID              LeadID
	BusinessID      BusinessID
	CustomerID      CustomerID
	Status          string
	ResourceVersion ResourceVersion
}
type TransactionView struct {
	ID              TransactionID
	BusinessID      BusinessID
	CustomerID      CustomerID
	State           string
	TransactionType string
	ResourceVersion ResourceVersion
}
type AIDecisionView struct {
	ID                 AIDecisionID
	BusinessID         BusinessID
	ConversationID     *ConversationID
	IntentBase         string
	DomainContext      string
	Entities           []byte
	EvidenceReferences []byte
	RequestedAction    string
	RequiresHuman      bool
	MissingInformation []byte
	ReasonCodes        []byte
	PolicyVersion      string
	Lifecycle          string
	HumanReviewReason  string
	ResourceVersion    ResourceVersion
	CreatedAt          time.Time
}
type AuditEventView struct {
	ID                AuditEventID
	BusinessID        BusinessID
	ActorType         string
	ActorReference    string
	Action            string
	ResourceType      string
	ResourceID        string
	Metadata          []byte
	DecisionReference string
	Result            string
	ReasonCode        string
	BeforeReference   string
	AfterReference    string
	OccurredAt        time.Time
}
type ListResult[T any] struct {
	Items      []T
	NextCursor string
	HasMore    bool
}

// CatalogEntityContractView is the application-layer view of the Catalog
// Entity Contract descriptor (contract ⑤ §8). It mirrors the DTO shape
// (dto.CatalogEntityContract) minus the dto-specific concerns.
//
// The view is constructed from services.DefaultCatalogEntityContractDescriptor()
// — the canonical, in-memory source of truth that is also sent to Gemini
// as part of system_instruction per contract ⑤ §7.
type CatalogEntityContractView struct {
	PricingModes              map[string]string
	AvailabilityModes         map[string]string
	AvailabilityStatuses      map[string]string
	PriceVerificationStatuses map[string]string
	FulfillmentModes          map[string]string
	// ItemStatuses per migration 000016 catalog_items_status_chk: draft, active, inactive, archived.
	// Offer status is separate and additionally allows expired. Sourced from the
	// descriptor's ItemStatuses field (no inline hardcoding here).
	ItemStatuses map[string]string
}
