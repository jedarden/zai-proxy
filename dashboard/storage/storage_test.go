package storage

import (
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"github.com/ardenone/ardenone-cluster/containers/zai-proxy-dashboard/model"
)

func TestStorage_WriteAndRead(t *testing.T) {
	// Create temporary database
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	config := Config{
		DBPath:      dbPath,
		Retention5s: 24 * time.Hour,
		Retention1m: 7 * 24 * time.Hour,
	}

	store, err := NewStorage(config)
	if err != nil {
		t.Fatalf("failed to create storage: %v", err)
	}
	defer store.Close()

	// Write a snapshot
	snapshot := &model.MetricSnapshot{
		Timestamp:          time.Now().UnixMilli(),
		Variant:            "production",
		Requests2xx:        100,
		Requests4xx:        10,
		Requests5xx:        5,
		ReqRate:            2.5,
		LatencyP50:         150.0,
		WorkerUtilization:  0.75,
	}

	store.Write(snapshot)

	// Wait for write to complete
	time.Sleep(100 * time.Millisecond)

	// Read it back
	snapshots, err := store.QueryRange(1*time.Hour, "production")
	if err != nil {
		t.Fatalf("failed to query: %v", err)
	}

	if len(snapshots) != 1 {
		t.Fatalf("expected 1 snapshot, got %d", len(snapshots))
	}

	read := snapshots[0]
	if read.Variant != "production" {
		t.Errorf("variant mismatch: got %s", read.Variant)
	}
	if read.Requests2xx != 100 {
		t.Errorf("requests_2xx mismatch: got %f", read.Requests2xx)
	}
	if read.ReqRate != 2.5 {
		t.Errorf("req_rate mismatch: got %f", read.ReqRate)
	}
}

func TestStorage_RangeQuery(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	config := Config{
		DBPath:      dbPath,
		Retention5s: 24 * time.Hour,
		Retention1m: 7 * 24 * time.Hour,
	}

	store, err := NewStorage(config)
	if err != nil {
		t.Fatalf("failed to create storage: %v", err)
	}
	defer store.Close()

	now := time.Now()

	// Write multiple snapshots at different times
	for i := 0; i < 10; i++ {
		snapshot := &model.MetricSnapshot{
			Timestamp: now.Add(-time.Duration(i) * time.Minute).UnixMilli(),
			Variant:   "production",
			ReqRate:   float64(10 - i),
		}
		store.Write(snapshot)
	}

	time.Sleep(100 * time.Millisecond)

	// Query for last 5 minutes
	snapshots, err := store.QueryRange(5*time.Minute, "production")
	if err != nil {
		t.Fatalf("failed to query: %v", err)
	}

	// Should get snapshots from last 5 minutes (0-4 inclusive)
	if len(snapshots) < 5 {
		t.Errorf("expected at least 5 snapshots, got %d", len(snapshots))
	}
}

func TestStorage_VariantFilter(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	config := Config{
		DBPath:      dbPath,
		Retention5s: 24 * time.Hour,
		Retention1m: 7 * 24 * time.Hour,
	}

	store, err := NewStorage(config)
	if err != nil {
		t.Fatalf("failed to create storage: %v", err)
	}
	defer store.Close()

	now := time.Now()

	// Write snapshots for both variants
	store.Write(&model.MetricSnapshot{
		Timestamp: now.UnixMilli(),
		Variant:   "production",
		ReqRate:   100,
	})
	store.Write(&model.MetricSnapshot{
		Timestamp: now.UnixMilli(),
		Variant:   "canary",
		ReqRate:   50,
	})

	time.Sleep(100 * time.Millisecond)

	// Query only production
	prodSnapshots, err := store.QueryRange(1*time.Hour, "production")
	if err != nil {
		t.Fatalf("failed to query: %v", err)
	}

	for _, s := range prodSnapshots {
		if s.Variant != "production" {
			t.Errorf("expected only production variant, got %s", s.Variant)
		}
	}

	// Query all variants
	allSnapshots, err := store.QueryRange(1*time.Hour, "all")
	if err != nil {
		t.Fatalf("failed to query: %v", err)
	}

	if len(allSnapshots) < 2 {
		t.Errorf("expected at least 2 snapshots for all variants, got %d", len(allSnapshots))
	}
}

func TestStorage_GetLatest(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	config := Config{
		DBPath:      dbPath,
		Retention5s: 24 * time.Hour,
		Retention1m: 7 * 24 * time.Hour,
	}

	store, err := NewStorage(config)
	if err != nil {
		t.Fatalf("failed to create storage: %v", err)
	}
	defer store.Close()

	now := time.Now()

	// Write multiple snapshots with different timestamps
	store.Write(&model.MetricSnapshot{
		Timestamp: now.Add(-2 * time.Minute).UnixMilli(),
		Variant:   "production",
		ReqRate:   100,
	})
	store.Write(&model.MetricSnapshot{
		Timestamp: now.Add(-1 * time.Minute).UnixMilli(),
		Variant:   "production",
		ReqRate:   200,
	})
	store.Write(&model.MetricSnapshot{
		Timestamp: now.UnixMilli(),
		Variant:   "production",
		ReqRate:   300,
	})

	time.Sleep(100 * time.Millisecond)

	latest, err := store.GetLatest()
	if err != nil {
		t.Fatalf("failed to get latest: %v", err)
	}

	prod, ok := latest["production"]
	if !ok {
		t.Fatal("production variant not found")
	}

	if prod.ReqRate != 300 {
		t.Errorf("expected latest req_rate 300, got %f", prod.ReqRate)
	}
}

func TestStorage_Downsampling(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	config := Config{
		DBPath:      dbPath,
		Retention5s: 24 * time.Hour,
		Retention1m: 7 * 24 * time.Hour,
	}

	store, err := NewStorage(config)
	if err != nil {
		t.Fatalf("failed to create storage: %v", err)
	}
	defer store.Close()

	// Write snapshots that span multiple minutes
	now := time.Now()
	for i := 0; i < 120; i++ {
		snapshot := &model.MetricSnapshot{
			Timestamp: now.Add(-time.Duration(i) * time.Second).UnixMilli(),
			Variant:   "production",
			ReqRate:   float64(i),
		}
		store.Write(snapshot)
	}

	// Trigger downsampling manually
	time.Sleep(100 * time.Millisecond)
	if err := store.Downsample(); err != nil {
		t.Fatalf("downsampling failed: %v", err)
	}

	// Query from 1m table
	end := time.Now()
	start := end.Add(-2 * time.Minute)
	snapshots, err := store.Query(start, end, "production", true)
	if err != nil {
		t.Fatalf("failed to query 1m table: %v", err)
	}

	// Should have downsampled data
	if len(snapshots) < 1 {
		t.Error("expected at least 1 downsampled snapshot")
	}
}

func TestStorage_Retention(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	config := Config{
		DBPath:      dbPath,
		Retention5s: 1 * time.Hour,
		Retention1m: 24 * time.Hour,
	}

	store, err := NewStorage(config)
	if err != nil {
		t.Fatalf("failed to create storage: %v", err)
	}
	defer store.Close()

	now := time.Now()

	// Write old snapshot (should be deleted)
	oldSnapshot := &model.MetricSnapshot{
		Timestamp: now.Add(-2 * time.Hour).UnixMilli(),
		Variant:   "production",
		ReqRate:   50,
	}
	store.Write(oldSnapshot)

	// Write recent snapshot (should be kept)
	recentSnapshot := &model.MetricSnapshot{
		Timestamp: now.Add(-30 * time.Minute).UnixMilli(),
		Variant:   "production",
		ReqRate:   100,
	}
	store.Write(recentSnapshot)

	time.Sleep(100 * time.Millisecond)

	// Trigger cleanup
	if err := store.Cleanup(); err != nil {
		t.Fatalf("cleanup failed: %v", err)
	}

	// Query all - should only have recent snapshot
	snapshots, err := store.QueryRange(3*time.Hour, "production")
	if err != nil {
		t.Fatalf("failed to query: %v", err)
	}

	if len(snapshots) != 1 {
		t.Errorf("expected 1 snapshot after cleanup, got %d", len(snapshots))
	}
}

func TestStorage_ConcurrentWrites(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	config := Config{
		DBPath:      dbPath,
		Retention5s: 24 * time.Hour,
		Retention1m: 7 * 24 * time.Hour,
	}

	store, err := NewStorage(config)
	if err != nil {
		t.Fatalf("failed to create storage: %v", err)
	}
	defer store.Close()

	// Write many snapshots concurrently
	done := make(chan bool)
	for i := 0; i < 100; i++ {
		go func(idx int) {
			snapshot := &model.MetricSnapshot{
				Timestamp: time.Now().Add(time.Duration(idx) * time.Second).UnixMilli(),
				Variant:   "production",
				ReqRate:   float64(idx),
			}
			store.Write(snapshot)
			done <- true
		}(i)
	}

	// Wait for all writes
	for i := 0; i < 100; i++ {
		<-done
	}

	time.Sleep(500 * time.Millisecond)

	// Should have all snapshots
	snapshots, err := store.QueryRange(1*time.Hour, "production")
	if err != nil {
		t.Fatalf("failed to query: %v", err)
	}

	if len(snapshots) < 100 {
		t.Errorf("expected at least 100 snapshots, got %d", len(snapshots))
	}
}

func TestSchema_Initialize(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	// Create database manually
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}
	defer db.Close()

	schema := NewSchema(db)
	if err := schema.Initialize(); err != nil {
		t.Fatalf("failed to initialize schema: %v", err)
	}

	// Verify tables exist
	var count int
	err = db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name IN ('metrics_5s', 'metrics_1m')`).Scan(&count)
	if err != nil {
		t.Fatalf("failed to query tables: %v", err)
	}
	if count != 2 {
		t.Errorf("expected 2 tables, got %d", count)
	}

	// Verify indexes exist
	err = db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='index' AND name IN ('idx_5s_ts', 'idx_1m_ts')`).Scan(&count)
	if err != nil {
		t.Fatalf("failed to query indexes: %v", err)
	}
	if count != 2 {
		t.Errorf("expected 2 indexes, got %d", count)
	}
}
