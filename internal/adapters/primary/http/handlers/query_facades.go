package handlers

import (
	"context"
	"encoding/json"
	"strconv"

	"github.com/Ammar777782439/mujeeb24-backend-go/internal/adapters/primary/http/contract"
	"github.com/Ammar777782439/mujeeb24-backend-go/internal/application/commands"
	appErrors "github.com/Ammar777782439/mujeeb24-backend-go/internal/application/errors"
	"github.com/Ammar777782439/mujeeb24-backend-go/internal/application/queries"
)

func (s *Server) dispatchAdditionalQuery(ctx context.Context, operationID string, input any) (any, bool) {
	if result, handled := s.dispatchTeamQuery(ctx, operationID, input); handled {
		return result, true
	}
	switch operationID {
	case "getBusinessPolicy":
		in := input.(*contract.BusinessPath)
		if s.deps.GetBusinessPolicy == nil {
			return mapApplicationError(appErrors.NotImplemented()), true
		}
		actor, err := s.requireScope(ctx, in.BusinessID)
		if err != nil {
			return mapApplicationError(err), true
		}
		view, err := s.deps.GetBusinessPolicy.Handle(ctx, queries.GetBusinessPolicyQuery{Meta: queryMeta(actor, "", "")})
		if err != nil {
			return mapApplicationError(err), true
		}
		out := &contract.Single[contract.BusinessPolicy]{}
		out.Body.Data = businessPolicyProjection(view)
		return out, true
	case "getDashboardOverview":
		in := input.(*contract.BusinessPath)
		if s.deps.GetDashboardOverview == nil {
			return mapApplicationError(appErrors.NotImplemented()), true
		}
		actor, err := s.requireScope(ctx, in.BusinessID)
		if err != nil {
			return mapApplicationError(err), true
		}
		view, err := s.deps.GetDashboardOverview.Handle(ctx, queries.GetDashboardOverviewQuery{Meta: queryMeta(actor, "", "")})
		if err != nil {
			return mapApplicationError(err), true
		}
		out := &contract.Single[contract.DashboardOverview]{}
		out.Body.Data = contract.DashboardOverview{OpenConversations: view.OpenConversations, WaitingHuman: view.WaitingHuman, NewCustomers: view.NewCustomers, NewLeads: view.NewLeads, TransactionsNeedingReview: view.TransactionsNeedingReview}
		return out, true
	case "listChannelConnections":
		in := input.(*contract.ConnectionListInput)
		if s.deps.ListChannelConnections == nil {
			return mapApplicationError(appErrors.NotImplemented()), true
		}
		actor, err := s.requireScope(ctx, in.BusinessID)
		if err != nil {
			return mapApplicationError(err), true
		}
		view, err := s.deps.ListChannelConnections.Handle(ctx, queries.ListChannelConnectionsQuery{Meta: queryMeta(actor, "", ""), Limit: in.Limit, Cursor: in.Cursor, Status: in.Status, Channel: in.Channel})
		if err != nil {
			return mapApplicationError(err), true
		}
		return channelConnectionList(view), true
	case "getChannelConnection":
		in := input.(*contract.ConnectionPath)
		if s.deps.GetChannelConnection == nil {
			return mapApplicationError(appErrors.NotImplemented()), true
		}
		actor, err := s.requireScope(ctx, in.BusinessID)
		if err != nil {
			return mapApplicationError(err), true
		}
		view, err := s.deps.GetChannelConnection.Handle(ctx, queries.GetChannelConnectionQuery{Meta: queryMeta(actor, "", ""), ConnectionID: commands.ConnectionID(in.ConnectionID)})
		if err != nil {
			return mapApplicationError(err), true
		}
		out := &contract.Single[contract.ChannelConnection]{}
		out.Body.Data = channelConnectionProjection(view)
		return out, true
	case "getConnectionCapabilities":
		in := input.(*contract.ConnectionPath)
		if s.deps.GetConnectionCapabilities == nil {
			return mapApplicationError(appErrors.NotImplemented()), true
		}
		actor, err := s.requireScope(ctx, in.BusinessID)
		if err != nil {
			return mapApplicationError(err), true
		}
		view, err := s.deps.GetConnectionCapabilities.Handle(ctx, queries.GetConnectionCapabilitiesQuery{Meta: queryMeta(actor, "", ""), ConnectionID: commands.ConnectionID(in.ConnectionID)})
		if err != nil {
			return mapApplicationError(err), true
		}
		return capabilityList(view), true
	case "listConversationMessages":
		in := input.(*contract.ConversationMessageListInput)
		if s.deps.ListConversationMessages == nil {
			return mapApplicationError(appErrors.NotImplemented()), true
		}
		actor, err := s.requireScope(ctx, in.BusinessID)
		if err != nil {
			return mapApplicationError(err), true
		}
		view, err := s.deps.ListConversationMessages.Handle(ctx, queries.ListConversationMessagesQuery{Meta: queryMeta(actor, "", ""), ConversationID: commands.ConversationID(in.ConversationID), Limit: in.Limit, Cursor: in.Cursor})
		if err != nil {
			return mapApplicationError(err), true
		}
		return messageList(view), true
	case "listCannedReplies":
		in := input.(*contract.CannedReplyListInput)
		if s.deps.ListCannedReplies == nil {
			return mapApplicationError(appErrors.NotImplemented()), true
		}
		actor, err := s.requireScope(ctx, in.BusinessID)
		if err != nil {
			return mapApplicationError(err), true
		}
		view, err := s.deps.ListCannedReplies.Handle(ctx, queries.ListCannedRepliesQuery{Meta: queryMeta(actor, "", ""), Status: in.Status, Limit: in.Limit, Cursor: in.Cursor})
		if err != nil {
			return mapApplicationError(err), true
		}
		return cannedReplyList(view), true
	case "listAutomationRules":
		in := input.(*contract.AutomationRuleListInput)
		if s.deps.ListAutomationRules == nil {
			return mapApplicationError(appErrors.NotImplemented()), true
		}
		actor, err := s.requireScope(ctx, in.BusinessID)
		if err != nil {
			return mapApplicationError(err), true
		}
		view, err := s.deps.ListAutomationRules.Handle(ctx, queries.ListAutomationRulesQuery{Meta: queryMeta(actor, "", ""), Status: in.Status, Limit: in.Limit, Cursor: in.Cursor})
		if err != nil {
			return mapApplicationError(err), true
		}
		return automationRuleList(view), true
	case "getCustomer":
		in := input.(*contract.CustomerInput)
		if s.deps.GetCustomer == nil {
			return mapApplicationError(appErrors.NotImplemented()), true
		}
		actor, err := s.requireScope(ctx, in.BusinessID)
		if err != nil {
			return mapApplicationError(err), true
		}
		view, err := s.deps.GetCustomer.Handle(ctx, queries.GetCustomerQuery{Meta: queryMeta(actor, "", ""), CustomerID: commands.CustomerID(in.CustomerID)})
		if err != nil {
			return mapApplicationError(err), true
		}
		out := &contract.Single[contract.Customer]{}
		out.Body.Data = customerProjection(view)
		return out, true
	case "listCustomerConversations":
		in := input.(*contract.CustomerConversationsInput)
		if s.deps.ListCustomerConversations == nil {
			return mapApplicationError(appErrors.NotImplemented()), true
		}
		actor, err := s.requireScope(ctx, in.BusinessID)
		if err != nil {
			return mapApplicationError(err), true
		}
		view, err := s.deps.ListCustomerConversations.Handle(ctx, queries.ListCustomerConversationsQuery{Meta: queryMeta(actor, "", ""), CustomerID: commands.CustomerID(in.CustomerID), Limit: in.Limit, Cursor: in.Cursor})
		if err != nil {
			return mapApplicationError(err), true
		}
		return conversationList(view), true
	case "listCustomerTransactions":
		in := input.(*contract.CustomerTransactionsInput)
		if s.deps.ListCustomerTransactions == nil {
			return mapApplicationError(appErrors.NotImplemented()), true
		}
		actor, err := s.requireScope(ctx, in.BusinessID)
		if err != nil {
			return mapApplicationError(err), true
		}
		view, err := s.deps.ListCustomerTransactions.Handle(ctx, queries.ListCustomerTransactionsQuery{Meta: queryMeta(actor, "", ""), CustomerID: commands.CustomerID(in.CustomerID), Limit: in.Limit, Cursor: in.Cursor})
		if err != nil {
			return mapApplicationError(err), true
		}
		return transactionList(view), true
	case "listCatalogs":
		in := input.(*contract.CatalogListInput)
		if s.deps.ListCatalogs == nil {
			return mapApplicationError(appErrors.NotImplemented()), true
		}
		actor, err := s.requireScope(ctx, in.BusinessID)
		if err != nil {
			return mapApplicationError(err), true
		}
		view, err := s.deps.ListCatalogs.Handle(ctx, queries.ListCatalogsQuery{Meta: queryMeta(actor, "", ""), Limit: in.Limit, Cursor: in.Cursor, Status: in.Status})
		if err != nil {
			return mapApplicationError(err), true
		}
		return catalogList(view), true
	case "getCatalog":
		in := input.(*contract.CatalogPath)
		if s.deps.GetCatalog == nil {
			return mapApplicationError(appErrors.NotImplemented()), true
		}
		actor, err := s.requireScope(ctx, in.BusinessID)
		if err != nil {
			return mapApplicationError(err), true
		}
		view, err := s.deps.GetCatalog.Handle(ctx, queries.GetCatalogQuery{Meta: queryMeta(actor, "", ""), CatalogID: commands.CatalogID(in.CatalogID)})
		if err != nil {
			return mapApplicationError(err), true
		}
		out := &contract.Single[contract.Catalog]{}
		out.Body.Data = catalogProjection(view)
		return out, true
	case "getCatalogEntityContract":
		// Per contract ⑤ §17, the Catalog Entity Contract is global
		// knowledge (same for every business). The BusinessID is used
		// only for auth scope validation; the returned value is
		// independent of which business asks.
		in := input.(*contract.CatalogEntityContractPath)
		if s.deps.GetCatalogEntityContract == nil {
			return mapApplicationError(appErrors.NotImplemented()), true
		}
		actor, err := s.requireScope(ctx, in.BusinessID)
		if err != nil {
			return mapApplicationError(err), true
		}
		view, err := s.deps.GetCatalogEntityContract.Handle(ctx, queries.GetCatalogEntityContractQuery{Meta: queryMeta(actor, "", "")})
		if err != nil {
			return mapApplicationError(err), true
		}
		out := &contract.Single[contract.CatalogEntityContract]{}
		out.Body.Data = contract.CatalogEntityContract{
			PricingModes:              view.PricingModes,
			AvailabilityModes:         view.AvailabilityModes,
			AvailabilityStatuses:      view.AvailabilityStatuses,
			PriceVerificationStatuses: view.PriceVerificationStatuses,
			FulfillmentModes:          view.FulfillmentModes,
			ItemStatuses:              view.ItemStatuses,
		}
		return out, true
	case "listCatalogItems":
		in := input.(*contract.CatalogItemsInput)
		if s.deps.ListCatalogItems == nil {
			return mapApplicationError(appErrors.NotImplemented()), true
		}
		actor, err := s.requireScope(ctx, in.BusinessID)
		if err != nil {
			return mapApplicationError(err), true
		}
		view, err := s.deps.ListCatalogItems.Handle(ctx, queries.ListCatalogItemsQuery{Meta: queryMeta(actor, "", ""), CatalogID: commands.CatalogID(in.CatalogID), Limit: in.Limit, Cursor: in.Cursor, Search: in.Search, Status: in.Status})
		if err != nil {
			return mapApplicationError(err), true
		}
		return catalogItemList(view), true
	case "getCatalogItem":
		in := input.(*contract.CatalogItemPath)
		if s.deps.GetCatalogItem == nil {
			return mapApplicationError(appErrors.NotImplemented()), true
		}
		actor, err := s.requireScope(ctx, in.BusinessID)
		if err != nil {
			return mapApplicationError(err), true
		}
		view, err := s.deps.GetCatalogItem.Handle(ctx, queries.GetCatalogItemQuery{Meta: queryMeta(actor, "", ""), CatalogID: commands.CatalogID(in.CatalogID), ItemID: commands.CatalogItemID(in.ItemID)})
		if err != nil {
			return mapApplicationError(err), true
		}
		out := &contract.Single[contract.CatalogItem]{}
		out.Body.Data = catalogItemProjection(view)
		return out, true
	case "listOffers":
		in := input.(*contract.ItemOffersInput)
		if s.deps.ListOffers == nil {
			return mapApplicationError(appErrors.NotImplemented()), true
		}
		actor, err := s.requireScope(ctx, in.BusinessID)
		if err != nil {
			return mapApplicationError(err), true
		}
		view, err := s.deps.ListOffers.Handle(ctx, queries.ListOffersQuery{Meta: queryMeta(actor, "", ""), ItemID: commands.CatalogItemID(in.ItemID), Limit: in.Limit, Cursor: in.Cursor, Status: in.Status})
		if err != nil {
			return mapApplicationError(err), true
		}
		return offerList(view), true
	case "listVariants":
		in := input.(*contract.ItemVariantsInput)
		if s.deps.ListVariants == nil {
			return mapApplicationError(appErrors.NotImplemented()), true
		}
		actor, err := s.requireScope(ctx, in.BusinessID)
		if err != nil {
			return mapApplicationError(err), true
		}
		view, err := s.deps.ListVariants.Handle(ctx, queries.ListVariantsQuery{Meta: queryMeta(actor, "", ""), ItemID: commands.CatalogItemID(in.ItemID), Limit: in.Limit, Cursor: in.Cursor, Status: in.Status})
		if err != nil {
			return mapApplicationError(err), true
		}
		return variantList(view), true
	case "listAttributeSchemas":
		in := input.(*contract.AttributeSchemasInput)
		if s.deps.ListAttributeSchemas == nil {
			return mapApplicationError(appErrors.NotImplemented()), true
		}
		actor, err := s.requireScope(ctx, in.BusinessID)
		if err != nil {
			return mapApplicationError(err), true
		}
		var version *int
		if in.Version > 0 {
			value := in.Version
			version = &value
		}
		view, err := s.deps.ListAttributeSchemas.Handle(ctx, queries.ListAttributeSchemasQuery{Meta: queryMeta(actor, "", ""), Limit: in.Limit, Cursor: in.Cursor, Name: in.Name, Version: version})
		if err != nil {
			return mapApplicationError(err), true
		}
		return attributeSchemaList(view), true
	case "getAttributeSchema":
		in := input.(*contract.SchemaPath)
		if s.deps.GetAttributeSchema == nil {
			return mapApplicationError(appErrors.NotImplemented()), true
		}
		actor, err := s.requireScope(ctx, in.BusinessID)
		if err != nil {
			return mapApplicationError(err), true
		}
		view, err := s.deps.GetAttributeSchema.Handle(ctx, queries.GetAttributeSchemaQuery{Meta: queryMeta(actor, "", ""), SchemaID: commands.AttributeSchemaID(in.SchemaID)})
		if err != nil {
			return mapApplicationError(err), true
		}
		out := &contract.Single[contract.AttributeSchema]{}
		out.Body.Data = attributeSchemaProjection(view)
		return out, true
	case "listLeads":
		in := input.(*contract.LeadListInput)
		if s.deps.ListLeads == nil {
			return mapApplicationError(appErrors.NotImplemented()), true
		}
		actor, err := s.requireScope(ctx, in.BusinessID)
		if err != nil {
			return mapApplicationError(err), true
		}
		view, err := s.deps.ListLeads.Handle(ctx, queries.ListLeadsQuery{Meta: queryMeta(actor, "", ""), Limit: in.Limit, Cursor: in.Cursor, Status: in.Status, CustomerID: optionalCustomerID(in.CustomerID), ScoreBand: in.ScoreBand})
		if err != nil {
			return mapApplicationError(err), true
		}
		return leadList(view), true
	case "getLead":
		in := input.(*contract.LeadPath)
		if s.deps.GetLead == nil {
			return mapApplicationError(appErrors.NotImplemented()), true
		}
		actor, err := s.requireScope(ctx, in.BusinessID)
		if err != nil {
			return mapApplicationError(err), true
		}
		view, err := s.deps.GetLead.Handle(ctx, queries.GetLeadQuery{Meta: queryMeta(actor, "", ""), LeadID: commands.LeadID(in.LeadID)})
		if err != nil {
			return mapApplicationError(err), true
		}
		out := &contract.Single[contract.Lead]{}
		out.Body.Data = leadProjection(view)
		return out, true
	case "listLeadAttributions":
		in := input.(*contract.LeadAttributionsInput)
		if s.deps.ListLeadAttributions == nil {
			return mapApplicationError(appErrors.NotImplemented()), true
		}
		actor, err := s.requireScope(ctx, in.BusinessID)
		if err != nil {
			return mapApplicationError(err), true
		}
		view, err := s.deps.ListLeadAttributions.Handle(ctx, queries.ListLeadAttributionsQuery{Meta: queryMeta(actor, "", ""), LeadID: commands.LeadID(in.LeadID), Limit: in.Limit, Cursor: in.Cursor})
		if err != nil {
			return mapApplicationError(err), true
		}
		return leadAttributionList(view), true
	case "listLeadScores":
		in := input.(*contract.LeadScoresInput)
		if s.deps.ListLeadScores == nil {
			return mapApplicationError(appErrors.NotImplemented()), true
		}
		actor, err := s.requireScope(ctx, in.BusinessID)
		if err != nil {
			return mapApplicationError(err), true
		}
		view, err := s.deps.ListLeadScores.Handle(ctx, queries.ListLeadScoresQuery{Meta: queryMeta(actor, "", ""), LeadID: commands.LeadID(in.LeadID), Limit: in.Limit, Cursor: in.Cursor})
		if err != nil {
			return mapApplicationError(err), true
		}
		return leadScoreList(view), true
	case "listTransactions":
		in := input.(*contract.TransactionListInput)
		if s.deps.ListTransactions == nil {
			return mapApplicationError(appErrors.NotImplemented()), true
		}
		actor, err := s.requireScope(ctx, in.BusinessID)
		if err != nil {
			return mapApplicationError(err), true
		}
		view, err := s.deps.ListTransactions.Handle(ctx, queries.ListTransactionsQuery{Meta: queryMeta(actor, "", ""), Limit: in.Limit, Cursor: in.Cursor, State: in.State, TransactionType: in.TransactionType, CustomerID: optionalCustomerID(in.CustomerID)})
		if err != nil {
			return mapApplicationError(err), true
		}
		return transactionList(view), true
	case "getTransaction":
		in := input.(*contract.TransactionPath)
		if s.deps.GetTransaction == nil {
			return mapApplicationError(appErrors.NotImplemented()), true
		}
		actor, err := s.requireScope(ctx, in.BusinessID)
		if err != nil {
			return mapApplicationError(err), true
		}
		view, err := s.deps.GetTransaction.Handle(ctx, queries.GetTransactionQuery{Meta: queryMeta(actor, "", ""), TransactionID: commands.TransactionID(in.TransactionID)})
		if err != nil {
			return mapApplicationError(err), true
		}
		out := &contract.Single[contract.CommercialTransaction]{}
		out.Body.Data = transactionProjection(view)
		return out, true
	case "getTransactionReview":
		in := input.(*contract.TransactionPath)
		if s.deps.GetTransactionReview == nil {
			return mapApplicationError(appErrors.NotImplemented()), true
		}
		actor, err := s.requireScope(ctx, in.BusinessID)
		if err != nil {
			return mapApplicationError(err), true
		}
		view, err := s.deps.GetTransactionReview.Handle(ctx, queries.GetTransactionReviewQuery{Meta: queryMeta(actor, "", ""), TransactionID: commands.TransactionID(in.TransactionID)})
		if err != nil {
			return mapApplicationError(err), true
		}
		out := &contract.Single[contract.TransactionReview]{}
		out.Body.Data = contract.TransactionReview{Required: view.Required, Status: view.Status, ReasonCodes: view.ReasonCodes, ReviewerReference: optionalString(view.ReviewerReference)}
		return out, true
	case "listAIDecisions":
		in := input.(*contract.AIDecisionListInput)
		if s.deps.ListAIDecisions == nil {
			return mapApplicationError(appErrors.NotImplemented()), true
		}
		actor, err := s.requireScope(ctx, in.BusinessID)
		if err != nil {
			return mapApplicationError(err), true
		}
		var requiresHuman *bool
		if in.RequiresHuman.Present {
			requiresHuman = &in.RequiresHuman.Value
		}
		view, err := s.deps.ListAIDecisions.Handle(ctx, queries.ListAIDecisionsQuery{Meta: queryMeta(actor, "", ""), Limit: in.Limit, Cursor: in.Cursor, Lifecycle: in.Lifecycle, ConversationID: optionalConversationID(in.ConversationID), RequiresHuman: requiresHuman})
		if err != nil {
			return mapApplicationError(err), true
		}
		return aiDecisionList(view), true
	case "getAIDecision":
		in := input.(*contract.DecisionPath)
		if s.deps.GetAIDecision == nil {
			return mapApplicationError(appErrors.NotImplemented()), true
		}
		actor, err := s.requireScope(ctx, in.BusinessID)
		if err != nil {
			return mapApplicationError(err), true
		}
		view, err := s.deps.GetAIDecision.Handle(ctx, queries.GetAIDecisionQuery{Meta: queryMeta(actor, "", ""), DecisionID: commands.AIDecisionID(in.DecisionID)})
		if err != nil {
			return mapApplicationError(err), true
		}
		out := &contract.Single[contract.AIDecision]{}
		out.Body.Data = aiDecisionProjection(view)
		return out, true
	case "listAuditEvents":
		in := input.(*contract.AuditListInput)
		if s.deps.ListAuditEvents == nil {
			return mapApplicationError(appErrors.NotImplemented()), true
		}
		actor, err := s.requireScope(ctx, in.BusinessID)
		if err != nil {
			return mapApplicationError(err), true
		}
		view, err := s.deps.ListAuditEvents.Handle(ctx, queries.ListAuditEventsQuery{Meta: queryMeta(actor, "", ""), Limit: in.Limit, Cursor: in.Cursor, ActorType: in.ActorType, Action: in.Action, ResourceType: in.ResourceType, From: in.From, Until: in.Until})
		if err != nil {
			return mapApplicationError(err), true
		}
		return auditEventList(view), true
	case "getAuditEvent":
		in := input.(*contract.AuditPath)
		if s.deps.GetAuditEvent == nil {
			return mapApplicationError(appErrors.NotImplemented()), true
		}
		actor, err := s.requireScope(ctx, in.BusinessID)
		if err != nil {
			return mapApplicationError(err), true
		}
		view, err := s.deps.GetAuditEvent.Handle(ctx, queries.GetAuditEventQuery{Meta: queryMeta(actor, "", ""), AuditEventID: commands.AuditEventID(in.AuditEventID)})
		if err != nil {
			return mapApplicationError(err), true
		}
		out := &contract.Single[contract.AuditEvent]{}
		out.Body.Data = auditEventProjection(view)
		return out, true
	default:
		return nil, false
	}
}

func businessPolicyProjection(v commands.BusinessPolicyView) contract.BusinessPolicy {
	return contract.BusinessPolicy{BusinessID: contract.UUID(v.BusinessID), AIMode: v.AIMode, DefaultHumanReview: v.DefaultHumanReview, AllowAutoReply: v.AllowAutoReply, AllowAutoLeadCreation: v.AllowAutoLeadCreation, AllowAutoTransactionDraft: v.AllowAutoTransactionDraft, AllowAutoConfirmation: v.AllowAutoConfirmation, ResourceVersion: string(v.ResourceVersion)}
}
func listPage[T any](items []T, cursor string, more bool) *contract.List[T] {
	out := &contract.List[T]{}
	out.Body.Data = items
	out.Body.Pagination = contract.Page{NextCursor: optionalString(cursor), HasMore: more}
	return out
}
func conversationList(v commands.ListResult[commands.ConversationView]) *contract.List[contract.Conversation] {
	items := make([]contract.Conversation, 0, len(v.Items))
	for _, item := range v.Items {
		items = append(items, conversationProjection(item))
	}
	return listPage(items, v.NextCursor, v.HasMore)
}
func messageList(v commands.ListResult[commands.MessageView]) *contract.List[contract.Message] {
	items := make([]contract.Message, 0, len(v.Items))
	for _, item := range v.Items {
		items = append(items, contract.Message{ID: contract.UUID(item.ID), ConversationID: contract.UUID(item.ConversationID), Direction: item.Direction, Origin: item.Origin, Status: item.Status, Text: item.Text, ProviderMessageReference: item.ProviderMessageReference, OccurredAt: item.OccurredAt, CreatedAt: item.CreatedAt, Private: item.Private})
	}
	return listPage(items, v.NextCursor, v.HasMore)
}
func transactionList(v commands.ListResult[commands.TransactionView]) *contract.List[contract.CommercialTransaction] {
	items := make([]contract.CommercialTransaction, 0, len(v.Items))
	for _, item := range v.Items {
		items = append(items, contract.CommercialTransaction{ID: contract.UUID(item.ID), BusinessID: contract.UUID(item.BusinessID), CustomerID: contract.UUID(item.CustomerID), TransactionType: item.TransactionType, State: item.State, ResourceVersion: string(item.ResourceVersion)})
	}
	return listPage(items, v.NextCursor, v.HasMore)
}
func channelProvisioningProjection(v commands.ChannelProvisioningView) contract.ChannelProvisioning {
	return contract.ChannelProvisioning{ID: contract.UUID(v.ID), BusinessID: contract.UUID(v.BusinessID), Provider: v.Provider, Channel: v.Channel, Status: v.Status, AuthorizationURL: v.AuthorizationURL}
}
func channelConnectionProjection(v commands.ChannelConnectionView) contract.ChannelConnection {
	return contract.ChannelConnection{ID: contract.UUID(v.ID), BusinessID: contract.UUID(v.BusinessID), Provider: v.Provider, Channel: v.Channel, Status: v.Status, ExternalAccountReference: optionalString(v.ExternalAccountReference), ResourceVersion: string(v.ResourceVersion)}
}
func conversationReadProjection(v commands.ConversationReadResult) contract.ConversationRead {
	var messageID *contract.UUID
	if v.LastReadMessageID != nil {
		id := contract.UUID(*v.LastReadMessageID)
		messageID = &id
	}
	return contract.ConversationRead{ConversationID: contract.UUID(v.ConversationID), LastReadMessageID: messageID, Status: v.Status}
}
func cannedReplyProjection(v commands.CannedReplyView) contract.CannedReply {
	return contract.CannedReply{ID: contract.UUID(v.ID), BusinessID: contract.UUID(v.BusinessID), Title: v.Title, Shortcut: v.Shortcut, Body: v.Body, Status: v.Status, ResourceVersion: string(v.ResourceVersion), CreatedAt: v.CreatedAt, UpdatedAt: v.UpdatedAt}
}
func singleCannedReply(v commands.CannedReplyView) *contract.Single[contract.CannedReply] {
	out := &contract.Single[contract.CannedReply]{}
	out.Body.Data = cannedReplyProjection(v)
	return out
}
func cannedReplyList(v commands.ListResult[commands.CannedReplyView]) *contract.List[contract.CannedReply] {
	items := make([]contract.CannedReply, 0, len(v.Items))
	for _, item := range v.Items {
		items = append(items, cannedReplyProjection(item))
	}
	return listPage(items, v.NextCursor, v.HasMore)
}
func automationRuleProjection(v commands.AutomationRuleView) contract.AutomationRule {
	return contract.AutomationRule{ID: contract.UUID(v.ID), BusinessID: contract.UUID(v.BusinessID), Name: v.Name, Status: v.Status, TriggerKind: v.TriggerKind, Conditions: jsonObject(v.Conditions), ActionKind: v.ActionKind, ActionPayload: jsonObject(v.ActionPayload), Position: v.Position, ResourceVersion: string(v.ResourceVersion), CreatedAt: v.CreatedAt, UpdatedAt: v.UpdatedAt}
}
func singleAutomationRule(v commands.AutomationRuleView) *contract.Single[contract.AutomationRule] {
	out := &contract.Single[contract.AutomationRule]{}
	out.Body.Data = automationRuleProjection(v)
	return out
}
func automationRuleList(v commands.ListResult[commands.AutomationRuleView]) *contract.List[contract.AutomationRule] {
	items := make([]contract.AutomationRule, 0, len(v.Items))
	for _, item := range v.Items {
		items = append(items, automationRuleProjection(item))
	}
	return listPage(items, v.NextCursor, v.HasMore)
}
func channelConnectionList(v commands.ListResult[commands.ChannelConnectionView]) *contract.List[contract.ChannelConnection] {
	items := make([]contract.ChannelConnection, 0, len(v.Items))
	for _, item := range v.Items {
		items = append(items, channelConnectionProjection(item))
	}
	return listPage(items, v.NextCursor, v.HasMore)
}
func capabilityList(v commands.ListResult[queries.ConnectionCapabilityView]) *contract.List[contract.Capability] {
	items := make([]contract.Capability, 0, len(v.Items))
	for _, item := range v.Items {
		evidence := optionalString(item.EvidenceSource)
		items = append(items, contract.Capability{Name: item.Name, Enabled: item.Enabled, CheckedAt: item.CheckedAt, EvidenceSource: evidence})
	}
	return listPage(items, v.NextCursor, v.HasMore)
}
func customerList(v commands.ListResult[commands.CustomerView]) *contract.List[contract.Customer] {
	items := make([]contract.Customer, 0, len(v.Items))
	for _, item := range v.Items {
		items = append(items, customerProjection(item))
	}
	return listPage(items, v.NextCursor, v.HasMore)
}
func catalogProjection(v commands.CatalogView) contract.Catalog {
	return contract.Catalog{ID: contract.UUID(v.ID), BusinessID: contract.UUID(v.BusinessID), Name: v.Name, Description: v.Description, Status: v.Status, CreatedAt: v.CreatedAt, UpdatedAt: v.UpdatedAt, ResourceVersion: string(v.ResourceVersion)}
}
func catalogList(v commands.ListResult[commands.CatalogView]) *contract.List[contract.Catalog] {
	items := make([]contract.Catalog, 0, len(v.Items))
	for _, item := range v.Items {
		items = append(items, catalogProjection(item))
	}
	return listPage(items, v.NextCursor, v.HasMore)
}
func catalogItemProjection(v commands.CatalogItemView) contract.CatalogItem {
	return contract.CatalogItem{ID: contract.UUID(v.ID), BusinessID: contract.UUID(v.BusinessID), CatalogID: contract.UUID(v.CatalogID), AttributeSchemaID: optionalAttributeSchemaUUID(v.AttributeSchemaID), AttributeSchemaVersion: v.AttributeSchemaVersion, ItemType: v.ItemType, Name: v.Name, ShortDescription: v.ShortDescription, LongDescription: v.LongDescription, Status: v.Status, PricingMode: v.PricingMode, AvailabilityMode: v.AvailabilityMode, FulfillmentMode: v.FulfillmentMode, RequiresConfirmation: v.RequiresConfirmation, Attributes: jsonObject(v.Attributes), ResourceVersion: string(v.ResourceVersion)}
}
func catalogItemList(v commands.ListResult[commands.CatalogItemView]) *contract.List[contract.CatalogItem] {
	items := make([]contract.CatalogItem, 0, len(v.Items))
	for _, item := range v.Items {
		items = append(items, catalogItemProjection(item))
	}
	return listPage(items, v.NextCursor, v.HasMore)
}
func offerProjection(v commands.OfferView) contract.Offer {
	return contract.Offer{ID: contract.UUID(v.ID), BusinessID: contract.UUID(v.BusinessID), CatalogItemID: contract.UUID(v.CatalogItemID), VariantID: optionalUUID(v.VariantID), Name: v.Name, PricingMode: v.PricingMode, Amount: decimalFloat(v.Amount), Currency: v.Currency, PricingUnit: v.PricingUnit, PriceSource: v.PriceSource, PriceVerificationStatus: v.PriceVerificationStatus, PriceCheckedAt: v.PriceCheckedAt, AvailabilityMode: v.AvailabilityMode, AvailabilitySource: v.AvailabilitySource, AvailabilityCheckedAt: v.AvailabilityCheckedAt, AvailabilityValidUntil: v.AvailabilityValidUntil, AvailabilityEvidenceRef: v.AvailabilityEvidenceRef, FulfillmentMode: v.FulfillmentMode, ValidityFrom: v.ValidityFrom, ValidityUntil: v.ValidityUntil, AvailabilityStatus: v.AvailabilityStatus, Status: v.Status, ResourceVersion: string(v.ResourceVersion)}
}
func offerList(v commands.ListResult[commands.OfferView]) *contract.List[contract.Offer] {
	items := make([]contract.Offer, 0, len(v.Items))
	for _, item := range v.Items {
		items = append(items, offerProjection(item))
	}
	return listPage(items, v.NextCursor, v.HasMore)
}
func variantProjection(v commands.VariantView) contract.Variant {
	return contract.Variant{ID: contract.UUID(v.ID), BusinessID: contract.UUID(v.BusinessID), CatalogItemID: contract.UUID(v.CatalogItemID), Name: v.Name, Attributes: jsonObject(v.Attributes), Status: v.Status, ResourceVersion: string(v.ResourceVersion)}
}
func variantList(v commands.ListResult[commands.VariantView]) *contract.List[contract.Variant] {
	items := make([]contract.Variant, 0, len(v.Items))
	for _, item := range v.Items {
		items = append(items, variantProjection(item))
	}
	return listPage(items, v.NextCursor, v.HasMore)
}
func attributeSchemaProjection(v commands.AttributeSchemaView) contract.AttributeSchema {
	definitions := make([]contract.AttributeDefinition, 0, len(v.Definitions))
	for _, item := range v.Definitions {
		definitions = append(definitions, contract.AttributeDefinition{ID: contract.UUID(item.ID), Key: item.Key, Label: item.Label, DataType: item.DataType, Required: item.Required, ValidationRules: item.ValidationRules, DisplayOrder: item.DisplayOrder})
	}
	return contract.AttributeSchema{ID: contract.UUID(v.ID), BusinessID: contract.UUID(v.BusinessID), Name: v.Name, Version: v.Version, Definitions: definitions}
}

func jsonObject(value []byte) map[string]any {
	if len(value) == 0 {
		return nil
	}
	var object map[string]any
	if err := json.Unmarshal(value, &object); err != nil {
		return nil
	}
	return object
}

func decimalFloat(value *string) *float64 {
	if value == nil {
		return nil
	}
	parsed, err := strconv.ParseFloat(*value, 64)
	if err != nil {
		return nil
	}
	return &parsed
}
func attributeSchemaList(v commands.ListResult[commands.AttributeSchemaView]) *contract.List[contract.AttributeSchema] {
	items := make([]contract.AttributeSchema, 0, len(v.Items))
	for _, item := range v.Items {
		items = append(items, attributeSchemaProjection(item))
	}
	return listPage(items, v.NextCursor, v.HasMore)
}
func optionalUUID(v commands.VariantID) *contract.UUID {
	if v == "" {
		return nil
	}
	id := contract.UUID(v)
	return &id
}
func optionalAttributeSchemaUUID(v *commands.AttributeSchemaID) *contract.UUID {
	if v == nil || *v == "" {
		return nil
	}
	id := contract.UUID(*v)
	return &id
}

func leadProjection(v commands.LeadView) contract.Lead {
	return contract.Lead{ID: contract.UUID(v.ID), BusinessID: contract.UUID(v.BusinessID), CustomerID: contract.UUID(v.CustomerID), Status: v.Status, ResourceVersion: string(v.ResourceVersion)}
}
func leadList(v commands.ListResult[commands.LeadView]) *contract.List[contract.Lead] {
	items := make([]contract.Lead, 0, len(v.Items))
	for _, item := range v.Items {
		items = append(items, leadProjection(item))
	}
	return listPage(items, v.NextCursor, v.HasMore)
}
func leadAttributionProjection(v queries.LeadAttributionView) contract.LeadAttribution {
	var conversationID *contract.UUID
	if v.SourceConversationID != nil {
		id := contract.UUID(*v.SourceConversationID)
		conversationID = &id
	}
	sourceChannel := optionalString(v.SourceChannel)
	return contract.LeadAttribution{ID: contract.UUID(v.ID), LeadID: contract.UUID(v.LeadID), SourceConversationID: conversationID, SourceChannel: sourceChannel}
}
func leadAttributionList(v commands.ListResult[queries.LeadAttributionView]) *contract.List[contract.LeadAttribution] {
	items := make([]contract.LeadAttribution, 0, len(v.Items))
	for _, item := range v.Items {
		items = append(items, leadAttributionProjection(item))
	}
	return listPage(items, v.NextCursor, v.HasMore)
}
func leadScoreProjection(v queries.LeadScoreView) contract.LeadScore {
	return contract.LeadScore{ID: contract.UUID(v.ID), LeadID: contract.UUID(v.LeadID), Value: v.Value, Band: v.Band}
}
func leadScoreList(v commands.ListResult[queries.LeadScoreView]) *contract.List[contract.LeadScore] {
	items := make([]contract.LeadScore, 0, len(v.Items))
	for _, item := range v.Items {
		items = append(items, leadScoreProjection(item))
	}
	return listPage(items, v.NextCursor, v.HasMore)
}
func transactionProjection(v commands.TransactionView) contract.CommercialTransaction {
	return contract.CommercialTransaction{ID: contract.UUID(v.ID), BusinessID: contract.UUID(v.BusinessID), CustomerID: contract.UUID(v.CustomerID), TransactionType: v.TransactionType, State: v.State, ResourceVersion: string(v.ResourceVersion)}
}
func aiDecisionProjection(v commands.AIDecisionView) contract.AIDecision {
	var conversationID *contract.UUID
	if v.ConversationID != nil {
		id := contract.UUID(*v.ConversationID)
		conversationID = &id
	}
	return contract.AIDecision{ID: contract.UUID(v.ID), BusinessID: contract.UUID(v.BusinessID), ConversationID: conversationID, IntentBase: v.IntentBase, Entities: jsonObject(v.Entities), EvidenceReferences: jsonStrings(v.EvidenceReferences), RequestedAction: v.RequestedAction, RequiresHuman: v.RequiresHuman, MissingInformation: jsonStrings(v.MissingInformation), PolicyVersion: v.PolicyVersion, Lifecycle: v.Lifecycle, CreatedAt: v.CreatedAt}
}
func aiDecisionList(v commands.ListResult[commands.AIDecisionView]) *contract.List[contract.AIDecision] {
	items := make([]contract.AIDecision, 0, len(v.Items))
	for _, item := range v.Items {
		items = append(items, aiDecisionProjection(item))
	}
	return listPage(items, v.NextCursor, v.HasMore)
}
func auditEventProjection(v commands.AuditEventView) contract.AuditEvent {
	return contract.AuditEvent{ID: contract.UUID(v.ID), BusinessID: contract.UUID(v.BusinessID), ActorType: v.ActorType, ActorReference: optionalString(v.ActorReference), Action: v.Action, ResourceType: v.ResourceType, ResourceID: optionalString(v.ResourceID), Metadata: jsonObject(v.Metadata), OccurredAt: v.OccurredAt}
}
func auditEventList(v commands.ListResult[commands.AuditEventView]) *contract.List[contract.AuditEvent] {
	items := make([]contract.AuditEvent, 0, len(v.Items))
	for _, item := range v.Items {
		items = append(items, auditEventProjection(item))
	}
	return listPage(items, v.NextCursor, v.HasMore)
}
func optionalConversationID(v contract.UUID) *commands.ConversationID {
	if v == "" {
		return nil
	}
	id := commands.ConversationID(v)
	return &id
}
func jsonStrings(value []byte) []string {
	if len(value) == 0 {
		return nil
	}
	var values []string
	if err := json.Unmarshal(value, &values); err != nil {
		return nil
	}
	return values
}
