// Regression test for index.html redirect loop bug (bd-m6ah)
package main

import (
	"io/fs"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ardenone/ardenone-cluster/containers/zai-proxy-dashboard/api"
	"github.com/ardenone/ardenone-cluster/containers/zai-proxy-dashboard/logger"
	"github.com/ardenone/ardenone-cluster/containers/zai-proxy-dashboard/storage"
)

// TestIndexRedirectLoopBug verifies that / and /index.html return 200 OK
// instead of redirecting infinitely (bd-m6ah).
func TestIndexRedirectLoopBug(t *testing.T) {
	// Initialize logger
	logger.Init(logger.DefaultConfig())

	// Create test storage
	store, err := storage.NewStorage(storage.DefaultConfig())
	if err != nil {
		t.Fatalf("failed to create storage: %v", err)
	}
	defer store.Close()

	// Initialize hub
	hub := api.NewSSEHub(api.DefaultConfig())

	// Create router
	router := api.NewRouter(hub, store, api.DefaultConfig())

	// Create test server with the same setup as main.go
	mux := http.NewServeMux()
	router.SetupRoutes(mux)

	// Set up static file handlers (copied from main.go)
	frontendSub, err := fs.Sub(frontendFS, "frontend/dist")
	if err != nil {
		t.Fatalf("failed to create frontend sub FS: %v", err)
	}
	frontendHandler := http.FileServer(http.FS(frontendSub))
	mux.Handle("/assets/", frontendHandler)

	// Serve index.html for root path - directly read to avoid redirect loop
	indexHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		content, err := frontendFS.ReadFile("frontend/dist/index.html")
		if err != nil {
			http.Error(w, "index.html not found", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write(content)
	})

	// Serve index.html for all other routes (SPA)
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" || r.URL.Path == "/index.html" {
			indexHandler.ServeHTTP(w, r)
			return
		}
		// Try static files first, then fallback to index.html for SPA routing
		if _, err := frontendFS.Open("frontend/dist" + r.URL.Path); err == nil {
			frontendHandler.ServeHTTP(w, r)
			return
		}
		// SPA fallback - serve index.html for client-side routes
		indexHandler.ServeHTTP(w, r)
	})

	// Apply middleware
	handler := api.Chain(mux,
		api.RecoveryMiddleware,
		api.LoggingMiddleware,
		api.CORSMiddleware,
	)

	server := httptest.NewServer(handler)
	defer server.Close()

	t.Run("root path returns 200", func(t *testing.T) {
		resp, err := http.Get(server.URL + "/")
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("expected status 200, got %d", resp.StatusCode)
		}

		ct := resp.Header.Get("Content-Type")
		if !strings.Contains(ct, "text/html") {
			t.Errorf("expected Content-Type to contain text/html, got %s", ct)
		}
	})

	t.Run("index.html returns 200 not 301", func(t *testing.T) {
		// Don't follow redirects - we want to catch any 301 redirect
		client := &http.Client{
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				t.Errorf("got redirect to %s, expected direct 200 response", req.URL.String())
				return http.ErrUseLastResponse
			},
		}

		resp, err := client.Get(server.URL + "/index.html")
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		defer resp.Body.Close()

		// This is the key test - should be 200, NOT 301
		if resp.StatusCode != http.StatusOK {
			t.Errorf("expected status 200, got %d", resp.StatusCode)
		}

		ct := resp.Header.Get("Content-Type")
		if !strings.Contains(ct, "text/html") {
			t.Errorf("expected Content-Type to contain text/html, got %s", ct)
		}

		// Verify it's actually HTML content
		body := make([]byte, 1024)
		n, _ := resp.Body.Read(body)
		bodyStr := string(body[:n])
		if !strings.Contains(bodyStr, "<!doctype html>") {
			t.Errorf("response body doesn't appear to be HTML")
		}
	})

	t.Run("SPA route fallback returns index.html", func(t *testing.T) {
		resp, err := http.Get(server.URL + "/dashboard")
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("expected status 200, got %d", resp.StatusCode)
		}

		ct := resp.Header.Get("Content-Type")
		if !strings.Contains(ct, "text/html") {
			t.Errorf("expected Content-Type to contain text/html, got %s", ct)
		}
	})
}
