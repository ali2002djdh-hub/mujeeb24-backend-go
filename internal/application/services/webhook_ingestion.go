package services

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"strings"
	"time"

	"github.com/Ammar777782439/mujeeb24-backend-go/internal/application/commands"
	appErrors "github.com/Ammar777782439/mujeeb24-backend-go/internal/application/errors"
	"github.com/Ammar777782439/mujeeb24-backend-go/internal/application/ports"
	"github.com/Ammar777782439/mujeeb24-backend-go/internal/domain/channel"
	"github.com/google/uuid"
)

type SocialAPIWebhookService struct {
	Receiver         ports.WebhookReceiver
	RawPayloads      ports.RawPayloadStore
	Connections      ports.ChannelConnectionRepository
	Events           ports.EventStore
	Inbound          ports.ProviderInboundStore
	DeliveryStatuses ports.DeliveryStatusStore
	Automation       commands.ApplyInboundAutomationHandler
	AutoReply        commands.AutoReplyHandler
	Realtime         ports.RealtimePublisher
	Now              func() time.Time
	// Enricher (optional) — when wired, fetches the customer's display_name
	// and picture from the provider's REST API after Materialize. The
	// provider's webhook payload for DMs carries only author.id; the name
	// is only available via the inbox/conversations REST endpoint.
	Enricher ports.ConversationEnricher
	// Customers (optional) — required for Enricher to persist the fetched
	// display_name/picture into the customer.profile JSONB column.
	Customers ports.CustomerRuntimeRepository
	// AICostProtectionChecker (optional) — per AIUsageTokenTelemetry.md §19:
	// gates AutoReply execution when AI runtime is disabled (P0-2), when
	// merchant AI Reply entitlement is exhausted (P0-3), or when the
	// subscription's cost budget is EXCEEDED.
	AICostProtectionChecker AICostProtectionChecker
	// AutoReplyWorkerPool (optional) — per P1-10: bounds the concurrency
	// of AutoReply goroutines so a viral DM burst doesn't spawn an
	// unbounded number of concurrent Gemini calls. When nil, falls back
	// to the legacy per-DM goroutine (deprecated — production MUST wire
	// a worker pool).
	AutoReplyWorkerPool *AutoReplyWorkerPool
}

// AICostProtectionChecker verifies whether the business's active subscription
// allows a new AI execution. Returns (allowed=true) if the budget is within
// limits (NORMAL or WARNING). Returns (allowed=false, reason) if EXCEEDED.
// Per AIUsageTokenTelemetry.md §19-21.
type AICostProtectionChecker interface {
	IsAIExecutionAllowed(ctx context.Context, businessID string) (allowed bool, reason string)
}

func (s SocialAPIWebhookService) Handle(ctx context.Context, command commands.IngestWebhookCommand) (commands.WebhookAcceptedResult, error) {
	if s.Receiver == nil || s.RawPayloads == nil || s.Connections == nil || s.Events == nil {
		return commands.WebhookAcceptedResult{}, appErrors.NotImplemented()
	}
	if err := validateWebhookCommand(command); err != nil {
		return commands.WebhookAcceptedResult{}, err
	}
	if err := s.Receiver.VerifyWebhook(ctx, command.ProviderHeaders, command.RawPayload); err != nil {
		return commands.WebhookAcceptedResult{}, webhookAuthenticationError(err)
	}
	events, err := s.Receiver.NormalizeWebhook(ctx, command.ProviderHeaders, command.RawPayload)
	if err != nil {
		return commands.WebhookAcceptedResult{}, webhookAuthenticationError(err)
	}
	if len(events) == 0 {
		return commands.WebhookAcceptedResult{Accepted: true, Ignored: true, RequestID: command.RequestID}, nil
	}
	if s.Now == nil {
		s.Now = time.Now
	}
	stored, err := s.RawPayloads.Put(ctx, "socialapi", command.DeliveryID, command.RawPayload)
	if err != nil {
		return commands.WebhookAcceptedResult{}, externalDependencyError("raw webhook payload could not be stored", err)
	}
	result := commands.WebhookAcceptedResult{Accepted: true, RequestID: command.RequestID}
	for _, event := range events {
		if event.Provider != channel.ProviderSocialAPI || strings.TrimSpace(event.ProviderConnectionID) == "" {
			return commands.WebhookAcceptedResult{}, appErrors.New(appErrors.CodeValidation, "verified SocialAPI webhook has no provider account reference")
		}
		connection, resolveErr := s.Connections.GetByProviderReferences(ctx, string(channel.ProviderSocialAPI), event.ProviderConnectionID, "")
		var businessID, connectionID *string
		if resolveErr == nil {
			businessIDValue, connectionIDValue := connection.BusinessID, connection.ID
			businessID, connectionID = &businessIDValue, &connectionIDValue
			result.Resolved = true
		} else if repositoryErrorKind(resolveErr) == "not_found" {
			result.Resolved = false
		} else if repositoryErrorKind(resolveErr) == "conflict" {
			return commands.WebhookAcceptedResult{}, appErrors.New(appErrors.CodeConflict, "provider account maps to multiple channel connections")
		} else {
			return commands.WebhookAcceptedResult{}, externalDependencyError("channel connection lookup failed", resolveErr)
		}
		created, record, recordErr := s.Events.RecordIfAbsent(ctx, ports.InboundEventDraft{
			ID:                     event.ID,
			ProviderRef:            string(event.Provider),
			ProviderConnectionRef:  event.ProviderConnectionID,
			ProviderEventID:        event.ProviderEventID,
			DedupeStrategy:         nonEmptyOr(event.DedupeStrategy, "provider_event_id"),
			BusinessID:             businessID,
			ConnectionID:           connectionID,
			EventType:              event.EventType,
			InteractionKind:        stringPointer(string(event.InteractionKind)),
			ProviderMessageID:      stringPointer(event.ProviderMessageID),
			ProviderConversationID: stringPointer(event.ProviderConversationID),
			ExternalUserID:         stringPointer(event.ExternalUserID),
			ExternalCreatedAt:      event.ExternalCreatedAt,
			ReceivedAt:             event.ReceivedAt,
			RawPayloadReference:    stored.Reference,
			PayloadHash:            stored.SHA256,
			SignatureVerified:      true,
			ProcessingState:        nonEmptyOr(dedupeState(businessID), "unresolved"),
			CreatedAt:              s.Now().UTC(),
			UpdatedAt:              s.Now().UTC(),
		})
		if recordErr != nil {
			return commands.WebhookAcceptedResult{}, externalDependencyError("inbound event could not be recorded", recordErr)
		}
		if !created {
			result.Duplicate = true
		}
		if resolveErr == nil && event.EventType == "delivery_status_changed" {
			if s.DeliveryStatuses == nil {
				return commands.WebhookAcceptedResult{}, appErrors.NotImplemented()
			}
			statusResult, statusErr := s.DeliveryStatuses.Apply(ctx, ports.DeliveryStatusDraft{InboundEventID: record.ID, BusinessID: connection.BusinessID, ConnectionID: connection.ID, ProviderRef: string(event.Provider), ProviderAccountRef: event.ProviderConnectionID, ProviderMessageID: event.ProviderMessageID, Status: event.DeliveryStatus, OccurredAt: event.ReceivedAt})
			if statusErr != nil {
				return commands.WebhookAcceptedResult{}, externalDependencyError("SocialAPI delivery status could not be applied", statusErr)
			}
			if statusResult.Duplicate {
				result.Duplicate = true
			} else if s.Realtime != nil {
				statusData, statusMarshalErr := json.Marshal(map[string]any{
					"provider_message_id": event.ProviderMessageID,
					"status":              event.DeliveryStatus,
					"occurred_at":         event.ReceivedAt,
				})
				if statusMarshalErr != nil {
					log.Printf("[Webhook] REALTIME_MARSHAL_FAILED business=%s message=%s err=%v",
						connection.BusinessID, event.ProviderMessageID, statusMarshalErr)
				} else {
					statusEvent := ports.RealtimeEvent{
						EventID:      uuid.NewString(),
						EventType:    "conversation.message_status_changed",
						BusinessID:   connection.BusinessID,
						ResourceType: "message",
						ResourceID:   event.ProviderMessageID,
						OccurredAt:   event.ReceivedAt,
						Data:         statusData,
					}
					if statusPublishErr := s.Realtime.Publish(ctx, statusEvent); statusPublishErr != nil {
						log.Printf("[Webhook] REALTIME_PUBLISH_FAILED business=%s message=%s event=conversation.message_status_changed err=%v",
							connection.BusinessID, event.ProviderMessageID, statusPublishErr)
					}
				}
			}
			continue
		}
		if resolveErr == nil {
			if s.Inbound == nil {
				return commands.WebhookAcceptedResult{}, appErrors.NotImplemented()
			}
			materialized, materializeErr := s.Inbound.Materialize(ctx, ports.ProviderInboundDraft{
				InboundEventID:         record.ID,
				BusinessID:             connection.BusinessID,
				ConnectionID:           connection.ID,
				ProviderRef:            string(event.Provider),
				ProviderEventID:        event.ProviderEventID,
				Channel:                string(event.Channel),
				ProviderAccountRef:     event.ProviderConnectionID,
				ProviderConversationID: event.ProviderConversationID,
				ExternalUserID:         event.ExternalUserID,
				ProviderMessageID:      event.ProviderMessageID,
				EventType:              event.EventType,
				InteractionKind:        string(event.InteractionKind),
				Text:                   event.Text,
				ExternalCreatedAt:      event.ExternalCreatedAt,
				ReceivedAt:             event.ReceivedAt,
				RawPayloadReference:    stored.Reference,
				PayloadHash:            stored.SHA256,
			})
			if materializeErr != nil {
				return commands.WebhookAcceptedResult{}, externalDependencyError("SocialAPI inbound event could not be materialized", materializeErr)
			}
			if materialized.Duplicate {
				result.Duplicate = true
				continue
			}
			// Per the SocialAPI docs (verified against docs.social-api.ai/guides/webhooks):
			// DM webhook payloads carry only `author.id` (WhatsApp wa_id /
			// Facebook PSID / Instagram IGSID). The customer's display_name and
			// picture are NOT in the webhook — they must be fetched via
			// GET /v1/inbox/conversations/{conversation_id}. This enrichment
			// runs BEFORE Automation/AutoReply so the AI context builder
			// picks up the customer's name for the response.
			//
			// Per-merchant isolation: the SocialAPI client (and its API key)
			// is shared across all merchants. The conversation_id from the
			// webhook is anchored to ONE merchant's account_id; we additionally
			// verify that the participant_id returned by SocialAPI matches the
			// ExternalUserID we extracted from the webhook payload. Any mismatch
			// is logged as a SECURITY warning and the update is skipped.
			//
			// Enrichment NEVER fails the webhook — every error path returns
			// 202 Accepted with the existing result. The webhook response
			// time stays bounded by the SocialAPI REST call (~1-2s).
			if s.Enricher != nil && s.Customers != nil && strings.TrimSpace(materialized.CustomerID) != "" && strings.TrimSpace(event.ProviderConversationID) != "" {
				enrichCustomerProfile(ctx, s, connection.BusinessID, materialized.CustomerID, event.ProviderConversationID, event.ExternalUserID)
			}
			if s.Realtime != nil {
				msgData, marshalErr := json.Marshal(map[string]any{
					"message_id":          materialized.CommunicationMessageID,
					"conversation_id":     materialized.ConversationID,
					"direction":           "inbound",
					"origin":              "customer",
					"text":                event.Text,
					"provider_message_id": event.ProviderMessageID,
					"channel":             string(event.Channel),
					"received_at":         event.ReceivedAt,
				})
				if marshalErr != nil {
					log.Printf("[Webhook] REALTIME_MARSHAL_FAILED business=%s conversation=%s err=%v",
						connection.BusinessID, materialized.ConversationID, marshalErr)
				} else if publishErr := s.Realtime.Publish(ctx, ports.RealtimeEvent{
					EventID:      uuid.NewString(),
					EventType:    "conversation.message_received",
					BusinessID:   connection.BusinessID,
					ResourceType: "conversation",
					ResourceID:   materialized.ConversationID,
					OccurredAt:   event.ReceivedAt,
					Data:         msgData,
				}); publishErr != nil {
					log.Printf("[Webhook] REALTIME_PUBLISH_FAILED business=%s conversation=%s event=conversation.message_received err=%v",
						connection.BusinessID, materialized.ConversationID, publishErr)
				}
			}
			if s.Automation != nil && event.EventType == "interaction_received" && event.Direction == channel.DirectionInbound && event.Origin == channel.OriginCustomer && strings.TrimSpace(event.ProviderMessageID) != "" {
				if _, automationErr := s.Automation.Handle(ctx, commands.ApplyInboundAutomationCommand{BusinessID: commands.BusinessID(connection.BusinessID), ConversationID: commands.ConversationID(materialized.ConversationID), InboundEventID: commands.ID(record.ID), Channel: string(event.Channel), Text: event.Text}); automationErr != nil {
					return commands.WebhookAcceptedResult{}, externalDependencyError("SocialAPI inbound automation could not be executed", automationErr)
				}
			}
			if s.AutoReply != nil && event.EventType == "interaction_received" && event.Direction == channel.DirectionInbound && event.Origin == channel.OriginCustomer && strings.TrimSpace(event.ProviderMessageID) != "" && strings.TrimSpace(event.Text) != "" {
				// Per AIUsageTokenTelemetry.md §19: AI Cost Protection —
				// if the business's active subscription has budget_status ==
				// EXCEEDED, no new Auto AI Execution starts. Human replies,
				// dashboard, customer data, leads, orders, and channel
				// reception continue to work normally.
				if s.AICostProtectionChecker == nil {
					log.Printf("[Webhook] AUTO_REPLY_BLOCKED business=%s conversation=%s reason=ai_cost_protection_unavailable", connection.BusinessID, materialized.ConversationID)
					continue
				}
				allowed, reason := s.AICostProtectionChecker.IsAIExecutionAllowed(ctx, connection.BusinessID)
				if !allowed {
					log.Printf("[Webhook] AUTO_REPLY_BLOCKED business=%s conversation=%s reason=%s", connection.BusinessID, materialized.ConversationID, reason)
					continue
				}
				log.Printf("[Webhook] AUTO_REPLY_TRIGGER business=%s conversation=%s text=%q", connection.BusinessID, materialized.ConversationID, truncate(event.Text, 60))
				// Run AutoReply in a detached context with a generous timeout.
				// The HTTP request context (ctx) gets cancelled when SocialAPI
				// disconnects or when the HTTP server's WriteTimeout fires.
				// If we pass ctx to AutoReply.Handle, the Gemini API call
				// fails with "context canceled" mid-flight.
				//
				// Per contract ⑨ §11: every external operation (Gemini call)
				// must have a Timeout — but that timeout should be generous
				// enough for the full AutoReply flow (context build + Gemini
				// call + validation + outbound message creation).
				//
				// Per ADR-014 (At-Least-Once + Idempotency): the webhook
				// handler returns 202 Accepted immediately; AutoReply runs
				// asynchronously. The outbox pattern ensures the reply is
				// delivered even if the webhook handler has already returned.
				autoReplyCmd := commands.AutoReplyCommand{
					Meta:                   commands.CommandMeta{Actor: commands.ActorContext{BusinessID: commands.BusinessID(connection.BusinessID)}},
					ConversationID:         commands.ConversationID(materialized.ConversationID),
					SourceMessageReference: event.ProviderMessageID,
					Text:                   event.Text,
					Channel:                string(event.Channel),
					ProviderRef:            string(event.Provider),
				}
				// Per P1-10: prefer the bounded AutoReplyWorkerPool when
				// wired. The pool enforces a configurable concurrency
				// limit so a viral DM burst doesn't spawn unbounded
				// goroutines — each consuming Gemini quota + DB conns.
				// When nil (deprecated), falls back to the legacy
				// per-DM goroutine (no concurrency bound).
				if s.AutoReplyWorkerPool != nil {
					task := autoReplyTask{
						cmd:              autoReplyCmd,
						businessID:       connection.BusinessID,
						conversationID:   materialized.ConversationID,
						handler:          s.AutoReply,
						executionTimeout: 120 * time.Second,
					}
					// Per Item 5: Submit is NON-BLOCKING. If the queue is
					// full, the task is dropped immediately + the webhook
					// returns 202 to the caller. The previous implementation
					// blocked for up to 30s — that made the webhook synchronous
					// + caused upstream timeouts. Dropping is the correct
					// backpressure signal (the merchant's customer sees no AI
					// reply, but the webhook stays responsive).
					if !s.AutoReplyWorkerPool.Submit(task) {
						log.Printf("[Webhook] AUTO_REPLY_QUEUE_FULL business=%s conversation=%s — worker pool queue saturated, dropping task (per Item 5 non-blocking submit)", connection.BusinessID, materialized.ConversationID)
					}
				} else {
					// Legacy unbounded path — kept for backward compat
					// with tests that don't wire a worker pool.
					go func(cmd commands.AutoReplyCommand, businessID, conversationID string) {
						// Per audit B-CRIT-1: a panic in this detached goroutine
						// would crash the entire API process. AutoReply calls
						// Gemini HTTP + Postgres writes + catalog batch logic —
						// any of those can panic on adversarial input. Wrap the
						// entire goroutine in recover so a single bad payload
						// doesn't take down the webhook service.
						defer func() {
							if r := recover(); r != nil {
								log.Printf("[Webhook] AUTO_REPLY_PANIC business=%s conversation=%s recovered=%v", businessID, conversationID, r)
							}
						}()
						autoReplyCtx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
						defer cancel()
						if _, autoReplyErr := s.AutoReply.Handle(autoReplyCtx, cmd); autoReplyErr != nil {
							log.Printf("[Webhook] AUTO_REPLY_ERROR business=%s conversation=%s err=%v", businessID, conversationID, autoReplyErr)
						}
					}(autoReplyCmd, connection.BusinessID, materialized.ConversationID)
				}
			}
		}

	}
	return result, nil
}

func validateWebhookCommand(command commands.IngestWebhookCommand) error {
	if strings.TrimSpace(command.RouteKey) == "" {
		return appErrors.New(appErrors.CodeValidation, "webhook route key is required")
	}
	if len(command.RawPayload) == 0 {
		return appErrors.New(appErrors.CodeValidation, "webhook raw payload is required")
	}
	return nil
}

func webhookAuthenticationError(err error) error {
	return &appErrors.Error{Code: appErrors.CodeUnauthenticated, Message: "webhook signature verification failed", Cause: err}
}

func externalDependencyError(message string, err error) error {
	return &appErrors.Error{Code: appErrors.CodeExternalDependency, Message: message, Retryable: true, Cause: err}
}

func repositoryErrorKind(err error) string {
	var classified interface{ ErrorKind() string }
	if errors.As(err, &classified) {
		return classified.ErrorKind()
	}
	return ""
}

func dedupeState(businessID *string) string {
	if businessID != nil {
		return "received"
	}
	return "unresolved"
}

func stringPointer(value string) *string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	return &value
}

func nonEmptyOr(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

// enrichCustomerProfile fetches the customer's display_name and picture from
// the provider's REST API and merges them into the local customer record.
//
// This is a best-effort enrichment: every failure path logs and returns
// silently (the webhook already returned 202 via the caller). The function
// must NOT propagate errors to the webhook handler — enrichment is not
// part of the webhook's transactional guarantee.
//
// Tracing: every branch emits a log line tagged `[Webhook] CUSTOMER_ENRICH_*`
// with business_id, conversation_id (provider-side), and customer_id (local
// UUID). This makes enrichment failures debuggable from logs alone.
//
// Per-merchant isolation: the customer record is filtered by
// (businessID, customerID) — both come from the local Materialize() result,
// never from the provider response. The participant_id returned by the
// provider is verified against the webhook's ExternalUserID as a defensive
// check against any cross-merchant contamination in the shared-API-key model.
func enrichCustomerProfile(ctx context.Context, s SocialAPIWebhookService, businessID, customerID, providerConversationID, externalUserID string) {
	profile, err := s.Enricher.GetConversationProfile(ctx, providerConversationID)
	if err != nil {
		log.Printf("[Webhook] CUSTOMER_ENRICH_FETCH_FAILED business=%s customer=%s conversation=%s err=%v",
			businessID, customerID, providerConversationID, err)
		return
	}
	// Defensive per-merchant check: the participant_id returned by SocialAPI
	// MUST match the ExternalUserID we extracted from the webhook. A mismatch
	// would indicate either (a) a race condition where the conversation was
	// re-bound to a different customer on SocialAPI's side, or (b) a cross-
	// merchant leak — both are unacceptable. Log + skip the update.
	if strings.TrimSpace(profile.ParticipantID) != "" && strings.TrimSpace(externalUserID) != "" && profile.ParticipantID != externalUserID {
		log.Printf("[Webhook] CUSTOMER_ENRICH_ID_MISMATCH business=%s customer=%s conversation=%s webhook_user=%q provider_user=%q — skipping update",
			businessID, customerID, providerConversationID, externalUserID, profile.ParticipantID)
		return
	}
	if strings.TrimSpace(profile.ParticipantName) == "" && strings.TrimSpace(profile.ParticipantPicture) == "" {
		// SocialAPI returned no name and no picture — common for WhatsApp
		// users who have no profile name set. Nothing to merge.
		log.Printf("[Webhook] CUSTOMER_ENRICH_EMPTY business=%s customer=%s conversation=%s participant=%q — no name/picture to merge",
			businessID, customerID, providerConversationID, profile.ParticipantID)
		return
	}
	mergeErr := s.Customers.MergeProfile(ctx, ports.CustomerProfileMerge{
		BusinessID:     businessID,
		CustomerID:     customerID,
		DisplayName:    profile.ParticipantName,
		Picture:        profile.ParticipantPicture,
		ExternalUserID: profile.ParticipantID,
	})
	if mergeErr != nil {
		log.Printf("[Webhook] CUSTOMER_ENRICH_MERGE_FAILED business=%s customer=%s conversation=%s name=%q err=%v",
			businessID, customerID, providerConversationID, profile.ParticipantName, mergeErr)
		return
	}
	log.Printf("[Webhook] CUSTOMER_ENRICH_OK business=%s customer=%s conversation=%s participant=%q name=%q",
		businessID, customerID, providerConversationID, profile.ParticipantID, truncate(profile.ParticipantName, 60))
}

var _ commands.IngestSocialAPIWebhookHandler = SocialAPIWebhookService{}
