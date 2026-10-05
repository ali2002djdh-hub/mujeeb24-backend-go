package dto

import (
	"strconv"
	"time"

	"github.com/danielgtaylor/huma/v2"
)

type UUID string
type Timestamp time.Time

type OptionalBool struct {
	Value   bool
	Present bool
}

func (b *OptionalBool) UnmarshalText(value []byte) error {
	parsed, err := strconv.ParseBool(string(value))
	if err != nil {
		return err
	}
	b.Value = parsed
	b.Present = true
	return nil
}

func (OptionalBool) Schema(huma.Registry) *huma.Schema {
	return &huma.Schema{Type: huma.TypeBoolean}
}

type Page struct {
	NextCursor *string `json:"next_cursor,omitempty"`
	HasMore    bool    `json:"has_more"`
}

type RequestMeta struct {
	RequestID string `json:"request_id"`
}
type Single[T any] struct {
	Body struct {
		Data      T      `json:"data"`
		RequestID string `json:"request_id"`
	}
}
type List[T any] struct {
	Body struct {
		Data       []T    `json:"data"`
		Pagination Page   `json:"pagination"`
		RequestID  string `json:"request_id"`
	}
}

type ErrorBody struct {
	Code      string            `json:"code"`
	Message   string            `json:"message"`
	Fields    map[string]string `json:"fields,omitempty"`
	Details   map[string]any    `json:"details,omitempty"`
	Retryable bool              `json:"retryable"`
}
type ErrorEnvelope struct {
	Error     ErrorBody `json:"error"`
	RequestID string    `json:"request_id"`
}
type ErrorResponse struct {
	Body ErrorEnvelope
}

type Principal struct {
	PrincipalID UUID    `json:"principal_id"`
	DisplayName string  `json:"display_name"`
	Email       *string `json:"email,omitempty"`
}
type Business struct {
	ID              UUID      `json:"id"`
	Name            string    `json:"name"`
	Slug            string    `json:"slug"`
	Status          string    `json:"status"`
	VerticalType    string    `json:"vertical_type"`
	Timezone        string    `json:"timezone"`
	DefaultCurrency string    `json:"default_currency"`
	Locale          string    `json:"locale"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
	ResourceVersion string    `json:"resource_version"`
}
type BusinessMembership struct {
	Business    Business `json:"business"`
	Role        string   `json:"role"`
	Permissions []string `json:"permissions,omitempty"`
}
type TeamMember struct {
	PrincipalID UUID   `json:"principal_id"`
	Email       string `json:"email" format:"email"`
	DisplayName string `json:"display_name"`
	Role        string `json:"role"`
	Status      string `json:"status"`
}
type TeamInvitation struct {
	ID        UUID      `json:"id"`
	Email     string    `json:"email" format:"email"`
	Role      string    `json:"role"`
	Status    string    `json:"status"`
	ExpiresAt time.Time `json:"expires_at"`
}
type TeamInvitationCreated struct {
	Invitation      TeamInvitation `json:"invitation"`
	AcceptanceToken string         `json:"acceptance_token"`
}
type BusinessPolicy struct {
	BusinessID                UUID   `json:"business_id"`
	AIMode                    string `json:"ai_mode"`
	DefaultHumanReview        bool   `json:"default_human_review"`
	AllowAutoReply            bool   `json:"allow_auto_reply"`
	AllowAutoLeadCreation     bool   `json:"allow_auto_lead_creation"`
	AllowAutoTransactionDraft bool   `json:"allow_auto_transaction_draft"`
	AllowAutoConfirmation     bool   `json:"allow_auto_confirmation"`
	ResourceVersion           string `json:"resource_version"`
}
type DashboardOverview struct {
	OpenConversations         int            `json:"open_conversations"`
	WaitingHuman              int            `json:"waiting_human"`
	NewCustomers              int            `json:"new_customers"`
	NewLeads                  int            `json:"new_leads"`
	TransactionsNeedingReview int            `json:"transactions_needing_review"`
	Connections               map[string]int `json:"connections,omitempty"`
}

type ChannelConnection struct {
	ID                       UUID         `json:"id"`
	BusinessID               UUID         `json:"business_id"`
	Provider                 string       `json:"provider"`
	Channel                  string       `json:"channel"`
	Status                   string       `json:"status"`
	ExternalAccountReference *string      `json:"external_account_reference,omitempty"`
	Capabilities             []Capability `json:"capabilities"`
	CreatedAt                time.Time    `json:"created_at"`
	UpdatedAt                time.Time    `json:"updated_at"`
	ResourceVersion          string       `json:"resource_version"`
}
type Capability struct {
	Name           string    `json:"name"`
	Enabled        bool      `json:"enabled"`
	CheckedAt      time.Time `json:"checked_at"`
	EvidenceSource *string   `json:"evidence_source,omitempty"`
}

type CustomerSummary struct {
	ID          UUID    `json:"id"`
	DisplayName *string `json:"display_name,omitempty"`
}
type Conversation struct {
	ID              UUID              `json:"id"`
	BusinessID      UUID              `json:"business_id"`
	Customer        CustomerSummary   `json:"customer"`
	State           string            `json:"state"`
	Ownership       string            `json:"ownership"`
	AIMode          string            `json:"ai_mode"`
	Priority        string            `json:"priority"`
	Assignment      map[string]string `json:"assignment,omitempty"`
	Labels          []string          `json:"labels,omitempty"`
	LastActivityAt  time.Time         `json:"last_activity_at"`
	ResourceVersion string            `json:"resource_version"`
}
type Message struct {
	ID                       UUID      `json:"id"`
	ConversationID           UUID      `json:"conversation_id"`
	Direction                string    `json:"direction"`
	Origin                   string    `json:"origin"`
	Status                   string    `json:"status"`
	Text                     string    `json:"text"`
	ProviderMessageReference *string   `json:"provider_message_reference,omitempty"`
	OccurredAt               time.Time `json:"occurred_at"`
	CreatedAt                time.Time `json:"created_at"`
	Private                  bool      `json:"private"`
}
type ConversationRead struct {
	ConversationID    UUID   `json:"conversation_id"`
	LastReadMessageID *UUID  `json:"last_read_message_id,omitempty"`
	Status            string `json:"status"`
}
type CannedReply struct {
	ID              UUID      `json:"id"`
	BusinessID      UUID      `json:"business_id"`
	Title           string    `json:"title"`
	Shortcut        string    `json:"shortcut"`
	Body            string    `json:"body"`
	Status          string    `json:"status"`
	ResourceVersion string    `json:"resource_version"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}
type AutomationRule struct {
	ID              UUID           `json:"id"`
	BusinessID      UUID           `json:"business_id"`
	Name            string         `json:"name"`
	Status          string         `json:"status"`
	TriggerKind     string         `json:"trigger_kind"`
	Conditions      map[string]any `json:"conditions"`
	ActionKind      string         `json:"action_kind"`
	ActionPayload   map[string]any `json:"action_payload"`
	Position        int            `json:"position"`
	ResourceVersion string         `json:"resource_version"`
	CreatedAt       time.Time      `json:"created_at"`
	UpdatedAt       time.Time      `json:"updated_at"`
}
type ContactPoint struct {
	Kind               string `json:"kind"`
	ValueNormalized    string `json:"value_normalized"`
	VerificationStatus string `json:"verification_status"`
}
type Customer struct {
	ID              UUID           `json:"id"`
	BusinessID      UUID           `json:"business_id"`
	DisplayName     *string        `json:"display_name,omitempty"`
	Profile         map[string]any `json:"profile,omitempty"`
	ContactPoints   []ContactPoint `json:"contact_points,omitempty"`
	Status          string         `json:"status"`
	CreatedAt       time.Time      `json:"created_at"`
	UpdatedAt       time.Time      `json:"updated_at"`
	ResourceVersion string         `json:"resource_version"`
}

type Catalog struct {
	ID              UUID      `json:"id"`
	BusinessID      UUID      `json:"business_id"`
	Name            string    `json:"name"`
	Description     *string   `json:"description,omitempty"`
	Status          string    `json:"status"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
	ResourceVersion string    `json:"resource_version"`
}
type CatalogItem struct {
	ID                     UUID           `json:"id"`
	BusinessID             UUID           `json:"business_id"`
	CatalogID              UUID           `json:"catalog_id"`
	AttributeSchemaID      *UUID          `json:"attribute_schema_id,omitempty"`
	AttributeSchemaVersion *int           `json:"attribute_schema_version,omitempty"`
	ItemType               string         `json:"item_type"`
	Name                   string         `json:"name"`
	ShortDescription       *string        `json:"short_description,omitempty"`
	LongDescription        *string        `json:"long_description,omitempty"`
	Status                 string         `json:"status"`
	PricingMode            string         `json:"pricing_mode"`
	AvailabilityMode       string         `json:"availability_mode"`
	FulfillmentMode        string         `json:"fulfillment_mode"`
	RequiresConfirmation   bool           `json:"requires_confirmation"`
	Attributes             map[string]any `json:"attributes,omitempty"`
	ResourceVersion        string         `json:"resource_version"`
}
type Offer struct {
	ID                      UUID       `json:"id"`
	BusinessID              UUID       `json:"business_id"`
	CatalogItemID           UUID       `json:"catalog_item_id"`
	VariantID               *UUID      `json:"variant_id,omitempty"`
	Name                    string     `json:"name"`
	PricingMode             string     `json:"pricing_mode"`
	Amount                  *float64   `json:"amount,omitempty"`
	Currency                *string    `json:"currency,omitempty"`
	PricingUnit             *string    `json:"pricing_unit,omitempty"`
	PriceSource             *string    `json:"price_source,omitempty"`
	PriceVerificationStatus string     `json:"price_verification_status"`
	PriceCheckedAt          *time.Time `json:"price_checked_at,omitempty"`
	AvailabilityMode        string     `json:"availability_mode"`
	AvailabilitySource      *string    `json:"availability_source,omitempty"`
	AvailabilityCheckedAt   *time.Time `json:"availability_checked_at,omitempty"`
	AvailabilityValidUntil  *time.Time `json:"availability_valid_until,omitempty"`
	AvailabilityEvidenceRef *string    `json:"availability_evidence_ref,omitempty"`
	FulfillmentMode         string     `json:"fulfillment_mode"`
	ValidityFrom            *time.Time `json:"validity_from,omitempty"`
	ValidityUntil           *time.Time `json:"validity_until,omitempty"`
	AvailabilityStatus      string     `json:"availability_status"`
	Status                  string     `json:"status"`
	ResourceVersion         string     `json:"resource_version"`
}
type Variant struct {
	ID              UUID           `json:"id"`
	BusinessID      UUID           `json:"business_id"`
	CatalogItemID   UUID           `json:"catalog_item_id"`
	Name            string         `json:"name"`
	Attributes      map[string]any `json:"attributes,omitempty"`
	Status          string         `json:"status"`
	ResourceVersion string         `json:"resource_version"`
}
type AttributeDefinition struct {
	ID              UUID           `json:"id"`
	Key             string         `json:"key"`
	Label           string         `json:"label"`
	DataType        string         `json:"data_type"`
	Required        bool           `json:"required"`
	ValidationRules map[string]any `json:"validation_rules,omitempty"`
	DisplayOrder    int            `json:"display_order"`
}
type AttributeSchema struct {
	ID          UUID                  `json:"id"`
	BusinessID  UUID                  `json:"business_id"`
	Name        string                `json:"name"`
	Version     int                   `json:"version"`
	Definitions []AttributeDefinition `json:"definitions"`
}

type Lead struct {
	ID              UUID     `json:"id"`
	BusinessID      UUID     `json:"business_id"`
	CustomerID      UUID     `json:"customer_id"`
	Status          string   `json:"status"`
	CurrentScore    *float64 `json:"current_score,omitempty"`
	ScoreBand       string   `json:"score_band,omitempty"`
	CreatedBy       string   `json:"created_by"`
	ResourceVersion string   `json:"resource_version"`
}
type LeadAttribution struct {
	ID                   UUID      `json:"id"`
	LeadID               UUID      `json:"lead_id"`
	SourceConversationID *UUID     `json:"source_conversation_id,omitempty"`
	SourceChannel        *string   `json:"source_channel,omitempty"`
	CatalogItemID        *UUID     `json:"catalog_item_id,omitempty"`
	OfferID              *UUID     `json:"offer_id,omitempty"`
	CapturedAt           time.Time `json:"captured_at"`
}
type LeadScore struct {
	ID           UUID           `json:"id"`
	LeadID       UUID           `json:"lead_id"`
	Value        float64        `json:"value"`
	Band         string         `json:"band"`
	Factors      map[string]any `json:"factors"`
	CalculatedAt time.Time      `json:"calculated_at"`
}

type OrderLineSnapshot struct {
	ID                UUID     `json:"id"`
	CatalogItemID     UUID     `json:"catalog_item_id"`
	OfferID           *UUID    `json:"offer_id,omitempty"`
	VariantID         *UUID    `json:"variant_id,omitempty"`
	ItemNameSnapshot  string   `json:"item_name_snapshot"`
	Quantity          float64  `json:"quantity"`
	UnitPriceSnapshot *float64 `json:"unit_price_snapshot,omitempty"`
	Currency          *string  `json:"currency,omitempty"`
}
type CommercialTransaction struct {
	ID              UUID                `json:"id"`
	BusinessID      UUID                `json:"business_id"`
	CustomerID      UUID                `json:"customer_id"`
	LeadID          *UUID               `json:"lead_id,omitempty"`
	TransactionType string              `json:"transaction_type"`
	State           string              `json:"state"`
	Currency        *string             `json:"currency,omitempty"`
	TotalAmount     *float64            `json:"total_amount,omitempty"`
	Lines           []OrderLineSnapshot `json:"lines"`
	SchemaVersion   int                 `json:"schema_version"`
	ResourceVersion string              `json:"resource_version"`
	CreatedAt       time.Time           `json:"created_at"`
	UpdatedAt       time.Time           `json:"updated_at"`
}
type TransactionReview struct {
	Required          bool     `json:"required"`
	Status            string   `json:"status"`
	ReasonCodes       []string `json:"reason_codes"`
	ReviewerReference *string  `json:"reviewer_reference,omitempty"`
}

type AIDecision struct {
	ID                 UUID           `json:"id"`
	BusinessID         UUID           `json:"business_id"`
	ConversationID     *UUID          `json:"conversation_id,omitempty"`
	IntentBase         string         `json:"intent_base"`
	Entities           map[string]any `json:"entities"`
	EvidenceReferences []string       `json:"evidence_references"`
	RequestedAction    string         `json:"requested_action"`
	RequiresHuman      bool           `json:"requires_human"`
	MissingInformation []string       `json:"missing_information"`
	PolicyVersion      string         `json:"policy_version"`
	Lifecycle          string         `json:"lifecycle"`
	CreatedAt          time.Time      `json:"created_at"`
}
type AuditEvent struct {
	ID             UUID           `json:"id"`
	BusinessID     UUID           `json:"business_id"`
	ActorType      string         `json:"actor_type"`
	ActorReference *string        `json:"actor_reference,omitempty"`
	Action         string         `json:"action"`
	ResourceType   string         `json:"resource_type"`
	ResourceID     *string        `json:"resource_id,omitempty"`
	Metadata       map[string]any `json:"metadata"`
	OccurredAt     time.Time      `json:"occurred_at"`
}

// CatalogEntityContract is the public projection of the Catalog Entity
// Contract descriptor (contract ⑤ §8). Returned by GET
// /businesses/{business_id}/catalogs/entity-contract.
//
// Per contract ⑤ §17, this contains ONLY type/meaning definitions — no
// merchant-specific data. The same payload is sent to Gemini in
// system_instruction; this endpoint exposes it to the dashboard frontend
// so the UI can translate enum values to Arabic labels WITHOUT hardcoding
// them in the client (per the "no hardcoded code in frontend" directive).
//
// Every map below mirrors services.CatalogEntityContractDescriptor verbatim
// (sourced from the SQL migration CHECK constraints). If the SQL migrations
// add a new enum value, services.DefaultCatalogEntityContractDescriptor()
// is the source of truth and this DTO will carry the new value
// automatically.
type CatalogEntityContract struct {
	// PricingModes maps each pricing_mode enum value to its Arabic
	// description. Per migration 000016/000018 offers_pricing_mode_chk +
	// catalog_items_pricing_mode_chk, the 7 allowed values are:
	// fixed, starting_from, per_unit, per_person, per_day, quote_required, dynamic.
	PricingModes map[string]string `json:"pricing_modes"`
	// AvailabilityModes maps each availability_mode enum value to its Arabic
	// description. Per migration 000016/000018 catalog_items_availability_mode_chk +
	// offers_availability_mode_chk, the 5 allowed values are:
	// stock, schedule, supplier_check, always_available, unknown.
	AvailabilityModes map[string]string `json:"availability_modes"`
	// AvailabilityStatuses maps each availability_status enum value to its
	// Arabic description. Per migration 000018 offers_availability_status_chk,
	// the 5 allowed values are: available, unavailable, unknown, requires_check, stale.
	AvailabilityStatuses map[string]string `json:"availability_statuses"`
	// PriceVerificationStatuses maps each price_verification_status enum
	// value to its Arabic description. Per migration 000018
	// offers_price_verification_chk, the 4 allowed values are:
	// unverified, verified, stale, rejected.
	PriceVerificationStatuses map[string]string `json:"price_verification_statuses"`
	// FulfillmentModes maps each fulfillment_mode enum value to its Arabic
	// description. Per migration 000016/000018 catalog_items_fulfillment_mode_chk +
	// offers_fulfillment_mode_chk, the 6 allowed values are:
	// delivery, pickup, digital, appointment, travel, manual.
	FulfillmentModes map[string]string `json:"fulfillment_modes"`
	// ItemStatuses maps catalog_items.status values to their Arabic description.
	// Per migration 000016, CatalogItem allows: draft, active, inactive, archived.
	// Offer status is separate and additionally allows expired.
	ItemStatuses map[string]string `json:"item_statuses"`
}
