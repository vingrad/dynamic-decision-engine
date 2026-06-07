-- 0005: goal lifecycle. A goal now carries a status (its "case file" state) and,
-- once terminal, a resolution. Existing rows default to 'active' with no
-- resolution, preserving prior behaviour. updated_at backfills from created_at.
-- Additive and idempotent; applied after 0004 by lexical ordering.
ALTER TABLE goals ADD COLUMN IF NOT EXISTS status     TEXT NOT NULL DEFAULT 'active';
ALTER TABLE goals ADD COLUMN IF NOT EXISTS resolution JSONB;
ALTER TABLE goals ADD COLUMN IF NOT EXISTS updated_at TIMESTAMPTZ NOT NULL DEFAULT now();
-- Newly-added column defaults existing rows to now(); a goal that has never
-- transitioned should read updated_at == created_at, so backfill it.
UPDATE goals SET updated_at = created_at WHERE updated_at <> created_at;
