package merchantcatalogai

import (
	"context"
	"encoding/json"
	"log"
	"strings"
	"time"

	"github.com/Ammar777782439/mujeeb24-backend-go/internal/application/ports"
)

type listAttributeSchemasCapability struct {
	repository ports.CatalogRepository
}

// listAttributeSchemasCapability exposes tenant-scoped AttributeSchema definitions
// as optional existing evidence. Dynamic attributes do not require a schema. It
// is read-only; this capability never creates or mutates schemas.
func (c listAttributeSchemasCapability) Definition() MerchantCatalogDiscoveryToolDefinition {
	return MerchantCatalogDiscoveryToolDefinition{
		Name:        "merchant_catalog_list_attribute_schemas",
		Description: "List existing AttributeSchema versions and definitions for the current business when existing schema evidence is relevant. Dynamic attributes do not require a schema. This is read-only and tenant-scoped.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"name":    map[string]any{"type": "string", "description": "Optional schema name filter."},
				"version": map[string]any{"type": "integer", "description": "Optional exact schema version."},
			},
		},
	}
}

func (c listAttributeSchemasCapability) Execute(ctx context.Context, execCtx MerchantCatalogDiscoveryExecutionContext, rawParams []byte) (MerchantCatalogDiscoveryResult, error) {
	started := time.Now()
	var params struct {
		Name    string `json:"name"`
		Version *int   `json:"version"`
	}

	if len(rawParams) > 0 {
		if err := json.Unmarshal(rawParams, &params); err != nil {
			log.Printf("[MerchantCatalogAI][DISCOVERY][SCHEMA] ERROR business=%s session=%s stage=parse_params err=%v",
				execCtx.BusinessID, execCtx.ConversationID, err)
			return MerchantCatalogDiscoveryResult{}, err
		}
	}

	nameFilter := strings.TrimSpace(params.Name)
	log.Printf("[MerchantCatalogAI][DISCOVERY][SCHEMA] START business=%s session=%s name_filter=%q version_filter=%v",
		execCtx.BusinessID, execCtx.ConversationID, nameFilter, params.Version)

	page, err := c.repository.ListAttributeSchemas(ctx, execCtx.BusinessID, nameFilter, params.Version, 100, "")
	if err != nil {
		log.Printf("[MerchantCatalogAI][DISCOVERY][SCHEMA] ERROR business=%s session=%s stage=repository latency_ms=%d err=%v",
			execCtx.BusinessID, execCtx.ConversationID, time.Since(started).Milliseconds(), err)
		return MerchantCatalogDiscoveryResult{}, err
	}

	schemas := make([]map[string]any, 0, len(page.Items))
	definitionCount := 0
	for _, schema := range page.Items {
		definitions := make([]map[string]any, 0, len(schema.Definitions))
		definitionCount += len(schema.Definitions)
		for _, definition := range schema.Definitions {
			var rules any
			if len(definition.ValidationRules) > 0 {
				_ = json.Unmarshal(definition.ValidationRules, &rules)
			}
			definitions = append(definitions, map[string]any{
				"id":               definition.ID,
				"key":              definition.Key,
				"label":            definition.Label,
				"data_type":        definition.DataType,
				"required":         definition.Required,
				"validation_rules": rules,
				"display_order":    definition.DisplayOrder,
			})
		}
		schemas = append(schemas, map[string]any{
			"id":          schema.ID,
			"name":        schema.Name,
			"version":     schema.Version,
			"definitions": definitions,
		})
	}

	log.Printf("[MerchantCatalogAI][DISCOVERY][SCHEMA] OK business=%s session=%s schemas=%d definitions=%d has_more=%t next_cursor_present=%t latency_ms=%d",
		execCtx.BusinessID, execCtx.ConversationID, len(schemas), definitionCount, page.HasMore, strings.TrimSpace(page.NextCursor) != "",
		time.Since(started).Milliseconds())

	return MerchantCatalogDiscoveryResult{
		Data: map[string]any{
			"attribute_schemas": schemas,
			"has_more":          page.HasMore,
			"next_cursor":       page.NextCursor,
		},
		HasMore:    page.HasMore,
		NextCursor: page.NextCursor,
		Operation:  "list_attribute_schemas",
	}, nil
}
