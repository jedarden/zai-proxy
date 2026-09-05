package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"git.ardenone.com/jedarden/zai-proxy/dashboard/model"
	"git.ardenone.com/jedarden/zai-proxy/dashboard/storage"
)

func TestStatusAndMetricsSerializeQuotaTelemetry(t *testing.T) {
	store := storage.NewStorage(storage.Config{
		Retention5s: time.Hour,
		Retention1m: 24 * time.Hour,
		MaxVariants: 2,
	})
	defer store.Close()

	usage := 0.42
	remaining := 0.58
	reset := time.Now().Add(3 * time.Hour).Unix()
	store.Write(&model.MetricSnapshot{
		Timestamp: time.Now().UnixMilli(),
		Variant:   "production",
		Quota: &model.QuotaState{
			FiveHour: &model.QuotaWindowState{
				LimitType:      "CREDIT_LIMIT",
				UsageRatio:     &usage,
				RemainingRatio: &remaining,
				ResetTimeUnix:  &reset,
			},
			SampleAgeSeconds: ptrFloat(20),
		},
	})
	// The canary exports no quota series: its status must carry no quota key
	// rather than a fabricated empty block.
	store.Write(&model.MetricSnapshot{
		Timestamp: time.Now().UnixMilli(),
		Variant:   "canary",
	})

	router := NewRouter(NewSSEHub(DefaultConfig()), store, DefaultConfig())
	mux := http.NewServeMux()
	router.SetupRoutes(mux)

	statusResponse := httptest.NewRecorder()
	mux.ServeHTTP(statusResponse, httptest.NewRequest(http.MethodGet, "/api/status", nil))
	if statusResponse.Code != http.StatusOK {
		t.Fatalf("status code = %d, body = %s", statusResponse.Code, statusResponse.Body)
	}

	var status model.StatusResponse
	if err := json.Unmarshal(statusResponse.Body.Bytes(), &status); err != nil {
		t.Fatalf("decode status response: %v", err)
	}
	if status.Production == nil || status.Production.Quota == nil {
		t.Fatalf("production status must carry quota telemetry: %+v", status.Production)
	}
	fiveHour := status.Production.Quota.FiveHour
	if fiveHour == nil || fiveHour.UsageRatio == nil || *fiveHour.UsageRatio != usage ||
		fiveHour.RemainingRatio == nil || *fiveHour.RemainingRatio != remaining {
		t.Errorf("unexpected production quota window: %+v", fiveHour)
	}
	if fiveHour.ResetTimeUnix == nil || *fiveHour.ResetTimeUnix != reset {
		t.Errorf("reset stamp = %+v, want %d", fiveHour.ResetTimeUnix, reset)
	}
	if status.Canary == nil || status.Canary.Quota != nil {
		t.Errorf("canary without quota series must have nil quota, got %+v", status.Canary)
	}

	// The historical range endpoint passes the quota block through per sample.
	metricsResponse := httptest.NewRecorder()
	mux.ServeHTTP(metricsResponse, httptest.NewRequest(http.MethodGet, "/api/metrics?range=1h&variant=production", nil))
	if metricsResponse.Code != http.StatusOK {
		t.Fatalf("metrics code = %d, body = %s", metricsResponse.Code, metricsResponse.Body)
	}

	var snapshots []model.MetricSnapshot
	if err := json.Unmarshal(metricsResponse.Body.Bytes(), &snapshots); err != nil {
		t.Fatalf("decode metrics response: %v", err)
	}
	if len(snapshots) != 1 || snapshots[0].Quota == nil || snapshots[0].Quota.FiveHour == nil ||
		snapshots[0].Quota.FiveHour.UsageRatio == nil || *snapshots[0].Quota.FiveHour.UsageRatio != usage {
		t.Errorf("metrics response lost the quota block: %+v", snapshots)
	}
}

func ptrFloat(v float64) *float64 { return &v }
