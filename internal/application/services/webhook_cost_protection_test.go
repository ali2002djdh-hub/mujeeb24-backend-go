package services

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/Ammar777782439/mujeeb24-backend-go/internal/application/ports"
)

// stubAICostProtectionChecker returns a configurable allowed/reason.
type stubAICostProtectionChecker struct {
	allowed bool
	reason  string
}

func (s *stubAICostProtectionChecker) IsAIExecutionAllowed(_ context.Context, _ string) (bool, string) {
	return s.allowed, s.reason
}

// countingRealtimePublisher counts Publish calls.
type countingRealtimePublisher struct {
	count int32
}

func (c *countingRealtimePublisher) Publish(_ context.Context, _ ports.RealtimeEvent) error {
	atomic.AddInt32(&c.count, 1)
	return nil
}
func (c *countingRealtimePublisher) GetCount() int { return int(atomic.LoadInt32(&c.count)) }

func TestAICostProtectionBlocksAutoReplyWhenExceeded(t *testing.T) {
	autoReply := newAutoReplyHandler()
	service := resolvedSocialWebhookService(&providerInboundStore{result: ports.ProviderInboundResult{BusinessID: "business-1", CustomerID: "customer-1", ConversationID: "conversation-1", CommunicationMessageID: "message-1"}})
	service.AutoReply = autoReply
	service.AICostProtectionChecker = &stubAICostProtectionChecker{allowed: false, reason: "ai_cost_budget_exceeded"}

	body := []byte(`{"event":"dm.received","data":{"id":"event-cost-1","type":"dm","platform":"instagram","account_id":"account-1","conversation_id":"conversation-1","author":{"id":"customer-1"},"content":{"text":"hello"}}}`)
	result, err := service.Handle(context.Background(), signedSocialCommand(t, body))
	if err != nil || !result.Accepted {
		t.Fatalf("Handle: err=%v result=%#v", err, result)
	}
	if autoReply.getCalls() != 0 {
		t.Fatalf("expected 0 AutoReply calls when budget EXCEEDED, got %d", autoReply.getCalls())
	}
}

func TestAICostProtectionAllowsAutoReplyWhenNormal(t *testing.T) {
	autoReply := newAutoReplyHandler()
	service := resolvedSocialWebhookService(&providerInboundStore{result: ports.ProviderInboundResult{BusinessID: "business-1", CustomerID: "customer-1", ConversationID: "conversation-2", CommunicationMessageID: "message-2"}})
	service.AutoReply = autoReply
	service.AICostProtectionChecker = &stubAICostProtectionChecker{allowed: true, reason: ""}

	body := []byte(`{"event":"dm.received","data":{"id":"event-cost-2","type":"dm","platform":"instagram","account_id":"account-1","conversation_id":"conversation-2","author":{"id":"customer-1"},"content":{"text":"hello"}}}`)
	result, err := service.Handle(context.Background(), signedSocialCommand(t, body))
	if err != nil || !result.Accepted {
		t.Fatalf("Handle: err=%v result=%#v", err, result)
	}
	autoReply.wait()
	if autoReply.getCalls() != 1 {
		t.Fatalf("expected 1 AutoReply call when budget NORMAL, got %d", autoReply.getCalls())
	}
}

func TestAICostProtectionDoesNotBlockHumanReply(t *testing.T) {
	autoReply := newAutoReplyHandler()
	rtCount := &countingRealtimePublisher{}
	service := resolvedSocialWebhookService(&providerInboundStore{result: ports.ProviderInboundResult{BusinessID: "business-1", CustomerID: "customer-1", ConversationID: "conversation-3", CommunicationMessageID: "message-3"}})
	service.AutoReply = autoReply
	service.Realtime = rtCount
	service.AICostProtectionChecker = &stubAICostProtectionChecker{allowed: false, reason: "ai_cost_budget_exceeded"}

	body := []byte(`{"event":"dm.received","data":{"id":"event-cost-3","type":"dm","platform":"instagram","account_id":"account-1","conversation_id":"conversation-3","author":{"id":"customer-1"},"content":{"text":"I need help"}}}`)
	result, err := service.Handle(context.Background(), signedSocialCommand(t, body))
	if err != nil || !result.Accepted {
		t.Fatalf("Handle: err=%v result=%#v", err, result)
	}
	if autoReply.getCalls() != 0 {
		t.Fatalf("expected 0 AutoReply calls when EXCEEDED, got %d", autoReply.getCalls())
	}
	if rtCount.GetCount() == 0 {
		t.Fatalf("expected realtime event published (Human Reply unaffected — merchant can see customer message + reply manually), got 0 publishes")
	}
}

func TestAICostProtectionMissingCheckerBlocksAutoReply(t *testing.T) {
	autoReply := newAutoReplyHandler()
	service := resolvedSocialWebhookService(&providerInboundStore{result: ports.ProviderInboundResult{BusinessID: "business-1", CustomerID: "customer-1", ConversationID: "conversation-missing-protection", CommunicationMessageID: "message-missing-protection"}})
	service.AutoReply = autoReply
	service.AICostProtectionChecker = nil

	body := []byte(`{"event":"dm.received","data":{"id":"event-cost-missing","type":"dm","platform":"instagram","account_id":"account-1","conversation_id":"conversation-missing-protection","author":{"id":"customer-1"},"content":{"text":"hello"}}}`)
	result, err := service.Handle(context.Background(), signedSocialCommand(t, body))
	if err != nil || !result.Accepted {
		t.Fatalf("Handle: err=%v result=%#v", err, result)
	}
	if autoReply.getCalls() != 0 {
		t.Fatalf("expected 0 AutoReply calls when cost protection checker is missing, got %d", autoReply.getCalls())
	}
}
