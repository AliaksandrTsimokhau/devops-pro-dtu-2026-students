package main

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"time"
)

//go:embed web/*
var assets embed.FS

var buildVersion = "development"

type releaseInfo struct {
	Version     string `json:"version"`
	Environment string `json:"environment"`
	Release     string `json:"release"`
	PipelineURL string `json:"pipelineUrl"`
	Hostname    string `json:"hostname"`
	StartedAt   string `json:"startedAt"`
}

func newHandler(info releaseInfo, ready *atomic.Bool) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("/readyz", func(w http.ResponseWriter, r *http.Request) {
		if !ready.Load() {
			http.Error(w, "draining", http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("/api/release", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method != http.MethodHead {
			_ = json.NewEncoder(w).Encode(info)
		}
	})
	files, err := fs.Sub(assets, "web")
	if err != nil {
		panic(err)
	}
	mux.Handle("/", http.FileServer(http.FS(files)))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; object-src 'none'; base-uri 'none'; frame-ancestors 'none'")
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		mux.ServeHTTP(w, r)
	})
}

func checkRelease(baseURL, expected string) error {
	client := &http.Client{Timeout: 5 * time.Second}
	for _, path := range []string{"/readyz", "/api/release"} {
		response, err := client.Get(strings.TrimRight(baseURL, "/") + path)
		if err != nil {
			return err
		}
		data, readErr := io.ReadAll(io.LimitReader(response.Body, 1<<20))
		_ = response.Body.Close()
		if readErr != nil || response.StatusCode != http.StatusOK {
			return fmt.Errorf("%s: status=%d read_error=%v", path, response.StatusCode, readErr)
		}
		if path == "/api/release" {
			var info releaseInfo
			if err := json.Unmarshal(data, &info); err != nil {
				return err
			}
			if info.Version != expected {
				return fmt.Errorf("wrong release: got %q, want %q", info.Version, expected)
			}
		}
	}
	return nil
}

func run() error {
	if len(os.Args) > 1 && os.Args[1] == "check" {
		flags := flag.NewFlagSet("check", flag.ContinueOnError)
		url := flags.String("url", "", "service base URL")
		version := flags.String("version", "", "expected commit")
		if err := flags.Parse(os.Args[2:]); err != nil {
			return err
		}
		if *url == "" || *version == "" {
			return errors.New("check requires --url and --version")
		}
		return checkRelease(*url, *version)
	}
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	number, err := strconv.Atoi(port)
	if err != nil || number < 1 || number > 65535 {
		return errors.New("PORT must be between 1 and 65535")
	}
	hostname, _ := os.Hostname()
	info := releaseInfo{Version: buildVersion, Environment: os.Getenv("APP_ENVIRONMENT"),
		Release: os.Getenv("APP_RELEASE"), PipelineURL: os.Getenv("APP_PIPELINE_URL"),
		Hostname: hostname, StartedAt: time.Now().UTC().Format(time.RFC3339)}
	var ready atomic.Bool
	ready.Store(true)
	server := &http.Server{Addr: ":" + port, Handler: newHandler(info, &ready),
		ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second,
		WriteTimeout: 10 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 8192}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()
	shutdownDone := make(chan struct{})
	go func() {
		defer close(shutdownDone)
		<-ctx.Done()
		ready.Store(false)
		shutdown, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdown); err != nil {
			log.Printf("shutdown: %v", err)
		}
	}()
	log.Printf("release-board listening on :%s version=%s", port, buildVersion)
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	<-shutdownDone
	return nil
}

func main() {
	if err := run(); err != nil {
		log.Print(err)
		os.Exit(1)
	}
}
