//go:build integration

package postgres

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/Ammar777782439/mujeeb24-backend-go/internal/platform/database"
)

func TestCatalogAttributeSchemaTenantDefenseAgainstPostgres(t *testing.T) {
	dsn := os.Getenv("POSTGRES_TEST_DSN")
	if dsn == "" {
		t.Skip("POSTGRES_TEST_DSN is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if _, err := database.RunMigrations(ctx, dsn, time.Now().UTC()); err != nil {
		t.Fatalf("run migrations: %v", err)
	}
	adapter, err := Open(ctx, dsn, DefaultPoolConfig())
	if err != nil {
		t.Fatalf("open adapter: %v", err)
	}
	defer adapter.Close()

	businessA := "61000000-0000-0000-0000-000000000001"
	businessB := "61000000-0000-0000-0000-000000000002"
	schemaA := "61000000-0000-0000-0000-000000000011"
	schemaB := "61000000-0000-0000-0000-000000000012"
	defA := "61000000-0000-0000-0000-000000000021"
	defB := "61000000-0000-0000-0000-000000000022"

	cleanup := func() {
		_, _ = adapter.Pool().Exec(context.Background(), `DELETE FROM attribute_definitions WHERE id IN ($1::uuid, $2::uuid)`, defA, defB)
		_, _ = adapter.Pool().Exec(context.Background(), `DELETE FROM attribute_schemas WHERE id IN ($1::uuid, $2::uuid)`, schemaA, schemaB)
		_, _ = adapter.Pool().Exec(context.Background(), `DELETE FROM businesses WHERE id IN ($1::uuid, $2::uuid)`, businessA, businessB)
	}
	cleanup()
	defer cleanup()
	for _, id := range []string{businessA, businessB} {
		if _, err := adapter.Pool().Exec(ctx, `INSERT INTO businesses (id, name, slug, status, vertical_type, timezone, default_currency, locale, created_at, updated_at) VALUES ($1::uuid, $2, $3, 'active', 'retail', 'Asia/Aden', 'YER', 'ar-YE', now(), now())`, id, "Catalog Tenant "+id[len(id)-2:], "catalog-tenant-"+id[len(id)-2:]); err != nil {
			t.Fatalf("insert business %s: %v", id, err)
		}
	}
	if _, err := adapter.Pool().Exec(ctx, `INSERT INTO attribute_schemas (id, business_id, name, version, created_at, updated_at) VALUES ($1::uuid, $2::uuid, 'Schema A', 1, now(), now()), ($3::uuid, $4::uuid, 'Schema B', 1, now(), now())`, schemaA, businessA, schemaB, businessB); err != nil {
		t.Fatalf("insert schemas: %v", err)
	}
	if _, err := adapter.Pool().Exec(ctx, `INSERT INTO attribute_definitions (id, schema_id, attribute_key, label, data_type, is_required, validation_rules, display_order, created_at, updated_at) VALUES ($1::uuid, $2::uuid, 'color', 'Color', 'text', true, '{}'::jsonb, 0, now(), now()), ($3::uuid, $4::uuid, 'size', 'Size', 'text', false, '{}'::jsonb, 0, now(), now())`, defA, schemaA, defB, schemaB); err != nil {
		t.Fatalf("insert definitions: %v", err)
	}

	repo := NewCatalogRepository(adapter)

	// A -> A allowed, definitions include color
	schema, err := repo.GetAttributeSchema(ctx, businessA, schemaA)
	if err != nil || schema.ID != schemaA || len(schema.Definitions) != 1 || schema.Definitions[0].Key != "color" {
		t.Fatalf("A->A failed: schema=%#v err=%v", schema, err)
	}
	// B -> B allowed, definitions include size
	schema, err = repo.GetAttributeSchema(ctx, businessB, schemaB)
	if err != nil || schema.ID != schemaB || len(schema.Definitions) != 1 || schema.Definitions[0].Key != "size" {
		t.Fatalf("B->B failed: schema=%#v err=%v", schema, err)
	}
	// A -> B not_found (tenant isolation via parent + defense-in-depth JOIN)
	if _, err := repo.GetAttributeSchema(ctx, businessA, schemaB); !IsRepositoryKind(err, RepositoryNotFound) {
		t.Fatalf("A->B should be not_found, got %v", err)
	}
	if _, err := repo.GetAttributeSchema(ctx, businessB, schemaA); !IsRepositoryKind(err, RepositoryNotFound) {
		t.Fatalf("B->A should be not_found, got %v", err)
	}
	// Direct second-query defense: even if schema_id belongs to B, business A must not see its definitions
	// This is already proven by above, but also ensure count mismatch would be caught
}
