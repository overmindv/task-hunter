//go:build component

package collection

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/overmindv/task-hunter/internal/parser/domain"
)

// Общий advisory lock Claim действует на всю БД, включая разные схемы.
var leaseDBTestMu sync.Mutex

const (
	failureDatabase = "database_error"
	failureImport   = "import_cancel"
)

// leaseTestStore изолирует таблицы каждого теста в отдельной схеме тестовой БД.
func leaseTestStore(t *testing.T) *Store {
	t.Helper()
	leaseDBTestMu.Lock()
	t.Cleanup(leaseDBTestMu.Unlock)
	dsn := os.Getenv("COMPONENT_TEST_DSN")
	if dsn == "" {
		t.Fatal("COMPONENT_TEST_DSN must point to a dedicated test database")
	}
	ctx := context.Background()
	db, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	schema := "lease_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if _, err = db.Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = db.Exec(ctx, "DROP SCHEMA "+schema+" CASCADE"); db.Close() })
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = schema
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	migration, err := os.ReadFile("../../migrations/002_collection_jobs.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, strings.Split(string(migration), "-- +goose Down")[0]); err != nil {
		t.Fatal(err)
	}

	return NewStore(pool)
}

// newLeaseTestJob создаёт ручное задание без внешних запросов.
func newLeaseTestJob(t *testing.T, s *Store) Job {
	t.Helper()
	job, err := s.CreateManual(context.Background(), uuid.New(), CreateInput{IdempotencyKey: uuid.New(), TelegramChannels: []string{"test-channel"}, MaxItemsPerSource: 10})
	if err != nil {
		t.Fatal(err)
	}

	return job
}

// TestExpiredLeaseCannotRenew запрещает оживлять просроченную аренду.
func TestExpiredLeaseCannotRenew(t *testing.T) {
	t.Parallel()
	s := leaseTestStore(t)
	ctx := context.Background()
	job := newLeaseTestJob(t, s)
	claimed, err := s.Claim(ctx, "worker", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(ctx, "UPDATE collection_jobs SET lease_expires_at=clock_timestamp()-interval '1 second' WHERE id=$1", job.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.RenewLease(ctx, job.ID, claimed.LeaseToken, time.Minute); err == nil {
		t.Fatal("expired lease was renewed")
	}
}

// TestClaimFencesStaleWrites проверяет повторный claim тем же worker и поздние записи.
func TestClaimFencesStaleWrites(t *testing.T) {
	t.Parallel()
	s := leaseTestStore(t)
	ctx := context.Background()
	created := newLeaseTestJob(t, s)
	old, err := s.Claim(ctx, "same-worker", time.Minute)
	if err != nil || old == nil {
		t.Fatalf("claim: %v", err)
	}
	if err = s.StartSource(ctx, *old, created.Sources[0].ID); err != nil {
		t.Fatal(err)
	}
	if _, err = s.db.Exec(ctx, "UPDATE collection_jobs SET lease_expires_at=clock_timestamp()-interval '1 second' WHERE id=$1", old.ID); err != nil {
		t.Fatal(err)
	}
	current, err := s.Claim(ctx, "same-worker", time.Minute)
	if err != nil || current == nil {
		t.Fatalf("reclaim: %v", err)
	}
	if current.LeaseToken == old.LeaseToken {
		t.Fatal("reclaim reused fencing token")
	}
	result := current.Sources[0]
	result.Status = SourceSucceeded
	result.ImportedTotal = 2
	checkpoint := Checkpoint{SourceID: result.SourceID, LastMessageID: 2, LastPublishedAt: time.Now()}
	if err = s.FinishSource(ctx, *current, result); err != nil {
		t.Fatal(err)
	}
	if err = s.UpsertCheckpoint(ctx, *current, checkpoint); err != nil {
		t.Fatal(err)
	}
	stale := result
	stale.Status = SourceFailed
	stale.ImportedTotal = 99
	checkpoint.LastMessageID = 99
	writes := []func() error{
		func() error { return s.StartSource(ctx, *old, result.ID) },
		func() error { return s.FinishSource(ctx, *old, stale) },
		func() error { return s.UpsertCheckpoint(ctx, *old, checkpoint) },
		func() error { return s.RenewLease(ctx, old.ID, old.LeaseToken, time.Minute) },
		func() error { return s.FinishJob(ctx, *old) },
	}
	for i, write := range writes {
		if err = write(); !errors.Is(err, ErrLeaseLost) {
			t.Fatalf("stale write %d: %v", i, err)
		}
	}
	// Два finish одновременно: старая попытка обязана проиграть независимо от порядка.
	done := make(chan error, 1)
	go func() { done <- s.FinishJob(ctx, *old) }()
	if err = s.FinishJob(ctx, *current); err != nil {
		t.Fatal(err)
	}
	if err = <-done; !errors.Is(err, ErrLeaseLost) {
		t.Fatalf("concurrent stale finish: %v", err)
	}
	saved, err := s.Get(ctx, current.ID)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Status != JobSucceeded || saved.ImportedTotal != 2 {
		t.Fatalf("stale result persisted: %+v", saved)
	}
	cp, err := s.GetCheckpoint(ctx, result.SourceID)
	if err != nil || cp == nil || cp.LastMessageID != 2 {
		t.Fatalf("checkpoint changed: %+v %v", cp, err)
	}
	encoded, err := json.Marshal(current)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), current.LeaseToken) {
		t.Fatal("lease token exposed by API")
	}
}

// TestExpiredClaimCannotWrite не разрешает запись даже до появления нового worker.
func TestExpiredClaimCannotWrite(t *testing.T) {
	t.Parallel()
	s := leaseTestStore(t)
	ctx := context.Background()
	newLeaseTestJob(t, s)
	job, err := s.Claim(ctx, "worker", time.Minute)
	if err != nil || job == nil {
		t.Fatal(err)
	}
	if _, err = s.db.Exec(ctx, "UPDATE collection_jobs SET lease_expires_at=clock_timestamp()-interval '1 second' WHERE id=$1", job.ID); err != nil {
		t.Fatal(err)
	}
	if err = s.FinishSource(ctx, *job, job.Sources[0]); !errors.Is(err, ErrLeaseLost) {
		t.Fatalf("expired write: %v", err)
	}
	if err = s.FinishJob(ctx, *job); !errors.Is(err, ErrLeaseLost) {
		t.Fatalf("expired finish: %v", err)
	}
}

// cancelWebsiteReader ждёт отмены контекста, не обращаясь к внешнему источнику.
type cancelWebsiteReader struct{ started chan struct{} }

func (r *cancelWebsiteReader) ReadURL(ctx context.Context, _, _ string) (WebsiteBatch, error) {
	close(r.started)
	<-ctx.Done()

	return WebsiteBatch{}, ctx.Err()
}
func (r *cancelWebsiteReader) Close() {}

// TestWorkerCancelsAfterRenewalFailure проверяет потерю token и ошибку самой БД.
func TestWorkerCancelsAfterRenewalFailure(t *testing.T) {
	t.Parallel()
	for _, failure := range []string{"owner_changed", failureDatabase, failureImport, "renewal_timeout"} {
		t.Run(failure, func(t *testing.T) {
			t.Parallel()
			s := leaseTestStore(t)
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			job, err := s.CreateManual(ctx, uuid.New(), CreateInput{IdempotencyKey: uuid.New(), WebsiteURLs: []string{"https://leetcode.com/problems/two-sum/"}, MaxItemsPerSource: 10})
			if err != nil {
				t.Fatal(err)
			}
			reader := &cancelWebsiteReader{started: make(chan struct{})}
			worker := NewWorker(s, nil, reader, nil, "worker", time.Second, 300*time.Millisecond)
			started := reader.started
			if failure == failureImport {
				sink := &cancelCandidateSink{started: make(chan struct{})}
				worker.sink = sink
				worker.websites = &stubWebsiteReader{batch: WebsiteBatch{RawTasks: []domain.RawTask{{Source: domain.Source{ID: domain.SourceLeetCode, Name: "LeetCode", Type: domain.SourceTypeWebsite}, SourceURL: "https://leetcode.com/problems/two-sum/", Title: "Two sum", Statement: "Find two numbers in the array."}}}}
				started = sink.started
			}
			done := make(chan error, 1)
			go func() { done <- worker.runNext(ctx) }()
			select {
			case <-started:
			case <-ctx.Done():
				t.Fatal("reader did not start")
			}
			switch failure {
			case "owner_changed", failureImport:
				_, err = s.db.Exec(ctx, "UPDATE collection_jobs SET lease_owner='new-owner' WHERE id=$1", job.ID)
			case failureDatabase:
				_, err = s.db.Exec(ctx, "ALTER TABLE collection_jobs RENAME TO unavailable_jobs")
			default:
				// Удерживаем строку, чтобы продление упёрлось в lock до своего deadline.
				tx, beginErr := s.db.Begin(ctx)
				if beginErr != nil {
					t.Fatal(beginErr)
				}
				defer tx.Rollback(context.Background()) //nolint:errcheck
				_, err = tx.Exec(ctx, "SELECT id FROM collection_jobs WHERE id=$1 FOR UPDATE", job.ID)
			}
			if err != nil {
				t.Fatal(err)
			}
			select {
			case err = <-done:
				if err == nil {
					t.Fatal("lease loss reported success")
				}
			case <-time.After(time.Second):
				t.Fatal("reader was not canceled after renewal failure")
			}
			if failure == failureDatabase {
				if _, err = s.db.Exec(ctx, "ALTER TABLE unavailable_jobs RENAME TO collection_jobs"); err != nil {
					t.Fatal(err)
				}
			}
			saved, err := s.Get(context.Background(), job.ID)
			if err != nil {
				t.Fatal(err)
			}
			if saved.Status != JobRunning || saved.Sources[0].Status != SourceRunning {
				t.Fatalf("canceled attempt changed terminal state: %+v", saved)
			}
		})
	}
}

// cancelCandidateSink имитирует запрос импорта, ожидающий отмены контекста.
type cancelCandidateSink struct{ started chan struct{} }

func (s *cancelCandidateSink) Import(ctx context.Context, _ []Candidate) ([]ImportResult, error) {
	close(s.started)
	<-ctx.Done()

	return nil, ctx.Err()
}

// TestLeaseExpiresWhileWaitingForLock проверяет время после ожидания блокировки.
func TestLeaseExpiresWhileWaitingForLock(t *testing.T) {
	t.Parallel()
	s := leaseTestStore(t)
	ctx := context.Background()
	newLeaseTestJob(t, s)
	job, err := s.Claim(ctx, "worker", 250*time.Millisecond)
	if err != nil || job == nil {
		t.Fatal(err)
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	if _, err = tx.Exec(ctx, "SELECT id FROM collection_jobs WHERE id=$1 FOR UPDATE", job.ID); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- s.StartSource(ctx, *job, job.Sources[0].ID) }()
	// Аренда истекает, пока независимая транзакция удерживает строку.
	if _, err = tx.Exec(ctx, "SELECT pg_sleep(0.35)"); err != nil {
		t.Fatal(err)
	}
	if err = tx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	if err = <-done; !errors.Is(err, ErrLeaseLost) {
		t.Fatalf("expired waiter wrote state: %v", err)
	}
	saved, err := s.Get(ctx, job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Sources[0].Status != SourceQueued {
		t.Fatal("expired waiter started source")
	}
}

// releaseWebsiteReader удерживает сбор до явного завершения сценария.
type releaseWebsiteReader struct{ started, release chan struct{} }

func (r *releaseWebsiteReader) ReadURL(ctx context.Context, _, _ string) (WebsiteBatch, error) {
	close(r.started)
	select {
	case <-r.release:
		return WebsiteBatch{}, nil
	case <-ctx.Done():
		return WebsiteBatch{}, ctx.Err()
	}
}
func (r *releaseWebsiteReader) Close() {}

// TestWorkerRenewsAndCompletes проверяет успешный сбор дольше исходной аренды.
func TestWorkerRenewsAndCompletes(t *testing.T) {
	t.Parallel()
	s := leaseTestStore(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	job, err := s.CreateManual(ctx, uuid.New(), CreateInput{IdempotencyKey: uuid.New(), WebsiteURLs: []string{"https://leetcode.com/problems/two-sum/"}, MaxItemsPerSource: 10})
	if err != nil {
		t.Fatal(err)
	}
	reader := &releaseWebsiteReader{started: make(chan struct{}), release: make(chan struct{})}
	worker := NewWorker(s, nil, reader, nil, "worker", time.Second, 300*time.Millisecond)
	done := make(chan error, 1)
	go func() { done <- worker.runNext(ctx) }()
	select {
	case <-reader.started:
	case <-ctx.Done():
		t.Fatal("reader did not start")
	}
	var original time.Time
	if err = s.db.QueryRow(ctx, "SELECT lease_expires_at FROM collection_jobs WHERE id=$1", job.ID).Scan(&original); err != nil {
		t.Fatal(err)
	}
	if _, err = s.db.Exec(ctx, "SELECT pg_sleep(0.7)"); err != nil {
		t.Fatal(err)
	}
	var renewed time.Time
	if err = s.db.QueryRow(ctx, "SELECT lease_expires_at FROM collection_jobs WHERE id=$1", job.ID).Scan(&renewed); err != nil {
		t.Fatal(err)
	}
	if !renewed.After(original) {
		t.Fatal("lease was not extended")
	}
	other, err := s.Claim(ctx, "other-worker", time.Minute)
	if err != nil || other != nil {
		t.Fatalf("active job reclaimed: %+v %v", other, err)
	}
	close(reader.release)
	select {
	case err = <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal("worker did not complete")
	}
	saved, err := s.Get(ctx, job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Status != JobSucceeded {
		t.Fatalf("job status: %s", saved.Status)
	}
}
