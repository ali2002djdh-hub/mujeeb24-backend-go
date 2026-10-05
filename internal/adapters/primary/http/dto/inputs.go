package dto

import (
	"encoding/json"
	"github.com/Ammar777782439/mujeeb24-backend-go/internal/application/merchantcatalogai"
	"net/http"
)

// Common query and path DTOs. Huma reads the tags to generate OpenAPI parameters.
type EmptyInput struct{}
type BusinessPath struct {
	BusinessID UUID `path:"business_id" format:"uuid"`
}
type ConversationPath struct {
	BusinessID     UUID `path:"business_id" format:"uuid"`
	ConversationID UUID `path:"conversation_id" format:"uuid"`
}
type TeamMemberPath struct {
	BusinessID  UUID `path:"business_id" format:"uuid"`
	PrincipalID UUID `path:"principal_id" format:"uuid"`
}
type TeamMemberListInput struct {
	BusinessPath
	ListQuery
}
type TeamInvitationCreateInput struct {
	BusinessPath
	CommandHeaders
	Body CreateTeamInvitationRequest
}
type TeamInvitationAcceptInput struct {
	CommandHeaders
	Body AcceptTeamInvitationRequest
}
type TeamMemberRoleUpdateInput struct {
	TeamMemberPath
	CommandHeaders
	Body UpdateTeamMemberRoleRequest
}
type TeamMemberRevokeInput struct {
	TeamMemberPath
	CommandHeaders
}
type CannedReplyPath struct {
	BusinessID    UUID `path:"business_id" format:"uuid"`
	CannedReplyID UUID `path:"canned_reply_id" format:"uuid"`
}
type AutomationRulePath struct {
	BusinessID       UUID `path:"business_id" format:"uuid"`
	AutomationRuleID UUID `path:"automation_rule_id" format:"uuid"`
}
type CustomerPath struct {
	BusinessID UUID `path:"business_id" format:"uuid"`
	CustomerID UUID `path:"customer_id" format:"uuid"`
}
type ListQuery struct {
	Limit  int    `query:"limit" minimum:"1" maximum:"100" default:"25"`
	Cursor string `query:"cursor"`
}
type BusinessListInput struct {
	BusinessPath
	ListQuery
}
type MeBusinessListInput struct {
	ListQuery
}
type CatalogListInput struct {
	BusinessPath
	ListQuery
	Status string `query:"status"`
}
type ConversationListInput struct {
	BusinessPath
	ListQuery
	State      string `query:"state"`
	Ownership  string `query:"ownership"`
	Channel    string `query:"channel"`
	CustomerID UUID   `query:"customer_id" format:"uuid"`
}
type ConversationInput struct{ ConversationPath }
type ConversationMessageInput struct {
	ConversationPath
	ListQuery
	CommandHeaders
	Body SendMessageRequest
}
type ConversationReadInput struct {
	ConversationPath
	CommandHeaders
}
type CannedReplyListInput struct {
	BusinessPath
	ListQuery
	Status string `query:"status" enum:"active,archived"`
}
type CannedReplyCreateInput struct {
	BusinessPath
	CommandHeaders
	Body CreateCannedReplyRequest
}
type CannedReplyUpdateInput struct {
	CannedReplyPath
	CommandHeaders
	Body UpdateCannedReplyRequest
}
type SendCannedReplyInput struct {
	CannedReplyPath
	ConversationID UUID `path:"conversation_id" format:"uuid"`
	CommandHeaders
}
type AutomationRuleListInput struct {
	BusinessPath
	ListQuery
	Status string `query:"status" enum:"active,disabled"`
}
type AutomationRuleCreateInput struct {
	BusinessPath
	CommandHeaders
	Body CreateAutomationRuleRequest
}
type AutomationRuleUpdateInput struct {
	AutomationRulePath
	CommandHeaders
	Body UpdateAutomationRuleRequest
}
type CustomerInput struct{ CustomerPath }
type BusinessBodyInput struct {
	BusinessPath
	Body any
}

type SendMessageRequest struct {
	Text string `json:"text" minLength:"1" maxLength:"10000"`
}
type AssignConversationRequest struct {
	AssigneePrincipalID UUID `json:"assignee_principal_id" format:"uuid"`
}
type CreateTeamInvitationRequest struct {
	Email          string `json:"email" format:"email" minLength:"3" maxLength:"320"`
	Role           string `json:"role" enum:"admin,manager,agent,analyst,viewer"`
	ExpiresInHours int    `json:"expires_in_hours,omitempty" minimum:"1" maximum:"720"`
}
type AcceptTeamInvitationRequest struct {
	AcceptanceToken string `json:"acceptance_token" minLength:"1" maxLength:"200"`
}
type UpdateTeamMemberRoleRequest struct {
	Role string `json:"role" enum:"admin,manager,agent,analyst,viewer"`
}
type LabelsRequest struct {
	Add    []string `json:"add,omitempty" maxItems:"50"`
	Remove []string `json:"remove,omitempty" maxItems:"50"`
}
type PrivateNoteRequest struct {
	Text string `json:"text" minLength:"1" maxLength:"10000"`
}
type CreateCannedReplyRequest struct {
	Title    string `json:"title" minLength:"1" maxLength:"200"`
	Shortcut string `json:"shortcut" minLength:"1" maxLength:"80"`
	Body     string `json:"body" minLength:"1" maxLength:"10000"`
}
type UpdateCannedReplyRequest struct {
	Title    *string `json:"title,omitempty" maxLength:"200"`
	Shortcut *string `json:"shortcut,omitempty" maxLength:"80"`
	Body     *string `json:"body,omitempty" maxLength:"10000"`
	Status   *string `json:"status,omitempty" enum:"active,archived"`
}
type CreateAutomationRuleRequest struct {
	Name          string         `json:"name" minLength:"1" maxLength:"200"`
	Conditions    map[string]any `json:"conditions"`
	ActionKind    string         `json:"action_kind" enum:"add_label,set_priority,assign_human"`
	ActionPayload map[string]any `json:"action_payload"`
	Position      int            `json:"position,omitempty" minimum:"1"`
}
type UpdateAutomationRuleRequest struct {
	Name          *string        `json:"name,omitempty" maxLength:"200"`
	Status        *string        `json:"status,omitempty" enum:"active,disabled"`
	Conditions    map[string]any `json:"conditions,omitempty"`
	ActionKind    *string        `json:"action_kind,omitempty" enum:"add_label,set_priority,assign_human"`
	ActionPayload map[string]any `json:"action_payload,omitempty"`
	Position      *int           `json:"position,omitempty" minimum:"1"`
}
type CreateCustomerRequest struct {
	Profile          map[string]any      `json:"profile,omitempty"`
	ContactPoints    []ContactPointInput `json:"contact_points,omitempty"`
	LocalePreference string              `json:"locale_preference,omitempty" maxLength:"20"`
}
type ContactPointInput struct {
	Kind  string `json:"kind"`
	Value string `json:"value" minLength:"1" maxLength:"300"`
}
type MergeCustomerRequest struct {
	TargetCustomerID UUID   `json:"target_customer_id" format:"uuid"`
	Reason           string `json:"reason" minLength:"1" maxLength:"500"`
}
type BusinessUpdateRequest struct {
	Name            string `json:"name,omitempty" maxLength:"200"`
	VerticalType    string `json:"vertical_type,omitempty"`
	Timezone        string `json:"timezone,omitempty"`
	DefaultCurrency string `json:"default_currency,omitempty"`
	Locale          string `json:"locale,omitempty"`
}
type BusinessPolicyUpdateRequest struct {
	AIMode                    string `json:"ai_mode,omitempty"`
	DefaultHumanReview        *bool  `json:"default_human_review,omitempty"`
	AllowAutoReply            *bool  `json:"allow_auto_reply,omitempty"`
	AllowAutoLeadCreation     *bool  `json:"allow_auto_lead_creation,omitempty"`
	AllowAutoTransactionDraft *bool  `json:"allow_auto_transaction_draft,omitempty"`
	AllowAutoConfirmation     *bool  `json:"allow_auto_confirmation,omitempty"`
}

// Path DTOs keep every nested resource explicitly tenant-scoped.
type CatalogPath struct {
	BusinessID UUID `path:"business_id" format:"uuid"`
	CatalogID  UUID `path:"catalog_id" format:"uuid"`
}
type ItemPath struct {
	BusinessID UUID `path:"business_id" format:"uuid"`
	ItemID     UUID `path:"item_id" format:"uuid"`
}
type OfferPath struct {
	BusinessID UUID `path:"business_id" format:"uuid"`
	OfferID    UUID `path:"offer_id" format:"uuid"`
}
type VariantPath struct {
	BusinessID UUID `path:"business_id" format:"uuid"`
	VariantID  UUID `path:"variant_id" format:"uuid"`
}
type SchemaPath struct {
	BusinessID UUID `path:"business_id" format:"uuid"`
	SchemaID   UUID `path:"schema_id" format:"uuid"`
}
type LeadPath struct {
	BusinessID UUID `path:"business_id" format:"uuid"`
	LeadID     UUID `path:"lead_id" format:"uuid"`
}
type TransactionPath struct {
	BusinessID    UUID `path:"business_id" format:"uuid"`
	TransactionID UUID `path:"transaction_id" format:"uuid"`
}
type DecisionPath struct {
	BusinessID UUID `path:"business_id" format:"uuid"`
	DecisionID UUID `path:"decision_id" format:"uuid"`
}
type AuditPath struct {
	BusinessID   UUID `path:"business_id" format:"uuid"`
	AuditEventID UUID `path:"audit_event_id" format:"uuid"`
}
type ConnectionPath struct {
	BusinessID   UUID `path:"business_id" format:"uuid"`
	ConnectionID UUID `path:"connection_id" format:"uuid"`
}
type ItemOffersInput struct {
	ItemPath
	ListQuery
	Status string `query:"status"`
}
type ItemVariantsInput struct {
	ItemPath
	ListQuery
	Status string `query:"status"`
}
type CatalogItemsInput struct {
	CatalogPath
	ListQuery
	Status string `query:"status"`
	Search string `query:"search"`
}
type AttributeSchemasInput struct {
	BusinessPath
	ListQuery
	Name    string `query:"name"`
	Version int    `query:"version"`
}
type LeadListInput struct {
	BusinessPath
	ListQuery
	Status     string `query:"status"`
	CustomerID UUID   `query:"customer_id" format:"uuid"`
	ScoreBand  string `query:"score_band"`
}
type LeadAttributionsInput struct {
	LeadPath
	ListQuery
}
type LeadScoresInput struct {
	LeadPath
	ListQuery
}
type TransactionListInput struct {
	BusinessPath
	ListQuery
	State           string `query:"state"`
	TransactionType string `query:"transaction_type"`
	CustomerID      UUID   `query:"customer_id" format:"uuid"`
}
type AIDecisionListInput struct {
	BusinessPath
	ListQuery
	Lifecycle      string       `query:"lifecycle"`
	ConversationID UUID         `query:"conversation_id" format:"uuid"`
	RequiresHuman  OptionalBool `query:"requires_human"`
}
type AuditListInput struct {
	BusinessPath
	ListQuery
	ActorType    string `query:"actor_type"`
	Action       string `query:"action"`
	ResourceType string `query:"resource_type"`
	From         string `query:"from"`
	Until        string `query:"until"`
}

type CommandHeaders struct {
	IdempotencyKey string `header:"Idempotency-Key"`
	IfMatch        string `header:"If-Match"`
	XRequestID     string `header:"X-Request-ID"`
	XCorrelationID string `header:"X-Correlation-ID"`
}
type BusinessBody[T any] struct {
	BusinessPath
	CommandHeaders
	Body T
}
type ConnectionBody[T any] struct {
	ConnectionPath
	CommandHeaders
	Body T
}
type ConversationBody[T any] struct {
	ConversationPath
	CommandHeaders
	Body T
}
type CustomerBody[T any] struct {
	CustomerPath
	CommandHeaders
	Body T
}
type CatalogBody[T any] struct {
	CatalogPath
	CommandHeaders
	Body T
}
type ItemBody[T any] struct {
	ItemPath
	CommandHeaders
	Body T
}
type OfferBody[T any] struct {
	OfferPath
	CommandHeaders
	Body T
}
type VariantBody[T any] struct {
	VariantPath
	CommandHeaders
	Body T
}
type LeadBody[T any] struct {
	LeadPath
	CommandHeaders
	Body T
}
type TransactionBody[T any] struct {
	TransactionPath
	CommandHeaders
	Body T
}
type DecisionBody[T any] struct {
	DecisionPath
	CommandHeaders
	Body T
}

type BeginChannelConnectionRequest struct {
	Provider    string `json:"provider"`
	Channel     string `json:"channel"`
	DisplayName string `json:"display_name,omitempty"`
}
type ConnectionActionRequest struct {
	Reason string `json:"reason" minLength:"1" maxLength:"500"`
}
type UpdateConversationRequest struct {
	State          string `json:"state,omitempty"`
	Ownership      string `json:"ownership,omitempty"`
	AIModeOverride string `json:"ai_mode_override,omitempty"`
	Priority       string `json:"priority,omitempty"`
}
type CreateCatalogRequest struct {
	Name        string `json:"name" minLength:"1" maxLength:"300"`
	Description string `json:"description,omitempty" maxLength:"5000"`
}
type UpdateCatalogRequest struct {
	Name        string  `json:"name,omitempty" maxLength:"300"`
	Description *string `json:"description,omitempty"`
	Status      string  `json:"status,omitempty"`
}
type CreateAttributeSchemaRequest struct {
	Name        string                     `json:"name"`
	Definitions []AttributeDefinitionInput `json:"definitions"`
}
type AttributeDefinitionInput struct {
	Key             string         `json:"key"`
	Label           string         `json:"label"`
	DataType        string         `json:"data_type"`
	Required        bool           `json:"required"`
	ValidationRules map[string]any `json:"validation_rules,omitempty"`
	DisplayOrder    int            `json:"display_order"`
}
type CreateCatalogItemRequest struct {
	AttributeSchemaID    *UUID          `json:"attribute_schema_id,omitempty"`
	ItemType             string         `json:"item_type"`
	Name                 string         `json:"name"`
	ShortDescription     *string        `json:"short_description,omitempty"`
	LongDescription      *string        `json:"long_description,omitempty"`
	PricingMode          string         `json:"pricing_mode"`
	AvailabilityMode     string         `json:"availability_mode"`
	FulfillmentMode      string         `json:"fulfillment_mode"`
	RequiresConfirmation bool           `json:"requires_confirmation"`
	Attributes           map[string]any `json:"attributes,omitempty"`
}
type UpdateCatalogItemRequest struct {
	Name                 string         `json:"name,omitempty"`
	Status               string         `json:"status,omitempty"`
	ItemType             string         `json:"item_type,omitempty"`
	ShortDescription     *string        `json:"short_description,omitempty"`
	LongDescription      *string        `json:"long_description,omitempty"`
	PricingMode          string         `json:"pricing_mode,omitempty"`
	AvailabilityMode     string         `json:"availability_mode,omitempty"`
	FulfillmentMode      string         `json:"fulfillment_mode,omitempty"`
	RequiresConfirmation *bool          `json:"requires_confirmation,omitempty"`
	Attributes           map[string]any `json:"attributes,omitempty"`
}
type CreateOfferRequest struct {
	VariantID          *UUID    `json:"variant_id,omitempty"`
	Name               string   `json:"name"`
	PricingMode        string   `json:"pricing_mode"`
	Amount             *float64 `json:"amount,omitempty"`
	Currency           *string  `json:"currency,omitempty"`
	PricingUnit        *string  `json:"pricing_unit,omitempty"`
	AvailabilityMode   string   `json:"availability_mode"`
	AvailabilityStatus string   `json:"availability_status"`
	FulfillmentMode    string   `json:"fulfillment_mode"`
	Status             string   `json:"status,omitempty"`
}
type UpdateOfferRequest struct {
	Name               string   `json:"name,omitempty"`
	Amount             *float64 `json:"amount,omitempty"`
	AvailabilityStatus string   `json:"availability_status,omitempty"`
	Status             string   `json:"status,omitempty"`
}
type CreateVariantRequest struct {
	Name       string         `json:"name"`
	Attributes map[string]any `json:"attributes"`
}
type UpdateVariantRequest struct {
	Name       string         `json:"name,omitempty"`
	Attributes map[string]any `json:"attributes,omitempty"`
	Status     string         `json:"status,omitempty"`
}
type CreateLeadRequest struct {
	CustomerID           UUID           `json:"customer_id" format:"uuid"`
	QualificationContext map[string]any `json:"qualification_context,omitempty"`
}
type UpdateLeadRequest struct {
	QualificationContext map[string]any `json:"qualification_context,omitempty"`
}
type LeadDecisionRequest struct {
	Reason             string   `json:"reason,omitempty"`
	EvidenceReferences []string `json:"evidence_references,omitempty"`
}
type MarkLeadLostRequest struct {
	LostReason string `json:"lost_reason"`
}
type TransactionLineInput struct {
	CatalogItemID      UUID           `json:"catalog_item_id" format:"uuid"`
	OfferID            *UUID          `json:"offer_id,omitempty"`
	VariantID          *UUID          `json:"variant_id,omitempty"`
	Quantity           float64        `json:"quantity"`
	SelectedAttributes map[string]any `json:"selected_attributes,omitempty"`
}
type CreateTransactionDraftRequest struct {
	CustomerID      UUID                   `json:"customer_id"`
	LeadID          *UUID                  `json:"lead_id,omitempty"`
	TransactionType string                 `json:"transaction_type"`
	Currency        *string                `json:"currency,omitempty"`
	Lines           []TransactionLineInput `json:"lines"`
}
type UpdateTransactionDraftRequest struct {
	Currency *string                `json:"currency,omitempty"`
	Lines    []TransactionLineInput `json:"lines,omitempty"`
}
type ConfirmTransactionRequest struct {
	EvidenceReference string `json:"evidence_reference"`
	PolicyVersion     string `json:"policy_version,omitempty"`
}
type TransactionReasonRequest struct {
	Reason string `json:"reason"`
}
type SubmitReviewRequest struct {
	ReasonCodes []string `json:"reason_codes"`
}
type ReviewDecisionRequest struct {
	ReviewerReference string `json:"reviewer_reference"`
	Reason            string `json:"reason,omitempty"`
}
type AIHumanRequest struct {
	Reason string `json:"reason"`
}

type BusinessPolicyInput struct {
	BusinessPath
	CommandHeaders
	Body BusinessPolicyUpdateRequest
}
type ConnectionCapabilityInput struct{ ConnectionPath }
type CatalogCreateInput struct {
	BusinessPath
	CommandHeaders
	Body CreateCatalogRequest
}
type CatalogUpdateInput struct {
	CatalogPath
	CommandHeaders
	Body UpdateCatalogRequest
}
type CatalogItemPath struct {
	CatalogPath
	ItemID UUID `path:"item_id" format:"uuid"`
}
type CatalogItemCreateInput struct {
	CatalogPath
	CommandHeaders
	Body CreateCatalogItemRequest
}
type CatalogItemUpdateInput struct {
	CatalogItemPath
	CommandHeaders
	Body UpdateCatalogItemRequest
}
type OfferCreateInput struct {
	ItemPath
	CommandHeaders
	Body CreateOfferRequest
}
type OfferUpdateInput struct {
	OfferPath
	CommandHeaders
	Body UpdateOfferRequest
}
type VariantCreateInput struct {
	ItemPath
	CommandHeaders
	Body CreateVariantRequest
}
type VariantUpdateInput struct {
	VariantPath
	CommandHeaders
	Body UpdateVariantRequest
}
type AttributeSchemaCreateInput struct {
	BusinessPath
	CommandHeaders
	Body CreateAttributeSchemaRequest
}
type LeadCreateInput struct {
	BusinessPath
	CommandHeaders
	Body CreateLeadRequest
}
type LeadUpdateInput struct {
	LeadPath
	CommandHeaders
	Body UpdateLeadRequest
}
type LeadQualifyInput struct {
	LeadPath
	CommandHeaders
	Body LeadDecisionRequest
}
type LeadLostInput struct {
	LeadPath
	CommandHeaders
	Body MarkLeadLostRequest
}
type TransactionCreateInput struct {
	BusinessPath
	CommandHeaders
	Body CreateTransactionDraftRequest
}
type TransactionUpdateInput struct {
	TransactionPath
	CommandHeaders
	Body UpdateTransactionDraftRequest
}
type TransactionConfirmInput struct {
	TransactionPath
	CommandHeaders
	Body ConfirmTransactionRequest
}
type TransactionCancelInput struct {
	TransactionPath
	CommandHeaders
	Body TransactionReasonRequest
}
type TransactionReviewSubmitInput struct {
	TransactionPath
	CommandHeaders
	Body SubmitReviewRequest
}
type TransactionReviewDecisionInput struct {
	TransactionPath
	CommandHeaders
	Body ReviewDecisionRequest
}
type AIHumanInput struct {
	DecisionPath
	CommandHeaders
	Body AIHumanRequest
}

type LoginInput struct {
	Body struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
}
type RefreshInput struct {
	Cookie string `header:"Cookie"`
}
type LogoutInput struct {
	Cookie string `header:"Cookie"`
}
type NoContentOutput struct {
	// SetCookie is set only by the logout flow to expire the refresh
	// cookie on the client (defense-in-depth: server-side revocation
	// is the real boundary, but we also clear the cookie so the
	// browser stops sending it). All other NoContentOutput callers
	// leave this nil.
	SetCookie *http.Cookie `header:"Set-Cookie,omitempty"`
}
type AuthResponse struct {
	AccessToken     string    `json:"access_token"`
	TokenType       string    `json:"token_type"`
	ExpiresAt       string    `json:"expires_at"`
	IsPlatformAdmin bool      `json:"is_platform_admin"`
	Principal       Principal `json:"principal"`
}
type Health struct {
	Status string            `json:"status"`
	Checks map[string]string `json:"checks,omitempty"`
}
type MetricsOutput struct {
	Body string `contentType:"text/plain"`
}
type WebhookPath struct {
	RouteKey string `path:"route_key"`
}
type WebhookHeaders struct {
	Signature            string `header:"X-Provider-Signature"`
	Timestamp            string `header:"X-Provider-Timestamp"`
	XRequestID           string `header:"X-Request-ID"`
	SocialAPISignature   string `header:"X-SocialAPI-Signature"`
	SocialAPISignatureV2 string `header:"X-SocialAPI-Signature-V2"`
	SocialAPITimestamp   string `header:"X-SocialAPI-Timestamp"`
	SocialAPIDelivery    string `header:"X-SocialAPI-Delivery"`
	SocialAPIEvent       string `header:"X-SocialAPI-Event"`
}
type SocialWebhookInput struct {
	WebhookPath
	WebhookHeaders
	Body    json.RawMessage
	RawBody []byte
}
type WebhookAccepted struct {
	Accepted  bool   `json:"accepted"`
	RequestID string `json:"request_id"`
}
type WebhookAcceptedOutput struct{ Body WebhookAccepted }

type ConnectionCreateInput struct {
	BusinessPath
	CommandHeaders
	Body BeginChannelConnectionRequest
}
type ConnectionActionInput struct {
	ConnectionPath
	CommandHeaders
	Body ConnectionActionRequest
}
type ConversationAssignInput struct {
	ConversationPath
	CommandHeaders
	Body AssignConversationRequest
}
type ConversationLabelsInput struct {
	ConversationPath
	CommandHeaders
	Body LabelsRequest
}
type ConversationNoteInput struct {
	ConversationPath
	CommandHeaders
	Body PrivateNoteRequest
}
type CustomerConversationsInput struct {
	CustomerPath
	ListQuery
}
type CustomerTransactionsInput struct {
	CustomerPath
	ListQuery
}
type CustomerMergeInput struct {
	CustomerPath
	CommandHeaders
	Body MergeCustomerRequest
}

type ConnectionListInput struct {
	BusinessPath
	ListQuery
	Status  string `query:"status"`
	Channel string `query:"channel"`
}
type ConversationUpdateInput struct {
	ConversationPath
	CommandHeaders
	Body UpdateConversationRequest
}
type CustomerUpdateInput struct {
	CustomerPath
	CommandHeaders
	Body UpdateCustomerRequest
}

type UpdateCustomerRequest struct {
	Profile          map[string]any      `json:"profile,omitempty"`
	ContactPoints    []ContactPointInput `json:"contact_points,omitempty"`
	LocalePreference string              `json:"locale_preference,omitempty" maxLength:"20"`
}

// Named inputs used by operation registration. They remain DTOs; contract only
// registers these types and supplies operation metadata.
type BusinessUpdateInput struct {
	BusinessPath
	CommandHeaders
	Body BusinessUpdateRequest
}
type ConversationMessageListInput struct {
	ConversationPath
	ListQuery
}
type CustomerListInput struct {
	BusinessPath
	ListQuery
	Search string `query:"search"`
	Status string `query:"status"`
}
type CreateCustomerInput struct {
	BusinessPath
	IdempotencyKey string `header:"Idempotency-Key"`
	XRequestID     string `header:"X-Request-ID"`
	Body           CreateCustomerRequest
}

// CatalogEntityContractPath is the input shape for GET
// /businesses/{business_id}/catalogs/entity-contract. Per contract ⑤ §17,
// the Catalog Entity Contract is global (same for every business), but
// we scope the endpoint to a business_id for authentication/authorization
// consistency with other dashboard endpoints — the value itself is
// independent of business_id.

type MerchantCatalogAIV2Input struct {
	BusinessPath
	CommandHeaders
	Body MerchantCatalogAITurnRequest
}

type MerchantCatalogAITurnRequest struct {
	SessionID       string `json:"session_id,omitempty"`
	Message         string `json:"message" minLength:"1" maxLength:"8000"`
	TargetCatalogID string `json:"target_catalog_id,omitempty" format:"uuid"`
}

type MerchantCatalogAIResponse struct {
	SessionID   string                     `json:"session_id"`
	CatalogID   string                     `json:"catalog_id"`
	CatalogName string                     `json:"catalog_name"`
	Proposal    merchantcatalogai.Proposal `json:"proposal"`
}

type CatalogEntityContractPath struct {
	BusinessID UUID `path:"business_id" format:"uuid"`
}
