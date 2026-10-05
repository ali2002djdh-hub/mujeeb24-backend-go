package services

import (
	"context"
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/Ammar777782439/mujeeb24-backend-go/internal/application/ports"
)

const (
	CustomerSalesContextSchemaVersion = 1
	AIEvidenceSchemaVersion           = 1
	CustomerSalesContextFresh         = "fresh"
	CustomerSalesContextPartial       = "partial"
	CustomerSalesContextMissing       = "missing"
	CustomerSalesContextStale         = "stale"
	CustomerSalesContextGrounded      = "grounded"
)

// AutoReplyContextBuilder builds a bounded, tenant-scoped context from Mujeeb
// records. It never calls a provider and never treats model output as evidence.
type AutoReplyContextBuilder struct {
	Businesses    ports.BusinessRepository
	Conversations ports.ConversationRepository
	Customers     ports.CustomerRepository
	Catalogs      ports.CatalogRepository
	CatalogAI     ports.CatalogAIReadRepository
	Messages      ports.MessageRepository
	Knowledge     ports.KnowledgeDocumentRepository
	Policies      ports.BusinessPolicyRepository
	Now           func() time.Time
	TTL           time.Duration
	MaxCatalogs   int
	MaxItems      int
	MaxOffers     int
	MaxVariants   int
	MaxMessages   int
	MaxKnowledge  int
	MaxPolicies   int
}

func NewAutoReplyContextBuilder(businesses ports.BusinessRepository, conversations ports.ConversationRepository, customers ports.CustomerRepository, catalogs ports.CatalogRepository, messages ports.MessageRepository) AutoReplyContextBuilder {
	return AutoReplyContextBuilder{
		Businesses:    businesses,
		Conversations: conversations,
		Customers:     customers,
		Catalogs:      catalogs,
		Messages:      messages,
		Now:           func() time.Time { return time.Now().UTC() },
		TTL:           2 * time.Minute,
		MaxCatalogs:   10,
		MaxItems:      5,
		MaxOffers:     5,
		MaxVariants:   5,
		MaxMessages:   4, // Per ADR-038: reduced from 8 to 6 per IrisAgent/Microsoft Learn best-practice research (sliding window threshold).
		MaxKnowledge:  10,
		MaxPolicies:   10,
	}
}

func (b AutoReplyContextBuilder) Build(ctx context.Context, input ports.CustomerSalesContextInput) (ports.CustomerSalesContext, error) {
	if strings.TrimSpace(input.BusinessID) == "" || strings.TrimSpace(input.ConversationID) == "" || strings.TrimSpace(input.Text) == "" {
		return ports.CustomerSalesContext{}, errors.New("business, conversation, and message text are required to build AI context")
	}
	if b.Businesses == nil || b.Conversations == nil || b.Customers == nil || b.Catalogs == nil || b.Messages == nil {
		return ports.CustomerSalesContext{}, errors.New("AI context builder repositories are not configured")
	}
	now := b.now()
	ttl := b.TTL
	if ttl <= 0 {
		ttl = 2 * time.Minute
	}
	business, err := b.Businesses.GetByID(ctx, input.BusinessID)
	if err != nil {
		return ports.CustomerSalesContext{}, err
	}
	if business.ID != input.BusinessID {
		return ports.CustomerSalesContext{}, errors.New("AI context business scope mismatch")
	}
	conversation, err := b.Conversations.GetByID(ctx, input.BusinessID, input.ConversationID)
	if err != nil {
		return ports.CustomerSalesContext{}, err
	}
	if conversation.ID != input.ConversationID || conversation.BusinessID != input.BusinessID {
		return ports.CustomerSalesContext{}, errors.New("AI context conversation scope mismatch")
	}
	customer, err := b.Customers.GetByID(ctx, input.BusinessID, conversation.CustomerID)
	if err != nil {
		return ports.CustomerSalesContext{}, err
	}
	if customer.BusinessID != input.BusinessID {
		return ports.CustomerSalesContext{}, errors.New("AI context customer scope mismatch")
	}

	context := ports.CustomerSalesContext{
		SchemaVersion: CustomerSalesContextSchemaVersion,
		Freshness:     CustomerSalesContextFresh,
		Business: ports.CustomerSalesContextBusiness{
			Reference:       business.ID,
			Name:            business.Name,
			VerticalType:    business.VerticalType,
			Locale:          business.Locale,
			DefaultCurrency: business.DefaultCurrency,
		},
		Conversation: ports.CustomerSalesContextConversation{
			Reference:           conversation.ID,
			CustomerReference:   conversation.CustomerID,
			State:               conversation.State,
			Ownership:           conversation.Ownership,
			Priority:            conversation.Priority,
			AIModeOverride:      stringValue(conversation.AIModeOverride),
			AssignmentReference: stringValue(conversation.AssignmentReference),
			// Per Item 8: carry the conversation's last Gemini
			// interaction ID so AutoReply can pass it as
			// PreviousInteractionID to the next DecideContract
			// call (Gemini Interactions API chaining per §9).
			LastGeminiInteractionID: conversation.LastGeminiInteractionID,
		},
		Customer: ports.CustomerSalesContextCustomer{
			Reference:        customer.ID,
			LocalePreference: stringValue(customer.LocalePreference),
			Status:           customer.Status,
			Profile:          safeJSONDocument(customer.Profile),
			ContactPoints:    safeJSONDocument(customer.ContactPoints),
		},
		PolicyEvidence: ports.CustomerSalesPolicyEvidence{
			Reference:     "application-policy/" + nonEmpty(input.PolicyVersion, "auto-reply-v1"),
			Version:       nonEmpty(input.PolicyVersion, "auto-reply-v1"),
			State:         "application_policy_only",
			MissingReason: "catalog and business policy repository is not configured in this slice",
			RetrievedAt:   now,
			SchemaVersion: AIEvidenceSchemaVersion,
		},
		ConversationState: input.ConversationState,
		// Per ADR-039: pass the conversation summary through to Gemini.
		// The summary is generated by ConversationSummaryService and stored
		// in conversation_state.summary. It contains a compressed view of
		// older conversation turns beyond the sliding window of RecentMessages.
		KnowledgeState: CustomerSalesContextMissing,
		GeneratedAt:    now,
		ExpiresAt:      now.Add(ttl),
	}
	if input.ConversationState != nil {
		context.ConversationSummary = input.ConversationState.Summary
	}

	messagePage, err := b.Messages.ListByConversation(ctx, input.BusinessID, input.ConversationID, b.maxMessages(), "")
	if err != nil {
		return ports.CustomerSalesContext{}, err
	}
	context.RecentMessages = buildRecentMessageEvidence(messagePage.Items, input.SourceMessageReference, now)

	// Universal Catalog AI v3: every turn gets a bounded map of the whole
	// active catalog. This is discovery metadata, not executable evidence.
	if b.CatalogAI != nil {
		manifest, manifestErr := b.CatalogAI.GetManifest(ctx, input.BusinessID)
		if manifestErr != nil {
			return ports.CustomerSalesContext{}, manifestErr
		}
		context.CatalogManifest = &manifest
	}

	mode, focus, comparison := resolveRetrievalMode(input.ConversationState)
	switch mode {
	case retrievalScopedOffer, retrievalScopedItem, retrievalScopedCatalog, retrievalScopedVariant:
		scopedItems, scopedOffers, scopedVariants, err := b.retrieveScoped(ctx, input.BusinessID, focus, comparison, now)
		if err != nil {
			// Invalid/stale focus is not validated truth: fall through to broader.
			if !isScopedNotFound(err) {
				return ports.CustomerSalesContext{}, err
			}
		} else {
			context.CatalogEvidence = scopedItems
			context.OfferEvidence = scopedOffers
			context.VariantEvidence = scopedVariants
			return b.finalizeContext(ctx, context, input, now)
		}
	case retrievalScopedComparison:
		scopedItems, scopedOffers, scopedVariants, err := b.retrieveComparison(ctx, input.BusinessID, comparison, now)
		if err != nil {
			if !isScopedNotFound(err) {
				return ports.CustomerSalesContext{}, err
			}
		} else {
			context.CatalogEvidence = scopedItems
			context.OfferEvidence = scopedOffers
			context.VariantEvidence = scopedVariants
			return b.finalizeContext(ctx, context, input, now)
		}
	}
	// Without a validated conversation focus, only the bounded catalog manifest
	// is attached here. Item-level catalog data is handled by the complete
	// catalog evaluation path when the model requests additional data.

	if b.Knowledge != nil {
		knowledgeRecords, listErr := b.Knowledge.ListPublished(ctx, input.BusinessID, "", now, b.maxKnowledge()*3)
		if listErr != nil {
			return ports.CustomerSalesContext{}, listErr
		}
		for _, record := range rankKnowledgeRecords(knowledgeRecords, input.Text) {
			if record.BusinessID != input.BusinessID {
				return ports.CustomerSalesContext{}, errors.New("AI context knowledge scope mismatch")
			}
			context.KnowledgeEvidence = append(context.KnowledgeEvidence, ports.CustomerSalesKnowledgeEvidence{
				Reference: record.ID, KnowledgeKey: record.KnowledgeKey, Title: record.Title, Content: record.Content,
				ContentType: record.ContentType, SourceReference: record.SourceReference, Authority: record.Authority,
				EvidenceState: evidenceStateForValidity(now, record.ValidFrom, record.ValidUntil), Version: record.Version, ValidFrom: record.ValidFrom, ValidUntil: record.ValidUntil,
				RetrievedAt: now, SchemaVersion: AIEvidenceSchemaVersion,
			})
			if len(context.KnowledgeEvidence) >= b.maxKnowledge() {
				break
			}
		}
	}
	if b.Policies != nil {
		policyRecords, listErr := b.Policies.ListPublished(ctx, input.BusinessID, "", now, b.maxPolicies()*3)
		if listErr != nil {
			return ports.CustomerSalesContext{}, listErr
		}
		for _, record := range rankPolicyRecords(policyRecords, input.Text) {
			if record.BusinessID != input.BusinessID {
				return ports.CustomerSalesContext{}, errors.New("AI context policy scope mismatch")
			}
			context.BusinessPolicyEvidence = append(context.BusinessPolicyEvidence, ports.CustomerSalesBusinessPolicyEvidence{
				Reference: record.ID, PolicyKey: record.PolicyKey, Category: record.Category, Title: record.Title,
				Summary: record.Summary, Rules: safeJSONObject(record.Rules), Authority: record.Authority,
				EvidenceState: evidenceStateForValidity(now, record.ValidFrom, record.ValidUntil), Version: record.Version, ValidFrom: record.ValidFrom, ValidUntil: record.ValidUntil,
				RetrievedAt: now, SchemaVersion: AIEvidenceSchemaVersion,
			})
			if len(context.BusinessPolicyEvidence) >= b.maxPolicies() {
				break
			}
		}
		if len(context.BusinessPolicyEvidence) > 0 {
			first := context.BusinessPolicyEvidence[0]
			context.PolicyEvidence = ports.CustomerSalesPolicyEvidence{Reference: first.Reference, Version: "policy-v" + formatInt(first.Version), State: "published", RetrievedAt: now, SchemaVersion: AIEvidenceSchemaVersion}
		}
	}

	for _, item := range context.CatalogEvidence {
		offers, listErr := b.Catalogs.ListOffers(ctx, input.BusinessID, item.Reference, "active", b.maxOffers(), "")
		if listErr != nil {
			return ports.CustomerSalesContext{}, listErr
		}
		for _, offer := range offers.Items {
			if offer.BusinessID != input.BusinessID || offer.CatalogItemID != item.Reference {
				return ports.CustomerSalesContext{}, errors.New("AI context offer scope mismatch")
			}
			context.OfferEvidence = append(context.OfferEvidence, toOfferEvidence(offer, now))
		}
		variants, listErr := b.Catalogs.ListVariants(ctx, input.BusinessID, item.Reference, "active", b.maxVariants(), "")
		if listErr != nil {
			return ports.CustomerSalesContext{}, listErr
		}
		for _, variant := range variants.Items {
			if variant.BusinessID != input.BusinessID || variant.CatalogItemID != item.Reference {
				return ports.CustomerSalesContext{}, errors.New("AI context variant scope mismatch")
			}
			context.VariantEvidence = append(context.VariantEvidence, ports.CustomerSalesVariantEvidence{
				Reference:            variant.ID,
				CatalogItemReference: variant.CatalogItemID,
				Name:                 variant.Name,
				Status:               variant.Status,
				Attributes:           safeJSONObject(variant.Attributes),
				EvidenceState:        CustomerSalesContextFresh,
				RetrievedAt:          now,
				SchemaVersion:        AIEvidenceSchemaVersion,
			})
		}
	}
	if len(context.CatalogEvidence) > 0 || len(context.OfferEvidence) > 0 || len(context.VariantEvidence) > 0 {
		context.KnowledgeState = CustomerSalesContextPartial
	}
	if len(context.KnowledgeEvidence) > 0 || len(context.BusinessPolicyEvidence) > 0 {
		context.KnowledgeState = CustomerSalesContextGrounded
	}
	if len(context.CatalogEvidence) == 0 {
		context.Freshness = CustomerSalesContextPartial
	}
	for _, offer := range context.OfferEvidence {
		if offer.EvidenceState == CustomerSalesContextStale {
			context.Freshness = CustomerSalesContextStale
			context.KnowledgeState = CustomerSalesContextPartial
			break
		}
	}
	for _, evidence := range context.KnowledgeEvidence {
		if evidence.EvidenceState == CustomerSalesContextStale {
			context.Freshness = CustomerSalesContextStale
			context.KnowledgeState = CustomerSalesContextPartial
			break
		}
	}
	for _, evidence := range context.BusinessPolicyEvidence {
		if evidence.EvidenceState == CustomerSalesContextStale {
			context.Freshness = CustomerSalesContextStale
			context.KnowledgeState = CustomerSalesContextPartial
			break
		}
	}
	return context, nil
}

func buildRecentMessageEvidence(records []ports.CommunicationMessageRecord, sourceReference string, now time.Time) []ports.CustomerSalesRecentMessageEvidence {
	items := make([]ports.CustomerSalesRecentMessageEvidence, 0, len(records))
	for _, record := range records {
		if record.ProviderMessageID != nil && strings.TrimSpace(*record.ProviderMessageID) == strings.TrimSpace(sourceReference) {
			continue
		}
		text := ""
		if record.TextContent != nil {
			text = strings.TrimSpace(*record.TextContent)
		}
		if text == "" {
			continue
		}
		items = append(items, ports.CustomerSalesRecentMessageEvidence{
			Reference:     record.ID,
			Direction:     record.Direction,
			Origin:        record.Origin,
			Text:          text,
			OccurredAt:    record.OccurredAt,
			EvidenceState: CustomerSalesContextFresh,
			SchemaVersion: AIEvidenceSchemaVersion,
		})
	}
	sort.SliceStable(items, func(i, j int) bool {
		if !items[i].OccurredAt.Equal(items[j].OccurredAt) {
			return items[i].OccurredAt.Before(items[j].OccurredAt)
		}
		// Per ADR-051: tiebreaker — Reference (message ID), to fix time inversion
		return items[i].Reference < items[j].Reference
	})
	return items
}

func rankKnowledgeRecords(records []ports.KnowledgeDocumentRecord, text string) []ports.KnowledgeDocumentRecord {
	tokens := tokenize(text)
	type scored struct {
		record ports.KnowledgeDocumentRecord
		score  int
	}
	items := make([]scored, 0, len(records))
	for _, record := range records {
		searchable := normalizeArabic(strings.ToLower(record.KnowledgeKey + " " + record.Title + " " + record.Content))
		score := 0
		for _, token := range tokens {
			if strings.Contains(searchable, token) {
				score++
			}
		}
		items = append(items, scored{record: record, score: score})
	}
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].score != items[j].score {
			return items[i].score > items[j].score
		}
		return items[i].record.ID < items[j].record.ID
	})
	result := make([]ports.KnowledgeDocumentRecord, 0, len(items))
	for _, item := range items {
		result = append(result, item.record)
	}
	return result
}

func rankPolicyRecords(records []ports.BusinessPolicyRecord, text string) []ports.BusinessPolicyRecord {
	tokens := tokenize(text)
	type scored struct {
		record ports.BusinessPolicyRecord
		score  int
	}
	items := make([]scored, 0, len(records))
	for _, record := range records {
		searchable := normalizeArabic(strings.ToLower(record.PolicyKey + " " + record.Category + " " + record.Title + " " + record.Summary + " " + string(record.Rules)))
		score := 0
		for _, token := range tokens {
			if strings.Contains(searchable, token) {
				score++
			}
		}
		items = append(items, scored{record: record, score: score})
	}
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].score != items[j].score {
			return items[i].score > items[j].score
		}
		return items[i].record.ID < items[j].record.ID
	})
	result := make([]ports.BusinessPolicyRecord, 0, len(items))
	for _, item := range items {
		result = append(result, item.record)
	}
	return result
}

func (b AutoReplyContextBuilder) now() time.Time {
	if b.Now == nil {
		return time.Now().UTC()
	}
	return b.Now().UTC()
}

func (b AutoReplyContextBuilder) maxCatalogs() int {
	if b.MaxCatalogs <= 0 {
		return 10
	}
	return b.MaxCatalogs
}

func (b AutoReplyContextBuilder) maxItems() int {
	if b.MaxItems <= 0 {
		return 5
	}
	return b.MaxItems
}

func (b AutoReplyContextBuilder) maxOffers() int {
	if b.MaxOffers <= 0 {
		return 5
	}
	return b.MaxOffers
}

func (b AutoReplyContextBuilder) maxVariants() int {
	if b.MaxVariants <= 0 {
		return 5
	}
	return b.MaxVariants
}

func (b AutoReplyContextBuilder) maxKnowledge() int {
	if b.MaxKnowledge <= 0 {
		return 10
	}
	return b.MaxKnowledge
}

func (b AutoReplyContextBuilder) maxPolicies() int {
	if b.MaxPolicies <= 0 {
		return 10
	}
	return b.MaxPolicies
}

func (b AutoReplyContextBuilder) maxMessages() int {
	if b.MaxMessages <= 0 {
		return 4 // Per ADR-038: 6 turns sliding window per IrisAgent research.
	}
	return b.MaxMessages
}

func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(*value)
}

func nonEmpty(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return strings.TrimSpace(value)
}

var _ ports.CustomerSalesContextBuilder = AutoReplyContextBuilder{}

// formatPrice trims trailing zeros from a numeric string.
// "200.0000" → "200", "200.5000" → "200.5", "200" → "200"
func formatPrice(amount string) string {
	if !strings.Contains(amount, ".") {
		return amount
	}
	amount = strings.TrimRight(amount, "0")
	amount = strings.TrimRight(amount, ".")
	if amount == "" {
		return "0"
	}
	return amount
}

// formatCurrency translates ISO 4217 codes to Arabic.
// "YER" → "ريال يمني", "SAR" → "ريال سعودي", etc.
func formatCurrency(code string) string {
	switch strings.ToUpper(strings.TrimSpace(code)) {
	case "YER":
		return "ريال يمني"
	case "SAR":
		return "ريال سعودي"
	case "USD":
		return "دولار"
	case "AED":
		return "درهم إماراتي"
	case "KWD":
		return "دينار كويتي"
	case "QAR":
		return "ريال قطري"
	case "BHD":
		return "دينار بحريني"
	case "OMR":
		return "ريال عماني"
	default:
		return code
	}
}
