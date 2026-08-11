package collection

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/overmindv/task-hunter/internal/parser/domain"
	"github.com/overmindv/task-hunter/internal/parser/pipeline"
)

// Worker последовательно исполняет задания персистентной очереди.
type Worker struct {
	store        *Store
	telegram     TelegramReader
	websites     WebsiteReader
	sink         CandidateSink
	pipeline     *pipeline.Pipeline
	owner        string
	pollInterval time.Duration
	lease        time.Duration
	wg           sync.WaitGroup
}

// NewWorker создаёт worker с полноценным конвейером нормализации.
func NewWorker(store *Store, telegram TelegramReader, websites WebsiteReader, sink CandidateSink, owner string, pollInterval, lease time.Duration) *Worker {
	processor := pipeline.NewPipeline()
	processor.AddProcessor("extractor", pipeline.NewExtractor())
	processor.AddProcessor("parser", pipeline.NewParser())
	processor.AddProcessor("normalizer", pipeline.NewNormalizer())
	processor.AddProcessor("validator", pipeline.NewValidator())

	return &Worker{
		store:        store,
		telegram:     telegram,
		websites:     websites,
		sink:         sink,
		pipeline:     processor,
		owner:        owner,
		pollInterval: pollInterval,
		lease:        lease,
	}
}

// Run опрашивает очередь до отмены контекста и ожидает текущую job при shutdown.
func (w *Worker) Run(ctx context.Context) {
	ticker := time.NewTicker(w.pollInterval)
	defer ticker.Stop()
	for {
		if err := w.runNext(ctx); err != nil && ctx.Err() == nil {
			slog.Error("collection worker iteration failed", "error", err)
		}
		select {
		case <-ctx.Done():
			w.wg.Wait()

			return
		case <-ticker.C:
		}
	}
}

// runNext арендует и выполняет не более одного задания.
func (w *Worker) runNext(ctx context.Context) error {
	job, err := w.store.Claim(ctx, w.owner, w.lease)
	if err != nil {
		return fmt.Errorf("claim job: %w", err)
	}
	if job == nil {
		return nil
	}
	w.wg.Add(1)
	defer w.wg.Done()
	leaseCtx, cancelLease := context.WithCancel(ctx)
	defer cancelLease()
	go w.renewLease(leaseCtx, job.ID)

	for _, source := range job.Sources {
		if source.Status == SourceSucceeded || source.Status == SourceTruncated {
			continue
		}
		if err := w.store.StartSource(ctx, source.ID); err != nil {
			return fmt.Errorf("start source: %w", err)
		}
		result := w.processSource(ctx, *job, source)
		if err := w.store.FinishSource(ctx, result); err != nil {
			return fmt.Errorf("finish source: %w", err)
		}
	}
	if err := w.store.FinishJob(ctx, job.ID); err != nil {
		return fmt.Errorf("finish job: %w", err)
	}

	return nil
}

// renewLease не позволяет второй реплике забрать долгую активную job.
func (w *Worker) renewLease(ctx context.Context, jobID uuid.UUID) {
	interval := w.lease / 3
	if interval < time.Second {
		interval = time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := w.store.RenewLease(ctx, jobID, w.owner, w.lease); err != nil {
				slog.Error("renew collection job lease", "job_id", jobID, "error", err)
			}
		}
	}
}

// processSource изолирует ошибку одного источника от остальных источников job.
func (w *Worker) processSource(ctx context.Context, job Job, source JobSource) JobSource {
	result := source
	result.Status = SourceFailed
	var candidates []Candidate
	var err error
	switch source.Kind {
	case "telegram":
		candidates, err = w.collectTelegram(ctx, job, source)
	case "website":
		candidates, err = w.collectWebsite(ctx, job, source)
	default:
		err = fmt.Errorf("unsupported source kind %q", source.Kind)
	}
	if err != nil {
		result.ErrorMessage = safeError(err)

		return result
	}
	result.CollectedTotal = len(candidates)
	imported, duplicates, invalid, successful, err := w.importCandidates(ctx, candidates)
	result.ImportedTotal = imported
	result.DuplicatesTotal = duplicates
	result.InvalidTotal = invalid
	if err != nil {
		result.ErrorMessage = safeError(err)

		return result
	}
	if len(candidates) > 0 && len(successful) == 0 {
		result.ErrorMessage = "tasks-it не принял ни одного кандидата"

		return result
	}
	if source.Kind == "telegram" && job.Trigger != "manual" && len(successful) > 0 {
		last := successful[0]
		for _, candidate := range successful[1:] {
			if candidate.MessageID > last.MessageID {
				last = candidate
			}
		}
		if err := w.store.UpsertCheckpoint(ctx, Checkpoint{SourceID: source.SourceID, LastMessageID: last.MessageID, LastPublishedAt: *last.SourcePublishedAt}); err != nil {
			result.ErrorMessage = safeError(err)

			return result
		}
	}
	result.Status = SourceSucceeded
	if len(candidates) == job.MaxItemsPerSource {
		result.Status = SourceTruncated
	}

	return result
}

// collectTelegram учитывает checkpoint только для планового и bootstrap-сбора.
func (w *Worker) collectTelegram(ctx context.Context, job Job, source JobSource) ([]Candidate, error) {
	if job.PublishedFrom == nil || job.PublishedTo == nil {
		return nil, fmt.Errorf("Telegram job has no publication range")
	}
	afterID := int64(0)
	publishedFrom := *job.PublishedFrom
	if job.Trigger != "manual" {
		checkpoint, err := w.store.GetCheckpoint(ctx, source.SourceID)
		if err != nil {
			return nil, err
		}
		if checkpoint != nil {
			afterID = checkpoint.LastMessageID
			if checkpoint.LastPublishedAt.Before(publishedFrom) {
				publishedFrom = checkpoint.LastPublishedAt
			}
		}
	}
	messages, err := w.telegram.ReadRange(ctx, source.SourceID, publishedFrom, *job.PublishedTo, afterID, job.MaxItemsPerSource)
	if err != nil {
		return nil, err
	}
	items := make([]Candidate, 0, len(messages))
	for _, message := range messages {
		raw := domain.RawTask{
			Source:     domain.Source{ID: domain.SourceManual, Name: source.SourceID, Type: domain.SourceTypeTelegram},
			RawContent: []byte(message.Text), SourceURL: message.URL, RetrievedAt: time.Now().UTC(),
		}
		candidate, err := w.normalize(ctx, job.ID, "telegram:"+source.SourceID, fmt.Sprintf("%s:%d", source.SourceID, message.ID), raw, &message.PublishedAt, message.ID)
		if err != nil {
			return nil, fmt.Errorf("normalize Telegram message %d: %w", message.ID, err)
		}
		items = append(items, candidate)
	}

	return items, nil
}

// collectWebsite загружает и нормализует один URL.
func (w *Worker) collectWebsite(ctx context.Context, job Job, source JobSource) ([]Candidate, error) {
	raw, err := w.websites.ReadURL(ctx, source.SourceID, source.URL)
	if err != nil {
		return nil, err
	}
	candidate, err := w.normalize(ctx, job.ID, source.SourceID, source.URL, raw, nil, 0)
	if err != nil {
		return nil, err
	}

	return []Candidate{candidate}, nil
}

// normalize переводит parser domain в стабильный контракт tasks-it.
func (w *Worker) normalize(ctx context.Context, jobID uuid.UUID, sourceID, externalID string, raw domain.RawTask, publishedAt *time.Time, messageID int64) (Candidate, error) {
	result, err := w.pipeline.Run(ctx, raw)
	if err != nil {
		return Candidate{}, err
	}
	tags := make([]string, 0, len(result.Task.Tags))
	for _, tag := range result.Task.Tags {
		tags = append(tags, string(tag))
	}
	examples := make([]Example, 0, len(result.Task.Examples))
	for _, example := range result.Task.Examples {
		examples = append(examples, Example{Input: example.Input, Output: example.Output, Explanation: example.Explanation})
	}

	return Candidate{
		ExternalID: externalID, SourceID: sourceID, SourceName: result.Task.Source.Name,
		SourceURL: result.Task.SourceURL, SourceHash: result.Task.SourceHash, SourcePublishedAt: publishedAt,
		RetrievedAt: time.Now().UTC(), CollectionJobID: jobID, Title: result.Task.Title,
		Statement: result.Task.Description, Difficulty: result.Task.Difficulty.String(), Tags: tags,
		Examples: examples, Constraints: result.Task.Constraints, MessageID: messageID,
	}, nil
}

// importCandidates отправляет батчи и возвращает кандидатов, подтверждённых tasks-it.
func (w *Worker) importCandidates(ctx context.Context, candidates []Candidate) (int, int, int, []Candidate, error) {
	imported, duplicates, invalid := 0, 0, 0
	successful := make([]Candidate, 0, len(candidates))
	byExternalID := make(map[string]Candidate, len(candidates))
	for _, candidate := range candidates {
		byExternalID[candidate.ExternalID] = candidate
	}
	for start := 0; start < len(candidates); start += 100 {
		end := min(start+100, len(candidates))
		results, err := w.sink.Import(ctx, candidates[start:end])
		if err != nil {
			return imported, duplicates, invalid, successful, err
		}
		seen := make(map[string]struct{}, len(results))
		for _, result := range results {
			candidate, exists := byExternalID[result.ExternalID]
			if !exists {
				return imported, duplicates, invalid, successful, fmt.Errorf("tasks-it returned an unknown external_id")
			}
			seen[result.ExternalID] = struct{}{}
			switch result.Status {
			case "imported":
				imported++
				successful = append(successful, candidate)
			case "duplicate":
				duplicates++
				successful = append(successful, candidate)
			case "invalid":
				invalid++
			case "error":
				return imported, duplicates, invalid, successful, fmt.Errorf("tasks-it failed to persist a candidate")
			default:
				return imported, duplicates, invalid, successful, fmt.Errorf("tasks-it returned unsupported import status")
			}
		}
		for _, candidate := range candidates[start:end] {
			if _, ok := seen[candidate.ExternalID]; !ok {
				return imported, duplicates, invalid, successful, fmt.Errorf("tasks-it returned an incomplete batch response")
			}
		}
	}

	return imported, duplicates, invalid, successful, nil
}

// safeError обрезает безопасное техническое описание без входного содержимого.
func safeError(err error) string {
	message := strings.TrimSpace(err.Error())
	if len(message) > 500 {
		message = message[:500]
	}

	return message
}
