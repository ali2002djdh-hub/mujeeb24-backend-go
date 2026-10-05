package services

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/Ammar777782439/mujeeb24-backend-go/internal/application/commands"
	"github.com/Ammar777782439/mujeeb24-backend-go/internal/application/ports"
)

// comparisonStubRepo serves one catalog with three items, each with one offer.
type comparisonStubRepo struct {
	catalog ports.CatalogRecord
	items   []ports.CatalogItemRecord
	offers  map[string][]ports.OfferRecord
}

func newComparisonStubRepo() comparisonStubRepo {
	catalog := ports.CatalogRecord{ID: "cat-1", BusinessID: "b1", Name: "Main", Status: "active"}
	mkItem := func(id, name string) ports.CatalogItemRecord {
		return ports.CatalogItemRecord{ID: id, BusinessID: "b1", CatalogID: "cat-1", ItemType: "service", Name: name, Status: "active", Attributes: []byte(`{}`)}
	}
	mkOffer := func(id, itemID, amount string) ports.OfferRecord {
		amountCopy := amount
		currency := "YER"
		return ports.OfferRecord{ID: id, BusinessID: "b1", CatalogItemID: itemID, Name: "offer-" + itemID, PricingMode: "fixed", Amount: &amountCopy, Currency: &currency, AvailabilityStatus: "available", Status: "active"}
	}
	items := []ports.CatalogItemRecord{mkItem("item-a", "Basic"), mkItem("item-b", "Pro"), mkItem("item-c", "Extra")}
	offers := map[string][]ports.OfferRecord{
		"item-a": {mkOffer("offer-a", "item-a", "100")},
		"item-b": {mkOffer("offer-b", "item-b", "200")},
		"item-c": {mkOffer("offer-c", "item-c", "300")},
	}
	return comparisonStubRepo{catalog: catalog, items: items, offers: offers}
}

func (r comparisonStubRepo) ListCatalogs(context.Context, string, string, int, string) (ports.CatalogPage, error) {
	return ports.CatalogPage{Items: []ports.CatalogRecord{r.catalog}}, nil
}
func (r comparisonStubRepo) GetCatalog(_ context.Context, businessID, catalogID string) (ports.CatalogRecord, error) {
	if catalogID == r.catalog.ID && businessID == r.catalog.BusinessID {
		return r.catalog, nil
	}
	return ports.CatalogRecord{}, notFoundError("catalog.get")
}
func (r comparisonStubRepo) CreateCatalog(context.Context, ports.CatalogDraft) (ports.CatalogRecord, error) {
	return ports.CatalogRecord{}, nil
}
func (r comparisonStubRepo) UpdateCatalog(context.Context, ports.CatalogPatch) (ports.CatalogRecord, error) {
	return ports.CatalogRecord{}, nil
}
func (r comparisonStubRepo) ListCatalogItems(_ context.Context, businessID, catalogID, _ string, _ string, _ int, _ string) (ports.CatalogItemPage, error) {
	if catalogID != r.catalog.ID {
		return ports.CatalogItemPage{}, notFoundError("catalog_item.list")
	}
	return ports.CatalogItemPage{Items: r.items}, nil
}
func (r comparisonStubRepo) GetCatalogItem(_ context.Context, businessID, catalogID, itemID string) (ports.CatalogItemRecord, error) {
	for _, item := range r.items {
		if item.ID == itemID && item.CatalogID == catalogID {
			return item, nil
		}
	}
	return ports.CatalogItemRecord{}, notFoundError("catalog_item.get")
}
func (r comparisonStubRepo) CreateCatalogItem(context.Context, ports.CatalogItemDraft) (ports.CatalogItemRecord, error) {
	return ports.CatalogItemRecord{}, nil
}
func (r comparisonStubRepo) UpdateCatalogItem(context.Context, ports.CatalogItemPatch) (ports.CatalogItemRecord, error) {
	return ports.CatalogItemRecord{}, nil
}
func (r comparisonStubRepo) ListOffers(_ context.Context, _ string, itemID, _ string, _ int, _ string) (ports.OfferPage, error) {
	return ports.OfferPage{Items: r.offers[itemID]}, nil
}
func (r comparisonStubRepo) CreateOffer(context.Context, ports.OfferDraft) (ports.OfferRecord, error) {
	return ports.OfferRecord{}, nil
}
func (r comparisonStubRepo) UpdateOffer(context.Context, ports.OfferPatch) (ports.OfferRecord, error) {
	return ports.OfferRecord{}, nil
}
func (r comparisonStubRepo) ListVariants(context.Context, string, string, string, int, string) (ports.VariantPage, error) {
	return ports.VariantPage{}, nil
}
func (r comparisonStubRepo) CreateVariant(context.Context, ports.VariantDraft) (ports.VariantRecord, error) {
	return ports.VariantRecord{}, nil
}
func (r comparisonStubRepo) UpdateVariant(context.Context, ports.VariantPatch) (ports.VariantRecord, error) {
	return ports.VariantRecord{}, nil
}
func (r comparisonStubRepo) ListAttributeSchemas(context.Context, string, string, *int, int, string) (ports.AttributeSchemaPage, error) {
	return ports.AttributeSchemaPage{}, nil
}
func (r comparisonStubRepo) GetAttributeSchema(context.Context, string, string) (ports.AttributeSchemaRecord, error) {
	return ports.AttributeSchemaRecord{}, nil
}
func (r comparisonStubRepo) NextAttributeSchemaVersion(context.Context, string, string) (int, error) {
	return 1, nil
}
func (r comparisonStubRepo) CreateAttributeSchemaVersion(context.Context, ports.AttributeSchemaDraft) (ports.AttributeSchemaRecord, error) {
	return ports.AttributeSchemaRecord{}, nil
}

type notFoundTestError struct{ op string }

func (e notFoundTestError) Error() string     { return "not found: " + e.op }
func (e notFoundTestError) ErrorKind() string { return "not_found" }

func notFoundError(op string) error { return notFoundTestError{op: op} }

func comparisonTestBuilder(repo comparisonStubRepo) AutoReplyContextBuilder {
	return NewAutoReplyContextBuilder(
		contextBusinessRepository{record: ports.BusinessRecord{ID: "b1"}},
		contextConversationRepository{record: ports.ConversationRecord{ID: "c1", BusinessID: "b1", CustomerID: "cust-1"}},
		contextCustomerRepository{record: ports.CustomerRecord{ID: "cust-1", BusinessID: "b1"}},
		repo,
		contextMessageRepository{},
	)
}

func offerRefs(evidence []ports.CustomerSalesOfferEvidence) map[string]string {
	out := map[string]string{}
	for _, offer := range evidence {
		out[offer.Reference] = offer.Amount
	}
	return out
}

// Item-based comparison entries must still carry offers (prices).
func TestRetrieveComparisonItemBranchIncludesOffers(t *testing.T) {
	builder := comparisonTestBuilder(newComparisonStubRepo())
	ctx, err := builder.Build(context.Background(), ports.CustomerSalesContextInput{
		BusinessID: "b1", ConversationID: "c1", Text: "compare",
		ConversationState: &ports.ConversationStateRecord{
			BusinessID: "b1", ConversationID: "c1",
			Comparison: &ports.ConversationComparison{Type: "offer_set", IDs: []string{"item-a", "item-b"}},
		},
	})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	got := offerRefs(ctx.OfferEvidence)
	if got["offer-a"] == "" || got["offer-b"] == "" {
		t.Fatalf("comparison offers missing prices: %#v", got)
	}
}

// Comparison mode must still admit new candidates so the AI can leave it.
func TestRetrieveComparisonDoesNotBackendMatchSiblingCandidates(t *testing.T) {
	builder := comparisonTestBuilder(newComparisonStubRepo())
	ctx, err := builder.Build(context.Background(), ports.CustomerSalesContextInput{
		BusinessID: "b1", ConversationID: "c1", Text: "what about extra?",
		ConversationState: &ports.ConversationStateRecord{
			BusinessID: "b1", ConversationID: "c1",
			Comparison: &ports.ConversationComparison{Type: "offer_set", IDs: []string{"item-a", "item-b"}},
		},
	})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	for _, item := range ctx.CatalogEvidence {
		if item.Reference == "item-c" {
			t.Fatalf("backend must not add sibling candidates from message matching: %#v", ctx.CatalogEvidence)
		}
	}
	if len(ctx.CatalogEvidence) != 2 {
		t.Fatalf("comparison context must stay on validated IDs only, got %d items: %#v", len(ctx.CatalogEvidence), ctx.CatalogEvidence)
	}
}

// Explicit NO_REFERENCE resets a stale comparison but preserves focus/history.
func TestBuildValidatedStateNoReferenceResetsComparison(t *testing.T) {
	focus := &ports.ConversationFocus{Type: "item", ID: "item-a"}
	current := &ports.ConversationStateRecord{
		BusinessID: "b1", ConversationID: "c1", Focus: focus,
		Previous:   []ports.ConversationFocus{{Type: "item", ID: "item-b"}},
		Comparison: &ports.ConversationComparison{Type: "offer_set", IDs: []string{"item-a", "item-b"}},
	}
	next := buildValidatedState(current, "b1", "c1", ports.AIDecisionProposal{
		StateProposal: &ports.CustomerSalesStateProposal{Kind: "NO_REFERENCE"},
	})
	if next == nil {
		t.Fatal("expected state update resetting comparison")
	}
	if next.Comparison != nil {
		t.Fatalf("comparison must be reset, got %#v", next.Comparison)
	}
	if next.Focus == nil || next.Focus.ID != "item-a" {
		t.Fatalf("focus must be preserved, got %#v", next.Focus)
	}
	if len(next.Previous) != 1 || next.Previous[0].ID != "item-b" {
		t.Fatalf("previous must be preserved, got %#v", next.Previous)
	}
}

func TestBuildValidatedStateAmbiguousKeepsState(t *testing.T) {
	current := &ports.ConversationStateRecord{
		BusinessID: "b1", ConversationID: "c1",
		Focus:      &ports.ConversationFocus{Type: "item", ID: "item-a"},
		Comparison: &ports.ConversationComparison{Type: "offer_set", IDs: []string{"item-a", "item-b"}},
	}
	next := buildValidatedState(current, "b1", "c1", ports.AIDecisionProposal{
		StateProposal: &ports.CustomerSalesStateProposal{Kind: "AMBIGUOUS"},
	})
	if next != nil {
		t.Fatalf("ambiguous must not mutate state, got %#v", next)
	}
}

func TestIsSubscriptionHandoffIntent(t *testing.T) {
	for _, text := range []string{"subscription_request", "ACTIVATION_Request", "طلب الاشتراك", "أريد تفعيل الباقة", "purchase_order"} {
		if !isSubscriptionHandoffIntent(text) {
			t.Fatalf("expected handoff intent for %q", text)
		}
	}
	for _, text := range []string{"", "information_request", "inquiry", "greeting"} {
		if isSubscriptionHandoffIntent(text) {
			t.Fatalf("unexpected handoff intent for %q", text)
		}
	}
}

// TestAutoReplyHandoffFarewellOnSubscription verifies per contract ⑥ §14 that
// when Gemini requests human handoff AND the conversation intent is a
// subscription/activation request, Mujeeb sends the fixed farewell message
// (HandoffFarewellMessage — never model text) and keeps RequiresHuman=true.
//
// Per contract ④ §4, Gemini returns action=human_request. Per contract ⑥ §14,
// the farewell is product routing decided by Mujeeb, not by Gemini prompt text.
func TestAutoReplyHandoffFarewellOnSubscription(t *testing.T) {
	outbound := &fakeOutboundRepository{}
	outbox := &fakeOutboxStore{}
	// Per contract ④ §4, Gemini returns action=human_request with response_text
	// containing "subscribe"/"activat"/"purchase" keywords (the system prompt
	// instructs Gemini to use those words when handing off a subscription intent).
	service := NewAutoReplyService(
		&FakeCustomerSalesDecisionPort{Proposal: ports.CustomerSalesProposal{Status: ports.CustomerSalesProposalStatusResolved, Action: ports.CustomerSalesProposalActionHumanRequest, ResponseText: "model suggests subscribe"}},
		&fakeDecisionRepository{},
		fakeReferenceRepository{record: ports.ConversationReferenceRecord{ID: "reference-1", BusinessID: "business-1", ConversationID: "conversation-1", System: "socialapi", ProviderRef: "socialapi", ResourceID: "provider-conversation-1", ConnectionID: stringPtr("connection-1"), IsCurrent: true, MappingStatus: "active"}},
		outbound,
		outbox,
		fakeTransactionManager{},
	)
	// Per Item 4: wire AIUsage + Subscriptions so recordAIUsage doesn't fail.
	service.AIUsage = &stubAIUsageRepo{}
	service.Subscriptions = &stubSubscriptionsRepo{items: []ports.SubscriptionRecord{{ID: "sub-1", BusinessID: "business-1", Status: "ACTIVE"}}}
	service.AIPricing = &stubPricingRepoAlwaysFail{}
	service.NewID = func() string { return "test-id" }
	service.Validation = allowAllValidationPipeline()
	result, err := service.Handle(context.Background(), commands.AutoReplyCommand{Meta: commands.CommandMeta{Actor: commands.ActorContext{BusinessID: "business-1"}}, ConversationID: "conversation-1", SourceMessageReference: "inbound-1", Text: "أريد الاشتراك", Channel: "whatsapp", ProviderRef: "socialapi"})
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	// Per contract ⑥ §14, the proposal.Action is human_request but the farewell
	// flow overrides the action to "answer" (so it gets enqueued) and overrides
	// ResponseText with the fixed HandoffFarewellMessage.
	if !result.Enqueued || outbox.calls != 1 || result.Action != string(ports.CustomerSalesProposalActionAnswer) {
		t.Fatalf("farewell must be enqueued, got %#v calls=%d", result, outbox.calls)
	}
	if !result.Decision.RequiresHuman {
		t.Fatal("handoff decision must keep RequiresHuman for the dashboard")
	}
	text, err := DecodeInlineTextContentReference(outbound.record.ContentReference)
	if err != nil || text != HandoffFarewellMessage {
		t.Fatalf("outbound must carry fixed farewell, got %q err=%v", text, err)
	}
	var reasons []string
	_ = json.Unmarshal(result.Decision.ReasonCodes, &reasons)
	found := false
	for _, reason := range reasons {
		if reason == "handoff_farewell_sent" {
			found = true
		}
	}
	if !found {
		t.Fatalf("reason codes must include handoff_farewell_sent: %v", reasons)
	}
	if !strings.Contains(HandoffFarewellMessage, "ممثلي المبيعات") {
		t.Fatal("farewell template drifted from approved wording")
	}
}

func TestAutoReplyHandoffFarewellBlockedWhenApprovalRequired(t *testing.T) {
	outbox := &fakeOutboxStore{}
	service := NewAutoReplyService(
		&FakeCustomerSalesDecisionPort{Proposal: ports.CustomerSalesProposal{Status: ports.CustomerSalesProposalStatusResolved, Action: ports.CustomerSalesProposalActionHumanRequest, ResponseText: "model text"}},
		&fakeDecisionRepository{},
		fakeReferenceRepository{record: ports.ConversationReferenceRecord{ID: "reference-1", BusinessID: "business-1", ConversationID: "conversation-1", System: "socialapi", ProviderRef: "socialapi", ResourceID: "provider-conversation-1", ConnectionID: stringPtr("connection-1"), IsCurrent: true, MappingStatus: "active"}},
		&fakeOutboundRepository{},
		outbox,
		fakeTransactionManager{},
	)
	service.Validation = allowAllValidationPipeline()
	result, err := service.Handle(context.Background(), commands.AutoReplyCommand{Meta: commands.CommandMeta{Actor: commands.ActorContext{BusinessID: "business-1"}}, ConversationID: "conversation-1", SourceMessageReference: "inbound-1", Text: "أريد الاشتراك", Channel: "whatsapp", ProviderRef: "socialapi"})
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if result.Enqueued || outbox.calls != 0 {
		t.Fatalf("approval-gated handoff must not send, got %#v calls=%d", result, outbox.calls)
	}
}
