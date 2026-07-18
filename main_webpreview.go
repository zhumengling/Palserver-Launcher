//go:build webpreview

package main

import (
	"context"
	"flag"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"time"
)

func previewLoopbackAddress(address string) bool {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return false
	}
	host = strings.Trim(host, "[]")
	return host == "localhost" || net.ParseIP(host) != nil && net.ParseIP(host).IsLoopback()
}

func main() {
	listen := flag.String("listen", "127.0.0.1:18210", "local web preview listen address")
	dataDir := flag.String("data", filepath.Join(os.TempDir(), "palserver-launcher-web-preview"), "isolated preview data directory")
	platform := flag.String("platform", "linux", "frontend platform to simulate: linux or windows")
	flag.Parse()
	if !previewLoopbackAddress(*listen) {
		log.Fatal("web preview only permits a loopback listen address")
	}
	previewPlatform, err := normalizeAgentPlatform(*platform)
	if err != nil {
		log.Fatal(err)
	}
	if err := os.Setenv("PALSERVER_LAUNCHER_HOME", *dataDir); err != nil {
		log.Fatal(err)
	}
	app := NewApp()
	authFile := filepath.Join(*dataDir, "admin-auth.json")
	auth, err := newPersistentAgentAuth(authFile)
	if err != nil {
		log.Fatal(err)
	}
	app.setAgentAuthFile(authFile)
	app.startup(context.Background())
	defer app.shutdown(context.Background())
	handler, err := newAgentHTTPHandlerForPlatform(app, auth, previewPlatform)
	if err != nil {
		log.Fatal(err)
	}
	server := &http.Server{Addr: *listen, Handler: handler, ReadHeaderTimeout: 10 * time.Second, ReadTimeout: 30 * time.Second, WriteTimeout: 10 * time.Minute, IdleTimeout: 90 * time.Second}
	app.quit = func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = server.Shutdown(ctx)
	}
	interrupt := make(chan os.Signal, 1)
	signal.Notify(interrupt, os.Interrupt)
	go func() {
		<-interrupt
		app.quit()
	}()
	log.Printf("Web preview listening on http://%s (simulated platform: %s)", *listen, previewPlatform)
	log.Printf("Preview platform simulation only changes the web capability view; server operations still use the host operating system")
	if auth.setupRequired() {
		log.Printf("首次打开网页后请创建管理密码")
	}
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}
