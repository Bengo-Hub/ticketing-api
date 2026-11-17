package clients

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

func TestVeraTriage(t *testing.T) {
	tenant := uuid.New()
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/internal/triage" {
			t.Errorf("%s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("X-API-Key") != "svc-key" || r.Header.Get("X-Tenant-ID") != tenant.String() {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_ = json.NewDecoder(r.Body).Decode(&got)
		_, _ = w.Write([]byte(`{"category":"billing","priority":"high","confidence":0.8,"reasoning":"payment","requires_human_review":true,"injected":true}`))
	}))
	defer srv.Close()

	res, err := NewVeraClient(srv.URL+"/", "svc-key", time.Second, zap.NewNop()).
		Triage(context.Background(), TriageInput{TenantID: tenant, Subject: "Charged twice", Description: "My card"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Category != "billing" || res.Priority != "high" || res.Confidence != 0.8 || !res.RequiresHumanReview || !res.Injected {
		t.Errorf("result %+v", res)
	}
	if got["subject"] != "Charged twice" || got["description"] != "My card" || got["tenant_id"] != tenant.String() {
		t.Errorf("body %v", got)
	}
}

// marketflow-ai answers a failed model call with its neutral result; that is no classification.
func TestVeraTriageNeutralFallbackIsUnavailable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"category":"general","priority":"normal","confidence":0,"requires_human_review":true}`))
	}))
	defer srv.Close()
	_, err := NewVeraClient(srv.URL, "k", time.Second, zap.NewNop()).Triage(context.Background(), TriageInput{TenantID: uuid.New(), Subject: "s"})
	if !errors.Is(err, ErrAIUnavailable) {
		t.Fatalf("err = %v", err)
	}
}

// An AI call is never retried: a 5xx costs one model call, not several.
func TestVeraTaskIsNotRetried(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer srv.Close()
	_, err := NewVeraClient(srv.URL, "k", time.Second, zap.NewNop()).RunTask(context.Background(), TaskInput{RunID: "r", TenantID: uuid.New()})
	if !errors.Is(err, ErrAIUnavailable) {
		t.Fatalf("err = %v", err)
	}
	if n := atomic.LoadInt32(&hits); n != 1 {
		t.Fatalf("calls = %d, want 1", n)
	}
}

func TestVeraRunTask(t *testing.T) {
	tenant := uuid.New()
	var got map[string]any
	status := "completed"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/agent/run" || r.Header.Get("X-API-Key") != "k" || r.Header.Get("X-Tenant-ID") != tenant.String() {
			t.Errorf("%s %v", r.URL.Path, r.Header)
		}
		_ = json.NewDecoder(r.Body).Decode(&got)
		_ = json.NewEncoder(w).Encode(map[string]any{"run_id": got["run_id"], "status": status, "answer": " Hello ",
			"stopped_early": true, "stop_reason": "token_cap_reached",
			"review_notes": []string{"KES 500 is not in the instructions or the data"}, "tainted": true})
	}))
	defer srv.Close()
	c := NewVeraClient(srv.URL, "k", time.Second, zap.NewNop())

	res, err := c.RunTask(context.Background(), TaskInput{RunID: "run-1", TenantID: tenant, Instructions: "Summarize",
		Data: map[string]any{"subject": "Printer"}})
	if err != nil {
		t.Fatal(err)
	}
	if res.Answer != "Hello" || !res.StoppedEarly || res.StopReason != "token_cap_reached" || !res.Tainted ||
		len(res.ReviewNotes) != 1 || res.ReviewNotes[0] != "KES 500 is not in the instructions or the data" {
		t.Errorf("result %+v", res)
	}
	// A plain completion: no_tools, and no allowed_tools at all (an empty list means the knowledge tools).
	if _, sent := got["allowed_tools"]; sent || got["no_tools"] != true {
		t.Errorf("tools in body %v", got)
	}
	if got["billable"] != false || got["max_iterations"] != float64(1) ||
		got["system_prompt"] != "Summarize" || got["tenant_id"] != tenant.String() {
		t.Errorf("body %v", got)
	}
	if ctx, _ := got["context"].(map[string]any); ctx["subject"] != "Printer" {
		t.Errorf("context %v", got["context"])
	}

	status = "failed"
	if _, err := c.RunTask(context.Background(), TaskInput{RunID: "run-2", TenantID: tenant}); !errors.Is(err, ErrAIUnavailable) {
		t.Errorf("a failed run must be unavailable: %v", err)
	}
	if _, err := NewVeraClient(srv.URL, "", time.Second, zap.NewNop()).RunTask(context.Background(), TaskInput{TenantID: tenant}); !errors.Is(err, ErrAIUnavailable) {
		t.Errorf("a missing key must fail before the call: %v", err)
	}
}
