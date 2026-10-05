package postgres

import (
	"context"
	"fmt"
	"strings"

	"github.com/Ammar777782439/mujeeb24-backend-go/internal/application/ports"
)

// CatalogAIReadRepository is the read-only, tenant-scoped catalog reader used
// by AI. It bulk-loads nested records to avoid per-item N+1 queries.
type CatalogAIReadRepository struct {
	adapter *Adapter
}

const (
	catalogAIManifestCatalogLimit      = 40
	catalogAIManifestSchemaLimit       = 40
	catalogAIManifestItemTypeLimit     = 12
	catalogAIManifestAttributeKeyLimit = 24
)

func NewCatalogAIReadRepository(adapter *Adapter) *CatalogAIReadRepository {
	return &CatalogAIReadRepository{adapter: adapter}
}

func (r *CatalogAIReadRepository) executor(ctx context.Context) (SQLExecutor, error) {
	if r == nil || r.adapter == nil {
		return nil, ErrPoolClosed
	}
	return r.adapter.Executor(ctx)
}

func (r *CatalogAIReadRepository) GetManifest(ctx context.Context, businessID string) (ports.CatalogAIManifest, error) {
	if strings.TrimSpace(businessID) == "" {
		return ports.CatalogAIManifest{}, invalidRepositoryInput("catalog_ai.manifest", "business id is required")
	}
	executor, err := r.executor(ctx)
	if err != nil {
		return ports.CatalogAIManifest{}, err
	}

	var manifest ports.CatalogAIManifest
	if err := executor.QueryRow(ctx, `
		SELECT COUNT(DISTINCT c.id)::int, COUNT(ci.id)::int
		FROM catalogs c
		LEFT JOIN catalog_items ci
		  ON ci.business_id = c.business_id
		 AND ci.catalog_id = c.id
		 AND ci.status = 'active'
		WHERE c.business_id = $1::uuid
		  AND c.status = 'active'`, businessID).Scan(&manifest.TotalCatalogs, &manifest.TotalActiveItems); err != nil {
		return ports.CatalogAIManifest{}, catalogRepositoryError("catalog_ai.manifest.totals", err)
	}

	rows, err := executor.Query(ctx, `
		SELECT
			c.id::text,
			c.name,
			c.description,
			COALESCE(stats.item_count, 0)::int,
			COALESCE(types.item_types, ARRAY[]::text[]),
			COALESCE(types.total_types, 0)::int
		FROM catalogs c
		LEFT JOIN LATERAL (
			SELECT COUNT(*) AS item_count
			FROM catalog_items ci
			WHERE ci.business_id = c.business_id
			  AND ci.catalog_id = c.id
			  AND ci.status = 'active'
		) stats ON true
		LEFT JOIN LATERAL (
			SELECT
				ARRAY_AGG(limited.item_type ORDER BY limited.item_type) AS item_types,
				(
					SELECT COUNT(DISTINCT ci_all.item_type)
					FROM catalog_items ci_all
					WHERE ci_all.business_id = c.business_id
					  AND ci_all.catalog_id = c.id
					  AND ci_all.status = 'active'
				) AS total_types
			FROM (
				SELECT DISTINCT ci_type.item_type
				FROM catalog_items ci_type
				WHERE ci_type.business_id = c.business_id
				  AND ci_type.catalog_id = c.id
				  AND ci_type.status = 'active'
				ORDER BY ci_type.item_type
				LIMIT $3
			) limited
		) types ON true
		WHERE c.business_id = $1::uuid
		  AND c.status = 'active'
		ORDER BY c.name, c.id
		LIMIT $2`, businessID, catalogAIManifestCatalogLimit, catalogAIManifestItemTypeLimit)
	if err != nil {
		return ports.CatalogAIManifest{}, catalogRepositoryError("catalog_ai.manifest", err)
	}
	for rows.Next() {
		var catalog ports.CatalogAIManifestCatalog
		var totalTypes int
		if err := rows.Scan(
			&catalog.ID,
			&catalog.Name,
			&catalog.Description,
			&catalog.ItemCount,
			&catalog.ItemTypes,
			&totalTypes,
		); err != nil {
			return ports.CatalogAIManifest{}, catalogRepositoryError("catalog_ai.manifest", err)
		}
		catalog.ItemTypesTruncated = totalTypes > len(catalog.ItemTypes)
		manifest.Catalogs = append(manifest.Catalogs, catalog)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return ports.CatalogAIManifest{}, catalogRepositoryError("catalog_ai.manifest", err)
	}
	rows.Close()
	manifest.CatalogsTruncated = manifest.TotalCatalogs > len(manifest.Catalogs)

	sRows, err := executor.Query(ctx, `
		WITH used AS (
			SELECT s.id, s.name, s.version, COUNT(ci.id)::int AS usage_count
			FROM attribute_schemas s
			JOIN catalog_items ci
			  ON ci.business_id = s.business_id
			 AND ci.attribute_schema_id = s.id
			 AND ci.status = 'active'
			JOIN catalogs c
			  ON c.business_id = ci.business_id
			 AND c.id = ci.catalog_id
			 AND c.status = 'active'
			WHERE s.business_id = $1::uuid
			GROUP BY s.id, s.name, s.version
		)
		SELECT id::text, name, version, usage_count, COUNT(*) OVER()::int
		FROM used
		ORDER BY name, version, id
		LIMIT $2`, businessID, catalogAIManifestSchemaLimit)
	if err != nil {
		return ports.CatalogAIManifest{}, catalogRepositoryError("catalog_ai.manifest.schemas", err)
	}
	schemaIndex := make(map[string]int)
	schemaIDs := make([]string, 0)
	for sRows.Next() {
		var schema ports.CatalogAIManifestSchema
		var totalSchemas int
		if err := sRows.Scan(&schema.ID, &schema.Name, &schema.Version, &schema.UsageCount, &totalSchemas); err != nil {
			sRows.Close()
			return ports.CatalogAIManifest{}, catalogRepositoryError("catalog_ai.manifest.schemas", err)
		}
		manifest.TotalSchemas = totalSchemas
		schemaIndex[schema.ID] = len(manifest.Schemas)
		schemaIDs = append(schemaIDs, schema.ID)
		manifest.Schemas = append(manifest.Schemas, schema)
	}
	if err := sRows.Err(); err != nil {
		sRows.Close()
		return ports.CatalogAIManifest{}, catalogRepositoryError("catalog_ai.manifest.schemas", err)
	}
	sRows.Close()
	manifest.SchemasTruncated = manifest.TotalSchemas > len(manifest.Schemas)
	if len(schemaIDs) == 0 {
		return manifest, nil
	}

	dRows, err := executor.Query(ctx, `
		WITH ranked AS (
			SELECT d.schema_id::text AS schema_id,
			       d.attribute_key,
			       ROW_NUMBER() OVER (PARTITION BY d.schema_id ORDER BY d.display_order, d.id) AS rn,
			       COUNT(*) OVER (PARTITION BY d.schema_id) AS total_keys
			FROM attribute_definitions d
			JOIN attribute_schemas s ON s.id = d.schema_id
			WHERE s.business_id = $1::uuid
			  AND d.schema_id::text = ANY($2::text[])
		)
		SELECT schema_id, attribute_key, total_keys
		FROM ranked
		WHERE rn <= $3
		ORDER BY schema_id, rn`, businessID, schemaIDs, catalogAIManifestAttributeKeyLimit)
	if err != nil {
		return ports.CatalogAIManifest{}, catalogRepositoryError("catalog_ai.manifest.definitions", err)
	}
	for dRows.Next() {
		var schemaID, key string
		var totalKeys int64
		if err := dRows.Scan(&schemaID, &key, &totalKeys); err != nil {
			dRows.Close()
			return ports.CatalogAIManifest{}, catalogRepositoryError("catalog_ai.manifest.definitions", err)
		}
		if idx, ok := schemaIndex[schemaID]; ok {
			manifest.Schemas[idx].AttributeKeys = append(manifest.Schemas[idx].AttributeKeys, key)
			if totalKeys > int64(catalogAIManifestAttributeKeyLimit) {
				manifest.Schemas[idx].AttributeKeysTruncated = true
			}
		}
	}
	if err := dRows.Err(); err != nil {
		dRows.Close()
		return ports.CatalogAIManifest{}, catalogRepositoryError("catalog_ai.manifest.definitions", err)
	}
	dRows.Close()
	return manifest, nil
}

func (r *CatalogAIReadRepository) GetRevision(ctx context.Context, businessID string) (string, error) {
	if strings.TrimSpace(businessID) == "" {
		return "", invalidRepositoryInput("catalog_ai.revision", "business id is required")
	}
	executor, err := r.executor(ctx)
	if err != nil {
		return "", err
	}

	var revision string
	err = executor.QueryRow(ctx, `
		WITH active_items AS (
			SELECT i.*
			FROM catalog_items i
			JOIN catalogs c
			  ON c.business_id = i.business_id
			 AND c.id = i.catalog_id
			 AND c.status = 'active'
			WHERE i.business_id = $1::uuid
			  AND i.status = 'active'
		),
		used_schemas AS (
			SELECT DISTINCT attribute_schema_id AS id
			FROM active_items
			WHERE attribute_schema_id IS NOT NULL
		)
		SELECT concat_ws('|',
			(
				SELECT COUNT(*)::text
				FROM catalogs c
				WHERE c.business_id = $1::uuid
				  AND c.status = 'active'
			),
			COALESCE((
				SELECT MAX(c.updated_at)::text
				FROM catalogs c
				WHERE c.business_id = $1::uuid
				  AND c.status = 'active'
			), ''),
			(SELECT COUNT(*)::text FROM active_items),
			COALESCE((SELECT MAX(updated_at)::text FROM active_items), ''),
			(
				SELECT COUNT(*)::text
				FROM attribute_schemas s
				JOIN used_schemas u ON u.id = s.id
				WHERE s.business_id = $1::uuid
			),
			COALESCE((
				SELECT MAX(s.updated_at)::text
				FROM attribute_schemas s
				JOIN used_schemas u ON u.id = s.id
				WHERE s.business_id = $1::uuid
			), ''),
			(
				SELECT COUNT(*)::text
				FROM attribute_definitions d
				JOIN attribute_schemas s ON s.id = d.schema_id
				JOIN used_schemas u ON u.id = s.id
				WHERE s.business_id = $1::uuid
			),
			COALESCE((
				SELECT MAX(d.updated_at)::text
				FROM attribute_definitions d
				JOIN attribute_schemas s ON s.id = d.schema_id
				JOIN used_schemas u ON u.id = s.id
				WHERE s.business_id = $1::uuid
			), ''),
			(
				SELECT COUNT(*)::text
				FROM variants v
				JOIN active_items i ON i.id = v.catalog_item_id AND i.business_id = v.business_id
				WHERE v.business_id = $1::uuid
				  AND v.status = 'active'
			),
			COALESCE((
				SELECT MAX(v.updated_at)::text
				FROM variants v
				JOIN active_items i ON i.id = v.catalog_item_id AND i.business_id = v.business_id
				WHERE v.business_id = $1::uuid
				  AND v.status = 'active'
			), ''),
			(
				SELECT COUNT(*)::text
				FROM offers o
				JOIN active_items i ON i.id = o.catalog_item_id AND i.business_id = o.business_id
				WHERE o.business_id = $1::uuid
				  AND o.status = 'active'
			),
			COALESCE((
				SELECT MAX(o.updated_at)::text
				FROM offers o
				JOIN active_items i ON i.id = o.catalog_item_id AND i.business_id = o.business_id
				WHERE o.business_id = $1::uuid
				  AND o.status = 'active'
			), '')
		)`, businessID).Scan(&revision)
	if err != nil {
		return "", catalogRepositoryError("catalog_ai.revision", err)
	}
	return revision, nil
}

func (r *CatalogAIReadRepository) ListProjectionPage(ctx context.Context, request ports.CatalogAIProjectionRequest) (ports.CatalogAIProjectionPage, error) {
	if strings.TrimSpace(request.BusinessID) == "" {
		return ports.CatalogAIProjectionPage{}, invalidRepositoryInput("catalog_ai.page", "business id is required")
	}
	limit := request.Limit
	if limit <= 0 {
		limit = 200
	}
	if limit > 500 {
		limit = 500
	}
	executor, err := r.executor(ctx)
	if err != nil {
		return ports.CatalogAIProjectionPage{}, err
	}
	rows, err := executor.Query(ctx, `
		SELECT ci.id::text, ci.business_id::text, ci.catalog_id::text,
		       ci.attribute_schema_id::text, ci.attribute_schema_version,
		       ci.item_type, ci.name, ci.short_description, ci.long_description,
		       ci.status, ci.pricing_mode, ci.availability_mode, ci.fulfillment_mode,
		       ci.requires_confirmation, ci.attributes, ci.resource_version, ci.created_at, ci.updated_at
		FROM catalog_items ci
		JOIN catalogs c
		  ON c.business_id = ci.business_id
		 AND c.id = ci.catalog_id
		 AND c.status = 'active'
		WHERE ci.business_id=$1::uuid AND ci.status='active'
		  AND (NULLIF($2,'')::uuid IS NULL OR ci.catalog_id=NULLIF($2,'')::uuid)
		  AND (NULLIF($3,'')::uuid IS NULL OR ci.id>NULLIF($3,'')::uuid)
		ORDER BY ci.id
		LIMIT $4`, request.BusinessID, request.CatalogID, request.Cursor, limit+1)
	if err != nil {
		return ports.CatalogAIProjectionPage{}, catalogRepositoryError("catalog_ai.page", err)
	}
	var items []ports.CatalogItemRecord
	for rows.Next() {
		item, err := scanCatalogItem(rows)
		if err != nil {
			rows.Close()
			return ports.CatalogAIProjectionPage{}, catalogRepositoryError("catalog_ai.page", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return ports.CatalogAIProjectionPage{}, catalogRepositoryError("catalog_ai.page", err)
	}
	rows.Close()

	page := ports.CatalogAIProjectionPage{}
	if len(items) > limit {
		page.HasMore = true
		items = items[:limit]
	}
	if len(items) == 0 {
		return page, nil
	}
	if page.HasMore {
		page.NextCursor = items[len(items)-1].ID
	}
	ids := make([]string, 0, len(items))
	catalogIDs := make([]string, 0)
	seenCatalogs := map[string]bool{}
	for _, item := range items {
		ids = append(ids, item.ID)
		if !seenCatalogs[item.CatalogID] {
			seenCatalogs[item.CatalogID] = true
			catalogIDs = append(catalogIDs, item.CatalogID)
		}
	}

	if len(catalogIDs) > 0 {
		catRows, catErr := executor.Query(ctx, `
			SELECT id::text, business_id::text, name, description, status, resource_version, created_at, updated_at
			FROM catalogs
			WHERE business_id = $1::uuid
			  AND status = 'active'
			  AND id::text = ANY($2::text[])
			ORDER BY id`, request.BusinessID, catalogIDs)
		if catErr != nil {
			return ports.CatalogAIProjectionPage{}, catalogRepositoryError("catalog_ai.page.catalogs", catErr)
		}
		for catRows.Next() {
			var catalog ports.CatalogRecord
			if scanErr := catRows.Scan(
				&catalog.ID, &catalog.BusinessID, &catalog.Name, &catalog.Description,
				&catalog.Status, &catalog.ResourceVersion, &catalog.CreatedAt, &catalog.UpdatedAt,
			); scanErr != nil {
				catRows.Close()
				return ports.CatalogAIProjectionPage{}, catalogRepositoryError("catalog_ai.page.catalogs", scanErr)
			}
			page.Catalogs = append(page.Catalogs, catalog)
		}
		if rowsErr := catRows.Err(); rowsErr != nil {
			catRows.Close()
			return ports.CatalogAIProjectionPage{}, catalogRepositoryError("catalog_ai.page.catalogs", rowsErr)
		}
		catRows.Close()
		if len(page.Catalogs) != len(catalogIDs) {
			return ports.CatalogAIProjectionPage{}, catalogRepositoryError(
				"catalog_ai.page.catalogs",
				fmt.Errorf("catalog projection parent mismatch: expected %d active catalogs, loaded %d", len(catalogIDs), len(page.Catalogs)),
			)
		}
	}

	bundles, err := r.hydrate(ctx, executor, request.BusinessID, items, ids)
	if err != nil {
		return ports.CatalogAIProjectionPage{}, err
	}
	page.Items = bundles
	return page, nil
}

func (r *CatalogAIReadRepository) hydrate(ctx context.Context, executor SQLExecutor, businessID string, items []ports.CatalogItemRecord, ids []string) ([]ports.CatalogAIProjectionBundle, error) {
	bundles := make([]ports.CatalogAIProjectionBundle, len(items))
	index := map[string]int{}
	var schemaIDs []string
	seenSchema := map[string]bool{}
	for i, item := range items {
		bundles[i].Item = item
		index[item.ID] = i
		if item.AttributeSchemaID != nil && !seenSchema[*item.AttributeSchemaID] {
			seenSchema[*item.AttributeSchemaID] = true
			schemaIDs = append(schemaIDs, *item.AttributeSchemaID)
		}
	}

	vRows, err := executor.Query(ctx, `
		SELECT id::text,business_id::text,catalog_item_id::text,name,attributes,status,resource_version,created_at,updated_at
		FROM variants
		WHERE business_id=$1::uuid AND catalog_item_id::text=ANY($2::text[]) AND status='active'
		ORDER BY catalog_item_id,id`, businessID, ids)
	if err != nil {
		return nil, catalogRepositoryError("catalog_ai.variants", err)
	}
	for vRows.Next() {
		var v ports.VariantRecord
		if err := vRows.Scan(&v.ID,&v.BusinessID,&v.CatalogItemID,&v.Name,&v.Attributes,&v.Status,&v.ResourceVersion,&v.CreatedAt,&v.UpdatedAt); err != nil {
			vRows.Close()
			return nil, catalogRepositoryError("catalog_ai.variants", err)
		}
		if i, ok := index[v.CatalogItemID]; ok {
			bundles[i].Variants = append(bundles[i].Variants, v)
		}
	}
	if err := vRows.Err(); err != nil {
		vRows.Close()
		return nil, catalogRepositoryError("catalog_ai.variants", err)
	}
	vRows.Close()

	oRows, err := executor.Query(ctx, `
		SELECT id::text,business_id::text,catalog_item_id::text,variant_id::text,name,pricing_mode,amount::text,currency,
		       pricing_unit,price_source,price_verification_status,price_checked_at,availability_mode,availability_source,
		       availability_checked_at,availability_valid_until,availability_evidence_ref,fulfillment_mode,validity_from,
		       validity_until,availability_status,status,resource_version,created_at,updated_at
		FROM offers
		WHERE business_id=$1::uuid AND catalog_item_id::text=ANY($2::text[]) AND status='active'
		ORDER BY catalog_item_id,id`, businessID, ids)
	if err != nil {
		return nil, catalogRepositoryError("catalog_ai.offers", err)
	}
	for oRows.Next() {
		var o ports.OfferRecord
		if err := oRows.Scan(&o.ID,&o.BusinessID,&o.CatalogItemID,&o.VariantID,&o.Name,&o.PricingMode,&o.Amount,&o.Currency,
			&o.PricingUnit,&o.PriceSource,&o.PriceVerificationStatus,&o.PriceCheckedAt,&o.AvailabilityMode,&o.AvailabilitySource,
			&o.AvailabilityCheckedAt,&o.AvailabilityValidUntil,&o.AvailabilityEvidenceRef,&o.FulfillmentMode,&o.ValidityFrom,
			&o.ValidityUntil,&o.AvailabilityStatus,&o.Status,&o.ResourceVersion,&o.CreatedAt,&o.UpdatedAt); err != nil {
			oRows.Close()
			return nil, catalogRepositoryError("catalog_ai.offers", err)
		}
		if i, ok := index[o.CatalogItemID]; ok {
			bundles[i].Offers = append(bundles[i].Offers, o)
		}
	}
	if err := oRows.Err(); err != nil {
		oRows.Close()
		return nil, catalogRepositoryError("catalog_ai.offers", err)
	}
	oRows.Close()

	if len(schemaIDs) == 0 {
		return bundles, nil
	}
	sRows, err := executor.Query(ctx, `
		SELECT id::text,business_id::text,name,version
		FROM attribute_schemas
		WHERE business_id=$1::uuid AND id::text=ANY($2::text[])`, businessID, schemaIDs)
	if err != nil {
		return nil, catalogRepositoryError("catalog_ai.schemas", err)
	}
	schemas := map[string]*ports.AttributeSchemaRecord{}
	for sRows.Next() {
		var s ports.AttributeSchemaRecord
		if err := sRows.Scan(&s.ID,&s.BusinessID,&s.Name,&s.Version); err != nil {
			sRows.Close()
			return nil, catalogRepositoryError("catalog_ai.schemas", err)
		}
		copy := s
		schemas[s.ID] = &copy
	}
	if err := sRows.Err(); err != nil {
		sRows.Close()
		return nil, catalogRepositoryError("catalog_ai.schemas", err)
	}
	sRows.Close()
	if len(schemas) != len(schemaIDs) {
		return nil, catalogRepositoryError(
			"catalog_ai.schemas",
			fmt.Errorf("catalog projection schema mismatch: expected %d schemas, loaded %d", len(schemaIDs), len(schemas)),
		)
	}
	for _, item := range items {
		if item.AttributeSchemaID == nil {
			continue
		}
		schema := schemas[*item.AttributeSchemaID]
		if schema == nil {
			return nil, catalogRepositoryError("catalog_ai.schemas", fmt.Errorf("schema %s referenced by item %s is missing", *item.AttributeSchemaID, item.ID))
		}
		if item.AttributeSchemaVersion == nil || *item.AttributeSchemaVersion != schema.Version {
			return nil, catalogRepositoryError(
				"catalog_ai.schemas",
				fmt.Errorf("schema version mismatch for item %s: item=%v schema=%d", item.ID, item.AttributeSchemaVersion, schema.Version),
			)
		}
	}

	dRows, err := executor.Query(ctx, `
		SELECT d.schema_id::text,d.id::text,d.attribute_key,d.label,d.data_type,d.is_required,d.validation_rules,d.display_order
		FROM attribute_definitions d
		JOIN attribute_schemas s ON s.id=d.schema_id
		WHERE s.business_id=$1::uuid AND d.schema_id::text=ANY($2::text[])
		ORDER BY d.schema_id,d.display_order,d.id`, businessID, schemaIDs)
	if err != nil {
		return nil, catalogRepositoryError("catalog_ai.definitions", err)
	}
	for dRows.Next() {
		var schemaID string
		var d ports.AttributeDefinitionRecord
		if err := dRows.Scan(&schemaID,&d.ID,&d.Key,&d.Label,&d.DataType,&d.Required,&d.ValidationRules,&d.DisplayOrder); err != nil {
			dRows.Close()
			return nil, catalogRepositoryError("catalog_ai.definitions", err)
		}
		if s := schemas[schemaID]; s != nil {
			s.Definitions = append(s.Definitions, d)
		}
	}
	if err := dRows.Err(); err != nil {
		dRows.Close()
		return nil, catalogRepositoryError("catalog_ai.definitions", err)
	}
	dRows.Close()
	for i := range bundles {
		if bundles[i].Item.AttributeSchemaID == nil {
			continue
		}
		if s := schemas[*bundles[i].Item.AttributeSchemaID]; s != nil {
			copy := *s
			copy.Definitions = append([]ports.AttributeDefinitionRecord(nil), s.Definitions...)
			bundles[i].AttributeSchema = &copy
		}
	}
	return bundles, nil
}

var _ ports.CatalogAIReadRepository = (*CatalogAIReadRepository)(nil)
