// Package storage implements bounded in-memory metric storage.
package storage

import (
	"context"
	"sort"
	"sync"
	"time"

	"git.ardenone.com/jedarden/zai-proxy/dashboard/model"
)

// Storage keeps recent metric snapshots in two fixed-size, per-variant ring
// buffers. It is process-local: a dashboard restart starts with an empty
// history window instead of depending on a filesystem or database.
type Storage struct {
	mu          sync.RWMutex
	config      Config
	raw         bufferSet
	downsampled bufferSet
	ctx         context.Context
	cancel      context.CancelFunc
	wg          sync.WaitGroup
}

// NewStorage creates a dependency-free in-memory store.
func NewStorage(config Config) *Storage {
	config = config.normalized()
	ctx, cancel := context.WithCancel(context.Background())
	s := &Storage{
		config: config,
		raw: newBufferSet(
			capacityFor(config.Retention5s, rawResolution),
			config.MaxVariants,
		),
		downsampled: newBufferSet(
			capacityFor(config.Retention1m, downsampledResolution),
			config.MaxVariants,
		),
		ctx:    ctx,
		cancel: cancel,
	}

	s.wg.Add(1)
	go s.retentionLoop()
	return s
}

// Close stops background maintenance. It does not persist data.
func (s *Storage) Close() {
	s.cancel()
	s.wg.Wait()
}

// Write stores a snapshot in the high-resolution buffer. Snapshots outside the
// retention window are ignored so delayed collection cannot evict current data.
func (s *Storage) Write(snapshot *model.MetricSnapshot) {
	if snapshot == nil || snapshot.Timestamp/1000 < time.Now().Add(-s.config.Retention5s).Unix() {
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.raw.append(cloneSnapshot(snapshot))
}

// Query retrieves metrics for a time range. use1m selects the downsampled
// buffer; callers normally use QueryRange instead.
func (s *Storage) Query(start, end time.Time, variant string, use1m bool) ([]*model.MetricSnapshot, error) {
	startTs, endTs := start.Unix(), end.Unix()

	s.mu.RLock()
	defer s.mu.RUnlock()

	buffers := s.raw
	if use1m {
		buffers = s.downsampled
	}

	snapshots := buffers.query(startTs, endTs, variant)
	sortSnapshots(snapshots)
	return snapshots, nil
}

// QueryRange selects high-resolution data for the live hour and downsampled
// data for longer historical windows.
func (s *Storage) QueryRange(d time.Duration, variant string) ([]*model.MetricSnapshot, error) {
	end := time.Now()
	return s.Query(end.Add(-d), end, variant, d > time.Hour)
}

// retentionLoop periodically refreshes minute aggregates and removes expired
// samples. The operations are in-memory and cannot fail externally.
func (s *Storage) retentionLoop() {
	defer s.wg.Done()

	ticker := time.NewTicker(10 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-s.ctx.Done():
			return
		case <-ticker.C:
			_ = s.Downsample()
			_ = s.Cleanup()
		}
	}
}

// Downsample aggregates high-resolution data into one-minute averages.
func (s *Storage) Downsample() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	type bucketKey struct {
		timestamp int64
		variant   string
	}
	buckets := make(map[bucketKey][]*model.MetricSnapshot)
	for variant, buffer := range s.raw.buffers {
		for _, snapshot := range buffer.snapshots() {
			minute := (snapshot.Timestamp / 1000 / 60) * 60
			key := bucketKey{timestamp: minute, variant: variant}
			buckets[key] = append(buckets[key], snapshot)
		}
	}

	result := newBufferSet(s.downsampled.capacity, s.downsampled.maxVariants)
	keys := make([]bucketKey, 0, len(buckets))
	for key := range buckets {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].timestamp == keys[j].timestamp {
			return keys[i].variant < keys[j].variant
		}
		return keys[i].timestamp < keys[j].timestamp
	})

	for _, key := range keys {
		average := computeAverage(buckets[key])
		average.Timestamp = key.timestamp * 1000
		result.append(average)
	}
	s.downsampled = result
	return nil
}

// Cleanup removes samples outside their configured retention windows.
func (s *Storage) Cleanup() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.raw.removeBefore(time.Now().Add(-s.config.Retention5s).Unix())
	s.downsampled.removeBefore(time.Now().Add(-s.config.Retention1m).Unix())
	return nil
}

// GetLatest retrieves the most recent high-resolution snapshot for each
// retained variant.
func (s *Storage) GetLatest() (map[string]*model.MetricSnapshot, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.raw.latest(), nil
}

// bufferSet bounds the number of metric streams as well as the number of
// samples in each stream. The dashboard has production and canary streams.
type bufferSet struct {
	capacity    int
	maxVariants int
	buffers     map[string]*ringBuffer
}

func newBufferSet(capacity, maxVariants int) bufferSet {
	return bufferSet{
		capacity:    capacity,
		maxVariants: maxVariants,
		buffers:     make(map[string]*ringBuffer, maxVariants),
	}
}

func (b *bufferSet) append(snapshot *model.MetricSnapshot) {
	buffer, ok := b.buffers[snapshot.Variant]
	if !ok {
		if len(b.buffers) >= b.maxVariants {
			return
		}
		buffer = newRingBuffer(b.capacity)
		b.buffers[snapshot.Variant] = buffer
	}
	buffer.append(snapshot)
}

func (b bufferSet) query(startTs, endTs int64, variant string) []*model.MetricSnapshot {
	var result []*model.MetricSnapshot
	for name, buffer := range b.buffers {
		if variant != "" && variant != "all" && variant != name {
			continue
		}
		for _, snapshot := range buffer.snapshots() {
			timestamp := snapshot.Timestamp / 1000
			if timestamp >= startTs && timestamp <= endTs {
				result = append(result, snapshot)
			}
		}
	}
	return result
}

func (b *bufferSet) removeBefore(cutoff int64) {
	for variant, buffer := range b.buffers {
		buffer.removeBefore(cutoff)
		if buffer.size == 0 {
			delete(b.buffers, variant)
		}
	}
}

func (b bufferSet) latest() map[string]*model.MetricSnapshot {
	result := make(map[string]*model.MetricSnapshot, len(b.buffers))
	for variant, buffer := range b.buffers {
		if latest := buffer.latest(); latest != nil {
			result[variant] = latest
		}
	}
	return result
}

// ringBuffer is a fixed-size circular buffer ordered by insertion. Query
// methods sort selected results by timestamp so delayed samples remain ordered.
type ringBuffer struct {
	values []model.MetricSnapshot
	start  int
	size   int
}

func newRingBuffer(capacity int) *ringBuffer {
	return &ringBuffer{values: make([]model.MetricSnapshot, capacity)}
}

func (r *ringBuffer) append(snapshot *model.MetricSnapshot) {
	if len(r.values) == 0 {
		return
	}

	// Preserve the former SQLite primary-key behavior: one snapshot per
	// variant and Unix-second bucket, with the newer value replacing it.
	for i := 0; i < r.size; i++ {
		index := (r.start + i) % len(r.values)
		if r.values[index].Timestamp/1000 == snapshot.Timestamp/1000 {
			r.values[index] = *cloneSnapshot(snapshot)
			return
		}
	}

	index := (r.start + r.size) % len(r.values)
	if r.size == len(r.values) {
		index = r.start
		r.start = (r.start + 1) % len(r.values)
	} else {
		r.size++
	}
	r.values[index] = *cloneSnapshot(snapshot)
}

func (r *ringBuffer) snapshots() []*model.MetricSnapshot {
	result := make([]*model.MetricSnapshot, 0, r.size)
	for i := 0; i < r.size; i++ {
		result = append(result, cloneSnapshot(&r.values[(r.start+i)%len(r.values)]))
	}
	return result
}

func (r *ringBuffer) removeBefore(cutoff int64) {
	kept := make([]*model.MetricSnapshot, 0, r.size)
	for _, snapshot := range r.snapshots() {
		if snapshot.Timestamp/1000 >= cutoff {
			kept = append(kept, snapshot)
		}
	}
	r.start = 0
	r.size = 0
	for _, snapshot := range kept {
		r.append(snapshot)
	}
}

func (r *ringBuffer) latest() *model.MetricSnapshot {
	var latest *model.MetricSnapshot
	for _, snapshot := range r.snapshots() {
		if latest == nil || snapshot.Timestamp > latest.Timestamp {
			latest = snapshot
		}
	}
	return latest
}

func sortSnapshots(snapshots []*model.MetricSnapshot) {
	sort.Slice(snapshots, func(i, j int) bool {
		if snapshots[i].Timestamp == snapshots[j].Timestamp {
			return snapshots[i].Variant < snapshots[j].Variant
		}
		return snapshots[i].Timestamp < snapshots[j].Timestamp
	})
}

func cloneSnapshot(snapshot *model.MetricSnapshot) *model.MetricSnapshot {
	if snapshot == nil {
		return nil
	}
	clone := *snapshot
	if snapshot.StatusCodeRates != nil {
		clone.StatusCodeRates = make(map[string]float64, len(snapshot.StatusCodeRates))
		for status, rate := range snapshot.StatusCodeRates {
			clone.StatusCodeRates[status] = rate
		}
	}
	return &clone
}

// computeAverage computes an arithmetic mean for a minute bucket.
func computeAverage(snapshots []*model.MetricSnapshot) *model.MetricSnapshot {
	if len(snapshots) == 0 {
		return nil
	}

	n := float64(len(snapshots))
	average := &model.MetricSnapshot{Variant: snapshots[0].Variant}
	statusCodeRates := make(map[string]float64)

	for _, snapshot := range snapshots {
		average.Requests2xx += snapshot.Requests2xx
		average.Requests4xx += snapshot.Requests4xx
		average.Requests5xx += snapshot.Requests5xx
		average.TokensInput += snapshot.TokensInput
		average.TokensOutput += snapshot.TokensOutput
		average.TokensCacheRead += snapshot.TokensCacheRead
		average.TokensCacheWrite += snapshot.TokensCacheWrite
		average.ConcurrentRequests += snapshot.ConcurrentRequests
		average.MaxWorkers += snapshot.MaxWorkers
		average.RateLimitRps += snapshot.RateLimitRps
		average.RateLimitRejections += snapshot.RateLimitRejections
		average.RateLimitAdjIncrease += snapshot.RateLimitAdjIncrease
		average.RateLimitAdjDecrease += snapshot.RateLimitAdjDecrease
		average.UpstreamErrors += snapshot.UpstreamErrors
		average.RetryAttempts += snapshot.RetryAttempts
		average.LatencyP50 += snapshot.LatencyP50
		average.LatencyP95 += snapshot.LatencyP95
		average.LatencyP99 += snapshot.LatencyP99
		average.RequestSizeAvg += snapshot.RequestSizeAvg
		average.ResponseSizeAvg += snapshot.ResponseSizeAvg
		average.TokenRateIn += snapshot.TokenRateIn
		average.TokenRateOut += snapshot.TokenRateOut
		average.TokenRateCacheRead += snapshot.TokenRateCacheRead
		average.TokenRateCacheWrite += snapshot.TokenRateCacheWrite
		average.ReqRate += snapshot.ReqRate
		average.ErrorRatePct += snapshot.ErrorRatePct
		average.WorkerUtilization += snapshot.WorkerUtilization
		for status, rate := range snapshot.StatusCodeRates {
			statusCodeRates[status] += rate
		}
	}

	average.Requests2xx /= n
	average.Requests4xx /= n
	average.Requests5xx /= n
	average.TokensInput /= n
	average.TokensOutput /= n
	average.TokensCacheRead /= n
	average.TokensCacheWrite /= n
	average.ConcurrentRequests /= n
	average.MaxWorkers /= n
	average.RateLimitRps /= n
	average.RateLimitRejections /= n
	average.RateLimitAdjIncrease /= n
	average.RateLimitAdjDecrease /= n
	average.UpstreamErrors /= n
	average.RetryAttempts /= n
	average.LatencyP50 /= n
	average.LatencyP95 /= n
	average.LatencyP99 /= n
	average.RequestSizeAvg /= n
	average.ResponseSizeAvg /= n
	average.TokenRateIn /= n
	average.TokenRateOut /= n
	average.TokenRateCacheRead /= n
	average.TokenRateCacheWrite /= n
	average.ReqRate /= n
	average.ErrorRatePct /= n
	average.WorkerUtilization /= n
	if len(statusCodeRates) > 0 {
		average.StatusCodeRates = statusCodeRates
		for status := range average.StatusCodeRates {
			average.StatusCodeRates[status] /= n
		}
	}

	return average
}
