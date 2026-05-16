// Package storage implements SQLite-based metric storage.
package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/ardenone/ardenone-cluster/containers/zai-proxy-dashboard/model"
)

// Storage provides SQLite-based metric persistence.
type Storage struct {
	db         *sql.DB
	config     Config
	writeCh    chan *model.MetricSnapshot
	wg         sync.WaitGroup
	ctx        context.Context
	cancel     context.CancelFunc
}

// NewStorage creates a new Storage instance.
func NewStorage(config Config) (*Storage, error) {
	db, err := sql.Open("sqlite", config.DBPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Set connection pool settings for SQLite
	db.SetMaxOpenConns(1) // SQLite doesn't support multiple writers
	db.SetMaxIdleConns(1)

	schema := NewSchema(db)
	if err := schema.Initialize(); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to initialize schema: %w", err)
	}

	ctx, cancel := context.WithCancel(context.Background())

	s := &Storage{
		db:      db,
		config:  config,
		writeCh: make(chan *model.MetricSnapshot, 1000),
		ctx:     ctx,
		cancel:  cancel,
	}

	s.wg.Add(1)
	go s.writeLoop()

	s.wg.Add(1)
	go s.retentionLoop()

	return s, nil
}

// Close shuts down the storage.
func (s *Storage) Close() error {
	s.cancel()
	s.wg.Wait()
	return s.db.Close()
}

// Write queues a snapshot for writing.
func (s *Storage) Write(snapshot *model.MetricSnapshot) {
	select {
	case s.writeCh <- snapshot:
	default:
		log.Printf("storage: write channel full, dropping snapshot")
	}
}

// writeLoop processes write requests from the channel.
func (s *Storage) writeLoop() {
	defer s.wg.Done()

	for {
		select {
		case <-s.ctx.Done():
			return
		case snapshot := <-s.writeCh:
			if err := s.writeSnapshot(snapshot); err != nil {
				log.Printf("storage: failed to write snapshot: %v", err)
			}
		}
	}
}

// writeSnapshot writes a snapshot to the 5s table.
func (s *Storage) writeSnapshot(snapshot *model.MetricSnapshot) error {
	data, err := json.Marshal(snapshot)
	if err != nil {
		return fmt.Errorf("failed to marshal snapshot: %w", err)
	}

	ts := snapshot.Timestamp / 1000 // Convert ms to seconds

	_, err = s.db.Exec(
		`INSERT OR REPLACE INTO metrics_5s (ts, variant, data) VALUES (?, ?, ?)`,
		ts, snapshot.Variant, string(data),
	)
	if err != nil {
		return fmt.Errorf("failed to insert snapshot: %w", err)
	}

	return nil
}

// Query retrieves metrics for a time range.
func (s *Storage) Query(start, end time.Time, variant string, use1m bool) ([]*model.MetricSnapshot, error) {
	var table string
	if use1m {
		table = "metrics_1m"
	} else {
		table = "metrics_5s"
	}

	startTs := start.Unix()
	endTs := end.Unix()

	var rows *sql.Rows
	var err error

	if variant == "" || variant == "all" {
		rows, err = s.db.Query(
			fmt.Sprintf(`SELECT data FROM %s WHERE ts >= ? AND ts <= ? ORDER BY ts`, table),
			startTs, endTs,
		)
	} else {
		rows, err = s.db.Query(
			fmt.Sprintf(`SELECT data FROM %s WHERE ts >= ? AND ts <= ? AND variant = ? ORDER BY ts`, table),
			startTs, endTs, variant,
		)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to query metrics: %w", err)
	}
	defer rows.Close()

	var snapshots []*model.MetricSnapshot
	for rows.Next() {
		var data string
		if err := rows.Scan(&data); err != nil {
			return nil, fmt.Errorf("failed to scan row: %w", err)
		}

		var snapshot model.MetricSnapshot
		if err := json.Unmarshal([]byte(data), &snapshot); err != nil {
			log.Printf("storage: failed to unmarshal snapshot: %v", err)
			continue
		}
		snapshots = append(snapshots, &snapshot)
	}

	return snapshots, rows.Err()
}

// QueryRange is a convenience method that determines the appropriate table.
func (s *Storage) QueryRange(d time.Duration, variant string) ([]*model.MetricSnapshot, error) {
	end := time.Now()
	start := end.Add(-d)
	use1m := d > time.Hour
	return s.Query(start, end, variant, use1m)
}

// retentionLoop handles downsampling and cleanup.
func (s *Storage) retentionLoop() {
	defer s.wg.Done()

	ticker := time.NewTicker(10 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-s.ctx.Done():
			return
		case <-ticker.C:
			if err := s.downsample(); err != nil {
				log.Printf("storage: downsampling failed: %v", err)
			}
			if err := s.cleanup(); err != nil {
				log.Printf("storage: cleanup failed: %v", err)
			}
		}
	}
}

// Downsample aggregates 5s data into 1m buckets (exported for testing).
func (s *Storage) Downsample() error {
	return s.downsample()
}

// Cleanup removes old data based on retention policies (exported for testing).
func (s *Storage) Cleanup() error {
	return s.cleanup()
}

// downsample aggregates 5s data into 1m buckets.
func (s *Storage) downsample() error {
	// Find the last downsampled timestamp
	var lastTs sql.NullInt64
	err := s.db.QueryRow(`SELECT MAX(ts) FROM metrics_1m`).Scan(&lastTs)
	if err != nil && err != sql.ErrNoRows {
		return fmt.Errorf("failed to get last downsampled timestamp: %w", err)
	}

	// Get all data since last downsample
	var rows *sql.Rows
	if lastTs.Valid {
		rows, err = s.db.Query(
			`SELECT ts, variant, data FROM metrics_5s WHERE ts > ? ORDER BY ts, variant`,
			lastTs.Int64,
		)
	} else {
		rows, err = s.db.Query(`SELECT ts, variant, data FROM metrics_5s ORDER BY ts, variant`)
	}
	if err != nil {
		return fmt.Errorf("failed to query for downsampling: %w", err)
	}
	defer rows.Close()

	// Group by minute bucket and variant
	type bucketKey struct {
		ts      int64
		variant string
	}
	buckets := make(map[bucketKey][]*model.MetricSnapshot)

	for rows.Next() {
		var ts int64
		var variant, data string
		if err := rows.Scan(&ts, &variant, &data); err != nil {
			return fmt.Errorf("failed to scan row: %w", err)
		}

		var snapshot model.MetricSnapshot
		if err := json.Unmarshal([]byte(data), &snapshot); err != nil {
			continue
		}

		// Round down to minute
		minuteTs := (ts / 60) * 60
		key := bucketKey{ts: minuteTs, variant: variant}
		buckets[key] = append(buckets[key], &snapshot)
	}

	// Compute averages and insert
	for key, snapshots := range buckets {
		if len(snapshots) == 0 {
			continue
		}

		avg := computeAverage(snapshots)
		avg.Timestamp = key.ts * 1000 // Convert back to ms

		data, err := json.Marshal(avg)
		if err != nil {
			continue
		}

		_, err = s.db.Exec(
			`INSERT OR REPLACE INTO metrics_1m (ts, variant, data) VALUES (?, ?, ?)`,
			key.ts, key.variant, string(data),
		)
		if err != nil {
			log.Printf("storage: failed to insert downsampled data: %v", err)
		}
	}

	return nil
}

// computeAverage computes the average of multiple snapshots.
func computeAverage(snapshots []*model.MetricSnapshot) *model.MetricSnapshot {
	if len(snapshots) == 0 {
		return nil
	}

	n := float64(len(snapshots))
	avg := &model.MetricSnapshot{
		Variant: snapshots[0].Variant,
	}

	for _, s := range snapshots {
		avg.Requests2xx += s.Requests2xx
		avg.Requests4xx += s.Requests4xx
		avg.Requests5xx += s.Requests5xx
		avg.TokensInput += s.TokensInput
		avg.TokensOutput += s.TokensOutput
		avg.ConcurrentRequests += s.ConcurrentRequests
		avg.MaxWorkers += s.MaxWorkers
		avg.RateLimitRps += s.RateLimitRps
		avg.RateLimitRejections += s.RateLimitRejections
		avg.RateLimitAdjIncrease += s.RateLimitAdjIncrease
		avg.RateLimitAdjDecrease += s.RateLimitAdjDecrease
		avg.UpstreamErrors += s.UpstreamErrors
		avg.RetryAttempts += s.RetryAttempts
		avg.LatencyP50 += s.LatencyP50
		avg.LatencyP95 += s.LatencyP95
		avg.LatencyP99 += s.LatencyP99
		avg.RequestSizeAvg += s.RequestSizeAvg
		avg.ResponseSizeAvg += s.ResponseSizeAvg
		avg.TokenRateIn += s.TokenRateIn
		avg.TokenRateOut += s.TokenRateOut
		avg.ReqRate += s.ReqRate
		avg.ErrorRatePct += s.ErrorRatePct
		avg.WorkerUtilization += s.WorkerUtilization
	}

	avg.Requests2xx /= n
	avg.Requests4xx /= n
	avg.Requests5xx /= n
	avg.TokensInput /= n
	avg.TokensOutput /= n
	avg.ConcurrentRequests /= n
	avg.MaxWorkers /= n
	avg.RateLimitRps /= n
	avg.RateLimitRejections /= n
	avg.RateLimitAdjIncrease /= n
	avg.RateLimitAdjDecrease /= n
	avg.UpstreamErrors /= n
	avg.RetryAttempts /= n
	avg.LatencyP50 /= n
	avg.LatencyP95 /= n
	avg.LatencyP99 /= n
	avg.RequestSizeAvg /= n
	avg.ResponseSizeAvg /= n
	avg.TokenRateIn /= n
	avg.TokenRateOut /= n
	avg.ReqRate /= n
	avg.ErrorRatePct /= n
	avg.WorkerUtilization /= n

	return avg
}

// cleanup removes old data based on retention policies.
func (s *Storage) cleanup() error {
	cutoff5s := time.Now().Add(-s.config.Retention5s).Unix()
	cutoff1m := time.Now().Add(-s.config.Retention1m).Unix()

	if _, err := s.db.Exec(`DELETE FROM metrics_5s WHERE ts < ?`, cutoff5s); err != nil {
		return fmt.Errorf("failed to cleanup metrics_5s: %w", err)
	}

	if _, err := s.db.Exec(`DELETE FROM metrics_1m WHERE ts < ?`, cutoff1m); err != nil {
		return fmt.Errorf("failed to cleanup metrics_1m: %w", err)
	}

	return nil
}

// GetLatest retrieves the most recent snapshot for each variant.
func (s *Storage) GetLatest() (map[string]*model.MetricSnapshot, error) {
	rows, err := s.db.Query(`
		SELECT variant, data FROM metrics_5s
		WHERE (variant, ts) IN (
			SELECT variant, MAX(ts) FROM metrics_5s GROUP BY variant
		)
	`)
	if err != nil {
		return nil, fmt.Errorf("failed to query latest: %w", err)
	}
	defer rows.Close()

	result := make(map[string]*model.MetricSnapshot)
	for rows.Next() {
		var variant, data string
		if err := rows.Scan(&variant, &data); err != nil {
			return nil, fmt.Errorf("failed to scan row: %w", err)
		}

		var snapshot model.MetricSnapshot
		if err := json.Unmarshal([]byte(data), &snapshot); err != nil {
			continue
		}
		result[variant] = &snapshot
	}

	return result, rows.Err()
}

// GetDB returns the underlying database connection for testing.
func (s *Storage) GetDB() *sql.DB {
	return s.db
}
