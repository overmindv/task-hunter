-- +goose Up
CREATE TABLE tasks (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    title           TEXT NOT NULL,
    description     TEXT NOT NULL,
    source_id       TEXT NOT NULL,
    source_url      TEXT NOT NULL,
    source_hash     TEXT NOT NULL UNIQUE,
    type            INT NOT NULL,
    difficulty      INT NOT NULL DEFAULT 0,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE examples (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    task_id     UUID NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    input       TEXT NOT NULL,
    output      TEXT NOT NULL,
    explanation TEXT
);

CREATE TABLE task_tags (
    task_id UUID NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    tag     TEXT NOT NULL,
    PRIMARY KEY (task_id, tag)
);

CREATE INDEX idx_tasks_source_id      ON tasks(source_id);
CREATE INDEX idx_tasks_type           ON tasks(type);
CREATE INDEX idx_tasks_difficulty     ON tasks(difficulty);
CREATE INDEX idx_tasks_source_hash    ON tasks(source_hash);
CREATE INDEX idx_tasks_created_at     ON tasks(created_at DESC);
CREATE INDEX idx_examples_task_id     ON examples(task_id);
CREATE INDEX idx_task_tags_tag        ON task_tags(tag);

-- +goose Down
DROP TABLE IF EXISTS task_tags CASCADE;
DROP TABLE IF EXISTS examples CASCADE;
DROP TABLE IF EXISTS tasks CASCADE;
