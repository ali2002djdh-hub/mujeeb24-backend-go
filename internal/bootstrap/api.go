package bootstrap

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/Ammar777782439/mujeeb24-backend-go/internal/adapters/primary/http/contract"
	"github.com/Ammar777782439/mujeeb24-backend-go/internal/adapters/primary/http/handlers"
	"github.com/Ammar777782439/mujeeb24-backend-go/internal/adapters/primary/http/middleware"
	"github.com/Ammar777782439/mujeeb24-backend-go/internal/adapters/secondary/ai/gemini"
	"github.com/Ammar777782439/mujeeb24-backend-go/internal/adapters/secondary/auth/ed25519jwt"
	"github.com/Ammar777782439/mujeeb24-backend-go/internal/adapters/secondary/persistence/postgres"
	realtimePostgres "github.com/Ammar777782439/mujeeb24-backend-go/internal/adapters/secondary/realtime/postgres"
	"github.com/Ammar777782439/mujeeb24-backend-go/internal/application/commands"
	"github.com/Ammar777782439/mujeeb24-backend-go/internal/application/ports"
	"github.com/Ammar777782439/mujeeb24-backend-go/internal/application/services"
	"github.com/Ammar777782439/mujeeb24-backend-go/internal/platform/config"
	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"
)

type APIRuntime struct {
	HTTP                *http.Server
	Database            *postgres.Adapter
	Dependencies        handlers.Dependencies
	EventStore          ports.EventStore
	Outbox              ports.OutboxStore
	SocialAPI           ports.ChannelProvider
	SocialWebhook       ports.WebhookReceiver
	ChannelProvisioning *services.ChannelProvisioningService
	RealtimeBroker      *realtimePostgres.Broker
	RealtimeHub         *realtimePostgres.LocalHub
	// AutoReplyWorkerPool (optional) — per Item 5: when wired, the
	// webhook service uses it to bound AutoReply goroutine concurrency.
	// Shutdown() calls pool.Stop() to drain queued + in-flight tasks
	// before the HTTP server + database close. Without this, in-flight
	// AutoReply work would be dropped silently on shutdown.
	AutoReplyWorkerPool *services.AutoReplyWorkerPool
	closeOnce           sync.Once
}

type authenticationRuntime struct {
	Verifier   middleware.AccessTokenVerifier
	Repository *postgres.AuthenticationRepository
	Service    services.AuthenticationService
	// PlatformChecker verifies whether a principal is an active platform super
	// admin. Wired from the same authentication repository — the platform_super_admins
	// table is checked on every /api/v1/platform/* request via RequirePlatformAdminHuma.
	PlatformChecker middleware.PlatformSuperAdminChecker
}

func BuildAPI(ctx context.Context, cfg config.ProcessConfig) (*APIRuntime, error) {
	database, err := postgres.Open(ctx, cfg.DatabaseURL, postgres.PoolConfig{MaxConns: cfg.DBMaxConns, MinConns: cfg.DBMinConns, MaxConnLifetime: cfg.DBMaxConnLifetime, MaxConnIdleTime: cfg.DBMaxConnIdleTime, HealthCheckPeriod: cfg.DBHealthCheckPeriod, ConnectTimeout: cfg.DBConnectTimeout})
	if err != nil {
		return nil, err
	}
	authentication, err := buildAuthenticationRuntime(cfg, database)
	if err != nil {
		database.Close()
		return nil, err
	}
	runtime, err := newAPIWithExternalAndAuthentication(database, cfg.HTTPAddr, BuildExternalAdapters(cfg), authentication)
	if err != nil {
		database.Close()
		return nil, err
	}
	return runtime, nil
}

func NewAPI(database *postgres.Adapter, address string) (*APIRuntime, error) {
	return NewAPIWithExternal(database, address, ExternalAdapters{})
}

func NewAPIWithExternal(database *postgres.Adapter, address string, external ExternalAdapters) (*APIRuntime, error) {
	return newAPIWithExternalAndAuthentication(database, address, external, nil)
}

func newAPIWithExternalAndAuthentication(database *postgres.Adapter, address string, external ExternalAdapters, authentication *authenticationRuntime) (*APIRuntime, error) {
	if database == nil {
		return nil, errors.New("postgres adapter is required")
	}
	if external.LLMConfigError != nil {
		return nil, external.LLMConfigError
	}
	if address == "" {
		return nil, errors.New("http address is required")
	}
	realtimeHub := realtimePostgres.NewLocalHub()
	realtimeBroker := realtimePostgres.NewBroker(database.Pool(), realtimeHub)
	realtimeBroker.StartListener(context.Background())

	dependencies := BuildDependenciesWithRealtime(database, realtimeBroker)
	dependencies.GetReadiness = readinessQueryService{Ping: database.Ping, FeatureChecks: external.ReadinessChecks()}
	dependencies.BeginChannelConnection = services.ChannelProvisioningDisabledService{}
	dependencies.IngestSocialAPIWebhook = services.WebhookReceiverDisabledService{Receiver: "SocialAPI"}
	if disconnector, ok := external.SocialAPI.(ports.ChannelAccountDisconnector); ok {
		channelConnectionRepository := postgres.NewChannelConnectionRepository(database)
		channelRuntime := services.ChannelRuntimeService{
			Reader:       channelConnectionRepository,
			Runtime:      channelConnectionRepository,
			Transactions: database,
			Disconnector: disconnector,
			Provisioning: postgres.NewChannelProvisioningStore(database),
		}
		dependencies.ReconnectChannel = services.ReconnectChannelCommandService{ChannelRuntimeService: channelRuntime}
		dependencies.DisconnectChannel = services.DisconnectChannelCommandService{ChannelRuntimeService: channelRuntime}
	}
	if authentication != nil {
		dependencies.Scope = handlers.PostgresScopeProvider{Memberships: authentication.Repository}
		dependencies.PlatformAccess = authentication.PlatformChecker
		dependencies.AuthenticatePrincipal = authentication.Service
		dependencies.RotateRefreshSession = services.RefreshSessionRotationService{Authentication: authentication.Service}
		dependencies.RevokeRefreshSession = services.RefreshSessionRevocationService{Authentication: authentication.Service}
		principalQueries := services.PrincipalQueryService{Principals: authentication.Repository}
		dependencies.GetCurrentPrincipal = principalQueries
		dependencies.ListAccessibleBusinesses = services.MembershipQueryService{PrincipalQueryService: principalQueries}
	}
	var provisioningService *services.ChannelProvisioningService
	if external.ChannelProvisioningEnabled {
		if external.ChannelProvisioningError != nil {
			dependencies.BeginChannelConnection = services.ChannelProvisioningUnavailableService{Cause: external.ChannelProvisioningError}
		} else if external.ChannelProvisioningSocial == nil || external.ChannelProvisioningBrand == nil {
			dependencies.BeginChannelConnection = services.ChannelProvisioningUnavailableService{Cause: errors.New("channel provisioning adapters are not configured")}
		} else {
			service := services.ChannelProvisioningService{
				Sessions:       postgres.NewChannelProvisioningStore(database),
				Social:         external.ChannelProvisioningSocial,
				SocialBrands:   external.ChannelProvisioningBrand,
				ProviderBrands: postgres.NewProviderBrandRepository(database),
				Businesses:     postgres.NewBusinessRepository(database),
				Connections:    postgres.NewChannelConnectionRepository(database),
				RedirectURI:    external.ChannelProvisioningRedirectURI,
			}
			provisioningService = &service
			dependencies.BeginChannelConnection = &services.BeginChannelConnectionHandler{Provisioning: service}
		}
	}
	eventStore := postgres.NewInboundEventStore(database)
	outboxStore := postgres.NewPostgresOutboxStore(database)
	var autoReply commands.AutoReplyHandler
	// Per Item 1 + 2: hoist platformOperations to the top of the
	// function scope so it's available to ALL branches that need
	// the shared kill switch state (AutoReply webhook branch +
	// Summary service branch + Merchant AI agent branch). Before
	// this hoist, the variable was declared at line ~380 (after the
	// AutoReply-enabled branch) — so the Summary service wiring
	// (line ~280) and the Merchant AI wiring (line ~575) couldn't
	// reference it.
	//
	// The flags (aiConfigured/channelConfigured) are computed below
	// at the point of use; the InMemoryPlatformOperationsRepository
	// is stateless except for its runtime flag, so creating it early
	// is safe.
	platformOperations := services.NewInMemoryPlatformOperationsRepository(
		external.GeminiHTTPClient != nil && external.LLMConfigError == nil,
		external.SocialAPI != nil,
	)
	if external.GeminiHTTPClient != nil && external.LLMConfigError == nil {
		platformOperations.RegisterProbe("google_gemini", gemini.NewHealthCheckProbe(external.GeminiHTTPClient))
	}
	if external.SocialAPIHealthProbe != nil {
		platformOperations.RegisterProbe("socialapi", external.SocialAPIHealthProbe)
	}

	// Customer Sales deliberately has no catalog tool registry. The catalog has
	// one authoritative AI path: bounded manifest on the initial turn, then
	// complete catalog paging/batching when Gemini returns needs_more_data.
	// This prevents competing catalog-read paths and keeps evidence deterministic.
	// Per §1-2: create the AIConfigurationCache + AIProviderConfigRepository
	// unconditionally — the Platform Admin can manage credentials and models
	// even when AutoReply is disabled. The cache seeds from env on first
	// start, then switches to DB-backed dynamic config after the admin
	// stores a configuration via the Platform Admin API.
	//
	// Per P1-9: AI_CONFIG_ENCRYPTION_KEY is MANDATORY when AI Provider
	// Configuration is in use (i.e., when AIConfigRepo will be wired into
	// PlatformDeps below). Without it, the repository refuses to encrypt
	// or decrypt API keys — see ErrEncryptionKeyNotConfigured. We fail
	// fast at startup rather than silently falling back to base64.
	//
	// The check is enforced here (not in the repository constructor) so
	// the error message is actionable: the operator knows exactly which
	// env var to set. The repository remains a pure data layer.
	encryptionKey := []byte(os.Getenv("AI_CONFIG_ENCRYPTION_KEY"))
	if len(encryptionKey) == 0 {
		return nil, errors.New("AI_CONFIG_ENCRYPTION_KEY is not set — encrypted credential storage is mandatory. Set AI_CONFIG_ENCRYPTION_KEY to a 32-byte (or longer) random value before starting the API. Example: openssl rand -base64 32 | tr -d '\\n'")
	}
	// Per Item 11: do NOT pad short keys. The previous implementation
	// padded keys shorter than 32 bytes with zeros — that's a security
	// anti-pattern (reduces effective key entropy). The key MUST be
	// 32 bytes (AES-256) or longer. If the operator provides a short
	// key, fail fast with a clear message.
	if len(encryptionKey) < 32 {
		return nil, fmt.Errorf("AI_CONFIG_ENCRYPTION_KEY must be at least 32 bytes (AES-256), got %d bytes. Generate a proper key: openssl rand -base64 32 | tr -d '\\n'", len(encryptionKey))
	}
	// If the key is longer than 32 bytes, truncate to 32 (AES-256 block size).
	// This is safe — the key is still full-entropy.
	if len(encryptionKey) > 32 {
		encryptionKey = encryptionKey[:32]
	}
	aiConfigRepo := postgres.NewAIProviderConfigRepository(database, encryptionKey)
	aiConfigCache := services.NewAIConfigurationCache(aiConfigRepo, aiConfigRepo)
	aiConfigCache.LoadFromEnv(
		os.Getenv("GEMINI_API_KEY"),
		func() string {
			m := strings.TrimSpace(os.Getenv("GEMINI_MODEL"))
			if m == "" {
				m = "gemini-3.8-flash"
			}
			return m
		}(),
		"https://generativelanguage.googleapis.com",
		4096,  // LLMMaxOutputTokens default
		48000, // LLMMaxInputCharacters default; bounded app guard below Gemini 3.8 context capacity
		"",
	)

	if external.AutoReplyEnabled {
		if external.GeminiHTTPClient == nil {
			return nil, errors.New("AutoReply requires the Gemini customer-sales AI to be configured")
		}
		// Per contract ④ §8, adapt the Gemini provider client to the
		// CustomerSalesDecisionPort used by AutoReply and conversation summaries.
		var customerSalesDecision ports.CustomerSalesDecisionPort
		var geminiClient *gemini.GeminiHTTPClient
		var runRepo ports.AIRunRepository
		if external.GeminiHTTPClient != nil {
			geminiClient = external.GeminiHTTPClient
			customerSalesAdapter, err := gemini.NewGeminiCustomerSalesAdapter(geminiClient, nil)
			if err != nil {
				return nil, fmt.Errorf("build customer sales AI adapter: %w", err)
			}
			// Per §1: wire the dynamic config provider so every customer-sales Decide
			// call reads the ACTIVE config from cache/DB.
			customerSalesAdapter.SetConfigurationProvider(aiConfigCache)
			// Per the Tool Loop spec: wire the SAME runRepo
			// instance into the GeminiCustomerSalesAdapter so tool call
			// records are persisted during function calling.
			// Reuses the existing postgres.NewAIRunTraceRepository —
			// no second repository.
			runRepo = postgres.NewAIRunTraceRepository(database)
			customerSalesAdapter.SetRunRepository(runRepo)
			customerSalesAdapter.SetNewID(uuid.NewString)
			// Per fix #2: wire the existing AIRunLifecycle
			// into GeminiCustomerSalesAdapter via the ports.AIRunLifecyclePort
			// abstraction. The lifecycle instance is created
			// here (same pattern as AutoReplyService which
			// creates its own via NewAIRunLifecycle(repo)).
			customerSalesAdapter.SetLifecycle(services.NewAIRunLifecycle(runRepo))
			customerSalesDecision = customerSalesAdapter
		} else {
			// OpenAI-compatible providers do not implement the Customer Sales decision port yet.
			// There is no generic AI fallback path; AutoReply requires the Gemini customer-sales adapter.
			return nil, errors.New("AutoReply requires a gemini.Client (contract-aligned); OpenAI-compatible provider not yet supported")
		}
		referenceRepository := postgres.NewConversationReferenceRepository(database)
		service := services.NewAutoReplyService(
			customerSalesDecision,
			postgres.NewAIDecisionRepository(database),
			referenceRepository,
			postgres.NewOutboundMessageRepository(database),
			outboxStore,
			database,
		)
		// Per contract ⑧ §5, wire the AI Run trace repository
		// (reuse the one created above for the GeminiCustomerSalesAdapter).
		if runRepo != nil {
			service.RunRepository = runRepo
		} else {
			service.RunRepository = postgres.NewAIRunTraceRepository(database)
		}
		// Per AIUsageTokenTelemetry.md §6: wire the telemetry pipeline so
		// every Gemini call's tokens + cost are recorded to ai_usage_records
		// and flow through to the Platform Admin AI Usage view.
		service.AIUsage = postgres.NewAIUsageRepository(database)
		service.AIPricing = postgres.NewAIProviderPricingRepository(database)
		service.Subscriptions = postgres.NewSubscriptionRepository(database)
		// Per contract ⑤ §7, build the Catalog Entity Contract payload
		// once and reuse for every call.
		entityContract := services.BuildCatalogEntityContractPayload()
		payloadBytes, err := json.Marshal(entityContract)
		if err != nil {
			return nil, fmt.Errorf("marshal catalog entity contract: %w", err)
		}
		service.EntityContractPayload = payloadBytes
		contextBuilder := services.NewAutoReplyContextBuilder(
			postgres.NewBusinessRepository(database),
			postgres.NewConversationRepository(database),
			postgres.NewCustomerRepository(database),
			postgres.NewCatalogRepository(database),
			postgres.NewMessageRepository(database),
		)
		// Universal Catalog AI v3: shared read-only catalog boundary.
		// It supplies the bounded manifest and complete bulk projection pages.
		contextBuilder.CatalogAI = postgres.NewCatalogAIReadRepository(database)
		contextBuilder.Knowledge = postgres.NewKnowledgeDocumentRepository(database)
		contextBuilder.Policies = postgres.NewBusinessPolicyRepository(database)
		service.CustomerSalesContextBuilder = contextBuilder
		// Per ADR-039 + P1-5: summaries use the SAME GeminiCustomerSalesAdapter
		// instance as AutoReply. No generic AI fallback is used.
		// The GeminiCustomerSalesAdapter reads the active AI configuration through
		// the AIConfigurationCache and provides usage telemetry.
		service.SummaryService = services.NewConversationSummaryService(
			postgres.NewConversationStateRepository(database),
			postgres.NewMessageRepository(database),
			customerSalesDecision,
		)
		service.SummaryService.AIUsage = postgres.NewAIUsageRepository(database)
		service.SummaryService.AIPricing = postgres.NewAIProviderPricingRepository(database)
		service.SummaryService.Subscriptions = postgres.NewSubscriptionRepository(database)
		service.SummaryService.NewID = uuid.NewString
		// Per Item 2 (Kill Switch applies to Summary): wire the
		// shared AICostProtectionService so MaybeSummarize
		// respects platformAIDisable. Same shared
		// platformOperations instance — one kill switch state,
		// three enforcement points (webhook AutoReply + Merchant
		// AI + Summary).
		service.SummaryService.CostProtection = &services.AICostProtectionService{
			Subscriptions:      postgres.NewSubscriptionRepository(database),
			AIUsage:            postgres.NewAIUsageRepository(database),
			PlatformOperations: platformOperations,
		}
		// Per contract ⑥ §2, wire the validation pipeline with the
		// concrete Postgres validators. Per contract ⑥ §6-7 the
		// ReferenceValidator checks that every selected ID (item/variant/
		// offer) was actually sent to Gemini as evidence AND exists as
		// a real row in catalog_items/variants/offers. Per contract ⑥ §8-9
		// the TenantValidator checks that every selected reference
		// belongs to the current authenticated business (cross-tenant
		// protection).
		//
		// Per contract ⑥ §12-13, the PolicyEvaluator is the
		// Postgres-backed evaluator that reads business_policies per
		// migration 000002 and decides requires_approval — NOT Gemini.
		// Per contract ④ §5, requires_approval is decided by Mujeeb only.
		//
		// The AuthorizationService is left nil; per the pipeline logic,
		// nil Authorization means the PolicyDecision is treated as final
		// (allowed → proceed, requires_approval → wait for human,
		// denied → no execution).
		service.Validation = services.NewValidationPipeline(
			postgres.NewPostgresReferenceValidator(database),
			postgres.NewPostgresTenantValidator(database),
			postgres.NewPostgresCustomerSalesPolicyEvaluator(postgres.NewBusinessRepository(database)),
			nil, // AuthorizationService: nil means PolicyDecision is final
		)
		service.StateRepository = postgres.NewConversationStateRepository(database)
		service.MessageRepository = postgres.NewMessageRepository(database)
		service.Conversations = postgres.NewConversationRepository(database)
		service.Realtime = realtimeBroker

		// Per contract ② §9, wire the CatalogBatchController into the
		// AutoReply flow. Full catalog evaluation is invoked when the
		// initial proposal needs more data, returns not_found before
		// complete coverage, or selects references outside current evidence:
		// page catalog → exact-token batches → evaluate → aggregate → final.
		//
		// Per contract ② §2, TokenBudget is token-based (no hardcoded
		// item count). 8000 is a sensible default per runtime config.
		if geminiClient := external.GeminiHTTPClient; geminiClient != nil {
			batchTokenCounter, err := gemini.NewTokenCounter(gemini.TokenCounterConfig{
				BaseURL: geminiClient.BaseURL(),
				APIKey:  geminiClient.APIKey(),
				Model:   geminiClient.Model(),
			})
			if err != nil {
				return nil, fmt.Errorf("build Gemini token counter: %w", err)
			}
			batchClient, err := gemini.NewBatchClient(gemini.BatchClientConfig{
				BaseURL: geminiClient.BaseURL(),
				APIKey:  geminiClient.APIKey(),
				Model:   geminiClient.Model(),
			})
			if err != nil {
				return nil, fmt.Errorf("build Gemini catalog batch client: %w", err)
			}
			service.CatalogBatch = &services.CatalogBatchController{
				Catalogs:          postgres.NewCatalogRepository(database),
				CatalogAI:         postgres.NewCatalogAIReadRepository(database),
				ProjectionBuilder: &services.CatalogAIProjectionBuilder{},
				TokenCounter:      batchTokenCounter,
				Gemini:            batchClient,
				RunRepo:           postgres.NewAIRunTraceRepository(database),
				TokenBudget:       8000,
				Now:               func() time.Time { return time.Now().UTC() },
				NewID:             uuid.NewString,
				AIUsage:           postgres.NewAIUsageRepository(database),
				AIPricing:         postgres.NewAIProviderPricingRepository(database),
				Subscriptions:     postgres.NewSubscriptionRepository(database),
			}
			// Per §1: wire the dynamic config provider into BatchClient.
			batchClient.SetConfigurationProvider(aiConfigCache)
			// Per P1-4: wire the SAME dynamic config provider into
			// TokenCounter so the countTokens call uses the same
			// active model/apiKey/baseURL as the actual generateContent
			// call. Without this, a model switch via the Platform
			// Admin API would update BatchClient but leave
			// TokenCounter pinned to the old static model — causing
			// token counts to be computed against the wrong tokenizer
			// (different models have different tokenizers).
			if batchTokenCounter != nil {
				batchTokenCounter.SetConfigurationProvider(aiConfigCache)
			}
		}

		autoReply = service
	}
	inboundAutomation := services.InboundAutomationService{
		Rules:         postgres.NewAutomationRuleRepository(database),
		Executions:    postgres.NewAutomationExecutionRepository(database),
		Conversations: postgres.NewConversationRepository(database),
		Reader:        postgres.NewConversationRepository(database),
		Labels:        postgres.NewConversationLabelRepository(database),
		Assignees:     postgres.NewTeamRepository(database),
		Transactions:  database,
	}
	// Per P0-2 + Item 1 + Item 2: platformOperations is declared at
	// the top of this function (line 168) — hoisted so it's available
	// to ALL branches (AutoReply webhook + Summary + Merchant AI +
	// PlatformDeps wire-up). The previous local declaration here has
	// been removed. The flags below remain here for the
	// aiConfigured/channelConfigured dashboard view.
	aiConfigured := external.GeminiHTTPClient != nil && external.LLMConfigError == nil
	channelConfigured := external.SocialAPI != nil
	_ = aiConfigured
	_ = channelConfigured

	// Per Item 5: capture the worker pool reference so APIRuntime.Shutdown
	// can drain it gracefully. The variable is set inside the
	// SocialWebhook branch below; remains nil when the webhook is
	// not wired (the pool is only needed for the AutoReply webhook path).
	var autoReplyPool *services.AutoReplyWorkerPool

	if external.SocialWebhook != nil {
		// Per-merchant customer enrichment: SocialAPI's DM webhook payload
		// delivers only `author.id` (WhatsApp wa_id / FB PSID / IG IGSID);
		// the customer's display_name and picture must be fetched via the
		// REST endpoint GET /v1/inbox/conversations/{id} AFTER Materialize.
		// The Enricher interface is implemented by *socialapi.Client
		// (verified at compile time via var _ ports.ConversationEnricher).
		// The Customers repo is the SAME one used by the customer CRUD
		// endpoints (ports.CustomerRuntimeRepository) — no duplicate repo.
		webhookService := services.SocialAPIWebhookService{
			Receiver:         external.SocialWebhook,
			RawPayloads:      postgres.NewRawPayloadStore(database),
			Connections:      postgres.NewChannelConnectionRepository(database),
			Events:           eventStore,
			Inbound:          postgres.NewProviderInboundStore(database),
			DeliveryStatuses: postgres.NewDeliveryStatusStore(database),
			Automation:       inboundAutomation,
			AutoReply:        autoReply,
			Realtime:         realtimeBroker,
			Customers:        postgres.NewCustomerRepository(database),
		}
		// Wire the enricher ONLY when the SocialAPI client is configured.
		// The client implements both ports.ChannelProvider and
		// ports.ConversationEnricher; we type-assert to expose the
		// enrichment capability without breaking the existing webhook
		// receiver contract.
		if external.SocialAPI != nil {
			if enricher, ok := external.SocialAPI.(ports.ConversationEnricher); ok {
				webhookService.Enricher = enricher
			}
		}
		// Per P0-2 + P0-3 + AIUsageTokenTelemetry.md §19: wire the
		// AI Cost Protection checker with the shared Platform Operations
		// registry. This connects the platformAIDisable/platformAIEnable
		// kill switch (Contract §81) to the actual AutoReply execution
		// path. It also enforces the merchant AI Reply entitlement
		// (Contract §33) before Gemini is called.
		webhookService.AICostProtectionChecker = &services.AICostProtectionService{
			Subscriptions:      postgres.NewSubscriptionRepository(database),
			AIUsage:            postgres.NewAIUsageRepository(database),
			PlatformOperations: platformOperations,
		}
		// Per P1-10: wire the bounded AutoReplyWorkerPool so a viral
		// DM burst doesn't spawn unbounded goroutines (each consuming
		// Gemini quota + DB conns). 4 concurrent workers + 64-deep
		// queue absorbs a burst; larger values can be tuned via env
		// when the Gemini rate limit allows.
		webhookService.AutoReplyWorkerPool = services.NewAutoReplyWorkerPool(4, 64)
		autoReplyPool = webhookService.AutoReplyWorkerPool
		dependencies.IngestSocialAPIWebhook = webhookService
	}
	var assignBusinessOwner *services.AssignBusinessOwnerService
	if authentication != nil {
		assignBusinessOwner = &services.AssignBusinessOwnerService{
			Transactions:       database,
			PrincipalBootstrap: authentication.Repository,
			Business:           postgres.NewPlatformBusinessRepository(database),
		}
	}
	dashboardServer := handlers.NewServer(dependencies)
	dashboardServer = dashboardServer.WithPlatformDeps(handlers.PlatformDeps{
		Plans:               postgres.NewPlanRepository(database),
		PlatformBusiness:    postgres.NewPlatformBusinessRepository(database),
		PlatformAudit:       postgres.NewPlatformAuditRepository(database),
		Subscriptions:       postgres.NewSubscriptionRepository(database),
		Payments:            postgres.NewPaymentRepository(database),
		Support:             postgres.NewSupportRepository(database),
		AIUsage:             postgres.NewAIUsageRepository(database),
		AIProviderPricing:   postgres.NewAIProviderPricingRepository(database),
		Operations:          platformOperations,
		ChannelReader:       postgres.NewPlatformChannelReadRepository(database),
		AIConfigRepo:        aiConfigRepo,
		AIConfigCache:       aiConfigCache,
		ModelDiscovery:      gemini.NewModelsClient(),
		AssignBusinessOwner: assignBusinessOwner,
	})

	var merchantCatalogErr error
	dashboardServer, merchantCatalogErr = wireMerchantCatalogAuthoringAI(dashboardServer, database, external.GeminiHTTPClient, aiConfigCache)
	if merchantCatalogErr != nil {
		return nil, merchantCatalogErr
	}

	var apiMiddleware []func(ctx huma.Context, next func(huma.Context))
	if authentication != nil {
		apiMiddleware = append(apiMiddleware, middleware.RequireAccessTokenHuma(authentication.Verifier))
		// Per Platform Administration Contract §4, §55, §63-64: Platform
		// scope is a SEPARATE authorization layer from merchant membership.
		// RequirePlatformAdminHuma is intentionally a separate middleware
		// (not part of RequireAccessTokenHuma) so the /api/v1/platform/*
		// routes can be selectively protected while /api/v1/businesses/*
		// routes remain on the merchant requireScope boundary.
		//
		// The check internally verifies the principal is an active
		// platform_super_admin via the platform_access_repository. A
		// merchant owner token used against /platform/* returns 403.
		if authentication.PlatformChecker != nil {
			apiMiddleware = append(apiMiddleware, middleware.RequirePlatformAdminHuma(authentication.PlatformChecker))
		}
	}

	_, mux := contract.BuildAPIWithHandlersAndMiddleware(dashboardServer, apiMiddleware)

	realtimeSSEHandler := handlers.NewRealtimeSSEHandler(realtimeHub, dependencies.Scope)
	var sseHTTPHandler http.Handler = realtimeSSEHandler
	if authentication != nil {
		sseHTTPHandler = middleware.RequireAccessToken(authentication.Verifier, realtimeSSEHandler)
	}
	mux.Handle("GET /api/v1/businesses/{business_id}/realtime/events", sseHTTPHandler)

	if provisioningService != nil {
		mux.HandleFunc("/oauth/socialapi/callback", func(writer http.ResponseWriter, request *http.Request) {
			if request.Method != http.MethodGet {
				writer.WriteHeader(http.StatusMethodNotAllowed)
				return
			}
			query := request.URL.Query()
			callback := ports.SocialAuthorizationCallback{State: query.Get("state"), Status: query.Get("status"), Platform: query.Get("platform"), AccountID: query.Get("account_id"), ConnectionID: query.Get("connection_id"), PlatformAccountID: query.Get("platform_account_id"), PageIDs: append([]string(nil), query["page_id"]...)}
			if len(callback.PageIDs) == 0 && query.Get("page_ids") != "" {
				for _, pageID := range strings.Split(query.Get("page_ids"), ",") {
					if value := strings.TrimSpace(pageID); value != "" {
						callback.PageIDs = append(callback.PageIDs, value)
					}
				}
			}
			session, err := provisioningService.CompleteOAuthCallback(request.Context(), callback)
			if external.FrontendURL != "" {
				// FRONTEND_URL may include a UI route such as /landing. The
				// merchant dashboard lives at /channels, so appending /channels
				// to FRONTEND_URL directly would produce /landing/channels.
				// Redirect to the configured frontend origin instead.
				frontendRedirectBase := external.FrontendURL
				if parsed, parseErr := url.Parse(external.FrontendURL); parseErr == nil && parsed.Scheme != "" && parsed.Host != "" {
					frontendRedirectBase = parsed.Scheme + "://" + parsed.Host
				}
				var redirectURL string
				if err != nil {
					log.Printf("[ChannelProvisioning] OAuth callback failed: %v", err)
					redirectURL = fmt.Sprintf("%s/channels?status=failed&error=%s", strings.TrimRight(frontendRedirectBase, "/"), url.QueryEscape("تعذر إكمال ربط القناة. يرجى المحاولة مرة أخرى."))
				} else {
					redirectURL = fmt.Sprintf("%s/channels?status=connected&channel=%s&provisioning_id=%s", strings.TrimRight(frontendRedirectBase, "/"), url.QueryEscape(session.Channel), url.QueryEscape(session.ID))
				}
				http.Redirect(writer, request, redirectURL, http.StatusFound)
				return
			}
			writer.Header().Set("Content-Type", "application/json")
			if err != nil {
				writer.WriteHeader(http.StatusBadRequest)
				_, _ = fmt.Fprintf(writer, `{"status":"failed","error":%q}`, err.Error())
				return
			}
			_, _ = fmt.Fprintf(writer, `{"status":%q,"provisioning_id":%q,"provider":%q,"channel":%q}`, session.Status, session.ID, session.ProviderRef, session.Channel)
		})
	}
	mux.HandleFunc("/health", func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodGet {
			writer.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"status":"ok","service":"mujeeb24-api"}`))
	})
	// SECURITY audit H2: enforce a hard body-size limit on the public
	// webhook endpoint. Without this, an attacker could POST a multi-
	// hundred-MB body that Huma buffers into memory AND that the raw
	// payload store then persists as BYTEA — unbounded memory + DB
	// growth. The OpenAPI already advertises 413 as a possible error
	// response; this middleware makes it real.
	//
	// We wrap the mux in a small middleware that applies
	// http.MaxBytesReader to the webhook route specifically. Other
	// routes (auth, dashboard, SSE) are unaffected — their Huma
	// bodies are small JSON envelopes.
	wrappedMux := http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if strings.HasPrefix(request.URL.Path, "/api/v1/webhooks/") {
			// 256 KB — SocialAPI webhook payloads are typically
			// 1-5 KB. 256 KB is generous headroom and still
			// blocks the multi-MB adversarial case.
			const maxWebhookBodyBytes = 256 * 1024
			request.Body = http.MaxBytesReader(writer, request.Body, maxWebhookBodyBytes)
		}
		mux.ServeHTTP(writer, request)
	})
	// Cross-origin browser access is required because the production frontend is hosted
	// separately from the Render API. CORS is centralized in newCORSMiddleware so the
	// allowed request headers are covered by a regression-tested contract.
	corsMux := newCORSMiddleware(wrappedMux, external.FrontendURL)
	server := &http.Server{Addr: address, Handler: corsMux, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second}
	return &APIRuntime{
		HTTP:                server,
		Database:            database,
		Dependencies:        dependencies,
		EventStore:          eventStore,
		Outbox:              outboxStore,
		SocialAPI:           external.SocialAPI,
		SocialWebhook:       external.SocialWebhook,
		ChannelProvisioning: provisioningService,
		RealtimeBroker:      realtimeBroker,
		RealtimeHub:         realtimeHub,
		AutoReplyWorkerPool: autoReplyPool,
	}, nil
}

func buildAuthenticationRuntime(cfg config.ProcessConfig, database *postgres.Adapter) (*authenticationRuntime, error) {
	if !cfg.AuthEnabled {
		return nil, nil
	}
	issuer, err := ed25519jwt.New(ed25519jwt.Config{PrivateKeyBase64: cfg.JWTEd25519PrivateKey, PublicKeyBase64: cfg.JWTEd25519PublicKey, Issuer: cfg.JWTIssuer, AccessTTL: cfg.JWTAccessTTL})
	if err != nil {
		return nil, err
	}
	repository := postgres.NewAuthenticationRepository(database)
	service := services.AuthenticationService{Principals: repository, Sessions: repository, Tokens: issuer, RefreshTTL: cfg.RefreshSessionTTL}
	// Per Platform Administration Contract §4: Platform Scope is enforced
	// separately from Merchant Scope. The PlatformAccessRepository (which
	// implements both PlatformSuperAdminBootstrapRepository and the IsActiveSuperAdmin
	// checker) is wired here so RequirePlatformAdminHuma can verify every
	// /api/v1/platform/* request.
	platformAccess := postgres.NewPlatformAccessRepository(database)
	return &authenticationRuntime{
		Verifier:        issuer,
		Repository:      repository,
		Service:         service,
		PlatformChecker: platformAccess,
	}, nil
}

func (r *APIRuntime) Serve() error {
	if r == nil || r.HTTP == nil {
		return errors.New("api runtime is not configured")
	}
	err := r.HTTP.ListenAndServe()
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

func (r *APIRuntime) Shutdown(ctx context.Context) error {
	if r == nil {
		return nil
	}
	var shutdownErr error
	r.closeOnce.Do(func() {
		// Per Item 5: drain the AutoReply worker pool BEFORE closing
		// the HTTP server + database. The pool's Stop() closes the
		// tasks channel + waits for all workers to finish their
		// in-flight + queued tasks. Without this, in-flight AutoReply
		// work (which calls Gemini + writes to Postgres) would be
		// dropped silently when the database closes.
		if r.AutoReplyWorkerPool != nil {
			r.AutoReplyWorkerPool.Stop()
		}
		if r.RealtimeBroker != nil {
			r.RealtimeBroker.Close()
		}
		if r.HTTP != nil {
			shutdownErr = r.HTTP.Shutdown(ctx)
		}
		if r.Database != nil {
			r.Database.Close()
		}
	})
	return shutdownErr
}
