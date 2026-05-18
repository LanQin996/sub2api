-- Allow a channel model pricing entry to exclude selected groups from that model.
-- Empty array means the pricing/model applies to all groups attached to the channel.
ALTER TABLE channel_model_pricing
    ADD COLUMN IF NOT EXISTS excluded_group_ids BIGINT[] NOT NULL DEFAULT '{}';

COMMENT ON COLUMN channel_model_pricing.excluded_group_ids IS
    '该定价/模型不适用的渠道分组 ID 列表。空数组表示适用于渠道内所有分组。';
