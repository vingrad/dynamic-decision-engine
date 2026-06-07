-- 0001_init: core decision-engine schema.
-- PlanVersions are append-only: the application never issues UPDATE/DELETE against
-- plan_version, and UNIQUE(plan_id, version) prevents accidental overwrites.

CREATE TABLE IF NOT EXISTS players (
    id         TEXT PRIMARY KEY,
    kind       TEXT NOT NULL,
    name       TEXT NOT NULL,
    metadata   JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS goals (
    id         TEXT PRIMARY KEY,
    player_id  TEXT REFERENCES players(id) ON DELETE SET NULL,
    objective  TEXT NOT NULL,
    metric     TEXT NOT NULL DEFAULT '',
    target     TEXT NOT NULL DEFAULT '',
    context    JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS plan (
    id              TEXT PRIMARY KEY,
    goal_id         TEXT NOT NULL REFERENCES goals(id) ON DELETE CASCADE,
    current_version INTEGER NOT NULL DEFAULT 0,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX IF NOT EXISTS plan_goal_id_key ON plan (goal_id);

CREATE TABLE IF NOT EXISTS plan_version (
    plan_id           TEXT NOT NULL REFERENCES plan(id) ON DELETE CASCADE,
    version           INTEGER NOT NULL,
    goal              TEXT NOT NULL,
    summary           TEXT NOT NULL,
    ranked_moves      JSONB NOT NULL,
    provenance        JSONB NOT NULL,
    input_snapshot_id TEXT NOT NULL,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (plan_id, version)
);

CREATE TABLE IF NOT EXISTS signal (
    id          TEXT PRIMARY KEY,
    goal_id     TEXT NOT NULL REFERENCES goals(id) ON DELETE CASCADE,
    kind        TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    payload     JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS signal_goal_id_created_at_idx ON signal (goal_id, created_at DESC);

CREATE TABLE IF NOT EXISTS outcome (
    id               TEXT PRIMARY KEY,
    goal_id          TEXT NOT NULL REFERENCES goals(id) ON DELETE CASCADE,
    move_title       TEXT NOT NULL DEFAULT '',
    result           TEXT NOT NULL,
    observed_signals JSONB NOT NULL DEFAULT '[]'::jsonb,
    notes            TEXT NOT NULL DEFAULT '',
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS outcome_goal_id_created_at_idx ON outcome (goal_id, created_at DESC);
