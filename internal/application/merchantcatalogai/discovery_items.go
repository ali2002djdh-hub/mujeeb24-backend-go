package merchantcatalogai

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/Ammar777782439/mujeeb24-backend-go/internal/application/ports"
)

type listItemsCapability struct {
	repository        ports.CatalogRepository
	selectedCatalogID string
}

func (c listItemsCapability) Definition() MerchantCatalogDiscoveryToolDefinition {
	return MerchantCatalogDiscoveryToolDefinition{
		Name:        "merchant_catalog_list_items",
		Description: "Read a bounded page of items from the already selected merchant catalog in storage order. The catalog is selected by Mujeeb; the model must not choose it.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"status": map[string]any{"type": "string"},
				"limit":  map[string]any{"type": "integer"},
				"cursor": map[string]any{"type": "string"},
			},
			"required": []string{},
		},
	}
}

func (c listItemsCapability) Execute(ctx context.Context, execCtx MerchantCatalogDiscoveryExecutionContext, rawParams []byte) (MerchantCatalogDiscoveryResult, error) {
	params, err := jsonParams(rawParams)
	if err != nil {
		return MerchantCatalogDiscoveryResult{}, err
	}
	if c.selectedCatalogID == "" {
		return MerchantCatalogDiscoveryResult{}, errors.New("selected merchant catalog is required")
	}

	status, _ := params["status"].(string)
	cursor, _ := params["cursor"].(string)
	page, err := c.repository.ListCatalogItems(ctx, execCtx.BusinessID, c.selectedCatalogID, "", status, readLimit(params), cursor)
	if err != nil {
		return MerchantCatalogDiscoveryResult{}, err
	}

	items := make([]map[string]any, 0, len(page.Items))
	evidenceReferences := make([]string, 0, len(page.Items))
	for _, item := range page.Items {
		items = append(items, map[string]any{
			"id":                       item.ID,
			"catalog_id":               item.CatalogID,
			"attribute_schema_id":      item.AttributeSchemaID,
			"attribute_schema_version": item.AttributeSchemaVersion,
			"item_type":                item.ItemType,
			"name":                     item.Name,
			"short_description":        item.ShortDescription,
			"long_description":         item.LongDescription,
			"status":                   item.Status,
			"pricing_mode":             item.PricingMode,
			"availability_mode":        item.AvailabilityMode,
			"fulfillment_mode":         item.FulfillmentMode,
			"requires_confirmation":    item.RequiresConfirmation,
			"attributes":               json.RawMessage(item.Attributes),
		})
		evidenceReferences = append(evidenceReferences, item.ID)
	}

	return MerchantCatalogDiscoveryResult{
		Data:               items,
		EvidenceReferences: evidenceReferences,
		HasMore:            page.HasMore,
		NextCursor:         page.NextCursor,
		Operation:          "merchant_catalog_list_items",
	}, nil
}

type getItemCapability struct {
	repository        ports.CatalogRepository
	selectedCatalogID string
}

func (c getItemCapability) Definition() MerchantCatalogDiscoveryToolDefinition {
	return MerchantCatalogDiscoveryToolDefinition{
		Name:        "merchant_catalog_get_item",
		Description: "Read one item by exact item_id in the already selected merchant catalog using tenant-scoped repository access.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"item_id": map[string]any{"type": "string"},
			},
			"required": []string{"item_id"},
		},
	}
}

func (c getItemCapability) Execute(ctx context.Context, execCtx MerchantCatalogDiscoveryExecutionContext, rawParams []byte) (MerchantCatalogDiscoveryResult, error) {
	params, err := jsonParams(rawParams)
	if err != nil {
		return MerchantCatalogDiscoveryResult{}, err
	}
	if c.selectedCatalogID == "" {
		return MerchantCatalogDiscoveryResult{}, errors.New("selected merchant catalog is required")
	}

	itemID, err := requiredString(params, "item_id")
	if err != nil {
		return MerchantCatalogDiscoveryResult{}, err
	}
	item, err := c.repository.GetCatalogItem(ctx, execCtx.BusinessID, c.selectedCatalogID, itemID)
	if err != nil {
		return MerchantCatalogDiscoveryResult{}, err
	}

	return MerchantCatalogDiscoveryResult{
		Data: map[string]any{
			"id":                       item.ID,
			"catalog_id":               item.CatalogID,
			"attribute_schema_id":      item.AttributeSchemaID,
			"attribute_schema_version": item.AttributeSchemaVersion,
			"item_type":                item.ItemType,
			"name":                     item.Name,
			"short_description":        item.ShortDescription,
			"long_description":         item.LongDescription,
			"status":                   item.Status,
			"pricing_mode":             item.PricingMode,
			"availability_mode":        item.AvailabilityMode,
			"fulfillment_mode":         item.FulfillmentMode,
			"requires_confirmation":    item.RequiresConfirmation,
			"attributes":               json.RawMessage(item.Attributes),
		},
		EvidenceReferences: []string{item.ID},
		Operation:          "merchant_catalog_get_item",
	}, nil
}
