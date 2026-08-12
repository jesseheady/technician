// Package history is an optional SQLite store for long-term check history,
// beyond what the in-memory ring buffer holds. It is written write-through from
// the status store's hot path, but asynchronously: results are handed to a
// buffered channel and a single writer goroutine batches the inserts, so a slow
// disk can never add latency to result draining. Delivery is best-effort — if
// the buffer is full a result is dropped rather than blocking the caller.
//
// One concrete implementation (modernc.org/sqlite, pure Go, no cgo); there is no
// config-selectable driver. The status.json store keeps its non-series state
// (order, budget badges, down-since); this store only holds the time series.
package history

import (
	"database/sql"
	"fmt"
	"log/slog"
	"sync"
	"time"

	_ "modernc.org/sqlite"

	"github.com/jesseheady/technician/internal/check"
)

const (
	bufferSize = 1024            // pending results before Record drops
	batchSize  = 100             // rows per insert transaction
	flushEvery = 1 * time.Second // flush partial batches at least this often
	pruneEvery = 1 * time.Hour   // retention sweep cadence
	schemaSQL  = `
CREATE TABLE IF NOT EXISTS probe_results (
    id          INTEGER PRIMARY KEY,
    name        TEXT    NOT NULL,
    type        TEXT    NOT NULL,
    ts          INTEGER NOT NULL, -- unix millis
    success     INTEGER NOT NULL, -- 0/1
    degraded    INTEGER NOT NULL, -- 0/1
    duration_ms INTEGER NOT NULL,
    status_code INTEGER
);
CREATE INDEX IF NOT EXISTS idx_probe_results_name_ts ON probe_results(name, ts, success);`
	insertSQL = `INSERT INTO probe_results(name, type, ts, success, degraded, duration_ms, status_code) VALUES(?, ?, ?, ?, ?, ?, ?)`
)

// Store is an append-only history of check results with time-based retention.
type Store struct {
	db        *sql.DB
	retention time.Duration
	ch        chan *check.Result
	done      chan struct{}
	wg        sync.WaitGroup
}

// New opens (creating if needed) the SQLite database at path and starts the
// writer goroutine. Call Close to flush and shut down.
func New(path string, retention time.Duration) (*Store, error) {
	// WAL keeps readers (queries) from blocking the writer; busy_timeout avoids
	// spurious "database is locked" under brief contention.
	dsn := fmt.Sprintf("file:%s?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)", path)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("opening history db: %w", err)
	}
	// A single writer goroutine, so one connection avoids lock contention.
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(schemaSQL); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("creating history schema: %w", err)
	}

	s := &Store{
		db:        db,
		retention: retention,
		ch:        make(chan *check.Result, bufferSize),
		done:      make(chan struct{}),
	}
	s.wg.Add(1)
	go s.run()
	return s, nil
}

// Record queues a result for persistence. Non-blocking: if the buffer is full it
// drops the result (logged once per burst) to protect the caller's hot path.
// Infra errors are skipped — the target was never tested, so recording them
// would distort uptime, matching how the metrics treat them.
func (s *Store) Record(r *check.Result) {
	if r == nil || r.InfraError {
		return
	}
	select {
	case s.ch <- r:
	default:
		slog.Warn("history buffer full, dropping result", "name", r.Name)
	}
}

func (s *Store) run() {
	defer s.wg.Done()
	flush := time.NewTicker(flushEvery)
	prune := time.NewTicker(pruneEvery)
	defer flush.Stop()
	defer prune.Stop()

	batch := make([]*check.Result, 0, batchSize)
	for {
		select {
		case r := <-s.ch:
			batch = append(batch, r)
			if len(batch) >= batchSize {
				s.flush(batch)
				batch = batch[:0]
			}
		case <-flush.C:
			if len(batch) > 0 {
				s.flush(batch)
				batch = batch[:0]
			}
		case <-prune.C:
			s.pruneOld()
		case <-s.done:
			// Final drain: pull everything buffered, then flush once.
			for {
				select {
				case r := <-s.ch:
					batch = append(batch, r)
				default:
					if len(batch) > 0 {
						s.flush(batch)
					}
					return
				}
			}
		}
	}
}

func (s *Store) flush(batch []*check.Result) {
	tx, err := s.db.Begin()
	if err != nil {
		slog.Warn("history flush: begin failed", "error", err)
		return
	}
	stmt, err := tx.Prepare(insertSQL)
	if err != nil {
		_ = tx.Rollback()
		slog.Warn("history flush: prepare failed", "error", err)
		return
	}
	defer func() { _ = stmt.Close() }()
	for _, r := range batch {
		if _, err := stmt.Exec(r.Name, string(r.Type), r.Timestamp.UnixMilli(),
			boolToInt(r.Success), boolToInt(r.Degraded), r.Duration.Milliseconds(), r.StatusCode); err != nil {
			_ = tx.Rollback()
			slog.Warn("history flush: insert failed", "error", err)
			return
		}
	}
	if err := tx.Commit(); err != nil {
		slog.Warn("history flush: commit failed", "error", err)
	}
}

func (s *Store) pruneOld() {
	cutoff := time.Now().Add(-s.retention).UnixMilli()
	if _, err := s.db.Exec(`DELETE FROM probe_results WHERE ts < ?`, cutoff); err != nil {
		slog.Warn("history prune failed", "error", err)
	}
}

// UptimeOverWindow returns the fraction of successful results (0..1) for a check
// over the trailing window. ok is false when there are no results in the window.
func (s *Store) UptimeOverWindow(name string, window time.Duration) (ratio float64, ok bool, err error) {
	since := time.Now().Add(-window).UnixMilli()
	var total, up int
	row := s.db.QueryRow(`SELECT COUNT(*), COALESCE(SUM(success), 0) FROM probe_results WHERE name = ? AND ts >= ?`, name, since)
	if err := row.Scan(&total, &up); err != nil {
		return 0, false, err
	}
	if total == 0 {
		return 0, false, nil
	}
	return float64(up) / float64(total), true, nil
}

// Close stops the writer, flushes any buffered results, and closes the database.
func (s *Store) Close() error {
	close(s.done)
	s.wg.Wait()
	return s.db.Close()
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
