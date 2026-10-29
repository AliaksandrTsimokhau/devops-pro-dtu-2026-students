package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func fixture() (http.Handler, *atomic.Bool) {
	ready := &atomic.Bool{}
	ready.Store(true)
	return newHandler(releaseInfo{Version: "abc123", Environment: "student", Release: "0.1.2"}, ready), ready
}

func TestReleaseAPIContract(t *testing.T) {
	handler, _ := fixture()
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/release", nil))
	var data map[string]string
	if err := json.Unmarshal(response.Body.Bytes(), &data); err != nil {
		t.Fatal(err)
	}
	if response.Code != http.StatusOK || data["version"] != "abc123" || data["environment"] != "student" {
		t.Fatalf("broken client contract: status=%d body=%s", response.Code, response.Body.String())
	}
	if response.Header().Get("Content-Type") != "application/json" {
		t.Fatal("API must advertise JSON")
	}
}

func TestReadinessDrainsWithoutFailingLiveness(t *testing.T) {
	handler, ready := fixture()
	ready.Store(false)
	for path, expected := range map[string]int{"/readyz": 503, "/healthz": 200, "/not-ready": 404} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
		if response.Code != expected {
			t.Errorf("%s: got %d, want %d", path, response.Code, expected)
		}
	}
}

func TestReadOnlyMethodsAndFrontend(t *testing.T) {
	handler, _ := fixture()
	for _, path := range []string{"/", "/app.js", "/style.css"} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
		if response.Code != http.StatusOK || response.Header().Get("X-Content-Type-Options") != "nosniff" {
			t.Errorf("frontend resource or security headers failed: %s", path)
		}
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/release", strings.NewReader("{}")))
	if response.Code != http.StatusMethodNotAllowed {
		t.Fatal("release API must reject writes")
	}
}

func TestReleaseCheckRejectsWrongCommitAndUnreadyPod(t *testing.T) {
	handler, ready := fixture()
	server := httptest.NewServer(handler)
	defer server.Close()
	if err := checkRelease(server.URL, "abc123"); err != nil {
		t.Fatal(err)
	}
	if err := checkRelease(server.URL, "old-commit"); err == nil {
		t.Fatal("a healthy endpoint serving the wrong commit must fail verification")
	}
	ready.Store(false)
	if err := checkRelease(server.URL, "abc123"); err == nil {
		t.Fatal("an unready pod must fail verification")
	}
}
