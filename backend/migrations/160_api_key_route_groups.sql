-- 160_api_key_route_groups.sql
-- API Key ordered multi-group routing queue. group_id on api_keys remains the default/billing group.

CREATE TABLE IF NOT EXISTS api_key_route_groups (
    id BIGSERIAL PRIMARY KEY,
    api_key_id BIGINT NOT NULL REFERENCES api_keys(id) ON DELETE CASCADE,
    group_id BIGINT NOT NULL REFERENCES groups(id) ON DELETE CASCADE,
    sort_order INTEGER NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (api_key_id, group_id)
);

CREATE INDEX IF NOT EXISTS idx_api_key_route_groups_api_key_order
    ON api_key_route_groups(api_key_id, sort_order, id);

CREATE INDEX IF NOT EXISTS idx_api_key_route_groups_group_id
    ON api_key_route_groups(group_id);
