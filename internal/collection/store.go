package collection

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
)

// Store сохраняет задания, источники и checkpoint в PostgreSQL.
type Store struct {
	db *sql.DB
}

// NewStore создаёт repository очереди заданий.
func NewStore(db *sql.DB) *Store {
	return &Store{db: db}
}

// Ping проверяет готовность PostgreSQL.
func (s *Store) Ping(ctx context.Context) error {
	if err := s.db.PingContext(ctx); err != nil {
		return fmt.Errorf("ping collection db: %w", err)
	}

	return nil
}

// CreateManual создаёт ручное задание или возвращает ранее созданное по idempotency key.
func (s *Store) CreateManual(ctx context.Context, actorID uuid.UUID, input CreateInput) (Job, error) {
	job := Job{
		ID:                uuid.New(),
		Trigger:           "manual",
		RequestedBy:       &actorID,
		IdempotencyKey:    input.IdempotencyKey,
		PublishedFrom:     input.PublishedFrom,
		PublishedTo:       input.PublishedTo,
		MaxItemsPerSource: input.MaxItemsPerSource,
		Status:            JobQueued,
	}

	sources := make([]JobSource, 0, len(input.TelegramChannels)+len(input.WebsiteURLs))
	for _, channel := range input.TelegramChannels {
		sources = append(sources, JobSource{
			ID:       uuid.New(),
			JobID:    job.ID,
			Kind:     "telegram",
			SourceID: channel,
			Status:   SourceQueued,
		})
	}

	seenURLs := make(map[string]struct{}, len(input.WebsiteURLs))
	for _, rawURL := range input.WebsiteURLs {
		sourceID, normalized, err := NormalizeWebsiteURL(rawURL)
		if err != nil {
			return Job{}, err
		}
		if _, exists := seenURLs[normalized]; exists {
			continue
		}
		seenURLs[normalized] = struct{}{}
		sources = append(sources, JobSource{
			ID:       uuid.New(),
			JobID:    job.ID,
			Kind:     "website",
			SourceID: sourceID,
			URL:      normalized,
			Status:   SourceQueued,
		})
	}

	created, err := s.create(ctx, job, sources)
	if err != nil {
		return Job{}, err
	}
	if !created {
		return s.GetByIdempotency(ctx, input.IdempotencyKey)
	}

	return s.Get(ctx, job.ID)
}

// CreateScheduled создаёт идемпотентное scheduled или bootstrap задание.
func (s *Store) CreateScheduled(ctx context.Context, trigger string, key uuid.UUID, channels []string, from, to time.Time, limit int) (Job, error) {
	job := Job{
		ID:                uuid.New(),
		Trigger:           trigger,
		IdempotencyKey:    key,
		PublishedFrom:     &from,
		PublishedTo:       &to,
		MaxItemsPerSource: limit,
		Status:            JobQueued,
	}

	sources := make([]JobSource, 0, len(channels))
	for _, channel := range channels {
		sources = append(sources, JobSource{
			ID:       uuid.New(),
			JobID:    job.ID,
			Kind:     "telegram",
			SourceID: channel,
			Status:   SourceQueued,
		})
	}

	created, err := s.create(ctx, job, sources)
	if err != nil {
		return Job{}, err
	}
	if !created {
		return s.GetByIdempotency(ctx, key)
	}

	return s.Get(ctx, job.ID)
}

// create сохраняет задание и его источники одной транзакцией.
func (s *Store) create(ctx context.Context, job Job, sources []JobSource) (bool, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, fmt.Errorf("begin create collection job: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	_, err = tx.ExecContext(ctx, `INSERT INTO collection_jobs (id,trigger_type,requested_by,idempotency_key,published_from,published_to,max_items_per_source,status) VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`,
		job.ID, job.Trigger, job.RequestedBy, job.IdempotencyKey, job.PublishedFrom, job.PublishedTo, job.MaxItemsPerSource, job.Status)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return false, nil
		}

		return false, fmt.Errorf("insert collection job: %w", err)
	}

	for _, source := range sources {
		if _, err := tx.ExecContext(ctx, `INSERT INTO collection_job_sources (id,job_id,source_kind,source_id,source_url,status) VALUES ($1,$2,$3,$4,$5,$6)`, source.ID, source.JobID, source.Kind, source.SourceID, source.URL, source.Status); err != nil {
			return false, fmt.Errorf("insert collection job source: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return false, fmt.Errorf("commit collection job: %w", err)
	}

	return true, nil
}

// Claim атомарно арендует следующее задание worker'у.
func (s *Store) Claim(ctx context.Context, owner string, lease time.Duration) (*Job, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin claim collection job: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	var claimLock bool
	if err := tx.QueryRowContext(ctx, `SELECT pg_try_advisory_xact_lock(hashtext('task-hunter/collection-claim'))`).Scan(&claimLock); err != nil {
		return nil, fmt.Errorf("lock collection job claim: %w", err)
	}
	if !claimLock {
		return nil, nil
	}

	row := tx.QueryRowContext(ctx, `SELECT id FROM collection_jobs
WHERE (status='running' AND lease_expires_at < now())
   OR (status='queued' AND NOT EXISTS (
       SELECT 1 FROM collection_jobs active
       WHERE active.status='running' AND active.lease_expires_at >= now()
   ))
ORDER BY created_at,id FOR UPDATE SKIP LOCKED LIMIT 1`)

	var id uuid.UUID
	if err := row.Scan(&id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}

		return nil, fmt.Errorf("select collection job for claim: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE collection_jobs SET status='running',lease_owner=$1,lease_expires_at=now()+make_interval(secs => $2),started_at=COALESCE(started_at,now()),updated_at=now() WHERE id=$3`, owner, int(lease.Seconds()), id); err != nil {
		return nil, fmt.Errorf("claim collection job: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit collection job claim: %w", err)
	}
	job, err := s.Get(ctx, id)
	if err != nil {
		return nil, err
	}

	return &job, nil
}

// RenewLease продлевает аренду только текущему владельцу активного задания.
func (s *Store) RenewLease(ctx context.Context, id uuid.UUID, owner string, lease time.Duration) error {
	tag, err := s.db.ExecContext(ctx, `UPDATE collection_jobs SET lease_expires_at=now()+make_interval(secs => $1),updated_at=now() WHERE id=$2 AND status='running' AND lease_owner=$3`, int(lease.Seconds()), id, owner)
	if err != nil {
		return fmt.Errorf("renew collection job lease: %w", err)
	}

	affected, err := tag.RowsAffected()
	if err != nil {
		return fmt.Errorf("read collection job lease renewal: %w", err)
	}

	if affected == 0 {
		return fmt.Errorf("collection job lease is no longer owned")
	}

	return nil
}

// RequeueFailed возвращает полностью неуспешный bootstrap в очередь после устранения причины.
func (s *Store) RequeueFailed(ctx context.Context, id uuid.UUID) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin requeue collection job: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	if _, err := tx.ExecContext(ctx, `UPDATE collection_job_sources SET status='queued',collected_total=0,imported_total=0,duplicates_total=0,invalid_total=0,error_message='',updated_at=now() WHERE job_id=$1 AND status='failed'`, id); err != nil {
		return fmt.Errorf("requeue failed collection sources: %w", err)
	}

	tag, err := tx.ExecContext(ctx, `UPDATE collection_jobs SET status='queued',collected_total=0,imported_total=0,duplicates_total=0,invalid_total=0,error_count=0,error_message='',lease_owner=NULL,lease_expires_at=NULL,started_at=NULL,finished_at=NULL,updated_at=now() WHERE id=$1 AND status='failed'`, id)
	if err != nil {
		return fmt.Errorf("requeue failed collection job: %w", err)
	}

	affected, err := tag.RowsAffected()
	if err != nil {
		return fmt.Errorf("read requeue collection job result: %w", err)
	}

	if affected == 0 {
		return fmt.Errorf("collection job is not failed")
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit requeue collection job: %w", err)
	}

	return nil
}

// Get возвращает задание вместе с источниками.
func (s *Store) Get(ctx context.Context, id uuid.UUID) (Job, error) {
	job, err := scanJob(s.db.QueryRowContext(ctx, `SELECT id,trigger_type,requested_by,idempotency_key,published_from,published_to,max_items_per_source,status,collected_total,imported_total,duplicates_total,invalid_total,error_count,error_message,notification_acknowledged_at IS NOT NULL,started_at,finished_at,created_at,updated_at FROM collection_jobs WHERE id=$1`, id))
	if err != nil {
		return Job{}, fmt.Errorf("get collection job: %w", err)
	}

	sources, err := s.listSources(ctx, id)
	if err != nil {
		return Job{}, err
	}
	job.Sources = sources

	return job, nil
}

// GetByIdempotency возвращает ранее созданное задание.
func (s *Store) GetByIdempotency(ctx context.Context, key uuid.UUID) (Job, error) {
	var id uuid.UUID
	if err := s.db.QueryRowContext(ctx, `SELECT id FROM collection_jobs WHERE idempotency_key=$1`, key).Scan(&id); err != nil {
		return Job{}, fmt.Errorf("get collection job by idempotency: %w", err)
	}

	return s.Get(ctx, id)
}

// List возвращает журнал заданий; unreadOnly ограничивает уведомления инициатора.
func (s *Store) List(ctx context.Context, actorID uuid.UUID, unreadOnly bool, limit, offset int) ([]Job, error) {
	query := `SELECT id,trigger_type,requested_by,idempotency_key,published_from,published_to,max_items_per_source,status,collected_total,imported_total,duplicates_total,invalid_total,error_count,error_message,notification_acknowledged_at IS NOT NULL,started_at,finished_at,created_at,updated_at FROM collection_jobs`
	args := []any{limit, offset}

	if unreadOnly {
		query += ` WHERE requested_by=$3 AND trigger_type='manual' AND status IN ('succeeded','partial','failed') AND notification_acknowledged_at IS NULL`
		args = append(args, actorID)
	}

	query += ` ORDER BY created_at DESC,id DESC LIMIT $1 OFFSET $2`
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list collection jobs: %w", err)
	}
	defer func() { _ = rows.Close() }()

	items := make([]Job, 0)
	for rows.Next() {
		job, scanErr := scanJob(rows)
		if scanErr != nil {
			return nil, fmt.Errorf("scan collection job: %w", scanErr)
		}
		items = append(items, job)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate collection jobs: %w", err)
	}

	return items, nil
}

// StartSource переводит отдельный источник в running.
func (s *Store) StartSource(ctx context.Context, id uuid.UUID) error {
	_, err := s.db.ExecContext(ctx, `UPDATE collection_job_sources SET status='running',updated_at=now() WHERE id=$1`, id)
	if err != nil {
		return fmt.Errorf("start collection source: %w", err)

	}

	return nil
}

// FinishSource сохраняет итог отдельного источника.
func (s *Store) FinishSource(ctx context.Context, source JobSource) error {
	_, err := s.db.ExecContext(ctx, `UPDATE collection_job_sources SET status=$1,collected_total=$2,imported_total=$3,duplicates_total=$4,invalid_total=$5,error_message=$6,updated_at=now() WHERE id=$7`,
		source.Status, source.CollectedTotal, source.ImportedTotal, source.DuplicatesTotal, source.InvalidTotal, source.ErrorMessage, source.ID)
	if err != nil {
		return fmt.Errorf("finish collection source: %w", err)
	}

	return nil
}

// FinishJob агрегирует source-результаты и завершает job.
func (s *Store) FinishJob(ctx context.Context, id uuid.UUID) error {
	_, err := s.db.ExecContext(ctx, `UPDATE collection_jobs j SET
collected_total=x.collected,imported_total=x.imported,duplicates_total=x.duplicates,invalid_total=x.invalid,error_count=x.errors,
status=CASE WHEN x.errors=0 THEN 'succeeded' WHEN x.successes=0 THEN 'failed' ELSE 'partial' END,
error_message=CASE WHEN x.errors>0 THEN 'часть источников завершилась с ошибкой' ELSE '' END,
lease_owner=NULL,lease_expires_at=NULL,finished_at=now(),updated_at=now()
FROM (SELECT job_id,COALESCE(sum(collected_total),0) collected,COALESCE(sum(imported_total),0) imported,
COALESCE(sum(duplicates_total),0) duplicates,COALESCE(sum(invalid_total),0) invalid,
count(*) FILTER (WHERE status='failed') errors,count(*) FILTER (WHERE status IN ('succeeded','truncated')) successes
FROM collection_job_sources WHERE job_id=$1 GROUP BY job_id) x WHERE j.id=x.job_id`, id)
	if err != nil {
		return fmt.Errorf("finish collection job: %w", err)
	}

	return nil
}

// Acknowledge помечает terminal notification прочитанным только владельцем.
func (s *Store) Acknowledge(ctx context.Context, id, actorID uuid.UUID) error {
	tag, err := s.db.ExecContext(ctx, `UPDATE collection_jobs SET notification_acknowledged_at=now(),updated_at=now() WHERE id=$1 AND requested_by=$2 AND status IN ('succeeded','partial','failed')`, id, actorID)
	if err != nil {
		return fmt.Errorf("acknowledge collection job: %w", err)
	}

	affected, err := tag.RowsAffected()
	if err != nil {
		return fmt.Errorf("read acknowledge result: %w", err)
	}

	if affected == 0 {
		return fmt.Errorf("collection job not found or is not terminal")
	}

	return nil
}

// GetCheckpoint возвращает persisted Telegram checkpoint.
func (s *Store) GetCheckpoint(ctx context.Context, sourceID string) (*Checkpoint, error) {
	var checkpoint Checkpoint
	err := s.db.QueryRowContext(ctx, `SELECT source_id,last_message_id,last_published_at FROM collection_checkpoints WHERE source_id=$1`, sourceID).
		Scan(&checkpoint.SourceID, &checkpoint.LastMessageID, &checkpoint.LastPublishedAt)

	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get collection checkpoint: %w", err)
	}

	return &checkpoint, nil
}

// UpsertCheckpoint продвигает Telegram checkpoint только вперёд.
func (s *Store) UpsertCheckpoint(ctx context.Context, checkpoint Checkpoint) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO collection_checkpoints (source_id,last_message_id,last_published_at) VALUES ($1,$2,$3)
ON CONFLICT (source_id) DO UPDATE SET last_message_id=EXCLUDED.last_message_id,last_published_at=EXCLUDED.last_published_at,updated_at=now()
WHERE collection_checkpoints.last_message_id < EXCLUDED.last_message_id`, checkpoint.SourceID, checkpoint.LastMessageID, checkpoint.LastPublishedAt)
	if err != nil {
		return fmt.Errorf("upsert collection checkpoint: %w", err)
	}

	return nil
}

// listSources возвращает источники задания в стабильном порядке.
func (s *Store) listSources(ctx context.Context, jobID uuid.UUID) ([]JobSource, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,job_id,source_kind,source_id,source_url,status,collected_total,imported_total,duplicates_total,invalid_total,error_message FROM collection_job_sources WHERE job_id=$1 ORDER BY created_at,id`, jobID)
	if err != nil {
		return nil, fmt.Errorf("list collection job sources: %w", err)
	}
	defer func() { _ = rows.Close() }()

	items := make([]JobSource, 0)
	for rows.Next() {
		var item JobSource
		if err := rows.Scan(&item.ID, &item.JobID, &item.Kind, &item.SourceID, &item.URL, &item.Status, &item.CollectedTotal, &item.ImportedTotal, &item.DuplicatesTotal, &item.InvalidTotal, &item.ErrorMessage); err != nil {
			return nil, fmt.Errorf("scan collection job source: %w", err)
		}
		items = append(items, item)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate collection job sources: %w", err)
	}

	return items, nil
}

// scanJob считывает основную строку задания.
func scanJob(row interface{ Scan(...any) error }) (Job, error) {
	var job Job
	err := row.Scan(&job.ID, &job.Trigger, &job.RequestedBy, &job.IdempotencyKey, &job.PublishedFrom, &job.PublishedTo,
		&job.MaxItemsPerSource, &job.Status, &job.CollectedTotal, &job.ImportedTotal, &job.DuplicatesTotal,
		&job.InvalidTotal, &job.ErrorCount, &job.ErrorMessage, &job.NotificationAcknowledged,
		&job.StartedAt, &job.FinishedAt, &job.CreatedAt, &job.UpdatedAt)

	return job, err
}
