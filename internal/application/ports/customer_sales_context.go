package ports

import (
	"context"
	"time"
)

// CustomerSalesDecisionRequest is the customer-facing request supplied to the B2C AI.
type CustomerSalesDecisionRequest struct {
	BusinessID             string
	ConversationID         string
	SourceMessageReference string
	Text                   string
	Channel                string
	PolicyVersion          string
	Context                *CustomerSalesContext
}

// CustomerSalesContextInput is the tenant-scoped input for B2C customer-sales context building.
type CustomerSalesContextInput struct {
	BusinessID             string
	ConversationID         string
	SourceMessageReference string
	Text                   string
	Channel                string
	PolicyVersion          string
	ConversationState      *ConversationStateRecord
	RecentMessages         []CustomerSalesRecentMessageEvidence
}

type CustomerSalesContextBuilder interface {
	Build(context.Context, CustomerSalesContextInput) (CustomerSalesContext, error)
}

// CustomerSalesPolicyDecision is the deterministic B2C policy outcome
// applied after the customer-sales proposal has been validated.
type CustomerSalesPolicyDecision struct {
	Decision      string
	RequiresHuman bool
	Reason        string
}

// CustomerSalesPolicyPort evaluates merchant policy for a customer-sales proposal.
// It never interprets customer intent and never executes the proposed action.
type CustomerSalesPolicyPort interface {
	Evaluate(context.Context, CustomerSalesProposal, *CustomerSalesContext) CustomerSalesPolicyDecision
}

type CustomerSalesContext struct {
	SchemaVersion          int
	Freshness              string
	Business               CustomerSalesContextBusiness
	Conversation           CustomerSalesContextConversation
	Customer               CustomerSalesContextCustomer
	CatalogEvidence        []CustomerSalesCatalogEvidence
	CatalogSchemaEvidence  []CustomerSalesCatalogSchemaEvidence
	OfferEvidence          []CustomerSalesOfferEvidence
	VariantEvidence        []CustomerSalesVariantEvidence
	KnowledgeEvidence      []CustomerSalesKnowledgeEvidence
	BusinessPolicyEvidence []CustomerSalesBusinessPolicyEvidence
	RecentMessages         []CustomerSalesRecentMessageEvidence
	PolicyEvidence         CustomerSalesPolicyEvidence
	KnowledgeState         string
	ConversationState      *ConversationStateRecord
	GeneratedAt            time.Time
	ExpiresAt              time.Time
	// CatalogManifest is a bounded map of the complete active catalog shape.
	// It tells the model which catalogs, item types and dynamic schemas exist
	// without serializing every item on every turn. Specific commercial claims
	// still require detailed Catalog/Variant/Offer evidence.
	CatalogManifest *CatalogAIManifest
	// ConversationSummary is the LLM-generated running summary of older
	// conversation turns (everything older than the sliding window of
	// recent messages). Per ADR-039, this is sent to Gemini alongside
	// RecentMessages so it can understand long conversation context
	// without us sending the full history verbatim.
	ConversationSummary string
}

type CustomerSalesStateProposal struct {
	Focus         *ConversationFocus      `json:"focus,omitempty"`
	Comparison    *ConversationComparison `json:"comparison,omitempty"`
	Kind          string                  `json:"kind"`
	ReferenceText string                  `json:"reference_text,omitempty"`
	Alternatives  []ConversationFocus     `json:"alternatives,omitempty"`
}

type CustomerSalesContextBusiness struct {
	Reference       string
	Name            string
	VerticalType    string
	Locale          string
	DefaultCurrency string
}

type CustomerSalesContextConversation struct {
	Reference           string
	CustomerReference   string
	State               string
	Ownership           string
	Priority            string
	AIModeOverride      string
	AssignmentReference string
	// LastGeminiInteractionID per contract ③ §4 + migration 000057.
	//
	// Per Item 8: this carries the conversation's last successful Gemini
	// interaction ID from the context builder (which loaded the
	// ConversationRecord) to the AutoReply handler. The handler passes
	// it as PreviousInteractionID to the next CustomerSalesDecisionPort.Decide call —
	// enabling Gemini Interactions API chaining (store=true per §9).
	//
	// Per contract ③ §5: Mujeeb retention is canonical; this is just
	// continuity convenience. Empty/nil for the first turn.
	LastGeminiInteractionID *string
}

type CustomerSalesContextCustomer struct {
	Reference        string
	LocalePreference string
	Status           string
	Profile          []byte
	ContactPoints    []byte
}

type CustomerSalesCatalogEvidence struct {
	Reference                string
	CatalogReference         string
	AttributeSchemaReference *string
	AttributeSchemaVersion   *int
	ItemType                 string
	Name             string
	Status           string
	Attributes       []byte
	EvidenceState    string
	RetrievedAt      time.Time
	SchemaVersion    int
	// Per contract ① §1 + migration 000016 — the following fields are NOT NULL
	// in the DB and MUST be included in the evidence sent to Gemini.
	// Without them, Gemini can see the product exists but cannot answer
	// price/availability/fulfillment questions — leading to hallucination
	// or "we don't have this product" responses.
	ShortDescription     *string
	LongDescription      *string
	PricingMode          string
	AvailabilityMode     string
	FulfillmentMode      string
	RequiresConfirmation bool
}

type CustomerSalesCatalogSchemaEvidence struct {
	ID          string                                           `json:"id"`
	Name        string                                           `json:"name"`
	Version     int                                              `json:"version"`
	Definitions []CustomerSalesAttributeDefinitionEvidence       `json:"definitions"`
}

type CustomerSalesAttributeDefinitionEvidence struct {
	ID              string         `json:"id"`
	SchemaID        string         `json:"schema_id"`
	AttributeKey    string         `json:"attribute_key"`
	Label           string         `json:"label"`
	DataType        string         `json:"data_type"`
	IsRequired      bool           `json:"is_required"`
	ValidationRules map[string]any `json:"validation_rules,omitempty"`
	DisplayOrder    int            `json:"display_order"`
}

type CustomerSalesOfferEvidence struct {
	Reference                   string
	CatalogItemReference        string
	VariantReference            string
	Name                        string
	PricingMode                 string
	Amount                      string
	Currency                    string
	PricingUnit                 string     `json:"pricing_unit,omitempty"`
	PriceSource                 string     `json:"price_source,omitempty"`
	PriceVerificationStatus     string     `json:"price_verification_status,omitempty"`
	PriceCheckedAt              *time.Time `json:"price_checked_at,omitempty"`
	AvailabilityMode            string     `json:"availability_mode,omitempty"`
	AvailabilityStatus          string `json:"availability_status"`
	AvailabilitySource          string     `json:"availability_source,omitempty"`
	AvailabilityCheckedAt       *time.Time `json:"availability_checked_at,omitempty"`
	AvailabilityValidUntil      *time.Time `json:"availability_valid_until,omitempty"`
	AvailabilityEvidenceRef     string     `json:"availability_evidence_ref,omitempty"`
	FulfillmentMode             string     `json:"fulfillment_mode,omitempty"`
	ValidityFrom                *time.Time `json:"validity_from,omitempty"`
	ValidityUntil               *time.Time `json:"validity_until,omitempty"`
	Status                      string
	EvidenceState               string
	RetrievedAt                 time.Time
	SchemaVersion               int
}

type CustomerSalesKnowledgeEvidence struct {
	Reference       string
	KnowledgeKey    string
	Title           string
	Content         string
	ContentType     string
	SourceReference string
	Authority       string
	EvidenceState   string
	Version         int
	ValidFrom       time.Time
	ValidUntil      *time.Time
	RetrievedAt     time.Time
	SchemaVersion   int
}

type CustomerSalesBusinessPolicyEvidence struct {
	Reference     string
	PolicyKey     string
	Category      string
	Title         string
	Summary       string
	Rules         []byte
	Authority     string
	EvidenceState string
	Version       int
	ValidFrom     time.Time
	ValidUntil    *time.Time
	RetrievedAt   time.Time
	SchemaVersion int
}

type CustomerSalesVariantEvidence struct {
	Reference            string
	CatalogItemReference string
	Name                 string
	Status               string
	Attributes           []byte
	EvidenceState        string
	RetrievedAt          time.Time
	SchemaVersion        int
}

type CustomerSalesRecentMessageEvidence struct {
	Reference     string
	Direction     string
	Origin        string
	Text          string
	OccurredAt    time.Time
	EvidenceState string
	SchemaVersion int
}

type CustomerSalesPolicyEvidence struct {
	Reference     string
	Version       string
	State         string
	MissingReason string
	RetrievedAt   time.Time
	SchemaVersion int
}

// Catalog Retrieval State tracking has been REMOVED per contract ④ §5 + ⑥ §20.
// Per contract ⑧ §5, the operational trace (including catalog batch coverage)
// now lives in the ai_runs + ai_catalog_batches tables, NOT in the AI Proposal.
// Per contract ⑥ §20, validation is deterministic — no semantic re-matching.
// Contract ② §3 Coverage enforcement is handled by CatalogBatchController
// in services/catalog_batch_controller.go (using PartialProgressPolicy).

// AIDecisionProposal is the LEGACY proposal shape used only by ai_decisions row.
//
// Per contract ④ §5, the contract-aligned output shape is CustomerSalesProposal
// (status + action + response_text + selected[]) defined in ai_gemini_contract.go.
// New code MUST use CustomerSalesProposal + ValidationPipeline, not AIDecisionProposal.
//
// This struct is kept only as the persistence shape for ai_decisions (the
// business decision row), NOT as Gemini's output contract.
type AIDecisionProposal struct {
	IntentBase         string
	DomainContext      string
	Entities           []byte
	EvidenceReferences []byte
	RequestedAction    string
	ResponseText       string
	ConfidenceValue    string
	ConfidenceBand     string
	RequiresHuman      bool
	MissingInformation []byte
	ReasonCodes        []byte
	PolicyDecision     string
	PolicyVersion      string
	KnowledgeVersion   string
	ModelReference     string
	SchemaVersion      int
	StateProposal      *CustomerSalesStateProposal
	// Deprecated Mujeeb-side tracking fields (per contract ④ §5 + ⑥ §20):
	// Removed in favor of ai_runs table (operational) + ValidationPipeline
	// (deterministic). Kept struct minimal for persistence migration.
}
