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

func TestMetricsAndStatusUseInMemoryStorage(t *testing.T) {
	store := storage.NewStorage(storage.Config{
		Retention5s: time.Hour,
		Retention1m: 24 * time.Hour,
		MaxVariants: 2,
	})
	defer store.Close()

	now := time.Now().UnixMilli()
	store.Write(&model.MetricSnapshot{
		Timestamp:                 now,
		Variant:                   "production",
		ReqRate:                   12.5,
		EstimatedCostUSDInput:     0.125,
		EstimatedCostUSDOutput:    0.25,
		EstimatedCostUSDCacheRead: 0.01,
	})
	store.Write(&model.MetricSnapshot{Timestamp: now, Variant: "canary", ReqRate: 3.5})

	router := NewRouter(NewSSEHub(DefaultConfig()), store, DefaultConfig())
	mux := http.NewServeMux()
	router.SetupRoutes(mux)

	metricsRequest := httptest.NewRequest(http.MethodGet, "/api/metrics?range=1h&variant=production", nil)
	metricsResponse := httptest.NewRecorder()
	mux.ServeHTTP(metricsResponse, metricsRequest)
	if metricsResponse.Code != http.StatusOK {
		t.Fatalf("metrics status = %d, body = %s", metricsResponse.Code, metricsResponse.Body)
	}

	var snapshots []model.MetricSnapshot
	if err := json.Unmarshal(metricsResponse.Body.Bytes(), &snapshots); err != nil {
		t.Fatalf("decode metrics response: %v", err)
	}
	if len(snapshots) != 1 || snapshots[0].Variant != "production" || snapshots[0].ReqRate != 12.5 || snapshots[0].EstimatedCostUSDInput != 0.125 || snapshots[0].EstimatedCostUSDOutput != 0.25 || snapshots[0].EstimatedCostUSDCacheRead != 0.01 {
		t.Fatalf("unexpected metrics response: %+v", snapshots)
	}

	statusRequest := httptest.NewRequest(http.MethodGet, "/api/status", nil)
	statusResponse := httptest.NewRecorder()
	mux.ServeHTTP(statusResponse, statusRequest)
	if statusResponse.Code != http.StatusOK {
		t.Fatalf("status status = %d, body = %s", statusResponse.Code, statusResponse.Body)
	}

	var status model.StatusResponse
	if err := json.Unmarshal(statusResponse.Body.Bytes(), &status); err != nil {
		t.Fatalf("decode status response: %v", err)
	}
	if status.Production == nil || status.Production.ReqRate != 12.5 {
		t.Fatalf("unexpected production status: %+v", status.Production)
	}
	if status.Canary == nil || status.Canary.ReqRate != 3.5 {
		t.Fatalf("unexpected canary status: %+v", status.Canary)
	}
}
