-- 0002: per-goal decision domain (pack key). Existing rows default to '' (the
-- generic domain), preserving prior behaviour. Additive and idempotent; applied
-- after 0001 by lexical ordering.
ALTER TABLE goals ADD COLUMN IF NOT EXISTS domain TEXT NOT NULL DEFAULT '';
