-- 0003: durable per-signal replanning status, so async outcomes are observable.
-- Additive with defaults; existing signals read back as 'pending'. Applied after
-- 0002 by lexical ordering.
ALTER TABLE signal ADD COLUMN IF NOT EXISTS status         TEXT        NOT NULL DEFAULT 'pending';
ALTER TABLE signal ADD COLUMN IF NOT EXISTS reason         TEXT        NOT NULL DEFAULT '';
ALTER TABLE signal ADD COLUMN IF NOT EXISTS result_version INTEGER     NOT NULL DEFAULT 0;
ALTER TABLE signal ADD COLUMN IF NOT EXISTS error          TEXT        NOT NULL DEFAULT '';
ALTER TABLE signal ADD COLUMN IF NOT EXISTS processed_at   TIMESTAMPTZ NULL;
