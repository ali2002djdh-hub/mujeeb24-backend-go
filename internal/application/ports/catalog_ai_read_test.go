package ports

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestCatalogAIEvidenceSetContainsSelection(t *testing.T) {
	variantA := "variant-a"
	variantB := "variant-b"
	offerA := "offer-a"

	evidence := NewCatalogAIEvidenceSet()
	evidence.AddBundle(CatalogAIProjectionBundle{
		Item: CatalogItemRecord{ID: "item-a"},
		Variants: []VariantRecord{
			{ID: variantA, CatalogItemID: "item-a"},
		},
		Offers: []OfferRecord{
			{ID: offerA, CatalogItemID: "item-a", VariantID: &variantA},
		},
	})

	if !evidence.ContainsSelection(SelectedReference{ItemID: "item-a", VariantID: &variantA, OfferID: &offerA}) {
		t.Fatal("expected exact item/variant/offer tuple to be valid")
	}
	if evidence.ContainsSelection(SelectedReference{ItemID: "item-missing"}) {
		t.Fatal("invented item must not be valid evidence")
	}
	if evidence.ContainsSelection(SelectedReference{ItemID: "item-a", VariantID: &variantB}) {
		t.Fatal("variant not exposed under item must be rejected")
	}
	if evidence.ContainsSelection(SelectedReference{ItemID: "item-a", VariantID: &variantB, OfferID: &offerA}) {
		t.Fatal("offer/variant mismatch must be rejected")
	}
}

func TestCatalogAIEvidenceSetDoesNotTrustOfferFromAnotherItem(t *testing.T) {
	offer := "offer-b"
	evidence := NewCatalogAIEvidenceSet()
	evidence.AddBundle(CatalogAIProjectionBundle{
		Item: CatalogItemRecord{ID: "item-a"},
		Offers: []OfferRecord{
			{ID: offer, CatalogItemID: "item-b"},
		},
	})
	if evidence.ContainsSelection(SelectedReference{ItemID: "item-a", OfferID: &offer}) {
		t.Fatal("offer belonging to another item must never enter item evidence")
	}
}

func TestCatalogAIEvidenceSetMerge(t *testing.T) {
	v := "variant-a"
	first := NewCatalogAIEvidenceSet()
	first.AddBundle(CatalogAIProjectionBundle{
		Item: CatalogItemRecord{ID: "item-a"},
	})
	second := NewCatalogAIEvidenceSet()
	second.AddBundle(CatalogAIProjectionBundle{
		Item: CatalogItemRecord{ID: "item-a"},
		Variants: []VariantRecord{{ID: v, CatalogItemID: "item-a"}},
	})
	first.Merge(second)
	if !first.ContainsSelection(SelectedReference{ItemID: "item-a", VariantID: &v}) {
		t.Fatal("merged evidence must retain relational children")
	}
}

func TestCatalogAIManifestJSONIsCompactAndNonEvidentiary(t *testing.T) {
	description := "long internal description"
	manifest := CatalogAIManifest{
		TotalActiveItems:  2,
		TotalCatalogs:     1,
		CatalogsTruncated: false,
		Catalogs: []CatalogAIManifestCatalog{{
			ID:                 "catalog-secret-id",
			Name:               "Services",
			Description:        &description,
			ItemCount:          2,
			ItemTypes:          []string{"appointment", "service"},
			ItemTypesTruncated: false,
		}},
	}
	raw, err := json.Marshal(manifest)
	if err != nil {
		t.Fatalf("marshal manifest: %v", err)
	}
	got := string(raw)
	for _, forbidden := range []string{"catalog-secret-id", "long internal description"} {
		if strings.Contains(got, forbidden) {
			t.Fatalf("compact manifest leaked %q: %s", forbidden, got)
		}
	}
	for _, required := range []string{"Services", "appointment", "total_catalogs"} {
		if !strings.Contains(got, required) {
			t.Fatalf("compact manifest lost %q: %s", required, got)
		}
	}
}

func TestCatalogAIManifestSignalsTruncation(t *testing.T) {
	manifest := CatalogAIManifest{
		TotalActiveItems: 1000,
		TotalCatalogs: 100,
		CatalogsTruncated: true,
		Catalogs: []CatalogAIManifestCatalog{{
			Name: "Catalog A",
			ItemCount: 100,
			ItemTypes: []string{"service"},
			ItemTypesTruncated: true,
		}},
	}
	raw, err := json.Marshal(manifest)
	if err != nil {
		t.Fatalf("marshal manifest: %v", err)
	}
	got := string(raw)
	for _, required := range []string{"catalogs_truncated", "item_types_truncated", "total_catalogs"} {
		if !strings.Contains(got, required) {
			t.Fatalf("manifest must expose %q when incomplete: %s", required, got)
		}
	}
}
