// cmd/test-autoreply/main.go — Real integration test for AutoReplyService.
//
// Connects to real PostgreSQL + real Gemini API + runs the full
// AutoReplyService.Handle flow. No mocks, no stubs.
//
// Usage:
//
//	DATABASE_URL=postgres://mujeeb:pass@localhost:5433/mujeeb24?sslmode=disable \
//	GEMINI_API_KEY=your-key \
//	GEMINI_MODEL=gemini-3.8-flash \
//	go run cmd/test-autoreply/main.go
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/google/uuid"

	"github.com/Ammar777782439/mujeeb24-backend-go/internal/adapters/secondary/ai/gemini"
	"github.com/Ammar777782439/mujeeb24-backend-go/internal/adapters/secondary/persistence/postgres"
	"github.com/Ammar777782439/mujeeb24-backend-go/internal/application/commands"
	"github.com/Ammar777782439/mujeeb24-backend-go/internal/application/ports"
	"github.com/Ammar777782439/mujeeb24-backend-go/internal/application/services"
)

func main() {
	businessID := envOr("TEST_BUSINESS_ID", "00000000-0000-0000-0000-000000000002")
	apiKey := envOr("GEMINI_API_KEY", "")
	model := envOr("GEMINI_MODEL", "gemini-3.8-flash")
	dbURL := envOr("DATABASE_URL", "")

	if apiKey == "" {
		fmt.Println("❌ GEMINI_API_KEY is required")
		os.Exit(1)
	}
	if dbURL == "" {
		fmt.Println("❌ DATABASE_URL is required")
		fmt.Println("   For Docker Compose: postgres://mujeeb:PASSWORD@localhost:5433/mujeeb24?sslmode=disable")
		os.Exit(1)
	}

	fmt.Println("╔══════════════════════════════════════════════════════╗")
	fmt.Println("║  Mujeeb 24 — Real AutoReply Integration Test       ║")
	fmt.Println("╚══════════════════════════════════════════════════════╝")
	fmt.Printf("  Business:  %s\n", businessID)
	fmt.Printf("  Model:     %s\n", model)
	fmt.Printf("  DB URL:    %s\n", maskURL(dbURL))

	ctx := context.Background()

	// Step 1: Connect to PostgreSQL
	fmt.Println("\n■ Step 1: Connect to PostgreSQL")
	adapter, err := postgres.Open(ctx, dbURL, postgres.DefaultPoolConfig())
	if err != nil {
		fmt.Printf("  ❌ FAILED: %v\n", err)
		os.Exit(1)
	}
	defer adapter.Close()
	fmt.Println("  ✅ Connected")

	// Step 2: Verify business exists
	fmt.Println("\n■ Step 2: Verify business")
	businessRepo := postgres.NewBusinessRepository(adapter)
	business, err := businessRepo.GetByID(ctx, businessID)
	if err != nil {
		fmt.Printf("  ❌ Business not found: %v\n", err)
		fmt.Printf("  → Run: INSERT INTO businesses (id, name, slug, status, vertical_type, timezone, default_currency, locale, resource_version, created_at, updated_at) VALUES ('%s', 'Test Business', 'test', 'active', 'electronics', 'Asia/Aden', 'YER', 'ar', 1, NOW(), NOW());\n", businessID)
		os.Exit(1)
	}
	fmt.Printf("  ✅ Business: %s\n", business.Name)

	// Step 3: Check / fix AI policies
	fmt.Println("\n■ Step 3: Check AI policies")
	policy, err := businessRepo.GetRuntimePolicy(ctx, businessID)
	if err != nil {
		fmt.Printf("  ⚠️  No policy — creating default...")
		_, err = businessRepo.UpdateRuntimePolicy(ctx, ports.BusinessRuntimePolicyUpdate{
			BusinessID:                businessID,
			AIMode:                    strPtr("restricted_auto"),
			AllowAutoReply:            boolPtr(true),
			DefaultHumanReview:        boolPtr(false),
			AllowAutoLeadCreation:     boolPtr(true),
			AllowAutoTransactionDraft: boolPtr(true),
			AllowAutoConfirmation:     boolPtr(true),
		})
		if err != nil {
			fmt.Printf("  ❌ Create policy failed: %v\n", err)
			os.Exit(1)
		}
		policy, _ = businessRepo.GetRuntimePolicy(ctx, businessID)
	}
	fmt.Printf("  ai_mode:             %s\n", policy.AIMode)
	fmt.Printf("  allow_auto_reply:   %v\n", policy.AllowAutoReply)
	fmt.Printf("  default_human_review: %v\n", policy.DefaultHumanReview)
	if policy.AIMode == "disabled" || !policy.AllowAutoReply {
		fmt.Println("  ❌ AI is disabled or auto_reply not allowed")
		fmt.Println("  → Run: UPDATE business_policies SET ai_mode='restricted_auto', allow_auto_reply=true WHERE business_id='...';")
		os.Exit(1)
	}
	fmt.Println("  ✅ Policies OK")

	// Step 4: Setup test conversation
	fmt.Println("\n■ Step 4: Setup test conversation")
	pool := adapter.Pool()
	customerID := uuid.NewString()
	_, _ = pool.Exec(ctx,
		`INSERT INTO customers (id, business_id, status, profile, contact_points, resource_version, created_at, updated_at)
		 VALUES ($1, $2, 'active', '{}'::jsonb, '[]'::jsonb, 1, NOW(), NOW()) ON CONFLICT DO NOTHING`,
		customerID, businessID)

	conversationID := uuid.NewString()
	_, err = pool.Exec(ctx,
		`INSERT INTO conversations (id, business_id, customer_id, state, ownership, priority, resource_version, last_activity_at, created_at, updated_at)
		 VALUES ($1, $2, $3, 'open', 'ai', 'normal', 1, NOW(), NOW(), NOW())`,
		conversationID, businessID, customerID)
	if err != nil {
		fmt.Printf("  ❌ Conversation create: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("  ✅ Conversation: %s\n", conversationID)

	connID := uuid.NewString()
	_, _ = pool.Exec(ctx,
		`INSERT INTO channel_connections (id, business_id, provider_ref, channel, provider_account_ref, provider_connection_ref, status, resource_version, created_at, updated_at)
		 VALUES ($1, $2, 'socialapi', 'facebook', 'test-account', 'test-conn', 'active', 1, NOW(), NOW()) ON CONFLICT DO NOTHING`,
		connID, businessID)

	refID := uuid.NewString()
	_, err = pool.Exec(ctx,
		`INSERT INTO conversation_references (id, business_id, conversation_id, system, provider_ref, resource_id, connection_id, is_current, mapping_status, created_at, updated_at)
		 VALUES ($1, $2, $3, 'socialapi', 'socialapi', $4, $5, true, 'active', NOW(), NOW())`,
		refID, businessID, conversationID, "test-conv-"+conversationID[:8], connID)
	if err != nil {
		fmt.Printf("  ❌ Reference create: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("  ✅ Reference: %s\n", refID)

	// Step 5: Wire real components
	fmt.Println("\n■ Step 5: Wire real AutoReplyService")
	geminiClient, err := gemini.NewGeminiHTTPClient(gemini.GeminiHTTPClientConfig{
		BaseURL:        "https://generativelanguage.googleapis.com",
		APIKey:         apiKey,
		Model:          model,
		RequestTimeout: 45 * time.Second,
	})
	if err != nil {
		fmt.Printf("  ❌ Gemini client: %v\n", err)
		os.Exit(1)
	}
	catalogRepository := postgres.NewCatalogRepository(adapter)
	contractClient, err := gemini.NewGeminiCustomerSalesAdapter(geminiClient, nil)
	if err != nil {
		fmt.Printf("  ❌ GeminiCustomerSalesAdapter: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("  ✅ Gemini GeminiCustomerSalesAdapter ready")

	contextBuilder := services.NewAutoReplyContextBuilder(
		businessRepo,
		postgres.NewConversationRepository(adapter),
		postgres.NewCustomerRepository(adapter),
		postgres.NewCatalogRepository(adapter),
		postgres.NewMessageRepository(adapter),
	)
	contextBuilder.CatalogAI = postgres.NewCatalogAIReadRepository(adapter)
	contextBuilder.Knowledge = postgres.NewKnowledgeDocumentRepository(adapter)
	contextBuilder.Policies = postgres.NewBusinessPolicyRepository(adapter)

	service := services.NewAutoReplyService(
		contractClient,
		postgres.NewAIDecisionRepository(adapter),
		postgres.NewConversationReferenceRepository(adapter),
		postgres.NewOutboundMessageRepository(adapter),
		postgres.NewPostgresOutboxStore(adapter),
		adapter,
	)
	service.CustomerSalesContextBuilder = contextBuilder
	service.RunRepository = postgres.NewAIRunTraceRepository(adapter)
	service.Validation = services.NewValidationPipeline(
		postgres.NewPostgresReferenceValidator(adapter),
		postgres.NewPostgresTenantValidator(adapter),
		postgres.NewPostgresCustomerSalesPolicyEvaluator(businessRepo),
		nil,
	)
	service.Conversations = postgres.NewConversationRepository(adapter)
	service.MessageRepository = postgres.NewMessageRepository(adapter)
	service.StateRepository = postgres.NewConversationStateRepository(adapter)

	entityContractBytes, err := json.Marshal(services.BuildCatalogEntityContractPayload())
	if err != nil {
		fmt.Printf("  ❌ Entity contract: %v\n", err)
		os.Exit(1)
	}
	service.EntityContractPayload = entityContractBytes

	batchTokenCounter, err := gemini.NewTokenCounter(gemini.TokenCounterConfig{
		BaseURL: geminiClient.BaseURL(),
		APIKey:  geminiClient.APIKey(),
		Model:   geminiClient.Model(),
	})
	if err != nil {
		fmt.Printf("  ❌ Batch token counter: %v\n", err)
		os.Exit(1)
	}
	batchClient, err := gemini.NewBatchClient(gemini.BatchClientConfig{
		BaseURL: geminiClient.BaseURL(),
		APIKey:  geminiClient.APIKey(),
		Model:   geminiClient.Model(),
	})
	if err != nil {
		fmt.Printf("  ❌ Batch client: %v\n", err)
		os.Exit(1)
	}
	service.CatalogBatch = &services.CatalogBatchController{
		Catalogs:          catalogRepository,
		CatalogAI:         postgres.NewCatalogAIReadRepository(adapter),
		ProjectionBuilder: &services.CatalogAIProjectionBuilder{},
		TokenCounter:      batchTokenCounter,
		Gemini:            batchClient,
		RunRepo:           postgres.NewAIRunTraceRepository(adapter),
		TokenBudget:       8000,
		Now:               func() time.Time { return time.Now().UTC() },
		NewID:             uuid.NewString,
	}
	fmt.Println("  ✅ AutoReplyService fully wired")

	// Step 6: Call Handle
	fmt.Println("\n■ Step 6: Call AutoReplyService.Handle")
	msgRef := "test-" + uuid.NewString()
	result, err := service.Handle(ctx, commands.AutoReplyCommand{
		Meta: commands.CommandMeta{
			Actor: commands.ActorContext{BusinessID: commands.BusinessID(businessID)},
		},
		ConversationID:         commands.ConversationID(conversationID),
		SourceMessageReference: msgRef,
		Text:                   "مرحبا، كم سعر الآيفون 15؟",
		Channel:                "facebook",
		ProviderRef:            "socialapi",
	})
	if err != nil {
		fmt.Printf("  ❌ Handle FAILED: %v\n", err)
	} else {
		fmt.Println("  ✅ Handle succeeded!")
		fmt.Printf("  Action:            %s\n", result.Action)
		fmt.Printf("  Enqueued:          %v\n", result.Enqueued)
		fmt.Printf("  OutboundMessageID: %s\n", result.OutboundMessageID)
		fmt.Printf("  OutboxEntryID:     %s\n", result.OutboxEntryID)
		if result.Decision.ID != "" {
			fmt.Printf("  Decision ID:       %s\n", result.Decision.ID)
		}
	}

	// Step 7: Verify DB state
	fmt.Println("\n■ Step 7: Verify database state")
	var runCount, decCount, outCount, obCount int
	pool.QueryRow(ctx, "SELECT count(*) FROM ai_runs WHERE business_id=$1", businessID).Scan(&runCount)
	pool.QueryRow(ctx, "SELECT count(*) FROM ai_decisions WHERE business_id=$1", businessID).Scan(&decCount)
	pool.QueryRow(ctx, "SELECT count(*) FROM outbox_entries WHERE business_id=$1", businessID).Scan(&outCount)
	pool.QueryRow(ctx, "SELECT count(*) FROM outbound_messages WHERE business_id=$1", businessID).Scan(&obCount)
	fmt.Printf("  ai_runs:           %d\n", runCount)
	fmt.Printf("  ai_decisions:      %d\n", decCount)
	fmt.Printf("  outbox_entries:    %d\n", outCount)
	fmt.Printf("  outbound_messages: %d\n", obCount)

	if runCount > 0 {
		var status, stage, reason string
		pool.QueryRow(ctx, "SELECT status, COALESCE(failure_stage,''), COALESCE(failure_reason,'') FROM ai_runs WHERE business_id=$1 ORDER BY started_at DESC LIMIT 1", businessID).Scan(&status, &stage, &reason)
		fmt.Printf("  last run:          %s\n", status)
		if stage != "" {
			fmt.Printf("  failure_stage:     %s\n", stage)
			fmt.Printf("  failure_reason:    %s\n", reason)
		}
	}

	fmt.Println("\n╔══════════════════════════════════════════════════════╗")
	if err != nil {
		fmt.Printf("║  ❌ TEST FAILED: %s\n", err.Error())
	} else if result.Enqueued {
		fmt.Println("║  ✅ FULL SUCCESS — message enqueued for sending!    ║")
		fmt.Println("║  → Run the worker to actually send it.             ║")
	} else if runCount > 0 && outCount == 0 {
		fmt.Println("║  ⚠️  AI ran but no outbound message created.       ║")
		fmt.Println("║  → Check ai_runs.failure_stage above.            ║")
	} else {
		fmt.Println("║  ⚠️  Unexpected state — check DB numbers above.    ║")
	}
	fmt.Println("╚══════════════════════════════════════════════════════╝")
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func maskURL(s string) string {
	// Show scheme+host+port but mask password
	i := 0
	for i < len(s) {
		if s[i] == ':' && i+2 < len(s) && s[i+1] == '/' && s[i+2] == '/' {
			start := i + 3
			atIdx := -1
			for j := start; j < len(s); j++ {
				if s[j] == '@' {
					atIdx = j
					break
				}
			}
			if atIdx > 0 {
				return s[:start] + "***" + s[atIdx:]
			}
		}
		i++
	}
	return s
}

func strPtr(s string) *string { return &s }
func boolPtr(b bool) *bool    { return &b }
