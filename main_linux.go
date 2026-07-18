//go:build linux && !webpreview

package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

func loopbackListenAddress(address string) bool {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return false
	}
	host = strings.Trim(host, "[]")
	return host == "localhost" || net.ParseIP(host) != nil && net.ParseIP(host).IsLoopback()
}

func main() {
	if handled, exitCode := runLauncherUpdaterFromArgs(os.Args); handled {
		os.Exit(exitCode)
	}
	defaultBase, err := appDataDir()
	if err != nil {
		log.Fatal(err)
	}
	listen := flag.String("listen", "127.0.0.1:8210", "web console listen address")
	defaultAuthFile := filepath.Join(defaultBase, "admin-auth.json")
	authFile := flag.String("auth-file", defaultAuthFile, "administrator password credential file")
	tlsCert := flag.String("tls-cert", "", "TLS certificate path")
	tlsKey := flag.String("tls-key", "", "TLS private key path")
	allowHTTP := flag.Bool("allow-http", false, "allow clear-text HTTP on a non-loopback address")
	showVersion := flag.Bool("version", false, "print version and exit")
	selfTest := flag.Bool("self-test", false, "validate Linux Agent deployment permissions and environment")
	flag.Parse()
	credentialPath := *authFile
	if *showVersion {
		fmt.Println(LauncherVersion)
		return
	}
	if *selfTest {
		report, selfTestErr := runLinuxAgentSelfTest(defaultBase, credentialPath)
		if err := json.NewEncoder(os.Stdout).Encode(report); err != nil {
			log.Fatal(err)
		}
		if selfTestErr != nil {
			os.Exit(1)
		}
		return
	}
	if !loopbackListenAddress(*listen) && (*tlsCert == "" || *tlsKey == "") && !*allowHTTP {
		log.Fatal("refusing non-loopback HTTP; configure --tls-cert/--tls-key or explicitly pass --allow-http")
	}
	auth, err := newPersistentAgentAuth(credentialPath)
	if err != nil {
		log.Fatal(err)
	}
	if auth.setupRequired() {
		log.Printf("尚未创建管理密码；请首次打开网页控制台完成初始化")
	}
	app := NewApp()
	app.setAgentAuthFile(credentialPath)
	app.startup(context.Background())
	defer app.shutdown(context.Background())
	handler, err := newAgentHTTPHandler(app, auth)
	if err != nil {
		log.Fatal(err)
	}
	// Save migrations can legitimately be several gigabytes over a home
	// connection. MaxBytesReader on upload endpoints provides the size bound;
	// this longer deadline prevents the generic server timeout from aborting a
	// valid browser upload after 30 seconds.
	server := &http.Server{Addr: *listen, Handler: handler, ReadHeaderTimeout: 10 * time.Second, ReadTimeout: 2 * time.Hour, WriteTimeout: 10 * time.Minute, IdleTimeout: 90 * time.Second}
	app.quit = func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		_ = server.Shutdown(ctx)
	}
	interrupt := make(chan os.Signal, 1)
	signal.Notify(interrupt, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-interrupt
		app.quit()
	}()
	log.Printf("Palserver Linux Agent %s listening on %s", LauncherVersion, *listen)
	go func() {
		time.Sleep(30 * time.Second)
		if executable, executableErr := os.Executable(); executableErr == nil {
			_ = os.Remove(executable + ".previous")
		}
	}()
	if *tlsCert != "" && *tlsKey != "" {
		err = server.ListenAndServeTLS(*tlsCert, *tlsKey)
	} else {
		err = server.ListenAndServe()
	}
	if err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}
