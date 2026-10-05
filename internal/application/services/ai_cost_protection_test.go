package services

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Ammar777782439/mujeeb24-backend-go/internal/application/ports"
)

// ---- Stubs for testing AICostProtectionService ----

type stubSubscriptionsRepo struct {
	items []ports.SubscriptionRecord
	err   error
}

func (s *stubSubscriptionsRepo) List(_ context.Context, _ ports.SubscriptionListFilter) (ports.SubscriptionPage, error) {
	if s.err != nil {
		return ports.SubscriptionPage{}, s.err
	}
	return ports.SubscriptionPage{Items: s.items}, nil
}
func (s *stubSubscriptionsRepo) GetByID(_ context.Context, _ string) (ports.SubscriptionRecord, error) {
	return ports.SubscriptionRecord{}, nil
}
func (s *stubSubscriptionsRepo) Create(_ context.Context, _ ports.SubscriptionCreate) (ports.SubscriptionRecord, error) {
	return ports.SubscriptionRecord{}, nil
}
func (s *stubSubscriptionsRepo) Activate(_ context.Context, _ string, _ time.Time) (ports.SubscriptionRecord, error) {
	return ports.SubscriptionRecord{}, nil
}
func (s *stubSubscriptionsRepo) Cancel(_ context.Context, _ string, _ string, _ string, _ time.Time) (ports.SubscriptionRecord, error) {
	return ports.SubscriptionRecord{}, nil
}
func (s *stubSubscriptionsRepo) MarkExpired(_ context.Context, _ string, _ time.Time) (ports.SubscriptionRecord, error) {
	return ports.SubscriptionRecord{}, nil
}
func (s *stubSubscriptionsRepo) ApplyCostBudgetOverride(_ context.Context, _ string, _ int, _ string, _ string, _ time.Time) (ports.SubscriptionRecord, error) {
	return ports.SubscriptionRecord{}, nil
}
func (s *stubSubscriptionsRepo) CheckEntitlements(_ context.Context, _ string, _ int, _ int) error {
	return nil
}

type stubAIUsageRepo struct {
	agg ports.SubscriptionAIUsageAggregate
	err error
}

func (s *stubAIUsageRepo) AppendRecord(_ context.Context, _ ports.AIUsageAppend) (ports.AIUsageRecord, error) {
	return ports.AIUsageRecord{}, nil
}
func (s *stubAIUsageRepo) GetSubscriptionAIUsage(_ context.Context, _ string) (ports.SubscriptionAIUsageAggregate, error) {
	return s.agg, s.err
}
func (s *stubAIUsageRepo) RefreshAggregate(_ context.Context, _ string, _ time.Time) (ports.SubscriptionAIUsageAggregate, error) {
	return s.agg, nil
}
func (s *stubAIUsageRepo) GetPlatformAIUsageOverview(_ context.Context) (ports.SubscriptionAIUsageAggregate, error) {
	return s.agg, nil
}
func (s *stubAIUsageRepo) GetAIUsageByBusiness(_ context.Context, _ int) ([]ports.SubscriptionAIUsageAggregate, error) {
	return nil, nil
}

type stubPlatformOperationsRepo struct {
	state ports.AIRuntimeState
	err   error
}

func (s *stubPlatformOperationsRepo) GetRuntimeState(_ context.Context) (ports.AIRuntimeState, error) {
	return s.state, s.err
}
func (s *stubPlatformOperationsRepo) DisableRuntime(_ context.Context, _ time.Time) (ports.AIRuntimeState, error) {
	return s.state, nil
}
func (s *stubPlatformOperationsRepo) EnableRuntime(_ context.Context, _ time.Time) (ports.AIRuntimeState, error) {
	return s.state, nil
}
func (s *stubPlatformOperationsRepo) ListProviders(_ context.Context) ([]ports.ProviderRecord, error) {
	return nil, nil
}
func (s *stubPlatformOperationsRepo) GetProvider(_ context.Context, _ string) (ports.ProviderRecord, error) {
	return ports.ProviderRecord{}, nil
}
func (s *stubPlatformOperationsRepo) RunHealthCheck(_ context.Context, _ string, _ time.Time) (ports.ProviderRecord, error) {
	return ports.ProviderRecord{}, nil
}
func (s *stubPlatformOperationsRepo) RegisterProbe(_ string, _ ports.HealthCheckProbe) {}

// Ensure our stubs satisfy the ports at compile time.
var _ ports.SubscriptionRepository = (*stubSubscriptionsRepo)(nil)
var _ ports.AIUsageRepository = (*stubAIUsageRepo)(nil)
var _ ports.PlatformOperationsPort = (*stubPlatformOperationsRepo)(nil)

// ---- Tests ----

// Test P0-2: when AI Runtime is DISABLED at the platform level
// (Contract §81), IsAIExecutionAllowed returns false + reason
// "ai_runtime_disabled" — no matter the subscription state.
func TestAICostProtectionBlocksWhenRuntimeDisabled(t *testing.T) {
	t.Parallel()
	svc := &AICostProtectionService{
		Subscriptions: &stubSubscriptionsRepo{
			items: []ports.SubscriptionRecord{{ID: "sub-1", BusinessID: "b-1", Status: "ACTIVE"}},
		},
		AIUsage: &stubAIUsageRepo{
			agg: ports.SubscriptionAIUsageAggregate{
				AIReplyLimit: 100, AIRepliesUsed: 5, AIRepliesRemaining: 95,
				BudgetStatus: "NORMAL",
			},
		},
		PlatformOperations: &stubPlatformOperationsRepo{
			state: ports.AIRuntimeState{AdminState: ports.ProviderAdminDisabled},
		},
	}
	allowed, reason := svc.IsAIExecutionAllowed(context.Background(), "b-1")
	if allowed {
		t.Errorf("expected allowed=false when runtime DISABLED, got true")
	}
	if reason != "ai_runtime_disabled" {
		t.Errorf("expected reason=ai_runtime_disabled, got %q", reason)
	}
}

// Test P0-2: when AI Runtime is ENABLED, IsAIExecutionAllowed
// returns true (subscription state permitting).
func TestAICostProtectionAllowsWhenRuntimeEnabled(t *testing.T) {
	t.Parallel()
	svc := &AICostProtectionService{
		Subscriptions: &stubSubscriptionsRepo{
			items: []ports.SubscriptionRecord{{ID: "sub-1", BusinessID: "b-1", Status: "ACTIVE"}},
		},
		AIUsage: &stubAIUsageRepo{
			agg: ports.SubscriptionAIUsageAggregate{
				AIReplyLimit: 100, AIRepliesUsed: 5, AIRepliesRemaining: 95,
				BudgetStatus: "NORMAL",
			},
		},
		PlatformOperations: &stubPlatformOperationsRepo{
			state: ports.AIRuntimeState{AdminState: ports.ProviderAdminEnabled},
		},
	}
	allowed, reason := svc.IsAIExecutionAllowed(context.Background(), "b-1")
	if !allowed {
		t.Errorf("expected allowed=true when runtime ENABLED + replies remaining, got false (reason=%s)", reason)
	}
	if reason != "" {
		t.Errorf("expected empty reason when allowed, got %q", reason)
	}
}

// Missing runtime-state protection blocks Auto AI while leaving non-AI
// webhook processing untouched.
func TestAICostProtectionFailsClosedWhenPlatformOpsNil(t *testing.T) {
	t.Parallel()
	svc := &AICostProtectionService{
		Subscriptions: &stubSubscriptionsRepo{
			items: []ports.SubscriptionRecord{{ID: "sub-1", BusinessID: "b-1", Status: "ACTIVE"}},
		},
		AIUsage: &stubAIUsageRepo{
			agg: ports.SubscriptionAIUsageAggregate{
				AIReplyLimit: 100, AIRepliesUsed: 5, AIRepliesRemaining: 95,
				BudgetStatus: "NORMAL",
			},
		},
		// PlatformOperations: nil
	}
	allowed, reason := svc.IsAIExecutionAllowed(context.Background(), "b-1")
	if allowed {
		t.Errorf("expected allowed=false when platform ops is not wired")
	}
	if reason != "ai_runtime_state_unavailable" {
		t.Errorf("expected ai_runtime_state_unavailable, got %q", reason)
	}
}

// Runtime-state lookup failure blocks only Auto AI.
func TestAICostProtectionFailsClosedOnRuntimeCheckerError(t *testing.T) {
	t.Parallel()
	svc := &AICostProtectionService{
		Subscriptions: &stubSubscriptionsRepo{
			items: []ports.SubscriptionRecord{{ID: "sub-1", BusinessID: "b-1", Status: "ACTIVE"}},
		},
		AIUsage: &stubAIUsageRepo{
			agg: ports.SubscriptionAIUsageAggregate{
				AIReplyLimit: 100, AIRepliesUsed: 5, AIRepliesRemaining: 95,
				BudgetStatus: "NORMAL",
			},
		},
		PlatformOperations: &stubPlatformOperationsRepo{
			state: ports.AIRuntimeState{AdminState: ports.ProviderAdminEnabled},
			err:   errors.New("registry unavailable"),
		},
	}
	allowed, reason := svc.IsAIExecutionAllowed(context.Background(), "b-1")
	if allowed {
		t.Errorf("expected allowed=false on runtime checker error")
	}
	if reason != "ai_runtime_state_unavailable" {
		t.Errorf("expected ai_runtime_state_unavailable, got %q", reason)
	}
}

// Test P0-3: when AIRepliesRemaining == 0 (entitlement exhausted),
// IsAIExecutionAllowed returns false + "ai_replies_exhausted".
// Per Contract §33: AI Replies is the commercial metric.
func TestAICostProtectionBlocksWhenAIRepliesExhausted(t *testing.T) {
	t.Parallel()
	svc := &AICostProtectionService{
		Subscriptions: &stubSubscriptionsRepo{
			items: []ports.SubscriptionRecord{{ID: "sub-1", BusinessID: "b-1", Status: "ACTIVE"}},
		},
		AIUsage: &stubAIUsageRepo{
			agg: ports.SubscriptionAIUsageAggregate{
				AIReplyLimit: 100, AIRepliesUsed: 100, AIRepliesRemaining: 0,
				BudgetStatus: "NORMAL", // budget OK but entitlement exhausted
			},
		},
		PlatformOperations: &stubPlatformOperationsRepo{
			state: ports.AIRuntimeState{AdminState: ports.ProviderAdminEnabled},
		},
	}
	allowed, reason := svc.IsAIExecutionAllowed(context.Background(), "b-1")
	if allowed {
		t.Errorf("expected allowed=false when AI Replies exhausted, got true")
	}
	if reason != "ai_replies_exhausted" {
		t.Errorf("expected reason=ai_replies_exhausted, got %q", reason)
	}
}

// Test P0-3: when AIRepliesRemaining > 0, execution is allowed.
func TestAICostProtectionAllowsWhenRepliesRemaining(t *testing.T) {
	t.Parallel()
	svc := &AICostProtectionService{
		Subscriptions: &stubSubscriptionsRepo{
			items: []ports.SubscriptionRecord{{ID: "sub-1", BusinessID: "b-1", Status: "ACTIVE"}},
		},
		AIUsage: &stubAIUsageRepo{
			agg: ports.SubscriptionAIUsageAggregate{
				AIReplyLimit: 100, AIRepliesUsed: 99, AIRepliesRemaining: 1,
				BudgetStatus: "NORMAL",
			},
		},
		PlatformOperations: &stubPlatformOperationsRepo{
			state: ports.AIRuntimeState{AdminState: ports.ProviderAdminEnabled},
		},
	}
	allowed, _ := svc.IsAIExecutionAllowed(context.Background(), "b-1")
	if !allowed {
		t.Errorf("expected allowed=true when AIRepliesRemaining > 0, got false")
	}
}

// Test P0-3: the order of checks — runtime kill switch fires FIRST
// (before entitlement + cost). When runtime is disabled AND budget
// is EXCEEDED, the reason must be "ai_runtime_disabled" (the more
// authoritative signal — affects ALL businesses).
func TestAICostProtectionRuntimeCheckBeatsAllOtherChecks(t *testing.T) {
	t.Parallel()
	svc := &AICostProtectionService{
		Subscriptions: &stubSubscriptionsRepo{
			items: []ports.SubscriptionRecord{{ID: "sub-1", BusinessID: "b-1", Status: "ACTIVE"}},
		},
		AIUsage: &stubAIUsageRepo{
			agg: ports.SubscriptionAIUsageAggregate{
				AIReplyLimit: 100, AIRepliesUsed: 100, AIRepliesRemaining: 0,
				BudgetStatus: "EXCEEDED",
			},
		},
		PlatformOperations: &stubPlatformOperationsRepo{
			state: ports.AIRuntimeState{AdminState: ports.ProviderAdminDisabled},
		},
	}
	allowed, reason := svc.IsAIExecutionAllowed(context.Background(), "b-1")
	if allowed {
		t.Errorf("expected allowed=false")
	}
	if reason != "ai_runtime_disabled" {
		t.Errorf("expected reason=ai_runtime_disabled (kill switch wins), got %q", reason)
	}
}

// Contract §29: without an ACTIVE subscription, merchant AI entitlements stop.
func TestAICostProtectionBlocksWhenNoActiveSubscription(t *testing.T) {
	t.Parallel()
	svc := &AICostProtectionService{
		Subscriptions: &stubSubscriptionsRepo{
			items: nil, // no active subscription
		},
		AIUsage:            &stubAIUsageRepo{},
		PlatformOperations: &stubPlatformOperationsRepo{state: ports.AIRuntimeState{AdminState: ports.ProviderAdminEnabled}},
	}
	allowed, reason := svc.IsAIExecutionAllowed(context.Background(), "b-1")
	if allowed {
		t.Errorf("expected allowed=false when no active subscription")
	}
	if reason != "active_subscription_required" {
		t.Errorf("expected active_subscription_required, got %q", reason)
	}
}

func TestAICostProtectionNilServiceFailsClosed(t *testing.T) {
	var svc *AICostProtectionService
	allowed, reason := svc.IsAIExecutionAllowed(context.Background(), "b-1")
	if allowed {
		t.Fatal("nil cost protection service must not allow AI execution")
	}
	if reason != "ai_cost_protection_unavailable" {
		t.Fatalf("unexpected reason: %q", reason)
	}
}

func TestAICostProtectionFailsClosedOnUsageLookupError(t *testing.T) {
	t.Parallel()
	svc := &AICostProtectionService{
		Subscriptions: &stubSubscriptionsRepo{
			items: []ports.SubscriptionRecord{{ID: "sub-1", BusinessID: "b-1", Status: "ACTIVE"}},
		},
		AIUsage: &stubAIUsageRepo{err: errors.New("usage db unavailable")},
		PlatformOperations: &stubPlatformOperationsRepo{
			state: ports.AIRuntimeState{AdminState: ports.ProviderAdminEnabled},
		},
	}
	allowed, reason := svc.IsAIExecutionAllowed(context.Background(), "b-1")
	if allowed {
		t.Errorf("expected allowed=false when usage check fails")
	}
	if reason != "ai_usage_check_unavailable" {
		t.Errorf("expected ai_usage_check_unavailable, got %q", reason)
	}
}
