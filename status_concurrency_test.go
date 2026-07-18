package main

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

func TestServerStatusFetchesInfoAndMetricsConcurrently(t *testing.T) {
	started := make(chan string, 2)
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		started <- request.URL.Path
		<-release
		writer.Header().Set("Content-Type", "application/json")
		switch request.URL.Path {
		case "/v1/api/info":
			_, _ = writer.Write([]byte(`{"version":"1.0-test"}`))
		case "/v1/api/metrics":
			_, _ = writer.Write([]byte(`{"serverfps":120,"currentplayernum":2}`))
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()
	parsed, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(parsed.Port())
	if err != nil {
		t.Fatal(err)
	}
	previousRuntime := defaultProcessRuntime
	defaultProcessRuntime = &fakeProcessRuntime{}
	defer func() { defaultProcessRuntime = previousRuntime }()
	result := make(chan RuntimeStatus, 1)
	go func() {
		result <- serverStatusFromProcess(ServerInstance{RESTPort: port}, serverProcessSnapshot{PID: 42}, true)
	}()
	seen := map[string]bool{}
	for len(seen) < 2 {
		select {
		case path := <-started:
			seen[path] = true
		case <-time.After(time.Second):
			close(release)
			t.Fatalf("REST status requests were not concurrent: %#v", seen)
		}
	}
	close(release)
	status := <-result
	if !status.RESTAvailable || status.Version != "1.0-test" || status.FPS != 120 || status.Players != 2 {
		t.Fatalf("parallel REST status = %#v", status)
	}
}

type barrierProcessRuntime struct {
	started chan string
	release chan struct{}
}

func (runtime *barrierProcessRuntime) FindServerProcess(instance ServerInstance) (serverProcessSnapshot, bool, error) {
	runtime.started <- instance.ID
	<-runtime.release
	return serverProcessSnapshot{}, false, nil
}

func (*barrierProcessRuntime) HostResources() (HostResources, error)   { return HostResources{}, nil }
func (*barrierProcessRuntime) TCPListenerOwner(int) (int, bool, error) { return 0, false, nil }

func TestProcessMonitorPollsMultipleInstancesConcurrently(t *testing.T) {
	root := t.TempDir()
	instances := []ServerInstance{
		{ID: "server-a", RootPath: filepath.Join(root, "a")},
		{ID: "server-b", RootPath: filepath.Join(root, "b")},
	}
	app := &App{
		store:         &Store{path: filepath.Join(root, "config.json"), config: AppConfig{Instances: instances}},
		expectedStops: map[string]bool{}, startingServers: map[string]bool{}, operations: map[string]string{}, statusCache: map[string]cachedServerStatus{}, observedProcesses: map[string]observedServerProcess{},
	}
	barrier := &barrierProcessRuntime{started: make(chan string, len(instances)), release: make(chan struct{})}
	previousRuntime := defaultProcessRuntime
	defaultProcessRuntime = barrier
	defer func() { defaultProcessRuntime = previousRuntime }()
	done := make(chan struct{})
	go func() { app.pollServerProcesses(); close(done) }()
	seen := map[string]bool{}
	for len(seen) < len(instances) {
		select {
		case id := <-barrier.started:
			seen[id] = true
		case <-time.After(time.Second):
			close(barrier.release)
			t.Fatalf("instance polling was serialized: %#v", seen)
		}
	}
	close(barrier.release)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("parallel process monitor did not finish")
	}
}
