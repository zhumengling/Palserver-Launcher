package main

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func officialTestPort(t *testing.T, server *httptest.Server) int {
	t.Helper()
	parsed, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(parsed.Port())
	if err != nil {
		t.Fatal(err)
	}
	return port
}

func TestOfficialCacheCoalescesConcurrentRequestsAndSharesStatusData(t *testing.T) {
	var infoRequests atomic.Int32
	var metricRequests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		switch request.URL.Path {
		case "/v1/api/info":
			infoRequests.Add(1)
			time.Sleep(25 * time.Millisecond)
			_, _ = writer.Write([]byte(`{"version":"1.0-cache","servername":"Cache Test"}`))
		case "/v1/api/metrics":
			metricRequests.Add(1)
			time.Sleep(25 * time.Millisecond)
			_, _ = writer.Write([]byte(`{"serverfps":119,"currentplayernum":3,"maxplayernum":32}`))
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()
	instance := ServerInstance{ID: "srv-cache", RESTPort: officialTestPort(t, server)}
	app := &App{store: &Store{config: AppConfig{Instances: []ServerInstance{instance}}}, officialCache: map[string]*officialCacheEntry{}}

	var wait sync.WaitGroup
	for range 12 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			metrics, err := app.GetServerMetrics(instance.ID)
			if err != nil || metrics.ServerFPS != 119 {
				t.Errorf("GetServerMetrics() = %#v, %v", metrics, err)
			}
		}()
	}
	wait.Wait()
	if got := metricRequests.Load(); got != 1 {
		t.Fatalf("concurrent metric requests = %d, want 1", got)
	}

	previousRuntime := defaultProcessRuntime
	defaultProcessRuntime = &fakeProcessRuntime{}
	defer func() { defaultProcessRuntime = previousRuntime }()
	status := app.cachedStatusFromProcess(instance, serverProcessSnapshot{PID: 42}, true)
	if !status.RESTAvailable || status.Version != "1.0-cache" || status.FPS != 119 || status.Players != 3 {
		t.Fatalf("cached runtime status = %#v", status)
	}
	if got := infoRequests.Load(); got != 1 {
		t.Fatalf("info requests = %d, want 1", got)
	}
	if got := metricRequests.Load(); got != 1 {
		t.Fatalf("status did not reuse metric cache; requests = %d", got)
	}
}

func TestOfficialCacheExpiresAndCanBeInvalidated(t *testing.T) {
	app := &App{officialCache: map[string]*officialCacheEntry{}}
	var requests atomic.Int32
	fetch := func() (ServerMetrics, error) {
		requests.Add(1)
		return ServerMetrics{ServerFPS: float64(requests.Load())}, nil
	}
	first, err := cachedOfficial(app, "srv-expiry", "metrics", 25*time.Millisecond, fetch)
	if err != nil {
		t.Fatal(err)
	}
	second, err := cachedOfficial(app, "srv-expiry", "metrics", 25*time.Millisecond, fetch)
	if err != nil || first.ServerFPS != second.ServerFPS || requests.Load() != 1 {
		t.Fatalf("cache hit first=%#v second=%#v requests=%d err=%v", first, second, requests.Load(), err)
	}
	time.Sleep(35 * time.Millisecond)
	third, err := cachedOfficial(app, "srv-expiry", "metrics", 25*time.Millisecond, fetch)
	if err != nil || third.ServerFPS == second.ServerFPS || requests.Load() != 2 {
		t.Fatalf("expired cache third=%#v requests=%d err=%v", third, requests.Load(), err)
	}
	app.invalidateOfficialCache("srv-expiry", "metrics")
	fourth, err := cachedOfficial(app, "srv-expiry", "metrics", time.Minute, fetch)
	if err != nil || fourth.ServerFPS == third.ServerFPS || requests.Load() != 3 {
		t.Fatalf("invalidated cache fourth=%#v requests=%d err=%v", fourth, requests.Load(), err)
	}
}
