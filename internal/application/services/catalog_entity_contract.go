// Package services — Catalog Entity Contract (contract ⑤)
//
// This file is the CONTRACT ⑤ §2 definition of the six catalog entities.
// Per contract ⑤ §3, the Entity Contract is the DEFINITION of the catalog
// system; it is NOT merchant data.
//
// All enum values in this file are taken VERBATIM from the SQL migration
// constraints (migrations/000013..000018). The Catalog Contract document
// (contracts/domain_catalog_contract_review_ar.md) is the authoritative
// source; the SQL constraints enforce it.
//
// DO NOT INVENT enum values. If a value is not in the SQL CHECK constraint,
// it does not exist in the contract.

package services

import "sort"

// CatalogEntityContract is the canonical definition of the six catalog entities
// per contract ⑤ §2. It is sent to Gemini as part of the system context.
//
// Per contract ⑤ §17, this structure contains ONLY type/meaning definitions;
// no merchant-specific data lives here.
type CatalogEntityContract struct {
	Catalog             CatalogEntityDefinition             `json:"catalog"`
	CatalogItem         CatalogItemEntityDefinition         `json:"catalog_item"`
	AttributeSchema     AttributeSchemaEntityDefinition     `json:"attribute_schema"`
	AttributeDefinition AttributeDefinitionEntityDefinition `json:"attribute_definition"`
	Variant             VariantEntityDefinition             `json:"variant"`
	Offer               OfferEntityDefinition               `json:"offer"`
}

// CatalogEntityDefinition is the Catalog root entity definition per contract ⑤ §2.
//
// Per SQL migration 000013: status IN ('draft', 'active', 'archived').
type CatalogEntityDefinition struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Status      string `json:"status"`
}

// CatalogItemEntityDefinition is the CatalogItem entity per contract ⑤ §2.
//
// Per SQL migration 000016:
//   - item_type is TEXT NOT NULL with CHECK(length(btrim(item_type)) > 0).
//     It is NOT an enum — per the Catalog Contract §"Vertical Templates",
//     item_type is vertical-specific (physical_good, ticket, appointment,
//     service, etc.) and the merchant decides the value.
//   - status IN ('draft', 'active', 'inactive', 'archived')
//   - pricing_mode IN ('fixed', 'starting_from', 'per_unit', 'per_person',
//     'per_day', 'quote_required', 'dynamic')
//   - availability_mode IN ('stock', 'schedule', 'supplier_check',
//     'always_available', 'unknown')
//   - fulfillment_mode IN ('delivery', 'pickup', 'digital', 'appointment',
//     'travel', 'manual')
//   - attributes is a dynamic JSONB object. Keys are English snake_case; values may be any valid JSON value.
type CatalogItemEntityDefinition struct {
	ID                     string         `json:"id"`
	CatalogID              string         `json:"catalog_id"`
	AttributeSchemaID      *string        `json:"attribute_schema_id,omitempty"`
	AttributeSchemaVersion *int           `json:"attribute_schema_version,omitempty"`
	ItemType               string         `json:"item_type"`
	Name                   string         `json:"name"`
	ShortDescription       *string        `json:"short_description,omitempty"`
	LongDescription        *string        `json:"long_description,omitempty"`
	Status                 string         `json:"status"`
	PricingMode            string         `json:"pricing_mode"`
	AvailabilityMode       string         `json:"availability_mode"`
	FulfillmentMode        string         `json:"fulfillment_mode"`
	RequiresConfirmation   bool           `json:"requires_confirmation"`
	Attributes             map[string]any `json:"attributes,omitempty"`
}

// AttributeSchemaEntityDefinition per contract ⑤ §2.
type AttributeSchemaEntityDefinition struct {
	ID          string                                `json:"id"`
	Name        string                                `json:"name"`
	Version     int                                   `json:"version"`
	Definitions []AttributeDefinitionEntityDefinition `json:"definitions"`
}

// AttributeDefinitionEntityDefinition per contract ⑤ §2.
//
// Per Catalog Contract §6, the data_type MUST be one of:
//
//	text, number, boolean, date, datetime, select, multi_select, location, money.
//
// Per Catalog Contract §6: "RAM و Doctor و FlightNumber ليست أنواعًا؛ هي
// keys/values داخل schema. Validation rules تحدد القيم المقبولة."
type AttributeDefinitionEntityDefinition struct {
	ID              string         `json:"id"`
	SchemaID        string         `json:"schema_id"`
	AttributeKey    string         `json:"attribute_key"`
	Label           string         `json:"label"`
	DataType        string         `json:"data_type"`
	IsRequired      bool           `json:"is_required"`
	ValidationRules map[string]any `json:"validation_rules,omitempty"`
	DisplayOrder    int            `json:"display_order"`
}

// VariantEntityDefinition per contract ⑤ §2 and Catalog Contract §2.
//
// Per SQL migration 000017:
//   - status IN ('active', 'inactive', 'archived') — NO "draft" (variants
//     are not draftable; only CatalogItem and Offer have draft state).
//   - attributes is a dynamic JSONB object. Keys are English snake_case; values may be any valid JSON value.
type VariantEntityDefinition struct {
	ID            string         `json:"id"`
	CatalogItemID string         `json:"catalog_item_id"`
	Name          string         `json:"name"`
	Attributes    map[string]any `json:"attributes,omitempty"`
	Status        string         `json:"status"`
}

// OfferEntityDefinition per contract ⑤ §2 and Catalog Contract §3.
//
// Per SQL migration 000018:
//   - pricing_mode IN ('fixed', 'starting_from', 'per_unit', 'per_person',
//     'per_day', 'quote_required', 'dynamic')
//   - price_verification_status IN ('unverified', 'verified', 'stale', 'rejected')
//   - availability_mode IN ('stock', 'schedule', 'supplier_check',
//     'always_available', 'unknown')
//   - availability_status IN ('available', 'unavailable', 'unknown',
//     'requires_check', 'stale')
//   - fulfillment_mode IN ('delivery', 'pickup', 'digital', 'appointment',
//     'travel', 'manual')
//   - status IN ('draft', 'active', 'inactive', 'expired', 'archived')
//   - amount is NUMERIC(20,4), NULL allowed when pricing_mode is quote_required
//
// Per Catalog Contract §3:
//   - "unknown ≠ available" / "stale ≠ confirmed" / "requires_check ≠ confirmed"
//   - The non-breakable rule: AI must not treat unknown/stale/requires_check
//     as confirmed availability.
type OfferEntityDefinition struct {
	ID                      string  `json:"id"`
	CatalogItemID           string  `json:"catalog_item_id"`
	VariantID               *string `json:"variant_id,omitempty"`
	Name                    string  `json:"name"`
	PricingMode             string  `json:"pricing_mode"`
	Amount                  *string `json:"amount,omitempty"`
	Currency                *string `json:"currency,omitempty"`
	PricingUnit             *string `json:"pricing_unit,omitempty"`
	PriceSource             *string `json:"price_source,omitempty"`
	PriceVerificationStatus *string `json:"price_verification_status,omitempty"`
	PriceCheckedAt          *string `json:"price_checked_at,omitempty"`
	AvailabilityMode        *string `json:"availability_mode,omitempty"`
	AvailabilityStatus      *string `json:"availability_status,omitempty"`
	AvailabilitySource      *string `json:"availability_source,omitempty"`
	AvailabilityCheckedAt   *string `json:"availability_checked_at,omitempty"`
	AvailabilityValidUntil  *string `json:"availability_valid_until,omitempty"`
	AvailabilityEvidenceRef *string `json:"availability_evidence_ref,omitempty"`
	FulfillmentMode         *string `json:"fulfillment_mode,omitempty"`
	ValidityFrom            *string `json:"validity_from,omitempty"`
	ValidityUntil           *string `json:"validity_until,omitempty"`
	Status                  string  `json:"status"`
}

// CatalogEntityContractDescriptor is the human+machine-readable description
// that Mujeeb sends to Gemini as part of the system instruction. Per contract
// ⑤ §8, this tells Gemini the meaning of every enum value so it never has
// to guess.
//
// ALL enum values below are taken VERBATIM from the SQL migration CHECK
// constraints. DO NOT add values that are not in the SQL migrations.
type CatalogEntityContractDescriptor struct {
	PricingModes              map[string]string `json:"pricing_modes"`
	AvailabilityModes         map[string]string `json:"availability_modes"`
	AvailabilityStatuses      map[string]string `json:"availability_statuses"`
	PriceVerificationStatuses map[string]string `json:"price_verification_statuses"`
	FulfillmentModes          map[string]string `json:"fulfillment_modes"`
	// ItemStatuses is the CatalogItem status set from migration 000016.
	// CatalogItem does NOT allow expired; expired belongs to Offer status in migration 000018.
	CatalogStatuses map[string]string `json:"catalog_statuses"`
	ItemStatuses    map[string]string `json:"item_statuses"`
	VariantStatuses map[string]string `json:"variant_statuses"`
	OfferStatuses   map[string]string `json:"offer_statuses"`
	// NOTE: ItemTypes is intentionally ABSENT. Per SQL migration 000016,
	// item_type is TEXT (non-empty), NOT an enum. Per Catalog Contract
	// §"Vertical Templates", item_type is vertical-specific and the merchant
	// decides the value (e.g., "physical_good", "ticket", "appointment",
	// "service"). Mujeeb does NOT constrain item_type to a closed set.
	Relationships []EntityRelationship `json:"relationships"`
}

// EntityRelationship describes one relationship between entities per contract ⑤ §2.
type EntityRelationship struct {
	From        string `json:"from"`
	To          string `json:"to"`
	Cardinality string `json:"cardinality"` // one_to_many, many_to_one, etc.
	Description string `json:"description"`
}

// DefaultCatalogEntityContractDescriptor returns the canonical descriptor
// that all Mujeeb deployments use. Per contract ⑤ §8, this is fixed
// knowledge that Gemini needs; deployments do NOT customize it.
//
// Every value below is sourced VERBATIM from the SQL migration CHECK
// constraints. Per the "NO INVENTION" rule, no value may be added unless
// it appears in the SQL migrations OR an explicit ADR amends the contract.
func DefaultCatalogEntityContractDescriptor() CatalogEntityContractDescriptor {
	return CatalogEntityContractDescriptor{
		// Per SQL migration 000016/000018 offers_pricing_mode_chk +
		// catalog_items_pricing_mode_chk.
		// Per Catalog Contract §3:
		//   fixed + verified        → amount مطلوب
		//   per_unit/per_person     → amount مطلوب + unit مطلوب
		//   starting_from           → amount يمثل بداية وليس نهائيًا
		//   quote_required          → amount غير مطلوب
		//   dynamic                 → amount اختياري ويحتاج مصدر تحقق
		//   unknown                 → لا يجوز عرضه كرقم مؤكد (NOTE: 'unknown' is
		//                              NOT in the SQL enum; the SQL has 7 values,
		//                              'unknown' is the availability-side concept).
		//                              The Catalog Contract document mentions
		//                              'unknown' as a pricing state but the SQL
		//                              migration does not include it in
		//                              pricing_mode. We follow the SQL.
		PricingModes: map[string]string{
			"fixed":          "السعر ثابت ومحدد في العرض",
			"starting_from":  "السعر يبدأ من هذه القيمة وقد يزيد حسب الخيارات",
			"per_unit":       "السعر لكل وحدة؛ amount مطلوب + pricing_unit مطلوب",
			"per_person":     "السعر لكل شخص؛ amount مطلوب + pricing_unit مطلوب",
			"per_day":        "السعر لكل يوم؛ amount مطلوب + pricing_unit مطلوب",
			"quote_required": "السعر يُحدد عند الطلب؛ amount غير مطلوب",
			"dynamic":        "السعر متغير؛ amount اختياري ويحتاج مصدر تحقق",
		},
		// Per SQL migration 000016/000018 catalog_items_availability_mode_chk +
		// offers_availability_mode_chk.
		// Per Catalog Contract §4: "stock_quantity يمكن أن يظهر داخل Inventory
		// Adapter عندما يكون mode هو Stock، لكنه ليس حقلًا عالميًا."
		AvailabilityModes: map[string]string{
			"stock":            "التوفر يعتمد على مخزون فعلي (Inventory Adapter)",
			"schedule":         "التوفر يعتمد على جدول زمني (مثل المواعيد)",
			"supplier_check":   "التوفر يتطلب فحص المزود الفعلي (مثل السفر)",
			"always_available": "متاح دائمًا ضمن نطاق العرض",
			"unknown":          "التوفر غير معروف؛ لا يجوز اعتباره متاحًا",
		},
		// Per SQL migration 000018 offers_availability_status_chk.
		// Per Catalog Contract §4 — the non-breakable rule:
		//   unknown ≠ available
		//   stale ≠ confirmed
		//   requires_check ≠ confirmed
		AvailabilityStatuses: map[string]string{
			"available":      "متاح للبيع/الإستخدام الآن",
			"unavailable":    "غير متاح حاليًا",
			"unknown":        "الحالة غير معروفة؛ لا يجوز اعتباره متاحًا (≠ available)",
			"requires_check": "يتطلب فحصًا إضافيًا؛ ليس مؤكدًا (≠ confirmed)",
			"stale":          "البيانات قديمة؛ ليست مؤكدة (≠ confirmed)",
		},
		// Per SQL migration 000018 offers_price_verification_chk.
		PriceVerificationStatuses: map[string]string{
			"unverified": "السعر لم يُتحقق منه بعد؛ قد لا يكون دقيقًا",
			"verified":   "تم التحقق من السعر في آخر تحديث",
			"stale":      "السعر قديم وقد لا يعكس السعر الحالي",
			"rejected":   "تم رفض السعر أثناء التحقق؛ لا يجوز استخدامه",
		},
		// Per SQL migration 000016/000018 catalog_items_fulfillment_mode_chk +
		// offers_fulfillment_mode_chk.
		// Per Catalog Contract §5: "Fulfillment هو Default لا وعد نهائي."
		FulfillmentModes: map[string]string{
			"delivery":    "تسليم مادي للعميل (شحن/توصيل)",
			"pickup":      "استلام من المتجر/الموقع",
			"digital":     "تسليم رقمي/إلكتروني",
			"appointment": "موعد محدد (مثل عيادة أو خدمة)",
			"travel":      "حجز سفر/رحلة (يحتاج origin/destination/date)",
			"manual":      "تنفيذ يدوي/مخصص",
		},
		// Per SQL migration 000016 catalog_items_status_chk + migration
		// 000018 offers_status_chk. Both CHECK constraints use the same 5
		// values (draft, active, inactive, archived, expired). We expose
		// them as a single ItemStatuses map for the frontend's item-status
		// badges (and the archived-state restore flow).
		CatalogStatuses: map[string]string{
			"draft":    "مسودة",
			"active":   "نشط",
			"archived": "مؤرشف",
		},
		ItemStatuses: map[string]string{
			"draft":    "مسودة",
			"active":   "نشط",
			"inactive": "غير نشط",
			"archived": "مؤرشف",
		},
		VariantStatuses: map[string]string{
			"active":   "نشط",
			"inactive": "غير نشط",
			"archived": "مؤرشف",
		},
		OfferStatuses: map[string]string{
			"draft":    "مسودة",
			"active":   "نشط",
			"inactive": "غير نشط",
			"expired":  "منتهي",
			"archived": "مؤرشف",
		},
		Relationships: []EntityRelationship{
			{From: "Catalog", To: "CatalogItem", Cardinality: "one_to_many", Description: "Catalog يحتوي على عدة CatalogItems"},
			{From: "CatalogItem", To: "AttributeSchema", Cardinality: "many_to_one", Description: "CatalogItem يشير إلى AttributeSchema واحدة (اختياريًا)"},
			{From: "AttributeSchema", To: "AttributeDefinition", Cardinality: "one_to_many", Description: "AttributeSchema تحوي عدة AttributeDefinitions"},
			{From: "CatalogItem", To: "Variant", Cardinality: "one_to_many", Description: "CatalogItem قد يحوي عدة Variants (اختياري)"},
			{From: "CatalogItem", To: "Offer", Cardinality: "one_to_many", Description: "CatalogItem قد يحوي عدة Offers"},
			{From: "Variant", To: "Offer", Cardinality: "one_to_many", Description: "Offer قد يرتبط بـVariant واحد (اختياريًا)؛ يجب أن يكون من نفس Item وBusiness"},
		},
	}
}

// CatalogEntityContractPayload is the JSON-serializable payload sent to Gemini
// as part of the system instruction. Per contract ⑤ §7, this is shared
// across all batches and is sent once per AI Runtime invocation.
//
// The payload contains ONLY definitions and descriptors; no merchant data.
type CatalogAIEntityContractDescriptor struct {
	PricingModes              []string                    `json:"pricing_modes"`
	AvailabilityModes         []string                    `json:"availability_modes"`
	AvailabilityStatuses      []string                    `json:"availability_statuses"`
	PriceVerificationStatuses []string                    `json:"price_verification_statuses"`
	FulfillmentModes          []string                    `json:"fulfillment_modes"`
	CatalogStatuses           []string                    `json:"catalog_statuses"`
	ItemStatuses              []string                    `json:"item_statuses"`
	VariantStatuses           []string                    `json:"variant_statuses"`
	OfferStatuses             []string                      `json:"offer_statuses"`
	SemanticRules             []string                      `json:"semantic_rules"`
	Relationships             []CatalogAIEntityRelationship `json:"relationships"`
}

type CatalogAIEntityRelationship struct {
	From        string `json:"from"`
	To          string `json:"to"`
	Cardinality string `json:"cardinality"`
}

type CatalogEntityContractPayload struct {
	Contract   CatalogEntityContract             `json:"entity_contract"`
	Descriptor CatalogAIEntityContractDescriptor `json:"descriptor"`
}

// BuildCatalogEntityContractPayload returns the canonical payload to send to
// Gemini. Per contract ⑤ §7, this is built once per AI Runtime invocation
// and reused across all Batches.
//
// Per the "NO INVENTION" rule, every field value below is sourced VERBATIM
// from the SQL migration CHECK constraints. item_type is intentionally
// described as TEXT (non-empty) per SQL migration 000016's
// `CHECK (length(btrim(item_type)) > 0)` constraint — it is NOT an enum.
func BuildCatalogEntityContractPayload() CatalogEntityContractPayload {
	return CatalogEntityContractPayload{
		Contract: CatalogEntityContract{
			Catalog: CatalogEntityDefinition{
				ID:          "UUID",
				Name:        "TEXT",
				Description: "TEXT?",
				Status:      "draft|active|archived",
			},
			CatalogItem: CatalogItemEntityDefinition{
				ID:                     "UUID",
				CatalogID:              "UUID",
				AttributeSchemaID:      entityContractStrPtr("UUID?"),
				AttributeSchemaVersion: entityContractIntPtr(0),
				ItemType:               "TEXT (non-empty, vertical-specific; e.g., physical_good|ticket|appointment|service — NOT a closed enum)",
				Name:                   "TEXT",
				ShortDescription:       entityContractStrPtr("TEXT?"),
				LongDescription:        entityContractStrPtr("TEXT?"),
				Status:                 "draft|active|inactive|archived",
				PricingMode:            "fixed|starting_from|per_unit|per_person|per_day|quote_required|dynamic",
				AvailabilityMode:       "stock|schedule|supplier_check|always_available|unknown",
				FulfillmentMode:        "delivery|pickup|digital|appointment|travel|manual",
				RequiresConfirmation:   false,
				Attributes:             map[string]any{"attribute_key": "any_json_value"},
			},
			AttributeSchema: AttributeSchemaEntityDefinition{
				ID:      "UUID",
				Name:    "TEXT",
				Version: 1,
				Definitions: []AttributeDefinitionEntityDefinition{{
					ID:              "UUID",
					SchemaID:        "UUID",
					AttributeKey:    "TEXT (snake_case)",
					Label:           "TEXT (display)",
					DataType:        "text|number|boolean|date|datetime|select|multi_select|location|money",
					IsRequired:      false,
					ValidationRules: map[string]any{"rule_key": "rule_value"},
					DisplayOrder:    0,
				}},
			},
			AttributeDefinition: AttributeDefinitionEntityDefinition{
				ID:              "UUID",
				SchemaID:        "UUID",
				AttributeKey:    "TEXT (snake_case)",
				Label:           "TEXT (display)",
				DataType:        "text|number|boolean|date|datetime|select|multi_select|location|money",
				IsRequired:      false,
				ValidationRules: map[string]any{"rule_key": "rule_value"},
				DisplayOrder:    0,
			},
			// Variant status per SQL migration 000017:
			//   CHECK (status IN ('active', 'inactive', 'archived'))
			// NOTE: NO 'draft' — variants are not draftable.
			Variant: VariantEntityDefinition{
				ID:            "UUID",
				CatalogItemID: "UUID",
				Name:          "TEXT",
				Attributes:    map[string]any{"attribute_key": "any_json_value"},
				Status:        "active|inactive|archived",
			},
			// Offer status per SQL migration 000018:
			//   CHECK (status IN ('draft', 'active', 'inactive', 'expired', 'archived'))
			Offer: OfferEntityDefinition{
				ID:                      "UUID",
				CatalogItemID:           "UUID",
				VariantID:               entityContractStrPtr("UUID?"),
				Name:                    "TEXT",
				PricingMode:             "fixed|starting_from|per_unit|per_person|per_day|quote_required|dynamic",
				Amount:                  entityContractStrPtr("NUMERIC(20,4) as string?"),
				Currency:                entityContractStrPtr("CHAR(3) ISO4217?"),
				PricingUnit:             entityContractStrPtr("TEXT? (e.g., each|person|day|hour|route)"),
				PriceSource:             entityContractStrPtr("TEXT?"),
				PriceVerificationStatus: entityContractStrPtr("unverified|verified|stale|rejected?"),
				PriceCheckedAt:          entityContractStrPtr("ISO8601?"),
				AvailabilityMode:        entityContractStrPtr("stock|schedule|supplier_check|always_available|unknown?"),
				AvailabilityStatus:      entityContractStrPtr("available|unavailable|unknown|requires_check|stale?"),
				AvailabilitySource:      entityContractStrPtr("TEXT?"),
				AvailabilityCheckedAt:   entityContractStrPtr("ISO8601?"),
				AvailabilityValidUntil:  entityContractStrPtr("ISO8601?"),
				AvailabilityEvidenceRef: entityContractStrPtr("TEXT?"),
				FulfillmentMode:         entityContractStrPtr("delivery|pickup|digital|appointment|travel|manual?"),
				ValidityFrom:            entityContractStrPtr("ISO8601?"),
				ValidityUntil:           entityContractStrPtr("ISO8601?"),
				Status:                  "draft|active|inactive|expired|archived",
			},
		},
		Descriptor: compactCatalogEntityContractDescriptor(DefaultCatalogEntityContractDescriptor()),
	}
}

func compactCatalogEntityContractDescriptor(rich CatalogEntityContractDescriptor) CatalogAIEntityContractDescriptor {
	relationships := make([]CatalogAIEntityRelationship, 0, len(rich.Relationships))
	for _, rel := range rich.Relationships {
		relationships = append(relationships, CatalogAIEntityRelationship{
			From: rel.From, To: rel.To, Cardinality: rel.Cardinality,
		})
	}
	return CatalogAIEntityContractDescriptor{
		PricingModes:              sortedContractKeys(rich.PricingModes),
		AvailabilityModes:         sortedContractKeys(rich.AvailabilityModes),
		AvailabilityStatuses:      sortedContractKeys(rich.AvailabilityStatuses),
		PriceVerificationStatuses: sortedContractKeys(rich.PriceVerificationStatuses),
		FulfillmentModes:          sortedContractKeys(rich.FulfillmentModes),
		CatalogStatuses:           sortedContractKeys(rich.CatalogStatuses),
		ItemStatuses:              sortedContractKeys(rich.ItemStatuses),
		VariantStatuses:           sortedContractKeys(rich.VariantStatuses),
		OfferStatuses:             sortedContractKeys(rich.OfferStatuses),
		SemanticRules: []string{
			"starting_from amount is a lower bound, not a final price",
			"quote_required has no confirmed numeric price",
			"dynamic price requires current verification before presenting it as confirmed",
			"price_verification rejected is unusable; stale or unverified is not confirmed",
			"availability unknown, stale, or requires_check is not confirmed available",
			"fulfillment_mode is a default mode, not a fulfillment promise",
		},
		Relationships: relationships,
	}
}

func sortedContractKeys(values map[string]string) []string {
	out := make([]string, 0, len(values))
	for key := range values {
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}

func entityContractStrPtr(s string) *string { return &s }
func entityContractIntPtr(i int) *int       { return &i }
