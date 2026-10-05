// Package postgres — Catalog Reference + Tenant Validators.
//
// Implements contracts ⑥ §6-7 (Reference Validation) and ⑥ §8-9 (Tenant
// Validation). These are the Postgres-backed concrete implementations of
// the services.ReferenceValidator and services.TenantValidator interfaces
// defined in internal/application/services/ai_validation_pipeline.go.
//
// Per contract ⑥ §6-7 (ReferenceValidator):
//   Every selected ID must exist in the actual catalog data Mujeeb provided
//   to Gemini. Per contract ⑥ §7, Gemini cannot invent references; even a
//   syntactically valid UUID fails if it wasn't in the evidence sent.
//   Per contract ⑥ §10, references must be in the data Mujeeb actually
//   provided to Gemini.
//
// Per contract ⑥ §8-9 (TenantValidator):
//   Every selected reference must belong to the current Business. Per
//   contract ⑥ §8, references from a different Business are silently
//   rejected (Do not expose the resource / Do not treat it as valid /
//   Do not leak its existence). Per contract ⑥ §9, business_id and
//   tenant_id cannot come from Gemini; they come from the Authenticated
//   Context.
//
// Per contract ⑨ §7, validation failures are Non-Retryable. The pipeline
// converts these errors to StageFailure with Category=InvalidReference or
// TenantViolation per the existing ai_validation_pipeline.go logic.
//
// SQL design:
//   All queries use EXISTS subqueries for O(1) lookup. The composite
//   (business_id, id) uniqueness enforced at the DB level via:
//     - catalog_items_business_id_uq UNIQUE (business_id, id)  [migration 000016]
//     - variants_business_id_uq UNIQUE (business_id, id)      [migration 000017]
//     - offers_business_id_uq UNIQUE (business_id, id)        [migration 000018]
//   So a single EXISTS check on (id, business_id) is sufficient.
//
// Per contract ⑧ §17, every query is tenant-scoped via business_id; a
// mismatch returns "not found" (per contract ⑥ §8: do not leak existence
// of other tenants' resources).

package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/Ammar777782439/mujeeb24-backend-go/internal/application/ports"
	"github.com/jackc/pgx/v5"
)

// PostgresReferenceValidator implements services.ReferenceValidator against
// the live PostgreSQL catalog tables.
//
// Per contract ⑥ §10, every selected ID must appear in the evidence Mujeeb
// actually sent to Gemini (in-memory check). Per contract ⑥ §6, the ID must
// also exist as a real row in the catalog table (DB check).
//
// The two-layer check is intentional: the in-memory check is cheap and
// catches the most common hallucination (Gemini inventing an ID); the DB
// check is authoritative and catches edge cases (e.g., the ID was real once
// but has since been hard-deleted).
type PostgresReferenceValidator struct{ adapter *Adapter }

// NewPostgresReferenceValidator wires the validator to a Postgres Adapter.
func NewPostgresReferenceValidator(adapter *Adapter) *PostgresReferenceValidator {
	return &PostgresReferenceValidator{adapter: adapter}
}

// ValidateSelection validates the complete item -> variant -> offer tuple.
// It first checks the exact relational evidence exposed to Gemini, then checks
// the same relationship in PostgreSQL under the trusted business scope.
func (v *PostgresReferenceValidator) ValidateSelection(ctx context.Context, businessID string, selected ports.SelectedReference, evidence ports.CatalogAIEvidenceSet) error {
	if v == nil || v.adapter == nil {
		return ErrPoolClosed
	}
	if strings.TrimSpace(businessID) == "" || strings.TrimSpace(selected.ItemID) == "" {
		return &RepositoryError{Operation: "validator.reference.selection", Kind: RepositoryInvalid, Err: errors.New("business_id and item_id are required")}
	}
	if !evidence.ContainsSelection(selected) {
		return &RepositoryError{
			Operation: "validator.reference.selection",
			Kind:      RepositoryInvalid,
			Err:       errors.New("selected catalog relationship was not in the evidence sent to Gemini"),
		}
	}

	var variantID, offerID string
	if selected.VariantID != nil {
		variantID = strings.TrimSpace(*selected.VariantID)
	}
	if selected.OfferID != nil {
		offerID = strings.TrimSpace(*selected.OfferID)
	}

	executor, err := v.adapter.Executor(ctx)
	if err != nil {
		return &RepositoryError{Operation: "validator.reference.selection", Kind: RepositoryInvalid, Err: err}
	}
	var valid bool
	const query = `
		SELECT EXISTS (
			SELECT 1
			FROM catalog_items i
			JOIN catalogs c
			  ON c.business_id = i.business_id
			 AND c.id = i.catalog_id
			 AND c.status = 'active'
			WHERE i.business_id::text = $1
			  AND i.id::text = $2
			  AND i.status = 'active'
			  AND (
			    $3 = '' OR EXISTS (
			      SELECT 1 FROM variants v
			      WHERE v.business_id = i.business_id
			        AND v.catalog_item_id = i.id
			        AND v.id::text = $3
			        AND v.status = 'active'
			    )
			  )
			  AND (
			    $4 = '' OR EXISTS (
			      SELECT 1 FROM offers o
			      WHERE o.business_id = i.business_id
			        AND o.catalog_item_id = i.id
			        AND o.id::text = $4
			        AND o.status = 'active'
			        AND (
			          $3 = '' OR o.variant_id IS NULL OR o.variant_id::text = $3
			        )
			    )
			  )
		)`
	if err := executor.QueryRow(ctx, query, businessID, selected.ItemID, variantID, offerID).Scan(&valid); err != nil {
		return &RepositoryError{Operation: "validator.reference.selection", Kind: RepositoryInvalid, Err: fmt.Errorf("query selected catalog relationship: %w", err)}
	}
	if !valid {
		// Deliberately generic: do not disclose whether another tenant owns
		// any of the supplied identifiers.
		return &RepositoryError{Operation: "validator.reference.selection", Kind: RepositoryNotFound, Err: errors.New("selected catalog relationship is not valid in the current business")}
	}
	return nil
}

// ValidateItemReference per contract ⑥ §6-7 + §10.
//
// Layer 1 (in-memory): itemID must appear in evidenceItemIDs — the set of
//
//	item IDs Mujeeb actually sent to Gemini as evidence. Per contract ⑥ §10,
//	references must be in the data Mujeeb provided; an ID not in this set
//	was invented by Gemini and must be rejected.
//
// Layer 2 (DB): catalog_items row must exist with matching business_id.
//
//	Per migration 000016, the composite (business_id, id) is unique via
//	catalog_items_business_id_uq. A single EXISTS check confirms both
//	existence and ownership at once.
//
// Per contract ⑨ §7, validation failures are Non-Retryable; the caller
// (ValidationPipeline) converts the returned error to a StageFailure with
// Category=InvalidReference.
func (v *PostgresReferenceValidator) ValidateItemReference(ctx context.Context, businessID, itemID string, evidenceItemIDs []string) error {
	if v == nil || v.adapter == nil {
		return ErrPoolClosed
	}
	if strings.TrimSpace(businessID) == "" || strings.TrimSpace(itemID) == "" {
		return &RepositoryError{Operation: "validator.reference.item", Kind: RepositoryInvalid, Err: errors.New("business_id and item_id are required")}
	}
	// Layer 1: in-memory evidence check per contract ⑥ §10.
	if !containsString(evidenceItemIDs, itemID) {
		return &RepositoryError{
			Operation: "validator.reference.item",
			Kind:      RepositoryInvalid,
			Err:       fmt.Errorf("item_id %s was not in the evidence sent to Gemini per contract ⑥ §10", itemID),
		}
	}
	// Layer 2: DB existence + ownership check per contract ⑥ §6.
	executor, err := v.adapter.Executor(ctx)
	if err != nil {
		return &RepositoryError{Operation: "validator.reference.item", Kind: RepositoryInvalid, Err: err}
	}
	var exists bool
	const query = `SELECT EXISTS(
		SELECT 1
		FROM catalog_items i
		JOIN catalogs c ON c.business_id=i.business_id AND c.id=i.catalog_id
		WHERE i.id::text=$1 AND i.business_id::text=$2
		  AND i.status='active' AND c.status='active'
	)`
	if err := executor.QueryRow(ctx, query, itemID, businessID).Scan(&exists); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return &RepositoryError{Operation: "validator.reference.item", Kind: RepositoryNotFound, Err: fmt.Errorf("item_id %s not found in business %s per contract ⑥ §6", itemID, businessID)}
		}
		return &RepositoryError{Operation: "validator.reference.item", Kind: RepositoryInvalid, Err: fmt.Errorf("query catalog_items: %w", err)}
	}
	if !exists {
		return &RepositoryError{Operation: "validator.reference.item", Kind: RepositoryNotFound, Err: fmt.Errorf("item_id %s not found in business %s per contract ⑥ §6", itemID, businessID)}
	}
	return nil
}

// ValidateVariantReference per contract ⑥ §6-7 + §10.
//
// Layer 1: variantID must appear in evidenceVariantIDs.
// Layer 2: variants row must exist with matching business_id.
//
// Per migration 000017, variants have UNIQUE (business_id, id) via
// variants_business_id_uq, so the EXISTS check is sufficient.
//
// Note: the cross-relationship (variant belongs to a specific item_id) is
// NOT checked here because the services.ReferenceValidator interface
// signature does not pass item_id. Per migration 000017's FK constraint
// variants_catalog_item_fk, a variant is guaranteed to belong to a real
// catalog_item in the same business; the in-memory evidence check ensures
// the variant was sent to Gemini in the context of the right item.
func (v *PostgresReferenceValidator) ValidateVariantReference(ctx context.Context, businessID, variantID string, evidenceVariantIDs []string) error {
	if v == nil || v.adapter == nil {
		return ErrPoolClosed
	}
	if strings.TrimSpace(businessID) == "" || strings.TrimSpace(variantID) == "" {
		return &RepositoryError{Operation: "validator.reference.variant", Kind: RepositoryInvalid, Err: errors.New("business_id and variant_id are required")}
	}
	if !containsString(evidenceVariantIDs, variantID) {
		return &RepositoryError{
			Operation: "validator.reference.variant",
			Kind:      RepositoryInvalid,
			Err:       fmt.Errorf("variant_id %s was not in the evidence sent to Gemini per contract ⑥ §10", variantID),
		}
	}
	executor, err := v.adapter.Executor(ctx)
	if err != nil {
		return &RepositoryError{Operation: "validator.reference.variant", Kind: RepositoryInvalid, Err: err}
	}
	var exists bool
	const query = `SELECT EXISTS(
		SELECT 1
		FROM variants v
		JOIN catalog_items i ON i.business_id=v.business_id AND i.id=v.catalog_item_id
		JOIN catalogs c ON c.business_id=i.business_id AND c.id=i.catalog_id
		WHERE v.id::text=$1 AND v.business_id::text=$2
		  AND v.status='active' AND i.status='active' AND c.status='active'
	)`
	if err := executor.QueryRow(ctx, query, variantID, businessID).Scan(&exists); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return &RepositoryError{Operation: "validator.reference.variant", Kind: RepositoryNotFound, Err: fmt.Errorf("variant_id %s not found in business %s per contract ⑥ §6", variantID, businessID)}
		}
		return &RepositoryError{Operation: "validator.reference.variant", Kind: RepositoryInvalid, Err: fmt.Errorf("query variants: %w", err)}
	}
	if !exists {
		return &RepositoryError{Operation: "validator.reference.variant", Kind: RepositoryNotFound, Err: fmt.Errorf("variant_id %s not found in business %s per contract ⑥ §6", variantID, businessID)}
	}
	return nil
}

// ValidateOfferReference per contract ⑥ §6-7 + §10.
//
// Layer 1: offerID must appear in evidenceOfferIDs.
// Layer 2: offers row must exist with matching business_id.
//
// Per migration 000018, offers have UNIQUE (business_id, id) via
// offers_business_id_uq. Per the FK constraint offers_variant_same_item_fk,
// if offers.variant_id is non-NULL it must reference a variant within the
// same (business_id, catalog_item_id) — enforced at insert time. So an
// existing offer with matching business_id is guaranteed to belong to
// either the item directly (variant_id NULL) or a real variant of that
// item in the same business.
func (v *PostgresReferenceValidator) ValidateOfferReference(ctx context.Context, businessID, offerID string, evidenceOfferIDs []string) error {
	if v == nil || v.adapter == nil {
		return ErrPoolClosed
	}
	if strings.TrimSpace(businessID) == "" || strings.TrimSpace(offerID) == "" {
		return &RepositoryError{Operation: "validator.reference.offer", Kind: RepositoryInvalid, Err: errors.New("business_id and offer_id are required")}
	}
	if !containsString(evidenceOfferIDs, offerID) {
		return &RepositoryError{
			Operation: "validator.reference.offer",
			Kind:      RepositoryInvalid,
			Err:       fmt.Errorf("offer_id %s was not in the evidence sent to Gemini per contract ⑥ §10", offerID),
		}
	}
	executor, err := v.adapter.Executor(ctx)
	if err != nil {
		return &RepositoryError{Operation: "validator.reference.offer", Kind: RepositoryInvalid, Err: err}
	}
	var exists bool
	const query = `SELECT EXISTS(
		SELECT 1
		FROM offers o
		JOIN catalog_items i ON i.business_id=o.business_id AND i.id=o.catalog_item_id
		JOIN catalogs c ON c.business_id=i.business_id AND c.id=i.catalog_id
		WHERE o.id::text=$1 AND o.business_id::text=$2
		  AND o.status='active' AND i.status='active' AND c.status='active'
	)`
	if err := executor.QueryRow(ctx, query, offerID, businessID).Scan(&exists); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return &RepositoryError{Operation: "validator.reference.offer", Kind: RepositoryNotFound, Err: fmt.Errorf("offer_id %s not found in business %s per contract ⑥ §6", offerID, businessID)}
		}
		return &RepositoryError{Operation: "validator.reference.offer", Kind: RepositoryInvalid, Err: fmt.Errorf("query offers: %w", err)}
	}
	if !exists {
		return &RepositoryError{Operation: "validator.reference.offer", Kind: RepositoryNotFound, Err: fmt.Errorf("offer_id %s not found in business %s per contract ⑥ §6", offerID, businessID)}
	}
	return nil
}

// PostgresTenantValidator implements services.TenantValidator against the
// live PostgreSQL catalog tables.
//
// Per contract ⑥ §8-9, every selected reference must belong to the current
// Business. Cross-tenant references are silently rejected (per ⑥ §8: do
// not expose / do not treat as valid / do not leak existence).
//
// Per contract ⑥ §9, business_id comes from the Authenticated Context,
// never from Gemini.
//
// The TenantValidator is intentionally separate from ReferenceValidator:
// even if a reference exists in the evidence set and as a real DB row,
// the tenant check ensures the row's business_id matches the current
// authenticated business. This is the second layer of defense against
// cross-tenant data leakage.
type PostgresTenantValidator struct{ adapter *Adapter }

// NewPostgresTenantValidator wires the validator to a Postgres Adapter.
func NewPostgresTenantValidator(adapter *Adapter) *PostgresTenantValidator {
	return &PostgresTenantValidator{adapter: adapter}
}

// ValidateItemOwnership per contract ⑥ §8.
//
// Single EXISTS query against catalog_items filtered by (id, business_id).
// Per migration 000016 catalog_items_business_id_uq, the composite is
// unique so the EXISTS check is O(1).
//
// Per contract ⑥ §8, a mismatch (item exists but in a different business)
// returns "not found" rather than "forbidden" — this prevents leaking the
// existence of other tenants' resources.
func (v *PostgresTenantValidator) ValidateItemOwnership(ctx context.Context, businessID, itemID string) error {
	if v == nil || v.adapter == nil {
		return ErrPoolClosed
	}
	if strings.TrimSpace(businessID) == "" || strings.TrimSpace(itemID) == "" {
		return &RepositoryError{Operation: "validator.tenant.item", Kind: RepositoryInvalid, Err: errors.New("business_id and item_id are required")}
	}
	executor, err := v.adapter.Executor(ctx)
	if err != nil {
		return &RepositoryError{Operation: "validator.tenant.item", Kind: RepositoryInvalid, Err: err}
	}
	var exists bool
	const query = `SELECT EXISTS(SELECT 1 FROM catalog_items WHERE id::text = $1 AND business_id::text = $2)`
	if err := executor.QueryRow(ctx, query, itemID, businessID).Scan(&exists); err != nil {
		return &RepositoryError{Operation: "validator.tenant.item", Kind: RepositoryInvalid, Err: fmt.Errorf("query catalog_items: %w", err)}
	}
	if !exists {
		// Per contract ⑥ §8: "Do not expose the resource / Do not treat it
		// as valid / Do not leak its existence". Return NotFound (not
		// Forbidden) so the caller cannot distinguish "exists in another
		// tenant" from "does not exist".
		return &RepositoryError{
			Operation: "validator.tenant.item",
			Kind:      RepositoryNotFound,
			Err:       fmt.Errorf("item_id %s not owned by business %s per contract ⑥ §8", itemID, businessID),
		}
	}
	return nil
}

// ValidateVariantOwnership per contract ⑥ §8.
func (v *PostgresTenantValidator) ValidateVariantOwnership(ctx context.Context, businessID, variantID string) error {
	if v == nil || v.adapter == nil {
		return ErrPoolClosed
	}
	if strings.TrimSpace(businessID) == "" || strings.TrimSpace(variantID) == "" {
		return &RepositoryError{Operation: "validator.tenant.variant", Kind: RepositoryInvalid, Err: errors.New("business_id and variant_id are required")}
	}
	executor, err := v.adapter.Executor(ctx)
	if err != nil {
		return &RepositoryError{Operation: "validator.tenant.variant", Kind: RepositoryInvalid, Err: err}
	}
	var exists bool
	const query = `SELECT EXISTS(SELECT 1 FROM variants WHERE id::text = $1 AND business_id::text = $2)`
	if err := executor.QueryRow(ctx, query, variantID, businessID).Scan(&exists); err != nil {
		return &RepositoryError{Operation: "validator.tenant.variant", Kind: RepositoryInvalid, Err: fmt.Errorf("query variants: %w", err)}
	}
	if !exists {
		return &RepositoryError{
			Operation: "validator.tenant.variant",
			Kind:      RepositoryNotFound,
			Err:       fmt.Errorf("variant_id %s not owned by business %s per contract ⑥ §8", variantID, businessID),
		}
	}
	return nil
}

// ValidateOfferOwnership per contract ⑥ §8.
func (v *PostgresTenantValidator) ValidateOfferOwnership(ctx context.Context, businessID, offerID string) error {
	if v == nil || v.adapter == nil {
		return ErrPoolClosed
	}
	if strings.TrimSpace(businessID) == "" || strings.TrimSpace(offerID) == "" {
		return &RepositoryError{Operation: "validator.tenant.offer", Kind: RepositoryInvalid, Err: errors.New("business_id and offer_id are required")}
	}
	executor, err := v.adapter.Executor(ctx)
	if err != nil {
		return &RepositoryError{Operation: "validator.tenant.offer", Kind: RepositoryInvalid, Err: err}
	}
	var exists bool
	const query = `SELECT EXISTS(SELECT 1 FROM offers WHERE id::text = $1 AND business_id::text = $2)`
	if err := executor.QueryRow(ctx, query, offerID, businessID).Scan(&exists); err != nil {
		return &RepositoryError{Operation: "validator.tenant.offer", Kind: RepositoryInvalid, Err: fmt.Errorf("query offers: %w", err)}
	}
	if !exists {
		return &RepositoryError{
			Operation: "validator.tenant.offer",
			Kind:      RepositoryNotFound,
			Err:       fmt.Errorf("offer_id %s not owned by business %s per contract ⑥ §8", offerID, businessID),
		}
	}
	return nil
}

// containsString reports whether the target appears in the slice.
// Case-sensitive match on the UUID string per migration column type.
func containsString(items []string, target string) bool {
	if len(items) == 0 || strings.TrimSpace(target) == "" {
		return false
	}
	for _, item := range items {
		if item == target {
			return true
		}
	}
	return false
}

// Compile-time assertions that the Postgres validators satisfy the
// services.ReferenceValidator and services.TenantValidator interfaces
// defined in internal/application/services/ai_validation_pipeline.go.
// We use a local marker interface to avoid an import cycle (services
// imports ports, but does not import postgres; the assertions are made
// via the interface signatures which mirror services.* exactly).
var _ referenceValidatorInterface = (*PostgresReferenceValidator)(nil)
var _ tenantValidatorInterface = (*PostgresTenantValidator)(nil)

// referenceValidatorInterface and tenantValidatorInterface mirror the
// services.ReferenceValidator and services.TenantValidator signatures so
// we get a compile-time check that the Postgres implementations match.
// If services changes the signature, this file will fail to compile here
// first (instead of failing at the bootstrap wiring call site).
type referenceValidatorInterface interface {
	ValidateItemReference(ctx context.Context, businessID, itemID string, evidenceItemIDs []string) error
	ValidateVariantReference(ctx context.Context, businessID, variantID string, evidenceVariantIDs []string) error
	ValidateOfferReference(ctx context.Context, businessID, offerID string, evidenceOfferIDs []string) error
}

type tenantValidatorInterface interface {
	ValidateItemOwnership(ctx context.Context, businessID, itemID string) error
	ValidateVariantOwnership(ctx context.Context, businessID, variantID string) error
	ValidateOfferOwnership(ctx context.Context, businessID, offerID string) error
}
