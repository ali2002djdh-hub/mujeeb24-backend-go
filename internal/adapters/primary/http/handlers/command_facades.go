package handlers

import (
	"context"
	"math"
	"strconv"

	"github.com/Ammar777782439/mujeeb24-backend-go/internal/adapters/primary/http/contract"
	"github.com/Ammar777782439/mujeeb24-backend-go/internal/application/commands"
	appErrors "github.com/Ammar777782439/mujeeb24-backend-go/internal/application/errors"
)

func (s *Server) dispatchCommand(ctx context.Context, operationID string, input any) (any, bool) {
	if result, handled := s.dispatchSystemCommand(ctx, operationID, input); handled {
		return result, true
	}
	if result, handled := s.dispatchInboxCommand(ctx, operationID, input); handled {
		return result, true
	}
	if result, handled := s.dispatchConversationCommand(ctx, operationID, input); handled {
		return result, true
	}
	if result, handled := s.dispatchTeamCommand(ctx, operationID, input); handled {
		return result, true
	}
	switch operationID {
	case "updateBusinessProfile":
		in := input.(*contract.BusinessUpdateInput)
		actor, err := s.requireScope(ctx, in.BusinessID)
		if err != nil {
			return mapApplicationError(err), true
		}
		if s.deps.UpdateBusinessProfile == nil {
			return mapApplicationError(appErrors.NotImplemented()), true
		}
		result, err := s.deps.UpdateBusinessProfile.Handle(ctx, commands.UpdateBusinessProfileCommand{Meta: commandMeta(ctx, actor, in.CommandHeaders), Name: optionalStringPtr(in.Body.Name), VerticalType: optionalStringPtr(in.Body.VerticalType), Timezone: optionalStringPtr(in.Body.Timezone), DefaultCurrency: optionalStringPtr(in.Body.DefaultCurrency), Locale: optionalStringPtr(in.Body.Locale)})
		if err != nil {
			return mapApplicationError(err), true
		}
		out := &contract.Single[contract.Business]{}
		out.Body.Data = businessProjection(result.Business)
		return out, true
	case "updateBusinessPolicy":
		in := input.(*contract.BusinessPolicyInput)
		actor, err := s.requireScope(ctx, in.BusinessID)
		if err != nil {
			return mapApplicationError(err), true
		}
		if s.deps.UpdateBusinessPolicy == nil {
			return mapApplicationError(appErrors.NotImplemented()), true
		}
		body := in.Body
		result, err := s.deps.UpdateBusinessPolicy.Handle(ctx, commands.UpdateBusinessPolicyCommand{Meta: commandMeta(ctx, actor, in.CommandHeaders), AIMode: optionalStringPtr(body.AIMode), DefaultHumanReview: body.DefaultHumanReview, AllowAutoReply: body.AllowAutoReply, AllowAutoLeadCreation: body.AllowAutoLeadCreation, AllowAutoTransactionDraft: body.AllowAutoTransactionDraft, AllowAutoConfirmation: body.AllowAutoConfirmation})
		if err != nil {
			return mapApplicationError(err), true
		}
		out := &contract.Single[contract.BusinessPolicy]{}
		out.Body.Data = businessPolicyProjection(result.Policy)
		return out, true
	case "beginChannelConnection":
		in := input.(*contract.ConnectionCreateInput)
		actor, err := s.requireScope(ctx, in.BusinessID)
		if err != nil {
			return mapApplicationError(err), true
		}
		if s.deps.BeginChannelConnection == nil {
			return mapApplicationError(appErrors.NotImplemented()), true
		}
		result, err := s.deps.BeginChannelConnection.Handle(ctx, commands.BeginChannelConnectionCommand{Meta: commandMeta(ctx, actor, in.CommandHeaders), Provider: in.Body.Provider, Channel: in.Body.Channel, DisplayName: in.Body.DisplayName})
		if err != nil {
			return mapApplicationError(err), true
		}
		out := &contract.Single[contract.ChannelProvisioning]{}
		out.Body.Data = channelProvisioningProjection(result.Provisioning)
		return out, true
	case "reconnectChannel", "disconnectChannel":
		in := input.(*contract.ConnectionActionInput)
		actor, err := s.requireScope(ctx, in.BusinessID)
		if err != nil {
			return mapApplicationError(err), true
		}
		connectionID := commands.ConnectionID(in.ConnectionID)
		meta := commandMeta(ctx, actor, in.CommandHeaders)
		if operationID == "reconnectChannel" {
			if s.deps.ReconnectChannel == nil {
				return mapApplicationError(appErrors.NotImplemented()), true
			}
			result, err := s.deps.ReconnectChannel.Handle(ctx, commands.ReconnectChannelCommand{Meta: meta, ConnectionID: connectionID, Reason: in.Body.Reason})
			if err != nil {
				return mapApplicationError(err), true
			}
			out := &contract.Single[contract.ChannelConnection]{}
			out.Body.Data = channelConnectionProjection(result.Connection)
			return out, true
		}
		if s.deps.DisconnectChannel == nil {
			return mapApplicationError(appErrors.NotImplemented()), true
		}
		result, err := s.deps.DisconnectChannel.Handle(ctx, commands.DisconnectChannelCommand{Meta: meta, ConnectionID: connectionID, Reason: in.Body.Reason})
		if err != nil {
			return mapApplicationError(err), true
		}
		out := &contract.Single[contract.ChannelConnection]{}
		out.Body.Data = channelConnectionProjection(result.Connection)
		return out, true
	case "createCustomer":
		in := input.(*contract.CreateCustomerInput)
		actor, err := s.requireScope(ctx, in.BusinessID)
		if err != nil {
			return mapApplicationError(err), true
		}
		if s.deps.CreateCustomer == nil {
			return mapApplicationError(appErrors.NotImplemented()), true
		}
		result, err := s.deps.CreateCustomer.Handle(ctx, commands.CreateCustomerCommand{Meta: commandMeta(ctx, actor, contract.CommandHeaders{IdempotencyKey: in.IdempotencyKey, XRequestID: in.XRequestID}), Profile: in.Body.Profile, ContactPoints: contactPoints(in.Body.ContactPoints), LocalePreference: in.Body.LocalePreference})
		if err != nil {
			return mapApplicationError(err), true
		}
		return singleCustomer(result.Customer), true
	case "updateCustomer":
		in := input.(*contract.CustomerUpdateInput)
		actor, err := s.requireScope(ctx, in.BusinessID)
		if err != nil {
			return mapApplicationError(err), true
		}
		if s.deps.UpdateCustomer == nil {
			return mapApplicationError(appErrors.NotImplemented()), true
		}
		result, err := s.deps.UpdateCustomer.Handle(ctx, commands.UpdateCustomerCommand{Meta: commandMeta(ctx, actor, in.CommandHeaders), CustomerID: commands.CustomerID(in.CustomerID), Profile: in.Body.Profile, ContactPoints: contactPoints(in.Body.ContactPoints), LocalePreference: in.Body.LocalePreference})
		if err != nil {
			return mapApplicationError(err), true
		}
		return singleCustomer(result.Customer), true
	case "mergeCustomer":
		in := input.(*contract.CustomerMergeInput)
		actor, err := s.requireScope(ctx, in.BusinessID)
		if err != nil {
			return mapApplicationError(err), true
		}
		if s.deps.MergeCustomer == nil {
			return mapApplicationError(appErrors.NotImplemented()), true
		}
		result, err := s.deps.MergeCustomer.Handle(ctx, commands.MergeCustomerCommand{Meta: commandMeta(ctx, actor, in.CommandHeaders), CustomerID: commands.CustomerID(in.CustomerID), TargetCustomerID: commands.CustomerID(in.Body.TargetCustomerID), Reason: in.Body.Reason})
		if err != nil {
			return mapApplicationError(err), true
		}
		return singleCustomer(result.Customer), true
	case "createCatalog":
		in := input.(*contract.CatalogCreateInput)
		actor, err := s.requireScope(ctx, in.BusinessID)
		if err != nil {
			return mapApplicationError(err), true
		}
		if s.deps.CreateCatalog == nil {
			return mapApplicationError(appErrors.NotImplemented()), true
		}
		result, err := s.deps.CreateCatalog.Handle(ctx, commands.CreateCatalogCommand{Meta: commandMeta(ctx, actor, in.CommandHeaders), Name: in.Body.Name, Description: in.Body.Description})
		if err != nil {
			return mapApplicationError(err), true
		}
		return singleCatalog(result.Catalog), true
	case "updateCatalog":
		in := input.(*contract.CatalogUpdateInput)
		actor, err := s.requireScope(ctx, in.BusinessID)
		if err != nil {
			return mapApplicationError(err), true
		}
		if s.deps.UpdateCatalog == nil {
			return mapApplicationError(appErrors.NotImplemented()), true
		}
		result, err := s.deps.UpdateCatalog.Handle(ctx, commands.UpdateCatalogCommand{Meta: commandMeta(ctx, actor, in.CommandHeaders), CatalogID: commands.CatalogID(in.CatalogID), Name: optionalStringPtr(in.Body.Name), Description: in.Body.Description, Status: optionalStringPtr(in.Body.Status)})
		if err != nil {
			return mapApplicationError(err), true
		}
		return singleCatalog(result.Catalog), true
	case "createAttributeSchemaVersion":
		in := input.(*contract.AttributeSchemaCreateInput)
		actor, err := s.requireScope(ctx, in.BusinessID)
		if err != nil {
			return mapApplicationError(err), true
		}
		if s.deps.CreateAttributeSchemaVersion == nil {
			return mapApplicationError(appErrors.NotImplemented()), true
		}
		result, err := s.deps.CreateAttributeSchemaVersion.Handle(ctx, commands.CreateAttributeSchemaVersionCommand{Meta: commandMeta(ctx, actor, in.CommandHeaders), Name: in.Body.Name, Definitions: attributeDefinitions(in.Body.Definitions)})
		if err != nil {
			return mapApplicationError(err), true
		}
		return singleAttributeSchema(result.Schema), true
	case "createCatalogItem":
		in := input.(*contract.CatalogItemCreateInput)
		actor, err := s.requireScope(ctx, in.BusinessID)
		if err != nil {
			return mapApplicationError(err), true
		}
		if s.deps.CreateCatalogItem == nil {
			return mapApplicationError(appErrors.NotImplemented()), true
		}
		result, err := s.deps.CreateCatalogItem.Handle(ctx, commands.CreateCatalogItemCommand{Meta: commandMeta(ctx, actor, in.CommandHeaders), CatalogID: commands.CatalogID(in.CatalogID), AttributeSchemaID: optionalAttributeSchemaID(in.Body.AttributeSchemaID), ItemType: in.Body.ItemType, Name: in.Body.Name, ShortDescription: in.Body.ShortDescription, LongDescription: in.Body.LongDescription, PricingMode: in.Body.PricingMode, AvailabilityMode: in.Body.AvailabilityMode, FulfillmentMode: in.Body.FulfillmentMode, RequiresConfirmation: in.Body.RequiresConfirmation, Attributes: in.Body.Attributes})
		if err != nil {
			return mapApplicationError(err), true
		}
		return singleCatalogItem(result.Item), true
	case "updateCatalogItem":
		in := input.(*contract.CatalogItemUpdateInput)
		actor, err := s.requireScope(ctx, in.BusinessID)
		if err != nil {
			return mapApplicationError(err), true
		}
		if s.deps.UpdateCatalogItem == nil {
			return mapApplicationError(appErrors.NotImplemented()), true
		}
		result, err := s.deps.UpdateCatalogItem.Handle(ctx, commands.UpdateCatalogItemCommand{
			Meta:                 commandMeta(ctx, actor, in.CommandHeaders),
			CatalogItemID:        commands.CatalogItemID(in.ItemID),
			Name:                 optionalStringPtr(in.Body.Name),
			Status:               optionalStringPtr(in.Body.Status),
			ItemType:             optionalStringPtr(in.Body.ItemType),
			ShortDescription:     in.Body.ShortDescription,
			LongDescription:      in.Body.LongDescription,
			PricingMode:          optionalStringPtr(in.Body.PricingMode),
			AvailabilityMode:     optionalStringPtr(in.Body.AvailabilityMode),
			FulfillmentMode:      optionalStringPtr(in.Body.FulfillmentMode),
			RequiresConfirmation: in.Body.RequiresConfirmation,
			Attributes:           in.Body.Attributes,
		})
		if err != nil {
			return mapApplicationError(err), true
		}
		return singleCatalogItem(result.Item), true
	case "createOffer":
		in := input.(*contract.OfferCreateInput)
		actor, err := s.requireScope(ctx, in.BusinessID)
		if err != nil {
			return mapApplicationError(err), true
		}
		amount, err := minorUnits(in.Body.Amount)
		if err != nil {
			return mapApplicationError(err), true
		}
		if s.deps.CreateOffer == nil {
			return mapApplicationError(appErrors.NotImplemented()), true
		}
		result, err := s.deps.CreateOffer.Handle(ctx, commands.CreateOfferCommand{Meta: commandMeta(ctx, actor, in.CommandHeaders), CatalogItemID: commands.CatalogItemID(in.ItemID), VariantID: optionalVariantID(in.Body.VariantID), Name: in.Body.Name, PricingMode: in.Body.PricingMode, AmountMinor: amount, Currency: in.Body.Currency, PricingUnit: in.Body.PricingUnit, AvailabilityMode: in.Body.AvailabilityMode, AvailabilityStatus: in.Body.AvailabilityStatus, FulfillmentMode: in.Body.FulfillmentMode, Status: in.Body.Status})
		if err != nil {
			return mapApplicationError(err), true
		}
		return singleOffer(result.Offer), true
	case "updateOffer":
		in := input.(*contract.OfferUpdateInput)
		actor, err := s.requireScope(ctx, in.BusinessID)
		if err != nil {
			return mapApplicationError(err), true
		}
		amount, err := minorUnits(in.Body.Amount)
		if err != nil {
			return mapApplicationError(err), true
		}
		if s.deps.UpdateOffer == nil {
			return mapApplicationError(appErrors.NotImplemented()), true
		}
		result, err := s.deps.UpdateOffer.Handle(ctx, commands.UpdateOfferCommand{Meta: commandMeta(ctx, actor, in.CommandHeaders), OfferID: commands.OfferID(in.OfferID), Name: optionalStringPtr(in.Body.Name), AmountMinor: amount, AvailabilityStatus: optionalStringPtr(in.Body.AvailabilityStatus), Status: optionalStringPtr(in.Body.Status)})
		if err != nil {
			return mapApplicationError(err), true
		}
		return singleOffer(result.Offer), true
	case "createVariant":
		in := input.(*contract.VariantCreateInput)
		actor, err := s.requireScope(ctx, in.BusinessID)
		if err != nil {
			return mapApplicationError(err), true
		}
		if s.deps.CreateVariant == nil {
			return mapApplicationError(appErrors.NotImplemented()), true
		}
		result, err := s.deps.CreateVariant.Handle(ctx, commands.CreateVariantCommand{Meta: commandMeta(ctx, actor, in.CommandHeaders), CatalogItemID: commands.CatalogItemID(in.ItemID), Name: in.Body.Name, Attributes: in.Body.Attributes})
		if err != nil {
			return mapApplicationError(err), true
		}
		return singleVariant(result.Variant), true
	case "updateVariant":
		in := input.(*contract.VariantUpdateInput)
		actor, err := s.requireScope(ctx, in.BusinessID)
		if err != nil {
			return mapApplicationError(err), true
		}
		if s.deps.UpdateVariant == nil {
			return mapApplicationError(appErrors.NotImplemented()), true
		}
		result, err := s.deps.UpdateVariant.Handle(ctx, commands.UpdateVariantCommand{Meta: commandMeta(ctx, actor, in.CommandHeaders), VariantID: commands.VariantID(in.VariantID), Name: optionalStringPtr(in.Body.Name), Attributes: in.Body.Attributes, Status: optionalStringPtr(in.Body.Status)})
		if err != nil {
			return mapApplicationError(err), true
		}
		return singleVariant(result.Variant), true
	case "createLead":
		in := input.(*contract.LeadCreateInput)
		actor, err := s.requireScope(ctx, in.BusinessID)
		if err != nil {
			return mapApplicationError(err), true
		}
		if s.deps.CreateLead == nil {
			return mapApplicationError(appErrors.NotImplemented()), true
		}
		result, err := s.deps.CreateLead.Handle(ctx, commands.CreateLeadCommand{Meta: commandMeta(ctx, actor, in.CommandHeaders), CustomerID: commands.CustomerID(in.Body.CustomerID), QualificationContext: in.Body.QualificationContext})
		if err != nil {
			return mapApplicationError(err), true
		}
		return singleLead(result.Lead), true
	case "updateLead":
		in := input.(*contract.LeadUpdateInput)
		actor, err := s.requireScope(ctx, in.BusinessID)
		if err != nil {
			return mapApplicationError(err), true
		}
		if s.deps.UpdateLead == nil {
			return mapApplicationError(appErrors.NotImplemented()), true
		}
		result, err := s.deps.UpdateLead.Handle(ctx, commands.UpdateLeadCommand{Meta: commandMeta(ctx, actor, in.CommandHeaders), LeadID: commands.LeadID(in.LeadID), QualificationContext: in.Body.QualificationContext})
		if err != nil {
			return mapApplicationError(err), true
		}
		return singleLead(result.Lead), true
	case "qualifyLead":
		in := input.(*contract.LeadQualifyInput)
		actor, err := s.requireScope(ctx, in.BusinessID)
		if err != nil {
			return mapApplicationError(err), true
		}
		if s.deps.QualifyLead == nil {
			return mapApplicationError(appErrors.NotImplemented()), true
		}
		result, err := s.deps.QualifyLead.Handle(ctx, commands.QualifyLeadCommand{Meta: commandMeta(ctx, actor, in.CommandHeaders), LeadID: commands.LeadID(in.LeadID), Reason: in.Body.Reason, EvidenceReferences: in.Body.EvidenceReferences})
		if err != nil {
			return mapApplicationError(err), true
		}
		return singleLead(result.Lead), true
	case "markLeadLost":
		in := input.(*contract.LeadLostInput)
		actor, err := s.requireScope(ctx, in.BusinessID)
		if err != nil {
			return mapApplicationError(err), true
		}
		if s.deps.MarkLeadLost == nil {
			return mapApplicationError(appErrors.NotImplemented()), true
		}
		result, err := s.deps.MarkLeadLost.Handle(ctx, commands.MarkLeadLostCommand{Meta: commandMeta(ctx, actor, in.CommandHeaders), LeadID: commands.LeadID(in.LeadID), LostReason: in.Body.LostReason})
		if err != nil {
			return mapApplicationError(err), true
		}
		return singleLead(result.Lead), true
	case "createTransactionDraft":
		in := input.(*contract.TransactionCreateInput)
		actor, err := s.requireScope(ctx, in.BusinessID)
		if err != nil {
			return mapApplicationError(err), true
		}
		lines, err := transactionLines(in.Body.Lines)
		if err != nil {
			return mapApplicationError(err), true
		}
		if s.deps.CreateTransactionDraft == nil {
			return mapApplicationError(appErrors.NotImplemented()), true
		}
		result, err := s.deps.CreateTransactionDraft.Handle(ctx, commands.CreateTransactionDraftCommand{Meta: commandMeta(ctx, actor, in.CommandHeaders), CustomerID: commands.CustomerID(in.Body.CustomerID), LeadID: optionalLeadID(in.Body.LeadID), TransactionType: in.Body.TransactionType, Currency: in.Body.Currency, Lines: lines})
		if err != nil {
			return mapApplicationError(err), true
		}
		return singleTransaction(result.Transaction), true
	case "updateTransactionDraft":
		in := input.(*contract.TransactionUpdateInput)
		actor, err := s.requireScope(ctx, in.BusinessID)
		if err != nil {
			return mapApplicationError(err), true
		}
		lines, err := transactionLines(in.Body.Lines)
		if err != nil {
			return mapApplicationError(err), true
		}
		if s.deps.UpdateTransactionDraft == nil {
			return mapApplicationError(appErrors.NotImplemented()), true
		}
		result, err := s.deps.UpdateTransactionDraft.Handle(ctx, commands.UpdateTransactionDraftCommand{Meta: commandMeta(ctx, actor, in.CommandHeaders), TransactionID: commands.TransactionID(in.TransactionID), Currency: in.Body.Currency, Lines: lines})
		if err != nil {
			return mapApplicationError(err), true
		}
		return singleTransaction(result.Transaction), true
	case "confirmTransaction":
		in := input.(*contract.TransactionConfirmInput)
		actor, err := s.requireScope(ctx, in.BusinessID)
		if err != nil {
			return mapApplicationError(err), true
		}
		if s.deps.ConfirmTransaction == nil {
			return mapApplicationError(appErrors.NotImplemented()), true
		}
		result, err := s.deps.ConfirmTransaction.Handle(ctx, commands.ConfirmTransactionCommand{Meta: commandMeta(ctx, actor, in.CommandHeaders), TransactionID: commands.TransactionID(in.TransactionID), EvidenceReference: in.Body.EvidenceReference, PolicyVersion: in.Body.PolicyVersion})
		if err != nil {
			return mapApplicationError(err), true
		}
		return singleTransaction(result.Transaction), true
	case "cancelTransaction":
		in := input.(*contract.TransactionCancelInput)
		actor, err := s.requireScope(ctx, in.BusinessID)
		if err != nil {
			return mapApplicationError(err), true
		}
		if s.deps.CancelTransaction == nil {
			return mapApplicationError(appErrors.NotImplemented()), true
		}
		result, err := s.deps.CancelTransaction.Handle(ctx, commands.CancelTransactionCommand{Meta: commandMeta(ctx, actor, in.CommandHeaders), TransactionID: commands.TransactionID(in.TransactionID), Reason: in.Body.Reason})
		if err != nil {
			return mapApplicationError(err), true
		}
		return singleTransaction(result.Transaction), true
	case "submitTransactionReview":
		in := input.(*contract.TransactionReviewSubmitInput)
		actor, err := s.requireScope(ctx, in.BusinessID)
		if err != nil {
			return mapApplicationError(err), true
		}
		if s.deps.SubmitTransactionReview == nil {
			return mapApplicationError(appErrors.NotImplemented()), true
		}
		result, err := s.deps.SubmitTransactionReview.Handle(ctx, commands.SubmitTransactionReviewCommand{Meta: commandMeta(ctx, actor, in.CommandHeaders), TransactionID: commands.TransactionID(in.TransactionID), ReasonCodes: in.Body.ReasonCodes})
		if err != nil {
			return mapApplicationError(err), true
		}
		return singleTransactionReview(result.Review), true
	case "approveTransactionReview":
		in := input.(*contract.TransactionReviewDecisionInput)
		actor, err := s.requireScope(ctx, in.BusinessID)
		if err != nil {
			return mapApplicationError(err), true
		}
		if s.deps.ApproveTransactionReview == nil {
			return mapApplicationError(appErrors.NotImplemented()), true
		}
		result, err := s.deps.ApproveTransactionReview.Handle(ctx, commands.ApproveTransactionReviewCommand{Meta: commandMeta(ctx, actor, in.CommandHeaders), TransactionID: commands.TransactionID(in.TransactionID), ReviewerReference: in.Body.ReviewerReference, Reason: in.Body.Reason})
		if err != nil {
			return mapApplicationError(err), true
		}
		return singleTransactionReview(result.Review), true
	case "rejectTransactionReview":
		in := input.(*contract.TransactionReviewDecisionInput)
		actor, err := s.requireScope(ctx, in.BusinessID)
		if err != nil {
			return mapApplicationError(err), true
		}
		if s.deps.RejectTransactionReview == nil {
			return mapApplicationError(appErrors.NotImplemented()), true
		}
		result, err := s.deps.RejectTransactionReview.Handle(ctx, commands.RejectTransactionReviewCommand{Meta: commandMeta(ctx, actor, in.CommandHeaders), TransactionID: commands.TransactionID(in.TransactionID), ReviewerReference: in.Body.ReviewerReference, Reason: in.Body.Reason})
		if err != nil {
			return mapApplicationError(err), true
		}
		return singleTransactionReview(result.Review), true
	default:
		return nil, false
	}
}

func transactionLines(values []contract.TransactionLineInput) ([]commands.TransactionLine, error) {
	lines := make([]commands.TransactionLine, 0, len(values))
	for _, value := range values {
		if math.IsNaN(value.Quantity) || math.IsInf(value.Quantity, 0) || value.Quantity <= 0 {
			return nil, appErrors.New(appErrors.CodeValidation, "quantity must be a finite positive number")
		}
		lines = append(lines, commands.TransactionLine{CatalogItemID: commands.CatalogItemID(value.CatalogItemID), OfferID: optionalOfferID(value.OfferID), VariantID: optionalVariantID(value.VariantID), Quantity: commands.Decimal(strconv.FormatFloat(value.Quantity, 'f', -1, 64)), SelectedAttributes: value.SelectedAttributes})
	}
	return lines, nil
}
func optionalLeadID(v *contract.UUID) *commands.LeadID {
	if v == nil || *v == "" {
		return nil
	}
	id := commands.LeadID(*v)
	return &id
}
func optionalOfferID(v *contract.UUID) *commands.OfferID {
	if v == nil || *v == "" {
		return nil
	}
	id := commands.OfferID(*v)
	return &id
}

func minorUnits(value *float64) (*int64, error) {
	if value == nil {
		return nil, nil
	}
	if math.IsNaN(*value) || math.IsInf(*value, 0) {
		return nil, appErrors.New(appErrors.CodeValidation, "amount must be finite")
	}
	scaled := *value * 100
	if math.Abs(scaled-math.Round(scaled)) > 1e-7 {
		return nil, appErrors.New(appErrors.CodeValidation, "amount supports at most two decimal places")
	}
	if scaled > float64(math.MaxInt64) || scaled < float64(math.MinInt64) {
		return nil, appErrors.New(appErrors.CodeValidation, "amount is out of range")
	}
	minor := int64(math.Round(scaled))
	return &minor, nil
}

func optionalStringPtr(v string) *string {
	if v == "" {
		return nil
	}
	return &v
}
func contactPoints(values []contract.ContactPointInput) []commands.ContactPoint {
	points := make([]commands.ContactPoint, 0, len(values))
	for _, value := range values {
		points = append(points, commands.ContactPoint{Kind: value.Kind, Value: value.Value})
	}
	return points
}
func singleCustomer(v commands.CustomerView) *contract.Single[contract.Customer] {
	out := &contract.Single[contract.Customer]{}
	out.Body.Data = customerProjection(v)
	return out
}

func singleCatalog(v commands.CatalogView) *contract.Single[contract.Catalog] {
	out := &contract.Single[contract.Catalog]{}
	out.Body.Data = catalogProjection(v)
	return out
}
func singleAttributeSchema(v commands.AttributeSchemaView) *contract.Single[contract.AttributeSchema] {
	out := &contract.Single[contract.AttributeSchema]{}
	out.Body.Data = attributeSchemaProjection(v)
	return out
}
func singleCatalogItem(v commands.CatalogItemView) *contract.Single[contract.CatalogItem] {
	out := &contract.Single[contract.CatalogItem]{}
	out.Body.Data = catalogItemProjection(v)
	return out
}
func singleOffer(v commands.OfferView) *contract.Single[contract.Offer] {
	out := &contract.Single[contract.Offer]{}
	out.Body.Data = offerProjection(v)
	return out
}
func singleVariant(v commands.VariantView) *contract.Single[contract.Variant] {
	out := &contract.Single[contract.Variant]{}
	out.Body.Data = variantProjection(v)
	return out
}
func singleTransaction(v commands.TransactionView) *contract.Single[contract.CommercialTransaction] {
	out := &contract.Single[contract.CommercialTransaction]{}
	out.Body.Data = transactionProjection(v)
	return out
}
func singleTransactionReview(v commands.TransactionReviewView) *contract.Single[contract.TransactionReview] {
	out := &contract.Single[contract.TransactionReview]{}
	out.Body.Data = contract.TransactionReview{Required: v.Required, Status: v.Status, ReasonCodes: v.ReasonCodes, ReviewerReference: optionalString(v.ReviewerReference)}
	return out
}
func singleLead(v commands.LeadView) *contract.Single[contract.Lead] {
	out := &contract.Single[contract.Lead]{}
	out.Body.Data = leadProjection(v)
	return out
}
func optionalAttributeSchemaID(v *contract.UUID) *commands.AttributeSchemaID {
	if v == nil || *v == "" {
		return nil
	}
	id := commands.AttributeSchemaID(*v)
	return &id
}
func optionalVariantID(v *contract.UUID) *commands.VariantID {
	if v == nil || *v == "" {
		return nil
	}
	id := commands.VariantID(*v)
	return &id
}
func attributeDefinitions(values []contract.AttributeDefinitionInput) []commands.AttributeDefinition {
	result := make([]commands.AttributeDefinition, 0, len(values))
	for _, value := range values {
		result = append(result, commands.AttributeDefinition{Key: value.Key, Label: value.Label, DataType: value.DataType, Required: value.Required, ValidationRules: value.ValidationRules, DisplayOrder: value.DisplayOrder})
	}
	return result
}
