-- Universal Catalog AI v3
-- Read-path indexes for complete catalog paging and bulk hydration.

CREATE INDEX idx_catalog_items_ai_full_scan
    ON catalog_items (business_id, status, id)
    WHERE status = 'active';

CREATE INDEX idx_variants_ai_bulk
    ON variants (business_id, catalog_item_id, status, id)
    WHERE status = 'active';

CREATE INDEX idx_offers_ai_bulk
    ON offers (business_id, catalog_item_id, status, id)
    WHERE status = 'active';
