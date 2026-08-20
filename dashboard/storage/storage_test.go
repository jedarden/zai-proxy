package storage

import (
	"sync"
	"testing"
	"time"

	"git.ardenone.com/jedarden/zai-proxy/dashboard/model"
)

func testConfig() Config {
	return Config{
		Retention5s: 24 * time.Hour,
		Retention1m: 7 * 24 * time.Hour,
		MaxVariants: 2,
	}
}

func TestStorage_WriteAndRead(t *testing.T) {
	store := NewStorage(testConfig())
	defer store.Close()

	now := time.Now()
	store.Write(&model.MetricSnapshot{
		Timestamp:         now.UnixMilli(),
		Variant:           "production",
		Requests2xx:       100,
		ReqRate:           2.5,
		LatencyP50:        150,
		WorkerUtilization: 0.75,
	})

	snapshots, err := store.Query(now.Add(-time.Minute), now.Add(time.Minute), "production", false)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if len(snapshots) != 1 {
		t.Fatalf("expected 1 snapshot, got %d", len(snapshots))
	}
	if got := snapshots[0]; got.Requests2xx != 100 || got.ReqRate != 2.5 {
		t.Errorf("unexpected snapshot: %+v", got)
	}
}

func TestStorage_RangeQueryAndVariantFilter(t *testing.T) {
	store := NewStorage(testConfig())
	defer store.Close()

	now := time.Now().Truncate(time.Second)
	for i := 0; i < 10; i++ {
		store.Write(&model.MetricSnapshot{
			Timestamp: now.Add(-time.Duration(i) * time.Minute).UnixMilli(),
			Variant:   "production",
			ReqRate:   float64(10 - i),
		})
	}
	store.Write(&model.MetricSnapshot{Timestamp: now.UnixMilli(), Variant: "canary", ReqRate: 50})

	production, err := store.Query(now.Add(-5*time.Minute), now, "production", false)
	if err != nil {
		t.Fatalf("query production: %v", err)
	}
	if len(production) != 6 {
		t.Fatalf("expected 6 production snapshots, got %d", len(production))
	}
	for _, snapshot := range production {
		if snapshot.Variant != "production" {
			t.Errorf("expected production snapshot, got %q", snapshot.Variant)
		}
	}

	all, err := store.Query(now.Add(-5*time.Minute), now, "all", false)
	if err != nil {
		t.Fatalf("query all variants: %v", err)
	}
	if len(all) != 7 {
		t.Fatalf("expected 7 snapshots across variants, got %d", len(all))
	}
}

func TestStorage_GetLatest(t *testing.T) {
	store := NewStorage(testConfig())
	defer store.Close()

	now := time.Now().Truncate(time.Second)
	store.Write(&model.MetricSnapshot{Timestamp: now.Add(-2 * time.Minute).UnixMilli(), Variant: "production", ReqRate: 100})
	store.Write(&model.MetricSnapshot{Timestamp: now.Add(-time.Minute).UnixMilli(), Variant: "production", ReqRate: 200})
	store.Write(&model.MetricSnapshot{Timestamp: now.UnixMilli(), Variant: "production", ReqRate: 300})

	latest, err := store.GetLatest()
	if err != nil {
		t.Fatalf("get latest: %v", err)
	}
	if latest["production"] == nil || latest["production"].ReqRate != 300 {
		t.Errorf("unexpected latest production snapshot: %+v", latest["production"])
	}
}

func TestStorage_Downsampling(t *testing.T) {
	store := NewStorage(testConfig())
	defer store.Close()

	now := time.Now().Truncate(time.Minute)
	for i := 0; i < 12; i++ {
		store.Write(&model.MetricSnapshot{
			Timestamp: now.Add(-time.Minute + time.Duration(i)*5*time.Second).UnixMilli(),
			Variant:   "production",
			ReqRate:   float64(i),
		})
	}

	if err := store.Downsample(); err != nil {
		t.Fatalf("downsample: %v", err)
	}
	snapshots, err := store.Query(now.Add(-2*time.Minute), now, "production", true)
	if err != nil {
		t.Fatalf("query downsampled data: %v", err)
	}
	if len(snapshots) != 1 {
		t.Fatalf("expected 1 downsampled snapshot, got %d", len(snapshots))
	}
	if got := snapshots[0].ReqRate; got != 5.5 {
		t.Errorf("expected average req_rate 5.5, got %v", got)
	}
}

func TestStorage_RetentionAndCapacity(t *testing.T) {
	config := Config{Retention5s: 10 * time.Second, Retention1m: time.Hour, MaxVariants: 2}
	store := NewStorage(config)
	defer store.Close()

	now := time.Now().Truncate(time.Second)
	store.Write(&model.MetricSnapshot{Timestamp: now.Add(-time.Minute).UnixMilli(), Variant: "production"})
	store.Write(&model.MetricSnapshot{Timestamp: now.UnixMilli(), Variant: "production", ReqRate: 1})
	store.Write(&model.MetricSnapshot{Timestamp: now.Add(time.Second).UnixMilli(), Variant: "production", ReqRate: 2})
	store.Write(&model.MetricSnapshot{Timestamp: now.Add(2 * time.Second).UnixMilli(), Variant: "production", ReqRate: 3})

	snapshots, err := store.Query(now.Add(-time.Minute), now.Add(3*time.Second), "production", false)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if len(snapshots) != 2 {
		t.Fatalf("expected 2 snapshots from the fixed-size ring, got %d", len(snapshots))
	}
	if snapshots[0].ReqRate != 2 || snapshots[1].ReqRate != 3 {
		t.Errorf("expected two newest snapshots, got %+v", snapshots)
	}
}

func TestStorage_BoundsVariantStreamsAndStartsEmptyAfterRestart(t *testing.T) {
	config := Config{Retention5s: time.Hour, Retention1m: time.Hour, MaxVariants: 2}
	now := time.Now()

	store := NewStorage(config)
	store.Write(&model.MetricSnapshot{Timestamp: now.UnixMilli(), Variant: "production"})
	store.Write(&model.MetricSnapshot{Timestamp: now.UnixMilli(), Variant: "canary"})
	store.Write(&model.MetricSnapshot{Timestamp: now.UnixMilli(), Variant: "unexpected"})
	all, err := store.Query(now.Add(-time.Minute), now.Add(time.Minute), "all", false)
	store.Close()
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("expected two bounded variant streams, got %d snapshots", len(all))
	}

	restarted := NewStorage(config)
	defer restarted.Close()
	latest, err := restarted.GetLatest()
	if err != nil {
		t.Fatalf("get latest after restart: %v", err)
	}
	if len(latest) != 0 {
		t.Errorf("expected empty in-memory storage after restart, got %+v", latest)
	}
}

func TestStorage_ConcurrentWrites(t *testing.T) {
	store := NewStorage(testConfig())
	defer store.Close()

	now := time.Now().Truncate(time.Second)
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			store.Write(&model.MetricSnapshot{
				Timestamp: now.Add(time.Duration(index) * time.Second).UnixMilli(),
				Variant:   "production",
				ReqRate:   float64(index),
			})
		}(i)
	}
	wg.Wait()

	snapshots, err := store.Query(now.Add(-time.Second), now.Add(2*time.Minute), "production", false)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if len(snapshots) != 100 {
		t.Fatalf("expected 100 snapshots, got %d", len(snapshots))
	}
}
