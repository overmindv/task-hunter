-- +goose Up
CREATE TABLE collection_jobs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    trigger_type TEXT NOT NULL,
    requested_by UUID,
    idempotency_key UUID NOT NULL,
    published_from TIMESTAMPTZ,
    published_to TIMESTAMPTZ,
    max_items_per_source INTEGER NOT NULL DEFAULT 100,
    status TEXT NOT NULL DEFAULT 'queued',
    collected_total INTEGER NOT NULL DEFAULT 0,
    imported_total INTEGER NOT NULL DEFAULT 0,
    duplicates_total INTEGER NOT NULL DEFAULT 0,
    invalid_total INTEGER NOT NULL DEFAULT 0,
    error_count INTEGER NOT NULL DEFAULT 0,
    error_message TEXT NOT NULL DEFAULT '',
    notification_acknowledged_at TIMESTAMPTZ,
    lease_owner TEXT,
    lease_expires_at TIMESTAMPTZ,
    started_at TIMESTAMPTZ,
    finished_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT collection_jobs_trigger_check CHECK (trigger_type IN ('manual', 'scheduled', 'bootstrap')),
    CONSTRAINT collection_jobs_status_check CHECK (status IN ('queued', 'running', 'succeeded', 'partial', 'failed')),
    CONSTRAINT collection_jobs_limit_check CHECK (max_items_per_source BETWEEN 1 AND 500),
    CONSTRAINT collection_jobs_idempotency_key UNIQUE (idempotency_key)
);

CREATE TABLE collection_job_sources (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    job_id UUID NOT NULL REFERENCES collection_jobs(id) ON DELETE CASCADE,
    source_kind TEXT NOT NULL,
    source_id TEXT NOT NULL,
    source_url TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'queued',
    collected_total INTEGER NOT NULL DEFAULT 0,
    imported_total INTEGER NOT NULL DEFAULT 0,
    duplicates_total INTEGER NOT NULL DEFAULT 0,
    invalid_total INTEGER NOT NULL DEFAULT 0,
    error_message TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT collection_job_sources_kind_check CHECK (source_kind IN ('telegram', 'website')),
    CONSTRAINT collection_job_sources_status_check CHECK (status IN ('queued', 'running', 'succeeded', 'failed', 'truncated')),
    CONSTRAINT collection_job_sources_job_source_key UNIQUE (job_id, source_kind, source_id, source_url)
);

CREATE TABLE collection_checkpoints (
    source_id TEXT PRIMARY KEY,
    last_message_id BIGINT NOT NULL,
    last_published_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX collection_jobs_worker_idx
    ON collection_jobs (status, lease_expires_at, created_at)
    WHERE status IN ('queued', 'running');
CREATE INDEX collection_jobs_notifications_idx
    ON collection_jobs (requested_by, finished_at DESC)
    WHERE trigger_type = 'manual'
      AND status IN ('succeeded', 'partial', 'failed')
      AND notification_acknowledged_at IS NULL;
CREATE INDEX collection_job_sources_job_idx ON collection_job_sources (job_id, created_at, id);

-- +goose Down
DROP TABLE IF EXISTS collection_checkpoints;
DROP TABLE IF EXISTS collection_job_sources;
DROP TABLE IF EXISTS collection_jobs;
