-- 0004: address an outcome's move by its stable (plan_version, move_rank) location
-- in the immutable plan snapshot, instead of a free-text move_title. Additive with
-- defaults; applied after 0003 by lexical ordering.
--
-- No backfill: pre-0004 rows keep plan_version=0, move_rank=0 and their existing,
-- now-unverified move_title. Historical free-text titles cannot be honestly
-- retro-mapped to a (version, rank) address because moves were regenerated on each
-- replan, so inventing one would fabricate provenance.
ALTER TABLE outcome ADD COLUMN IF NOT EXISTS plan_version INTEGER NOT NULL DEFAULT 0;
ALTER TABLE outcome ADD COLUMN IF NOT EXISTS move_rank    INTEGER NOT NULL DEFAULT 0;
