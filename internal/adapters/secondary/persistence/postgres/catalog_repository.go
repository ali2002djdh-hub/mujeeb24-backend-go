package postgres

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/Ammar777782439/mujeeb24-backend-go/internal/application/ports"
	"github.com/jackc/pgx/v5"
)

type CatalogRepository struct{ adapter *Adapter }

func NewCatalogRepository(adapter *Adapter) *CatalogRepository {
	return &CatalogRepository{adapter: adapter}
}

type catalogCursor struct {
	UpdatedAt time.Time
	ID        string
}

type schemaCursor struct {
	Version int
	ID      string
}

func (r *CatalogRepository) ListCatalogs(ctx context.Context, businessID, status string, limit int, cursor string) (ports.CatalogPage, error) {
	executor, err := r.catalogExecutor(ctx, "catalog.list")
	if err != nil {
		return ports.CatalogPage{}, err
	}
	limit, decoded, err := catalogPageArgs(limit, cursor, "catalog.list")
	if err != nil {
		return ports.CatalogPage{}, err
	}
	var updatedAt any
	var id any
	if decoded != nil {
		updatedAt, id = decoded.UpdatedAt, decoded.ID
	}
	rows, err := executor.Query(ctx, `SELECT id::text, business_id::text, name, description, status, resource_version, created_at, updated_at FROM catalogs WHERE business_id = $1::uuid AND ($2 = '' OR status = $2) AND ($3::timestamptz IS NULL OR (updated_at, id) < ($3::timestamptz, $4::uuid)) ORDER BY updated_at DESC, id DESC LIMIT $5`, businessID, status, updatedAt, id, limit+1)
	if err != nil {
		return ports.CatalogPage{}, catalogRepositoryError("catalog.list", err)
	}
	defer rows.Close()
	items := make([]ports.CatalogRecord, 0, limit)
	for rows.Next() {
		var item ports.CatalogRecord
		if err := rows.Scan(&item.ID, &item.BusinessID, &item.Name, &item.Description, &item.Status, &item.ResourceVersion, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return ports.CatalogPage{}, catalogRepositoryError("catalog.list", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return ports.CatalogPage{}, catalogRepositoryError("catalog.list", err)
	}
	page := ports.CatalogPage{Items: items}
	if len(items) > limit {
		page.HasMore = true
		page.Items = items[:limit]
		page.NextCursor = encodeCatalogCursor(page.Items[len(page.Items)-1])
	}
	return page, nil
}

func (r *CatalogRepository) GetCatalog(ctx context.Context, businessID, catalogID string) (ports.CatalogRecord, error) {
	executor, err := r.catalogExecutor(ctx, "catalog.get")
	if err != nil {
		return ports.CatalogRecord{}, err
	}
	if businessID == "" || catalogID == "" {
		return ports.CatalogRecord{}, invalidRepositoryInput("catalog.get", "business and catalog ids are required")
	}
	var item ports.CatalogRecord
	if err := executor.QueryRow(ctx, `SELECT id::text, business_id::text, name, description, status, resource_version, created_at, updated_at FROM catalogs WHERE business_id = $1::uuid AND id = $2::uuid`, businessID, catalogID).Scan(&item.ID, &item.BusinessID, &item.Name, &item.Description, &item.Status, &item.ResourceVersion, &item.CreatedAt, &item.UpdatedAt); err != nil {
		return item, classifyRepositoryGetError("catalog.get", err)
	}
	return item, nil
}

func (r *CatalogRepository) ListCatalogItems(ctx context.Context, businessID, catalogID, search, status string, limit int, cursor string) (ports.CatalogItemPage, error) {
	executor, err := r.catalogExecutor(ctx, "catalog_item.list")
	if err != nil {
		return ports.CatalogItemPage{}, err
	}
	if err := r.ensureScopedParent(ctx, executor, "catalog_item.list", `SELECT EXISTS (SELECT 1 FROM catalogs WHERE business_id = $1::uuid AND id = $2::uuid)`, businessID, catalogID); err != nil {
		return ports.CatalogItemPage{}, err
	}
	limit, decoded, err := catalogPageArgs(limit, cursor, "catalog_item.list")
	if err != nil {
		return ports.CatalogItemPage{}, err
	}
	var updatedAt any
	var id any
	if decoded != nil {
		updatedAt, id = decoded.UpdatedAt, decoded.ID
	}
	rows, err := executor.Query(ctx, `SELECT id::text, business_id::text, catalog_id::text, attribute_schema_id::text, attribute_schema_version, item_type, name, short_description, long_description, status, pricing_mode, availability_mode, fulfillment_mode, requires_confirmation, attributes, resource_version, created_at, updated_at FROM catalog_items WHERE business_id = $1::uuid AND catalog_id = $2::uuid AND ($3 = '' OR status = $3) AND ($4 = '' OR name ILIKE '%' || $4 || '%') AND ($5::timestamptz IS NULL OR (updated_at, id) < ($5::timestamptz, $6::uuid)) ORDER BY updated_at DESC, id DESC LIMIT $7`, businessID, catalogID, status, search, updatedAt, id, limit+1)
	if err != nil {
		return ports.CatalogItemPage{}, catalogRepositoryError("catalog_item.list", err)
	}
	defer rows.Close()
	items := make([]ports.CatalogItemRecord, 0, limit)
	for rows.Next() {
		item, scanErr := scanCatalogItem(rows)
		if scanErr != nil {
			return ports.CatalogItemPage{}, catalogRepositoryError("catalog_item.list", scanErr)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return ports.CatalogItemPage{}, catalogRepositoryError("catalog_item.list", err)
	}
	page := ports.CatalogItemPage{Items: items}
	if len(items) > limit {
		page.HasMore = true
		page.Items = items[:limit]
		page.NextCursor = encodeCatalogItemCursor(page.Items[len(page.Items)-1])
	}
	return page, nil
}

func (r *CatalogRepository) GetCatalogItem(ctx context.Context, businessID, catalogID, itemID string) (ports.CatalogItemRecord, error) {
	executor, err := r.catalogExecutor(ctx, "catalog_item.get")
	if err != nil {
		return ports.CatalogItemRecord{}, err
	}
	if businessID == "" || catalogID == "" || itemID == "" {
		return ports.CatalogItemRecord{}, invalidRepositoryInput("catalog_item.get", "business, catalog, and item ids are required")
	}
	var item ports.CatalogItemRecord
	if err := executor.QueryRow(ctx, `SELECT id::text, business_id::text, catalog_id::text, attribute_schema_id::text, attribute_schema_version, item_type, name, short_description, long_description, status, pricing_mode, availability_mode, fulfillment_mode, requires_confirmation, attributes, resource_version, created_at, updated_at FROM catalog_items WHERE business_id = $1::uuid AND catalog_id = $2::uuid AND id = $3::uuid`, businessID, catalogID, itemID).Scan(&item.ID, &item.BusinessID, &item.CatalogID, &item.AttributeSchemaID, &item.AttributeSchemaVersion, &item.ItemType, &item.Name, &item.ShortDescription, &item.LongDescription, &item.Status, &item.PricingMode, &item.AvailabilityMode, &item.FulfillmentMode, &item.RequiresConfirmation, &item.Attributes, &item.ResourceVersion, &item.CreatedAt, &item.UpdatedAt); err != nil {
		return item, classifyRepositoryGetError("catalog_item.get", err)
	}
	return item, nil
}

func (r *CatalogRepository) ListOffers(ctx context.Context, businessID, itemID, status string, limit int, cursor string) (ports.OfferPage, error) {
	executor, err := r.catalogExecutor(ctx, "offer.list")
	if err != nil {
		return ports.OfferPage{}, err
	}
	if err := r.ensureScopedParent(ctx, executor, "offer.list", `SELECT EXISTS (SELECT 1 FROM catalog_items WHERE business_id = $1::uuid AND id = $2::uuid)`, businessID, itemID); err != nil {
		return ports.OfferPage{}, err
	}
	limit, decoded, err := catalogPageArgs(limit, cursor, "offer.list")
	if err != nil {
		return ports.OfferPage{}, err
	}
	var updatedAt any
	var id any
	if decoded != nil {
		updatedAt, id = decoded.UpdatedAt, decoded.ID
	}
	rows, err := executor.Query(ctx, `SELECT id::text, business_id::text, catalog_item_id::text, variant_id::text, name, pricing_mode, amount::text, currency, pricing_unit, price_source, price_verification_status, price_checked_at, availability_mode, availability_source, availability_checked_at, availability_valid_until, availability_evidence_ref, fulfillment_mode, validity_from, validity_until, availability_status, status, resource_version, created_at, updated_at FROM offers WHERE business_id = $1::uuid AND catalog_item_id = $2::uuid AND ($3 = '' OR status = $3) AND ($4::timestamptz IS NULL OR (updated_at, id) < ($4::timestamptz, $5::uuid)) ORDER BY updated_at DESC, id DESC LIMIT $6`, businessID, itemID, status, updatedAt, id, limit+1)
	if err != nil {
		return ports.OfferPage{}, catalogRepositoryError("offer.list", err)
	}
	defer rows.Close()
	items := make([]ports.OfferRecord, 0, limit)
	for rows.Next() {
		var item ports.OfferRecord
		if err := rows.Scan(&item.ID, &item.BusinessID, &item.CatalogItemID, &item.VariantID, &item.Name, &item.PricingMode, &item.Amount, &item.Currency, &item.PricingUnit, &item.PriceSource, &item.PriceVerificationStatus, &item.PriceCheckedAt, &item.AvailabilityMode, &item.AvailabilitySource, &item.AvailabilityCheckedAt, &item.AvailabilityValidUntil, &item.AvailabilityEvidenceRef, &item.FulfillmentMode, &item.ValidityFrom, &item.ValidityUntil, &item.AvailabilityStatus, &item.Status, &item.ResourceVersion, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return ports.OfferPage{}, catalogRepositoryError("offer.list", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return ports.OfferPage{}, catalogRepositoryError("offer.list", err)
	}
	page := ports.OfferPage{Items: items}
	if len(items) > limit {
		page.HasMore = true
		page.Items = items[:limit]
		page.NextCursor = encodeOfferCursor(page.Items[len(page.Items)-1])
	}
	return page, nil
}

func (r *CatalogRepository) ListVariants(ctx context.Context, businessID, itemID, status string, limit int, cursor string) (ports.VariantPage, error) {
	executor, err := r.catalogExecutor(ctx, "variant.list")
	if err != nil {
		return ports.VariantPage{}, err
	}
	if err := r.ensureScopedParent(ctx, executor, "variant.list", `SELECT EXISTS (SELECT 1 FROM catalog_items WHERE business_id = $1::uuid AND id = $2::uuid)`, businessID, itemID); err != nil {
		return ports.VariantPage{}, err
	}
	limit, decoded, err := catalogPageArgs(limit, cursor, "variant.list")
	if err != nil {
		return ports.VariantPage{}, err
	}
	var updatedAt any
	var id any
	if decoded != nil {
		updatedAt, id = decoded.UpdatedAt, decoded.ID
	}
	rows, err := executor.Query(ctx, `SELECT id::text, business_id::text, catalog_item_id::text, name, attributes, status, resource_version, created_at, updated_at FROM variants WHERE business_id = $1::uuid AND catalog_item_id = $2::uuid AND ($3 = '' OR status = $3) AND ($4::timestamptz IS NULL OR (updated_at, id) < ($4::timestamptz, $5::uuid)) ORDER BY updated_at DESC, id DESC LIMIT $6`, businessID, itemID, status, updatedAt, id, limit+1)
	if err != nil {
		return ports.VariantPage{}, catalogRepositoryError("variant.list", err)
	}
	defer rows.Close()
	items := make([]ports.VariantRecord, 0, limit)
	for rows.Next() {
		var item ports.VariantRecord
		if err := rows.Scan(&item.ID, &item.BusinessID, &item.CatalogItemID, &item.Name, &item.Attributes, &item.Status, &item.ResourceVersion, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return ports.VariantPage{}, catalogRepositoryError("variant.list", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return ports.VariantPage{}, catalogRepositoryError("variant.list", err)
	}
	page := ports.VariantPage{Items: items}
	if len(items) > limit {
		page.HasMore = true
		page.Items = items[:limit]
		page.NextCursor = encodeVariantCursor(page.Items[len(page.Items)-1])
	}
	return page, nil
}

func (r *CatalogRepository) ListAttributeSchemas(ctx context.Context, businessID, name string, version *int, limit int, cursor string) (ports.AttributeSchemaPage, error) {
	executor, err := r.catalogExecutor(ctx, "attribute_schema.list")
	if err != nil {
		return ports.AttributeSchemaPage{}, err
	}
	limit, decoded, err := schemaPageArgs(limit, cursor, "attribute_schema.list")
	if err != nil {
		return ports.AttributeSchemaPage{}, err
	}
	var versionValue any
	if version != nil {
		versionValue = *version
	}
	var cursorVersion any
	var cursorID any
	if decoded != nil {
		cursorVersion, cursorID = decoded.Version, decoded.ID
	}
	rows, err := executor.Query(ctx, `SELECT id::text, business_id::text, name, version FROM attribute_schemas WHERE business_id = $1::uuid AND ($2 = '' OR name = $2) AND ($3::int IS NULL OR version = $3) AND ($4::int IS NULL OR (version, id) < ($4::int, $5::uuid)) ORDER BY version DESC, id DESC LIMIT $6`, businessID, name, versionValue, cursorVersion, cursorID, limit+1)
	if err != nil {
		return ports.AttributeSchemaPage{}, catalogRepositoryError("attribute_schema.list", err)
	}
	defer rows.Close()
	items := make([]ports.AttributeSchemaRecord, 0, limit)
	for rows.Next() {
		var item ports.AttributeSchemaRecord
		if err := rows.Scan(&item.ID, &item.BusinessID, &item.Name, &item.Version); err != nil {
			return ports.AttributeSchemaPage{}, catalogRepositoryError("attribute_schema.list", err)
		}
		item.Definitions = make([]ports.AttributeDefinitionRecord, 0)
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return ports.AttributeSchemaPage{}, catalogRepositoryError("attribute_schema.list", err)
	}
	page := ports.AttributeSchemaPage{Items: items}
	if len(items) > limit {
		page.HasMore = true
		page.Items = items[:limit]
		page.NextCursor = encodeSchemaCursor(page.Items[len(page.Items)-1])
	}
	if len(page.Items) > 0 {
		schemaIDs := make([]string, len(page.Items))
		schemaIndexMap := make(map[string]int, len(page.Items))
		for i, s := range page.Items {
			schemaIDs[i] = s.ID
			schemaIndexMap[s.ID] = i
		}
		defRows, err := executor.Query(ctx, `SELECT d.id::text, d.schema_id::text, d.attribute_key, d.label, d.data_type, d.is_required, d.validation_rules, d.display_order FROM attribute_definitions d JOIN attribute_schemas s ON s.id = d.schema_id WHERE d.schema_id = ANY($1::uuid[]) AND s.business_id = $2::uuid ORDER BY d.display_order ASC, d.id ASC`, schemaIDs, businessID)
		if err != nil {
			return ports.AttributeSchemaPage{}, catalogRepositoryError("attribute_schema.list", err)
		}
		defer defRows.Close()
		for defRows.Next() {
			var def ports.AttributeDefinitionRecord
			var schemaID string
			if err := defRows.Scan(&def.ID, &schemaID, &def.Key, &def.Label, &def.DataType, &def.Required, &def.ValidationRules, &def.DisplayOrder); err != nil {
				return ports.AttributeSchemaPage{}, catalogRepositoryError("attribute_schema.list", err)
			}
			if idx, ok := schemaIndexMap[schemaID]; ok {
				page.Items[idx].Definitions = append(page.Items[idx].Definitions, def)
			}
		}
		if err := defRows.Err(); err != nil {
			return ports.AttributeSchemaPage{}, catalogRepositoryError("attribute_schema.list", err)
		}
	}
	return page, nil
}

func (r *CatalogRepository) GetAttributeSchema(ctx context.Context, businessID, schemaID string) (ports.AttributeSchemaRecord, error) {
	executor, err := r.catalogExecutor(ctx, "attribute_schema.get")
	if err != nil {
		return ports.AttributeSchemaRecord{}, err
	}
	if businessID == "" || schemaID == "" {
		return ports.AttributeSchemaRecord{}, invalidRepositoryInput("attribute_schema.get", "business and schema ids are required")
	}
	var item ports.AttributeSchemaRecord
	if err := executor.QueryRow(ctx, `SELECT id::text, business_id::text, name, version FROM attribute_schemas WHERE business_id = $1::uuid AND id = $2::uuid`, businessID, schemaID).Scan(&item.ID, &item.BusinessID, &item.Name, &item.Version); err != nil {
		return item, classifyRepositoryGetError("attribute_schema.get", err)
	}
	rows, err := executor.Query(ctx, `SELECT d.id::text, d.attribute_key, d.label, d.data_type, d.is_required, d.validation_rules, d.display_order FROM attribute_definitions d JOIN attribute_schemas s ON s.id = d.schema_id WHERE d.schema_id = $1::uuid AND s.business_id = $2::uuid ORDER BY d.display_order ASC, d.id ASC`, schemaID, businessID)
	if err != nil {
		return ports.AttributeSchemaRecord{}, catalogRepositoryError("attribute_schema.get", err)
	}
	defer rows.Close()
	item.Definitions = make([]ports.AttributeDefinitionRecord, 0)
	for rows.Next() {
		var definition ports.AttributeDefinitionRecord
		if err := rows.Scan(&definition.ID, &definition.Key, &definition.Label, &definition.DataType, &definition.Required, &definition.ValidationRules, &definition.DisplayOrder); err != nil {
			return ports.AttributeSchemaRecord{}, catalogRepositoryError("attribute_schema.get", err)
		}
		item.Definitions = append(item.Definitions, definition)
	}
	if err := rows.Err(); err != nil {
		return ports.AttributeSchemaRecord{}, catalogRepositoryError("attribute_schema.get", err)
	}
	return item, nil
}

func (r *CatalogRepository) catalogExecutor(ctx context.Context, operation string) (SQLExecutor, error) {
	if r == nil || r.adapter == nil {
		return nil, ErrPoolClosed
	}
	executor, err := r.adapter.Executor(ctx)
	if err != nil {
		return nil, err
	}
	return executor, nil
}

func (r *CatalogRepository) ensureScopedParent(ctx context.Context, executor SQLExecutor, operation, query, businessID, parentID string) error {
	if businessID == "" || parentID == "" {
		return invalidRepositoryInput(operation, "business and parent ids are required")
	}
	var exists bool
	if err := executor.QueryRow(ctx, query, businessID, parentID).Scan(&exists); err != nil {
		return classifyRepositoryGetError(operation, err)
	}
	if !exists {
		return &RepositoryError{Operation: operation, Kind: RepositoryNotFound, Err: pgx.ErrNoRows}
	}
	return nil
}

func catalogPageArgs(limit int, cursor, operation string) (int, *catalogCursor, error) {
	limit, err := normalizeCatalogLimit(limit, operation)
	if err != nil {
		return 0, nil, err
	}
	decoded, err := decodeCatalogCursor(cursor)
	if err != nil {
		return 0, nil, invalidRepositoryInput(operation, err.Error())
	}
	return limit, decoded, nil
}

func schemaPageArgs(limit int, cursor, operation string) (int, *schemaCursor, error) {
	limit, err := normalizeCatalogLimit(limit, operation)
	if err != nil {
		return 0, nil, err
	}
	decoded, err := decodeSchemaCursor(cursor)
	if err != nil {
		return 0, nil, invalidRepositoryInput(operation, err.Error())
	}
	return limit, decoded, nil
}

func normalizeCatalogLimit(limit int, operation string) (int, error) {
	if limit <= 0 {
		limit = 50
	}
	if limit > 10000 {
		return 0, invalidRepositoryInput(operation, "limit must not exceed 10000")
	}
	return limit, nil
}

func encodeCatalogCursor(item ports.CatalogRecord) string {
	return encodeCursorParts(item.UpdatedAt, item.ID)
}
func encodeCatalogItemCursor(item ports.CatalogItemRecord) string {
	return encodeCursorParts(item.UpdatedAt, item.ID)
}
func encodeOfferCursor(item ports.OfferRecord) string {
	return encodeCursorParts(item.UpdatedAt, item.ID)
}
func encodeVariantCursor(item ports.VariantRecord) string {
	return encodeCursorParts(item.UpdatedAt, item.ID)
}
func encodeCursorParts(updatedAt time.Time, id string) string {
	raw := updatedAt.UTC().Format(time.RFC3339Nano) + "|" + id
	return base64.RawURLEncoding.EncodeToString([]byte(raw))
}

func encodeSchemaCursor(item ports.AttributeSchemaRecord) string {
	raw := strconv.Itoa(item.Version) + "|" + item.ID
	return base64.RawURLEncoding.EncodeToString([]byte(raw))
}

func decodeCatalogCursor(value string) (*catalogCursor, error) {
	if strings.TrimSpace(value) == "" {
		return nil, nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return nil, errors.New("invalid cursor encoding")
	}
	parts := strings.Split(string(raw), "|")
	if len(parts) != 2 || parts[1] == "" {
		return nil, errors.New("invalid cursor")
	}
	updatedAt, err := time.Parse(time.RFC3339Nano, parts[0])
	if err != nil {
		return nil, errors.New("invalid cursor updated_at")
	}
	return &catalogCursor{UpdatedAt: updatedAt, ID: parts[1]}, nil
}

func decodeSchemaCursor(value string) (*schemaCursor, error) {
	if strings.TrimSpace(value) == "" {
		return nil, nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return nil, errors.New("invalid cursor encoding")
	}
	parts := strings.Split(string(raw), "|")
	if len(parts) != 2 || parts[1] == "" {
		return nil, errors.New("invalid cursor")
	}
	version, err := strconv.Atoi(parts[0])
	if err != nil || version <= 0 {
		return nil, errors.New("invalid cursor version")
	}
	return &schemaCursor{Version: version, ID: parts[1]}, nil
}

func scanCatalogItem(row interface{ Scan(...any) error }) (ports.CatalogItemRecord, error) {
	var item ports.CatalogItemRecord
	err := row.Scan(&item.ID, &item.BusinessID, &item.CatalogID, &item.AttributeSchemaID, &item.AttributeSchemaVersion, &item.ItemType, &item.Name, &item.ShortDescription, &item.LongDescription, &item.Status, &item.PricingMode, &item.AvailabilityMode, &item.FulfillmentMode, &item.RequiresConfirmation, &item.Attributes, &item.ResourceVersion, &item.CreatedAt, &item.UpdatedAt)
	return item, err
}

func catalogRepositoryError(operation string, err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return &RepositoryError{Operation: operation, Kind: RepositoryNotFound, Err: err}
	}
	return &RepositoryError{Operation: operation, Kind: RepositoryInvalid, Err: fmt.Errorf("%s: %w", operation, err)}
}

var _ ports.CatalogRepository = (*CatalogRepository)(nil)

func (r *CatalogRepository) CreateCatalog(ctx context.Context, draft ports.CatalogDraft) (ports.CatalogRecord, error) {
	executor, err := r.catalogExecutor(ctx, "catalog.create")
	if err != nil {
		return ports.CatalogRecord{}, err
	}
	if draft.ID == "" || draft.BusinessID == "" || draft.Name == "" || draft.Status == "" {
		return ports.CatalogRecord{}, invalidRepositoryInput("catalog.create", "id, business, name, and status are required")
	}
	if draft.CreatedAt.IsZero() || draft.UpdatedAt.IsZero() {
		return ports.CatalogRecord{}, invalidRepositoryInput("catalog.create", "created_at and updated_at are required")
	}
	return scanCatalogRecord(executor.QueryRow(ctx, `INSERT INTO catalogs (id, business_id, name, description, status, created_at, updated_at) VALUES ($1::uuid, $2::uuid, $3, $4, $5, $6, $7) RETURNING id::text, business_id::text, name, description, status, resource_version, created_at, updated_at`, draft.ID, draft.BusinessID, draft.Name, draft.Description, draft.Status, draft.CreatedAt, draft.UpdatedAt))
}

func (r *CatalogRepository) UpdateCatalog(ctx context.Context, patch ports.CatalogPatch) (ports.CatalogRecord, error) {
	executor, err := r.catalogExecutor(ctx, "catalog.update")
	if err != nil {
		return ports.CatalogRecord{}, err
	}
	if patch.ID == "" || patch.BusinessID == "" || patch.ExpectedVersion <= 0 {
		return ports.CatalogRecord{}, invalidRepositoryInput("catalog.update", "id, business, and positive expected version are required")
	}
	var record ports.CatalogRecord
	err = executor.QueryRow(ctx, `UPDATE catalogs SET name = COALESCE($3, name), description = COALESCE($4, description), status = COALESCE($5, status), resource_version = resource_version + 1, updated_at = $6 WHERE business_id = $1::uuid AND id = $2::uuid AND resource_version = $7 RETURNING id::text, business_id::text, name, description, status, resource_version, created_at, updated_at`, patch.BusinessID, patch.ID, patch.Name, patch.Description, patch.Status, patch.UpdatedAt, patch.ExpectedVersion).Scan(&record.ID, &record.BusinessID, &record.Name, &record.Description, &record.Status, &record.ResourceVersion, &record.CreatedAt, &record.UpdatedAt)
	if err == nil {
		return record, nil
	}
	return record, classifyCatalogUpdateMiss(ctx, executor, "catalog.update", `SELECT EXISTS (SELECT 1 FROM catalogs WHERE business_id = $1::uuid AND id = $2::uuid)`, patch.BusinessID, patch.ID, err)
}

func (r *CatalogRepository) CreateCatalogItem(ctx context.Context, draft ports.CatalogItemDraft) (ports.CatalogItemRecord, error) {
	executor, err := r.catalogExecutor(ctx, "catalog_item.create")
	if err != nil {
		return ports.CatalogItemRecord{}, err
	}
	if draft.ID == "" || draft.BusinessID == "" || draft.CatalogID == "" || draft.ItemType == "" || draft.Name == "" || draft.PricingMode == "" || draft.AvailabilityMode == "" || draft.FulfillmentMode == "" || draft.CreatedAt.IsZero() || draft.UpdatedAt.IsZero() {
		return ports.CatalogItemRecord{}, invalidRepositoryInput("catalog_item.create", "required catalog item fields are missing")
	}
	attributes := draft.Attributes
	if len(attributes) == 0 {
		attributes = []byte(`{}`)
	}
	return scanCatalogItem(executor.QueryRow(ctx, `INSERT INTO catalog_items (id, business_id, catalog_id, attribute_schema_id, attribute_schema_version, item_type, name, status, pricing_mode, availability_mode, fulfillment_mode, requires_confirmation, attributes, created_at, updated_at) VALUES ($1::uuid, $2::uuid, $3::uuid, $4::uuid, $5, $6, $7, 'draft', $8, $9, $10, $11, $12::jsonb, $13, $14) RETURNING id::text, business_id::text, catalog_id::text, attribute_schema_id::text, attribute_schema_version, item_type, name, short_description, long_description, status, pricing_mode, availability_mode, fulfillment_mode, requires_confirmation, attributes, resource_version, created_at, updated_at`, draft.ID, draft.BusinessID, draft.CatalogID, draft.AttributeSchemaID, draft.AttributeSchemaVersion, draft.ItemType, draft.Name, draft.PricingMode, draft.AvailabilityMode, draft.FulfillmentMode, draft.RequiresConfirmation, attributes, draft.CreatedAt, draft.UpdatedAt))
}

func (r *CatalogRepository) UpdateCatalogItem(ctx context.Context, patch ports.CatalogItemPatch) (ports.CatalogItemRecord, error) {
	executor, err := r.catalogExecutor(ctx, "catalog_item.update")
	if err != nil {
		return ports.CatalogItemRecord{}, err
	}
	if patch.ID == "" || patch.BusinessID == "" || patch.ExpectedVersion <= 0 {
		return ports.CatalogItemRecord{}, invalidRepositoryInput("catalog_item.update", "id, business, and positive expected version are required")
	}
	var item ports.CatalogItemRecord
	attributes := patch.Attributes
	if len(attributes) == 0 {
		attributes = nil
	}
	err = executor.QueryRow(ctx, `UPDATE catalog_items SET
                name = COALESCE($3, name),
                status = COALESCE($4, status),
                item_type = COALESCE($5, item_type),
                short_description = COALESCE($6, short_description),
                long_description = COALESCE($7, long_description),
                pricing_mode = COALESCE($8, pricing_mode),
                availability_mode = COALESCE($9, availability_mode),
                fulfillment_mode = COALESCE($10, fulfillment_mode),
                requires_confirmation = COALESCE($11, requires_confirmation),
                attributes = COALESCE($12::jsonb, attributes),
                resource_version = resource_version + 1,
                updated_at = $13
        WHERE business_id = $1::uuid AND id = $2::uuid AND resource_version = $14
        RETURNING id::text, business_id::text, catalog_id::text, attribute_schema_id::text, attribute_schema_version, item_type, name, short_description, long_description, status, pricing_mode, availability_mode, fulfillment_mode, requires_confirmation, attributes, resource_version, created_at, updated_at`,
		patch.BusinessID, patch.ID,
		patch.Name, patch.Status,
		patch.ItemType, patch.ShortDescription, patch.LongDescription,
		patch.PricingMode, patch.AvailabilityMode, patch.FulfillmentMode,
		patch.RequiresConfirmation,
		attributes, patch.UpdatedAt, patch.ExpectedVersion,
	).Scan(&item.ID, &item.BusinessID, &item.CatalogID, &item.AttributeSchemaID, &item.AttributeSchemaVersion, &item.ItemType, &item.Name, &item.ShortDescription, &item.LongDescription, &item.Status, &item.PricingMode, &item.AvailabilityMode, &item.FulfillmentMode, &item.RequiresConfirmation, &item.Attributes, &item.ResourceVersion, &item.CreatedAt, &item.UpdatedAt)
	if err == nil {
		return item, nil
	}
	return item, classifyCatalogUpdateMiss(ctx, executor, "catalog_item.update", `SELECT EXISTS (SELECT 1 FROM catalog_items WHERE business_id = $1::uuid AND id = $2::uuid)`, patch.BusinessID, patch.ID, err)
}

func (r *CatalogRepository) CreateOffer(ctx context.Context, draft ports.OfferDraft) (ports.OfferRecord, error) {
	executor, err := r.catalogExecutor(ctx, "offer.create")
	if err != nil {
		return ports.OfferRecord{}, err
	}
	if draft.ID == "" || draft.BusinessID == "" || draft.CatalogItemID == "" || draft.Name == "" || draft.PricingMode == "" || draft.AvailabilityMode == "" || draft.AvailabilityStatus == "" || draft.FulfillmentMode == "" || draft.Status == "" || draft.CreatedAt.IsZero() || draft.UpdatedAt.IsZero() {
		return ports.OfferRecord{}, invalidRepositoryInput("offer.create", "required offer fields are missing")
	}
	pricingUnit := draft.PricingUnit
	if (pricingUnit == nil || *pricingUnit == "") && (draft.PricingMode == "per_unit" || draft.PricingMode == "per_person" || draft.PricingMode == "per_day") {
		val := draft.PricingMode
		pricingUnit = &val
	}
	return scanOfferRecord(executor.QueryRow(ctx, `INSERT INTO offers (id, business_id, catalog_item_id, variant_id, name, pricing_mode, amount, currency, pricing_unit, availability_mode, availability_status, fulfillment_mode, price_verification_status, status, created_at, updated_at) VALUES ($1::uuid, $2::uuid, $3::uuid, $4::uuid, $5, $6, CASE WHEN $7::bigint IS NULL THEN NULL ELSE $7::numeric / 100 END, $8, $9, $10, $11, $12, 'unverified', $13, $14, $15) RETURNING id::text, business_id::text, catalog_item_id::text, variant_id::text, name, pricing_mode, amount::text, currency, pricing_unit, price_source, price_verification_status, price_checked_at, availability_mode, availability_source, availability_checked_at, availability_valid_until, availability_evidence_ref, fulfillment_mode, validity_from, validity_until, availability_status, status, resource_version, created_at, updated_at`, draft.ID, draft.BusinessID, draft.CatalogItemID, draft.VariantID, draft.Name, draft.PricingMode, draft.AmountMinor, draft.Currency, pricingUnit, draft.AvailabilityMode, draft.AvailabilityStatus, draft.FulfillmentMode, draft.Status, draft.CreatedAt, draft.UpdatedAt))
}

func (r *CatalogRepository) UpdateOffer(ctx context.Context, patch ports.OfferPatch) (ports.OfferRecord, error) {
	executor, err := r.catalogExecutor(ctx, "offer.update")
	if err != nil {
		return ports.OfferRecord{}, err
	}
	if patch.ID == "" || patch.BusinessID == "" || patch.ExpectedVersion <= 0 {
		return ports.OfferRecord{}, invalidRepositoryInput("offer.update", "id, business, and positive expected version are required")
	}
	var offer ports.OfferRecord
	err = executor.QueryRow(ctx, `UPDATE offers SET name = COALESCE($3, name), amount = CASE WHEN $4::bigint IS NULL THEN amount ELSE $4::numeric / 100 END, availability_status = COALESCE($5, availability_status), status = COALESCE($6, status), resource_version = resource_version + 1, updated_at = $7 WHERE business_id = $1::uuid AND id = $2::uuid AND resource_version = $8 RETURNING id::text, business_id::text, catalog_item_id::text, variant_id::text, name, pricing_mode, amount::text, currency, pricing_unit, price_source, price_verification_status, price_checked_at, availability_mode, availability_source, availability_checked_at, availability_valid_until, availability_evidence_ref, fulfillment_mode, validity_from, validity_until, availability_status, status, resource_version, created_at, updated_at`, patch.BusinessID, patch.ID, patch.Name, patch.AmountMinor, patch.AvailabilityStatus, patch.Status, patch.UpdatedAt, patch.ExpectedVersion).Scan(&offer.ID, &offer.BusinessID, &offer.CatalogItemID, &offer.VariantID, &offer.Name, &offer.PricingMode, &offer.Amount, &offer.Currency, &offer.PricingUnit, &offer.PriceSource, &offer.PriceVerificationStatus, &offer.PriceCheckedAt, &offer.AvailabilityMode, &offer.AvailabilitySource, &offer.AvailabilityCheckedAt, &offer.AvailabilityValidUntil, &offer.AvailabilityEvidenceRef, &offer.FulfillmentMode, &offer.ValidityFrom, &offer.ValidityUntil, &offer.AvailabilityStatus, &offer.Status, &offer.ResourceVersion, &offer.CreatedAt, &offer.UpdatedAt)
	if err == nil {
		return offer, nil
	}
	return offer, classifyCatalogUpdateMiss(ctx, executor, "offer.update", `SELECT EXISTS (SELECT 1 FROM offers WHERE business_id = $1::uuid AND id = $2::uuid)`, patch.BusinessID, patch.ID, err)
}

func (r *CatalogRepository) CreateVariant(ctx context.Context, draft ports.VariantDraft) (ports.VariantRecord, error) {
	executor, err := r.catalogExecutor(ctx, "variant.create")
	if err != nil {
		return ports.VariantRecord{}, err
	}
	if draft.ID == "" || draft.BusinessID == "" || draft.CatalogItemID == "" || draft.Name == "" || draft.CreatedAt.IsZero() || draft.UpdatedAt.IsZero() {
		return ports.VariantRecord{}, invalidRepositoryInput("variant.create", "id, business, item, name, and timestamps are required")
	}
	attributes := draft.Attributes
	if len(attributes) == 0 {
		attributes = []byte(`{}`)
	}
	status := draft.Status
	if status == "" {
		status = "active"
	}
	return scanVariantRecord(executor.QueryRow(ctx, `INSERT INTO variants (id, business_id, catalog_item_id, name, attributes, status, created_at, updated_at) VALUES ($1::uuid, $2::uuid, $3::uuid, $4, $5::jsonb, $6, $7, $8) RETURNING id::text, business_id::text, catalog_item_id::text, name, attributes, status, resource_version, created_at, updated_at`, draft.ID, draft.BusinessID, draft.CatalogItemID, draft.Name, attributes, status, draft.CreatedAt, draft.UpdatedAt))
}

func (r *CatalogRepository) UpdateVariant(ctx context.Context, patch ports.VariantPatch) (ports.VariantRecord, error) {
	executor, err := r.catalogExecutor(ctx, "variant.update")
	if err != nil {
		return ports.VariantRecord{}, err
	}
	if patch.ID == "" || patch.BusinessID == "" || patch.ExpectedVersion <= 0 {
		return ports.VariantRecord{}, invalidRepositoryInput("variant.update", "id, business, and positive expected version are required")
	}
	var variant ports.VariantRecord
	attributes := patch.Attributes
	if len(attributes) == 0 {
		attributes = nil
	}
	err = executor.QueryRow(ctx, `UPDATE variants SET name = COALESCE($3, name), attributes = COALESCE($4::jsonb, attributes), status = COALESCE($5, status), resource_version = resource_version + 1, updated_at = $6 WHERE business_id = $1::uuid AND id = $2::uuid AND resource_version = $7 RETURNING id::text, business_id::text, catalog_item_id::text, name, attributes, status, resource_version, created_at, updated_at`, patch.BusinessID, patch.ID, patch.Name, attributes, patch.Status, patch.UpdatedAt, patch.ExpectedVersion).Scan(&variant.ID, &variant.BusinessID, &variant.CatalogItemID, &variant.Name, &variant.Attributes, &variant.Status, &variant.ResourceVersion, &variant.CreatedAt, &variant.UpdatedAt)
	if err == nil {
		return variant, nil
	}
	return variant, classifyCatalogUpdateMiss(ctx, executor, "variant.update", `SELECT EXISTS (SELECT 1 FROM variants WHERE business_id = $1::uuid AND id = $2::uuid)`, patch.BusinessID, patch.ID, err)
}

func (r *CatalogRepository) CreateAttributeSchemaVersion(ctx context.Context, draft ports.AttributeSchemaDraft) (ports.AttributeSchemaRecord, error) {
	executor, err := r.catalogExecutor(ctx, "attribute_schema.create")
	if err != nil {
		return ports.AttributeSchemaRecord{}, err
	}
	if draft.ID == "" || draft.BusinessID == "" || draft.Name == "" || draft.Version <= 0 || draft.CreatedAt.IsZero() || draft.UpdatedAt.IsZero() {
		return ports.AttributeSchemaRecord{}, invalidRepositoryInput("attribute_schema.create", "id, business, name, positive version, and timestamps are required")
	}
	if _, err := executor.Exec(ctx, `INSERT INTO attribute_schemas (id, business_id, name, version, created_at, updated_at) VALUES ($1::uuid, $2::uuid, $3, $4, $5, $6)`, draft.ID, draft.BusinessID, draft.Name, draft.Version, draft.CreatedAt, draft.UpdatedAt); err != nil {
		return ports.AttributeSchemaRecord{}, classifyRepositoryWriteError("attribute_schema.create", err)
	}
	for _, definition := range draft.Definitions {
		rules := definition.ValidationRules
		if len(rules) == 0 {
			rules = []byte(`{}`)
		}
		if definition.ID == "" || definition.Key == "" || definition.Label == "" || definition.DataType == "" || definition.DisplayOrder < 0 {
			return ports.AttributeSchemaRecord{}, invalidRepositoryInput("attribute_schema.create", "attribute definition fields are invalid")
		}
		if _, err := executor.Exec(ctx, `INSERT INTO attribute_definitions (id, schema_id, attribute_key, label, data_type, is_required, validation_rules, display_order, created_at, updated_at) VALUES ($1::uuid, $2::uuid, $3, $4, $5, $6, $7::jsonb, $8, $9, $10)`, definition.ID, draft.ID, definition.Key, definition.Label, definition.DataType, definition.Required, rules, definition.DisplayOrder, definition.CreatedAt, definition.UpdatedAt); err != nil {
			return ports.AttributeSchemaRecord{}, classifyRepositoryWriteError("attribute_schema.create", err)
		}
	}
	return r.GetAttributeSchema(ctx, draft.BusinessID, draft.ID)
}

func scanCatalogRecord(row pgx.Row) (ports.CatalogRecord, error) {
	var record ports.CatalogRecord
	if err := row.Scan(&record.ID, &record.BusinessID, &record.Name, &record.Description, &record.Status, &record.ResourceVersion, &record.CreatedAt, &record.UpdatedAt); err != nil {
		return record, classifyRepositoryWriteError("catalog", err)
	}
	return record, nil
}

func scanOfferRecord(row pgx.Row) (ports.OfferRecord, error) {
	var record ports.OfferRecord
	if err := row.Scan(&record.ID, &record.BusinessID, &record.CatalogItemID, &record.VariantID, &record.Name, &record.PricingMode, &record.Amount, &record.Currency, &record.PricingUnit, &record.PriceSource, &record.PriceVerificationStatus, &record.PriceCheckedAt, &record.AvailabilityMode, &record.AvailabilitySource, &record.AvailabilityCheckedAt, &record.AvailabilityValidUntil, &record.AvailabilityEvidenceRef, &record.FulfillmentMode, &record.ValidityFrom, &record.ValidityUntil, &record.AvailabilityStatus, &record.Status, &record.ResourceVersion, &record.CreatedAt, &record.UpdatedAt); err != nil {
		return record, classifyRepositoryWriteError("offer", err)
	}
	return record, nil
}

func scanVariantRecord(row pgx.Row) (ports.VariantRecord, error) {
	var record ports.VariantRecord
	if err := row.Scan(&record.ID, &record.BusinessID, &record.CatalogItemID, &record.Name, &record.Attributes, &record.Status, &record.ResourceVersion, &record.CreatedAt, &record.UpdatedAt); err != nil {
		return record, classifyRepositoryWriteError("variant", err)
	}
	return record, nil
}

func classifyCatalogUpdateMiss(ctx context.Context, executor SQLExecutor, operation, existsQuery, businessID, id string, original error) error {
	if !errors.Is(original, pgx.ErrNoRows) {
		return classifyRepositoryWriteError(operation, original)
	}
	var exists bool
	if err := executor.QueryRow(ctx, existsQuery, businessID, id).Scan(&exists); err != nil {
		return classifyRepositoryWriteError(operation, err)
	}
	if !exists {
		return &RepositoryError{Operation: operation, Kind: RepositoryNotFound, Err: pgx.ErrNoRows}
	}
	return &RepositoryError{Operation: operation, Kind: RepositoryStale, Err: original}
}

func (r *CatalogRepository) NextAttributeSchemaVersion(ctx context.Context, businessID, name string) (int, error) {
	executor, err := r.catalogExecutor(ctx, "attribute_schema.next_version")
	if err != nil {
		return 0, err
	}
	if businessID == "" || name == "" {
		return 0, invalidRepositoryInput("attribute_schema.next_version", "business id and name are required")
	}
	var version int
	if err := executor.QueryRow(ctx, `SELECT COALESCE(MAX(version), 0) + 1 FROM attribute_schemas WHERE business_id = $1::uuid AND name = $2`, businessID, name).Scan(&version); err != nil {
		return 0, classifyRepositoryWriteError("attribute_schema.next_version", err)
	}
	return version, nil
}
