CREATE TABLE attribute_definitions (
    id               UUID PRIMARY KEY,
    schema_id        UUID NOT NULL,
    attribute_key    TEXT NOT NULL,
    label            TEXT NOT NULL,
    data_type        TEXT NOT NULL,
    is_required      BOOLEAN NOT NULL,
    validation_rules JSONB NOT NULL DEFAULT '{}'::jsonb,
    display_order    INTEGER NOT NULL,
    created_at       TIMESTAMPTZ NOT NULL,
    updated_at       TIMESTAMPTZ NOT NULL,

    CONSTRAINT attribute_definitions_schema_fk
        FOREIGN KEY (schema_id)
        REFERENCES attribute_schemas (id)
        ON DELETE RESTRICT,
    CONSTRAINT attribute_definitions_key_chk
        CHECK (length(btrim(attribute_key)) > 0),
    CONSTRAINT attribute_definitions_label_chk
        CHECK (length(btrim(label)) > 0),
    CONSTRAINT attribute_definitions_type_chk
        CHECK (data_type IN (
            'text', 'number', 'boolean', 'date', 'datetime',
            'select', 'multi_select', 'location', 'money'
        )),
    CONSTRAINT attribute_definitions_validation_object_chk
        CHECK (jsonb_typeof(validation_rules) = 'object'),
    CONSTRAINT attribute_definitions_order_chk
        CHECK (display_order >= 0),
    CONSTRAINT attribute_definitions_key_uq
        UNIQUE (schema_id, attribute_key)
);

CREATE INDEX idx_attribute_definitions_schema_order
    ON attribute_definitions (schema_id, display_order, id);
