//go:build integration

package postgres

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Ammar777782439/mujeeb24-backend-go/internal/application/commands"
	"github.com/Ammar777782439/mujeeb24-backend-go/internal/application/ports"
	"github.com/Ammar777782439/mujeeb24-backend-go/internal/application/queries"
	"github.com/Ammar777782439/mujeeb24-backend-go/internal/application/services"
	"github.com/Ammar777782439/mujeeb24-backend-go/internal/platform/database"
)

type legacyAutoReplyDecisionStub struct{}

func (legacyAutoReplyDecisionStub) Decide(context.Context, ports.CustomerSalesDecisionInput) (ports.CustomerSalesDecisionOutput, error) {
	return ports.CustomerSalesDecisionOutput{
		Proposal: ports.CustomerSalesProposal{
			Status:       ports.CustomerSalesProposalStatusResolved,
			Action:       ports.CustomerSalesProposalActionAnswer,
			ResponseText: "ok",
		},
	}, nil
}

var _ ports.CustomerSalesDecisionPort = legacyAutoReplyDecisionStub{}

func TestAIAuditRepositoriesAgainstPostgres(t *testing.T) {
	dsn := os.Getenv("POSTGRES_TEST_DSN")
	if dsn == "" {
		t.Skip("POSTGRES_TEST_DSN is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if _, err := database.RunMigrations(ctx, dsn, time.Now().UTC()); err != nil {
		t.Fatalf("run migrations: %v", err)
	}
	adapter, err := Open(ctx, dsn, DefaultPoolConfig())
	if err != nil {
		t.Fatalf("open adapter: %v", err)
	}
	defer adapter.Close()
	decisionRepo := NewAIDecisionRepository(adapter)
	auditRepo := NewAuditEventRepository(adapter)

	businessA := uuid.NewString()
	businessB := uuid.NewString()
	customerA := uuid.NewString()
	conversationA := uuid.NewString()
	decisionA1 := uuid.NewString()
	decisionA2 := uuid.NewString()
	decisionB := uuid.NewString()
	base := time.Date(2026, 8, 25, 8, 0, 0, 0, time.UTC)

	var assertionErr error
	fail := func(format string, args ...any) {
		if assertionErr == nil {
			assertionErr = fmt.Errorf(format, args...)
		}
	}
	withinErr := adapter.Within(ctx, func(txCtx context.Context) error {
		executor, executorErr := adapter.Executor(txCtx)
		if executorErr != nil {
			return executorErr
		}
		if _, err := executor.Exec(txCtx, `INSERT INTO businesses (id, name, slug, status, vertical_type, timezone, default_currency, locale, created_at, updated_at) VALUES ($1::uuid, 'AI Audit A', $1, 'active', 'retail', 'Asia/Aden', 'YER', 'ar-YE', $3, $3), ($2::uuid, 'AI Audit B', $2, 'active', 'travel', 'Asia/Aden', 'YER', 'ar-YE', $3, $3)`, businessA, businessB, base); err != nil {
			return err
		}
		if _, err := executor.Exec(txCtx, `INSERT INTO customers (id, business_id, profile, contact_points, status, created_at, updated_at) VALUES ($1::uuid, $2::uuid, '{}'::jsonb, '[]'::jsonb, 'active', $3, $3)`, customerA, businessA, base); err != nil {
			return err
		}
		if _, err := executor.Exec(txCtx, `INSERT INTO conversations (id, business_id, customer_id, state, ownership, priority, last_activity_at, created_at, updated_at) VALUES ($1::uuid, $2::uuid, $3::uuid, 'open', 'none', 'normal', $4, $4, $4)`, conversationA, businessA, customerA, base); err != nil {
			return err
		}
		insertDecision := func(id, businessID string, conversationID any, createdAt time.Time, lifecycle string, requiresHuman bool) error {
			_, insertErr := executor.Exec(txCtx, `INSERT INTO ai_decisions (id, business_id, conversation_id, source_message_reference, intent_base, domain_context, entities, evidence_references, requested_action, confidence_value, confidence_band, requires_human, missing_information, reason_codes, policy_reference, policy_version, knowledge_version, model_reference, schema_version, lifecycle, policy_decision, outcome, expires_at, created_at, updated_at) VALUES ($1::uuid, $2::uuid, $3::uuid, 'message-ref', 'product_question', 'verified catalog context', '{"item":"phone"}'::jsonb, '["catalog:item-1"]'::jsonb, 'answer', 0.9200, 'high', $4, '["color"]'::jsonb, '["needs_color"]'::jsonb, 'policy:v1', 'policy-v1', 'knowledge-v1', 'model:test', 1, $5, 'requires_approval', NULL, $6, $7, $7)`, id, businessID, conversationID, requiresHuman, lifecycle, createdAt.Add(24*time.Hour), createdAt)
			return insertErr
		}
		if err := insertDecision(decisionA1, businessA, conversationA, base.Add(2*time.Hour), "proposed", false); err != nil {
			return err
		}
		if err := insertDecision(decisionA2, businessA, nil, base.Add(1*time.Hour), "validated", true); err != nil {
			return err
		}
		if err := insertDecision(decisionB, businessB, nil, base.Add(3*time.Hour), "proposed", false); err != nil {
			return err
		}

		firstAuditID := uuid.NewString()
		secondAuditID := uuid.NewString()
		if _, err := auditRepo.Append(txCtx, ports.AuditEventDraft{ID: firstAuditID, BusinessID: businessA, ActorType: "human_agent", Action: "human.handoff_requested", ResourceType: "ai_decision", ResourceID: stringPointerForAIAudit(decisionA1), Metadata: []byte(`{"screen":"dashboard"}`), DecisionReference: stringPointerForAIAudit(decisionA1), Result: stringPointerForAIAudit("accepted"), ReasonCode: stringPointerForAIAudit("human_review_required"), OccurredAt: base.Add(4 * time.Hour), CreatedAt: base.Add(4 * time.Hour), SchemaVersion: 1, RedactionVersion: 1}); err != nil {
			return err
		}
		if _, err := auditRepo.Append(txCtx, ports.AuditEventDraft{ID: uuid.NewString(), BusinessID: businessA, ActorType: "human_agent", Action: "human.handoff_requested", ResourceType: "ai_decision", ResourceID: stringPointerForAIAudit(decisionA2), Metadata: []byte(`{"screen":"dashboard"}`), DecisionReference: stringPointerForAIAudit(decisionA2), Result: stringPointerForAIAudit("accepted"), ReasonCode: stringPointerForAIAudit("human_review_required"), OccurredAt: base.Add(4*time.Hour + 30*time.Minute), CreatedAt: base.Add(4*time.Hour + 30*time.Minute), SchemaVersion: 1, RedactionVersion: 1}); err != nil {
			return err
		}
		if _, err := auditRepo.Append(txCtx, ports.AuditEventDraft{ID: secondAuditID, BusinessID: businessA, ActorType: "system", Action: "ai.decision.created", ResourceType: "ai_decision", ResourceID: stringPointerForAIAudit(decisionA2), Metadata: []byte(`{"source":"pipeline"}`), OccurredAt: base.Add(5 * time.Hour), CreatedAt: base.Add(5 * time.Hour), SchemaVersion: 1, RedactionVersion: 1}); err != nil {
			return err
		}
		if _, err := auditRepo.Append(txCtx, ports.AuditEventDraft{ID: uuid.NewString(), BusinessID: businessB, ActorType: "ai", Action: "ai.decision.created", ResourceType: "ai_decision", Metadata: []byte(`{}`), OccurredAt: base.Add(6 * time.Hour), CreatedAt: base.Add(6 * time.Hour), SchemaVersion: 1, RedactionVersion: 1}); err != nil {
			return err
		}

		page, err := decisionRepo.List(txCtx, ports.AIDecisionFilter{BusinessID: businessA, Limit: 1})
		if err != nil || len(page.Items) != 1 || !page.HasMore || page.NextCursor == "" {
			fail("AI keyset page: %#v err=%v", page, err)
		}
		if _, err := decisionRepo.List(txCtx, ports.AIDecisionFilter{BusinessID: businessA, Limit: 1, Cursor: "malformed"}); !IsRepositoryKind(err, RepositoryInvalid) {
			fail("AI malformed cursor: %v", err)
		}
		human := true
		humanPage, err := decisionRepo.List(txCtx, ports.AIDecisionFilter{BusinessID: businessA, RequiresHuman: &human, Limit: 10})
		if err != nil || len(humanPage.Items) != 1 || humanPage.Items[0].ID != decisionA2 {
			fail("AI requires_human filter: %#v err=%v", humanPage, err)
		}
		conversationPage, err := decisionRepo.List(txCtx, ports.AIDecisionFilter{BusinessID: businessA, ConversationID: conversationA, Limit: 10})
		if err != nil || len(conversationPage.Items) != 1 || conversationPage.Items[0].ConversationID == nil {
			fail("AI conversation filter: %#v err=%v", conversationPage, err)
		}
		if _, err := decisionRepo.Get(txCtx, businessB, decisionA1); !IsRepositoryKind(err, RepositoryNotFound) {
			fail("AI tenant isolation: %v", err)
		}
		if _, err := decisionRepo.Get(txCtx, businessA, uuid.NewString()); !IsRepositoryKind(err, RepositoryNotFound) {
			fail("AI not_found: %v", err)
		}

		versionOne := int64(1)
		updated, err := decisionRepo.RequestHumanReview(txCtx, ports.HumanReviewPatch{BusinessID: businessA, DecisionID: decisionA1, Reason: "customer needs a specialist", RequestedBy: "agent-1", RequestedAt: base.Add(7 * time.Hour), ExpectedVersion: versionOne})
		if err != nil || updated.ResourceVersion != 2 || !updated.RequiresHuman || updated.Lifecycle != "proposed" {
			fail("AI request human transition: %#v err=%v", updated, err)
		}
		if _, err := decisionRepo.RequestHumanReview(txCtx, ports.HumanReviewPatch{BusinessID: businessA, DecisionID: decisionA1, Reason: "stale request", RequestedBy: "agent-2", RequestedAt: base.Add(8 * time.Hour), ExpectedVersion: versionOne}); !IsRepositoryKind(err, RepositoryStale) {
			fail("AI stale version: %v", err)
		}

		queryService := services.ListAIDecisionsQueryService{Repository: decisionRepo}
		mapped, err := queryService.Handle(txCtx, queries.ListAIDecisionsQuery{Meta: queries.QueryMeta{Actor: commands.ActorContext{BusinessID: commands.BusinessID(businessA)}}, Limit: 10, RequiresHuman: &human})
		if err != nil || len(mapped.Items) != 2 {
			fail("AI application mapping: %#v err=%v", mapped, err)
		}
		auditPage, err := auditRepo.List(txCtx, ports.AuditEventFilter{BusinessID: businessA, ActorType: "human_agent", Limit: 1})
		if err != nil || len(auditPage.Items) != 1 || !auditPage.HasMore || auditPage.NextCursor == "" {
			fail("audit keyset/filter: %#v err=%v", auditPage, err)
		}
		if _, err := auditRepo.List(txCtx, ports.AuditEventFilter{BusinessID: businessA, Limit: 1, Cursor: "malformed"}); !IsRepositoryKind(err, RepositoryInvalid) {
			fail("audit malformed cursor: %v", err)
		}
		from := base.Add(4 * time.Hour)
		until := base.Add(5 * time.Hour)
		rangePage, err := auditRepo.List(txCtx, ports.AuditEventFilter{BusinessID: businessA, From: &from, Until: &until, Limit: 10})
		if err != nil || len(rangePage.Items) != 3 {
			fail("audit time range: %#v err=%v", rangePage, err)
		}
		if _, err := auditRepo.Get(txCtx, businessB, firstAuditID); !IsRepositoryKind(err, RepositoryNotFound) {
			fail("audit tenant isolation: %v", err)
		}
		if _, err := auditRepo.Get(txCtx, businessA, uuid.NewString()); !IsRepositoryKind(err, RepositoryNotFound) {
			fail("audit not_found: %v", err)
		}
		auditQueryService := services.ListAuditEventsQueryService{Repository: auditRepo}
		mappedAudit, err := auditQueryService.Handle(txCtx, queries.ListAuditEventsQuery{Meta: queries.QueryMeta{Actor: commands.ActorContext{BusinessID: commands.BusinessID(businessA)}}, Limit: 10, From: from.Format(time.RFC3339), Until: until.Format(time.RFC3339)})
		if err != nil || len(mappedAudit.Items) != 3 {
			fail("audit application mapping: %#v err=%v", mappedAudit, err)
		}

		requestService := services.NewRequestHumanReviewCommandService(decisionRepo, auditRepo, passthroughTransactions{})
		expected := commands.ResourceVersion("1")
		requestResult, err := requestService.Handle(txCtx, commands.RequestHumanReviewCommand{Meta: commands.CommandMeta{Actor: commands.ActorContext{BusinessID: commands.BusinessID(businessA), PrincipalID: commands.PrincipalID("agent-3")}, ExpectedVersion: &expected, CorrelationID: uuid.NewString()}, DecisionID: commands.AIDecisionID(decisionA2), Reason: "specialist review required"})
		if err != nil || requestResult.ResourceVersion != "2" || requestResult.Status != "human_review_requested" || !requestResult.Accepted {
			fail("request human application service: %#v err=%v", requestResult, err)
		}

		badMetadataID := uuid.NewString()
		if _, err := executor.Exec(txCtx, `SAVEPOINT audit_bad_metadata`); err != nil {
			return err
		}
		_, badMetadataErr := auditRepo.Append(txCtx, ports.AuditEventDraft{ID: badMetadataID, BusinessID: businessA, ActorType: "system", Action: "ai.decision.created", ResourceType: "ai_decision", Metadata: []byte(`[]`), OccurredAt: base.Add(9 * time.Hour), CreatedAt: base.Add(9 * time.Hour), SchemaVersion: 1, RedactionVersion: 1})
		if badMetadataErr == nil {
			fail("audit metadata array was accepted")
		}
		if _, err := executor.Exec(txCtx, `ROLLBACK TO SAVEPOINT audit_bad_metadata`); err != nil {
			return err
		}

		for name, statement := range map[string]string{"update": `UPDATE audit_events SET action = 'tampered' WHERE id = $1::uuid`, "delete": `DELETE FROM audit_events WHERE id = $1::uuid`} {
			if _, err := executor.Exec(txCtx, `SAVEPOINT audit_append_only_`+name); err != nil {
				return err
			}
			_, mutationErr := executor.Exec(txCtx, statement, firstAuditID)
			if mutationErr == nil {
				fail("audit %s mutation was accepted", name)
			}
			if _, err := executor.Exec(txCtx, `ROLLBACK TO SAVEPOINT audit_append_only_`+name); err != nil {
				return err
			}
		}
		return assertionErr
	})
	if withinErr != nil && !errors.Is(withinErr, assertionErr) {
		t.Fatalf("AI/Audit integration transaction: %v", withinErr)
	}
	if assertionErr != nil {
		t.Fatal(assertionErr)
	}

	rollbackBusiness := uuid.NewString()
	rollbackAudit := uuid.NewString()
	rollbackErr := errors.New("AI/Audit rollback")
	if err := adapter.Within(ctx, func(txCtx context.Context) error {
		executor, err := adapter.Executor(txCtx)
		if err != nil {
			return err
		}
		if _, err := executor.Exec(txCtx, `INSERT INTO businesses (id, name, slug, status, vertical_type, timezone, default_currency, locale, created_at, updated_at) VALUES ($1::uuid, 'Rollback AI Audit', $1, 'active', 'retail', 'Asia/Aden', 'YER', 'ar-YE', now(), now())`, rollbackBusiness); err != nil {
			return err
		}
		if _, err := auditRepo.Append(txCtx, ports.AuditEventDraft{ID: rollbackAudit, BusinessID: rollbackBusiness, ActorType: "system", Action: "ai.decision.created", ResourceType: "ai_decision", Metadata: []byte(`{}`), OccurredAt: time.Now().UTC(), CreatedAt: time.Now().UTC(), SchemaVersion: 1, RedactionVersion: 1}); err != nil {
			return err
		}
		return rollbackErr
	}); !errors.Is(err, rollbackErr) {
		t.Fatalf("expected AI/Audit rollback: %v", err)
	}
	var remaining int
	if err := adapter.Pool().QueryRow(ctx, `SELECT count(*) FROM audit_events WHERE id = $1::uuid`, rollbackAudit).Scan(&remaining); err != nil {
		t.Fatalf("rollback audit query: %v", err)
	}
	if remaining != 0 {
		t.Fatalf("audit rollback leaked %d rows", remaining)
	}
}

type passthroughTransactions struct{}

func (passthroughTransactions) Within(ctx context.Context, fn func(context.Context) error) error {
	return fn(ctx)
}

func stringPointerForAIAudit(value string) *string { return &value }

var _ ports.TransactionManager = passthroughTransactions{}

func TestAutoReplyVerticalSliceAgainstPostgres(t *testing.T) {
	dsn := os.Getenv("POSTGRES_TEST_DSN")
	if dsn == "" {
		t.Skip("POSTGRES_TEST_DSN is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	if _, err := database.RunMigrations(ctx, dsn, time.Now().UTC()); err != nil {
		t.Fatalf("run migrations: %v", err)
	}
	adapter, err := Open(ctx, dsn, DefaultPoolConfig())
	if err != nil {
		t.Fatalf("open adapter: %v", err)
	}
	defer adapter.Close()

	businessID := uuid.NewString()
	customerID := uuid.NewString()
	conversationID := uuid.NewString()
	connectionID := uuid.NewString()
	referenceID := uuid.NewString()
	base := time.Date(2026, 8, 25, 13, 0, 0, 0, time.UTC)
	cleanup := func() {
		cleanupCtx := context.Background()
		_, _ = adapter.Pool().Exec(cleanupCtx, `DELETE FROM outbox_entries WHERE business_id = $1::uuid`, businessID)
		_, _ = adapter.Pool().Exec(cleanupCtx, `DELETE FROM outbound_messages WHERE business_id = $1::uuid`, businessID)
		_, _ = adapter.Pool().Exec(cleanupCtx, `DELETE FROM ai_decisions WHERE business_id = $1::uuid`, businessID)
		_, _ = adapter.Pool().Exec(cleanupCtx, `DELETE FROM conversation_references WHERE business_id = $1::uuid`, businessID)
		_, _ = adapter.Pool().Exec(cleanupCtx, `DELETE FROM channel_connections WHERE business_id = $1::uuid`, businessID)
		_, _ = adapter.Pool().Exec(cleanupCtx, `DELETE FROM conversations WHERE business_id = $1::uuid`, businessID)
		_, _ = adapter.Pool().Exec(cleanupCtx, `DELETE FROM customers WHERE business_id = $1::uuid`, businessID)
		_, _ = adapter.Pool().Exec(cleanupCtx, `DELETE FROM business_policies WHERE business_id = $1::uuid`, businessID)
		_, _ = adapter.Pool().Exec(cleanupCtx, `DELETE FROM businesses WHERE id = $1::uuid`, businessID)
	}
	defer cleanup()

	if _, err := adapter.Pool().Exec(ctx, `INSERT INTO businesses (id, name, slug, status, vertical_type, timezone, default_currency, locale, created_at, updated_at) VALUES ($1::uuid, 'Auto Reply Business', $1, 'active', 'retail', 'Asia/Aden', 'YER', 'ar-YE', $2, $2)`, businessID, base); err != nil {
		t.Fatalf("insert business: %v", err)
	}
	if _, err := adapter.Pool().Exec(ctx, `
		INSERT INTO business_policies (
			business_id, ai_mode, default_human_review, allow_auto_reply,
			allow_auto_lead_creation, allow_auto_transaction_draft,
			allow_auto_confirmation, created_at, updated_at
		)
		VALUES ($1::uuid, 'restricted_auto', false, true, true, true, true, $2, $2)
	`, businessID, base); err != nil {
		t.Fatalf("insert business policy: %v", err)
	}
	if _, err := adapter.Pool().Exec(ctx, `INSERT INTO customers (id, business_id, profile, contact_points, status, created_at, updated_at) VALUES ($1::uuid, $2::uuid, '{}'::jsonb, '[]'::jsonb, 'active', $3, $3)`, customerID, businessID, base); err != nil {
		t.Fatalf("insert customer: %v", err)
	}
	if _, err := adapter.Pool().Exec(ctx, `INSERT INTO conversations (id, business_id, customer_id, state, ownership, priority, last_activity_at, created_at, updated_at) VALUES ($1::uuid, $2::uuid, $3::uuid, 'open', 'none', 'normal', $4, $4, $4)`, conversationID, businessID, customerID, base); err != nil {
		t.Fatalf("insert conversation: %v", err)
	}
	if _, err := adapter.Pool().Exec(ctx, `INSERT INTO channel_connections (id, business_id, provider_ref, channel, provider_account_ref, provider_connection_ref, status, secret_reference, created_at, updated_at) VALUES ($1::uuid, $2::uuid, 'socialapi', 'facebook', 'account-' || $1::text, 'connection-' || $1::text, 'active', 'local-secret-ref', $3, $3)`, connectionID, businessID, base); err != nil {
		t.Fatalf("insert channel connection: %v", err)
	}
	if _, err := adapter.Pool().Exec(ctx, `INSERT INTO conversation_references (id, business_id, conversation_id, system, provider_ref, resource_type, resource_id, connection_id, conversation_kind, is_current, mapping_status, created_at, updated_at) VALUES ($1::uuid, $2::uuid, $3::uuid, 'provider', 'socialapi', 'conversation', 'provider-conversation-1', $4::uuid, 'dm', true, 'active', $5, $5)`, referenceID, businessID, conversationID, connectionID, base); err != nil {
		t.Fatalf("insert conversation reference: %v", err)
	}

	decisionRepo := NewAIDecisionRepository(adapter)
	referenceRepo := NewConversationReferenceRepository(adapter)
	outboundRepo := NewOutboundMessageRepository(adapter)
	outboxRepo := NewPostgresOutboxStore(adapter)
	service := services.NewAutoReplyService(legacyAutoReplyDecisionStub{}, decisionRepo, referenceRepo, outboundRepo, outboxRepo, adapter)
	service.Validation = services.NewValidationPipeline(
		NewPostgresReferenceValidator(adapter),
		NewPostgresTenantValidator(adapter),
		NewPostgresCustomerSalesPolicyEvaluator(NewBusinessRepository(adapter)),
		nil,
	)
	service.CustomerSalesContextBuilder = services.NewAutoReplyContextBuilder(
		NewBusinessRepository(adapter),
		NewConversationRepository(adapter),
		NewCustomerRepository(adapter),
		NewCatalogRepository(adapter),
		NewMessageRepository(adapter),
	)
	service.AIUsage = noopAIUsageRepository{}
	service.Subscriptions = activeSubscriptionStub{}
	result, err := service.Handle(ctx, commands.AutoReplyCommand{Meta: commands.CommandMeta{Actor: commands.ActorContext{BusinessID: commands.BusinessID(businessID)}}, ConversationID: commands.ConversationID(conversationID), SourceMessageReference: "inbound-success", Text: "مرحبا", Channel: "facebook", ProviderRef: "socialapi"})
	if err != nil {
		t.Fatalf("auto reply success: %v", err)
	}
	if !result.Enqueued || result.Action != "answer" || result.Decision.ID == "" || result.OutboundMessageID == "" || result.OutboxEntryID == "" {
		t.Fatalf("unexpected auto reply result: %#v", result)
	}
	var decisionCount, outboundCount, outboxCount int
	if err := adapter.Pool().QueryRow(ctx, `SELECT count(*) FROM ai_decisions WHERE business_id = $1::uuid AND source_message_reference = 'inbound-success' AND requested_action = 'answer' AND lifecycle = 'proposed'`, businessID).Scan(&decisionCount); err != nil {
		t.Fatal(err)
	}
	if err := adapter.Pool().QueryRow(ctx, `SELECT count(*) FROM outbound_messages WHERE business_id = $1::uuid AND id = $2::uuid AND status = 'pending' AND origin = 'ai' AND transport = 'provider'`, businessID, string(result.OutboundMessageID)).Scan(&outboundCount); err != nil {
		t.Fatal(err)
	}
	if err := adapter.Pool().QueryRow(ctx, `SELECT count(*) FROM outbox_entries WHERE business_id = $1::uuid AND id = $2::uuid AND command_type = $3 AND status = 'pending'`, businessID, string(result.OutboxEntryID), services.OutboundSendCommandType).Scan(&outboxCount); err != nil {
		t.Fatal(err)
	}
	if decisionCount != 1 || outboundCount != 1 || outboxCount != 1 {
		t.Fatalf("auto reply persistence counts decision=%d outbound=%d outbox=%d", decisionCount, outboundCount, outboxCount)
	}

	failingService := services.NewAutoReplyService(legacyAutoReplyDecisionStub{}, decisionRepo, referenceRepo, outboundRepo, failingEnqueueOutbox{OutboxStore: outboxRepo}, adapter)
	failingService.Validation = services.NewValidationPipeline(
		NewPostgresReferenceValidator(adapter),
		NewPostgresTenantValidator(adapter),
		NewPostgresCustomerSalesPolicyEvaluator(NewBusinessRepository(adapter)),
		nil,
	)
	failingService.CustomerSalesContextBuilder = services.NewAutoReplyContextBuilder(
		NewBusinessRepository(adapter),
		NewConversationRepository(adapter),
		NewCustomerRepository(adapter),
		NewCatalogRepository(adapter),
		NewMessageRepository(adapter),
	)
	failingService.AIUsage = noopAIUsageRepository{}
	failingService.Subscriptions = activeSubscriptionStub{}
	if _, err := failingService.Handle(ctx, commands.AutoReplyCommand{Meta: commands.CommandMeta{Actor: commands.ActorContext{BusinessID: commands.BusinessID(businessID)}}, ConversationID: commands.ConversationID(conversationID), SourceMessageReference: "inbound-rollback", Text: "رسالة ثانية", Channel: "facebook", ProviderRef: "socialapi"}); err == nil {
		t.Fatal("expected forced outbox failure")
	}
	var rollbackDecisions, rollbackOutbound int
	if err := adapter.Pool().QueryRow(ctx, `SELECT count(*) FROM ai_decisions WHERE business_id = $1::uuid AND source_message_reference = 'inbound-rollback'`, businessID).Scan(&rollbackDecisions); err != nil {
		t.Fatal(err)
	}
	if err := adapter.Pool().QueryRow(ctx, `SELECT count(*) FROM outbound_messages WHERE business_id = $1::uuid AND provider_idempotency_key = 'auto-reply:inbound-rollback'`, businessID).Scan(&rollbackOutbound); err != nil {
		t.Fatal(err)
	}
	if rollbackDecisions != 0 || rollbackOutbound != 0 {
		t.Fatalf("transaction leaked after outbox failure decisions=%d outbound=%d", rollbackDecisions, rollbackOutbound)
	}
}

type noopAIUsageRepository struct{}

func (noopAIUsageRepository) AppendRecord(context.Context, ports.AIUsageAppend) (ports.AIUsageRecord, error) {
	return ports.AIUsageRecord{}, nil
}

func (noopAIUsageRepository) GetSubscriptionAIUsage(context.Context, string) (ports.SubscriptionAIUsageAggregate, error) {
	return ports.SubscriptionAIUsageAggregate{}, nil
}

func (noopAIUsageRepository) RefreshAggregate(context.Context, string, time.Time) (ports.SubscriptionAIUsageAggregate, error) {
	return ports.SubscriptionAIUsageAggregate{}, nil
}

func (noopAIUsageRepository) GetPlatformAIUsageOverview(context.Context) (ports.SubscriptionAIUsageAggregate, error) {
	return ports.SubscriptionAIUsageAggregate{}, nil
}

func (noopAIUsageRepository) GetAIUsageByBusiness(context.Context, int) ([]ports.SubscriptionAIUsageAggregate, error) {
	return nil, nil
}

var _ ports.AIUsageRepository = noopAIUsageRepository{}

type activeSubscriptionStub struct{}

func (activeSubscriptionStub) Create(context.Context, ports.SubscriptionCreate) (ports.SubscriptionRecord, error) {
	return ports.SubscriptionRecord{}, errors.New("not used")
}

func (activeSubscriptionStub) GetByID(context.Context, string) (ports.SubscriptionRecord, error) {
	return ports.SubscriptionRecord{}, errors.New("not used")
}

func (activeSubscriptionStub) List(context.Context, ports.SubscriptionListFilter) (ports.SubscriptionPage, error) {
	return ports.SubscriptionPage{
		Items: []ports.SubscriptionRecord{{ID: "subscription-test", Status: "ACTIVE"}},
	}, nil
}

func (activeSubscriptionStub) Activate(context.Context, string, time.Time) (ports.SubscriptionRecord, error) {
	return ports.SubscriptionRecord{}, errors.New("not used")
}

func (activeSubscriptionStub) Cancel(context.Context, string, string, string, time.Time) (ports.SubscriptionRecord, error) {
	return ports.SubscriptionRecord{}, errors.New("not used")
}

func (activeSubscriptionStub) MarkExpired(context.Context, string, time.Time) (ports.SubscriptionRecord, error) {
	return ports.SubscriptionRecord{}, errors.New("not used")
}

func (activeSubscriptionStub) ApplyCostBudgetOverride(context.Context, string, int, string, string, time.Time) (ports.SubscriptionRecord, error) {
	return ports.SubscriptionRecord{}, errors.New("not used")
}

func (activeSubscriptionStub) CheckEntitlements(context.Context, string, int, int) error {
	return nil
}

var _ ports.SubscriptionRepository = activeSubscriptionStub{}

type failingEnqueueOutbox struct{ ports.OutboxStore }

func (failingEnqueueOutbox) Enqueue(context.Context, ports.OutboxEntryDraft) (ports.OutboxEntryRecord, error) {
	return ports.OutboxEntryRecord{}, errors.New("forced outbox enqueue failure")
}
