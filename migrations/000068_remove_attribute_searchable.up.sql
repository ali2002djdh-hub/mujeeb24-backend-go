-- Universal Catalog cleanup: attribute definitions describe semantics
-- and validation only. Catalog AI does not use backend search metadata.
ALTER TABLE attribute_definitions
    DROP COLUMN IF EXISTS is_searchable;
