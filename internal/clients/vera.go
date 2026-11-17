package clients

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	serviceclient "github.com/Bengo-Hub/shared-service-client"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

// ErrAIUnavailable means marketflow-ai gave no usable answer: it was down, timed out, refused the
// call, or fell back to its neutral answer. Nothing was produced, so nothing may be charged.
var ErrAIUnavailable = errors.New("ai: no usable answer from marketflow-ai")

// VeraClient calls marketflow-ai (Vera) over the internal service key. Two routes are used, both
// already served to other services:
//
//   - POST /api/v1/internal/triage, the ticket classifier marketflow-ai built for ticketing. It
//     fences and masks the ticket and caps the confidence of a ticket carrying an injection phrase
//     at 0.8, so such a ticket always needs a person.
//   - POST /api/v1/agent/run, the generic task route (custom agents). The instructions are
//     ticketing's own; the ticket data goes in as the run's trigger context, which marketflow-ai
//     fences as data and masks before any model sees it. no_tools makes the run a plain
//     completion with no tool.
//
// AI calls go through shared-service-client without an idempotency key, so a failed call is never
// sent twice: one model call per request.
type VeraClient struct {
	apiKey string
	client *serviceclient.Client
	logger *zap.Logger
}

// NewVeraClient creates the client. apiKey is the shared INTERNAL_SERVICE_KEY.
func NewVeraClient(baseURL, apiKey string, timeout time.Duration, logger *zap.Logger) *VeraClient {
	log := logger.Named("vera-client")
	cfg := serviceclient.DefaultConfig(strings.TrimSuffix(baseURL, "/"), "marketflow-ai", log)
	if timeout > 0 {
		cfg.Timeout = timeout
	}
	return &VeraClient{apiKey: apiKey, client: serviceclient.New(cfg), logger: log}
}

func (c *VeraClient) headers(tenantID uuid.UUID) map[string]string {
	return map[string]string{"X-API-Key": c.apiKey, "X-Tenant-ID": tenantID.String()}
}

// TriageInput is the draft or ticket to classify.
type TriageInput struct {
	TenantID    uuid.UUID
	Subject     string
	Description string
}

// TriageResult is marketflow-ai's classification. Category is one of billing, technical, product
// or general; Priority one of low, normal, high or urgent.
type TriageResult struct {
	Category            string  `json:"category"`
	Priority            string  `json:"priority"`
	Confidence          float64 `json:"confidence"`
	Reasoning           string  `json:"reasoning"`
	RequiresHumanReview bool    `json:"requires_human_review"`
	// Injected is true when the ticket text carries an injection phrase; confidence is then at most
	// 0.8 and RequiresHumanReview is always true.
	Injected bool `json:"injected"`
}

// Triage classifies a ticket. marketflow-ai answers a failed model call with a neutral result
// (general, normal, confidence 0, no reasoning); that is reported as ErrAIUnavailable, because it
// is not a classification and must not be charged.
func (c *VeraClient) Triage(ctx context.Context, in TriageInput) (TriageResult, error) {
	if c.apiKey == "" {
		return TriageResult{}, fmt.Errorf("%w: INTERNAL_SERVICE_KEY is not configured", ErrAIUnavailable)
	}
	body := map[string]any{"subject": in.Subject, "description": in.Description, "tenant_id": in.TenantID.String()}
	resp, err := c.client.Post(ctx, "/api/v1/internal/triage", body, c.headers(in.TenantID))
	if err != nil {
		return TriageResult{}, fmt.Errorf("%w: triage: %v", ErrAIUnavailable, err)
	}
	if !resp.IsSuccess() {
		return TriageResult{}, fmt.Errorf("%w: triage: status %d", ErrAIUnavailable, resp.StatusCode)
	}
	var out TriageResult
	if err := resp.DecodeJSON(&out); err != nil {
		return TriageResult{}, fmt.Errorf("%w: triage decode: %v", ErrAIUnavailable, err)
	}
	if out.Confidence <= 0 && strings.TrimSpace(out.Reasoning) == "" {
		return TriageResult{}, fmt.Errorf("%w: triage fell back to its neutral answer", ErrAIUnavailable)
	}
	return out, nil
}

// TaskInput is one ticketing task run on marketflow-ai's generic task route.
type TaskInput struct {
	// RunID names the run in marketflow-ai's logs; ticketing uses its credit key.
	RunID    string
	TenantID uuid.UUID
	// Instructions are ticketing's prompt for the task.
	Instructions string
	// Data is the ticket data the task reads. marketflow-ai fences it as data and masks emails
	// and phone numbers before the model sees it.
	Data map[string]any
}

// TaskResult is the run's answer. StoppedEarly and StopReason come from marketflow-ai's agent
// loop (for example a token cap); the answer may then be partial. ReviewNotes are marketflow-ai's
// figure and commitment checks of the answer against the instructions and the data (advice for
// the agent, never a block). Tainted is true when the run read text carrying an injection phrase.
type TaskResult struct {
	Answer       string
	StoppedEarly bool
	StopReason   string
	ReviewNotes  []string
	Tainted      bool
}

type agentRunResponse struct {
	Status       string   `json:"status"`
	Answer       string   `json:"answer"`
	Error        string   `json:"error"`
	StoppedEarly bool     `json:"stopped_early"`
	StopReason   string   `json:"stop_reason"`
	ReviewNotes  []string `json:"review_notes"`
	Tainted      bool     `json:"tainted"`
}

// RunTask runs one single-answer task: no_tools asks marketflow-ai for a plain completion with no
// tool at all. It is never billable on marketflow-ai's side: ticketing charges the tenant's AI
// credits itself, once, after a usable answer.
func (c *VeraClient) RunTask(ctx context.Context, in TaskInput) (TaskResult, error) {
	if c.apiKey == "" {
		return TaskResult{}, fmt.Errorf("%w: INTERNAL_SERVICE_KEY is not configured", ErrAIUnavailable)
	}
	body := map[string]any{
		"run_id":         in.RunID,
		"tenant_id":      in.TenantID.String(),
		"system_prompt":  in.Instructions,
		"no_tools":       true,
		"max_iterations": 1,
		"context":        in.Data,
		"billable":       false,
	}
	resp, err := c.client.Post(ctx, "/api/v1/agent/run", body, c.headers(in.TenantID))
	if err != nil {
		return TaskResult{}, fmt.Errorf("%w: task: %v", ErrAIUnavailable, err)
	}
	if !resp.IsSuccess() {
		return TaskResult{}, fmt.Errorf("%w: task: status %d", ErrAIUnavailable, resp.StatusCode)
	}
	var out agentRunResponse
	if err := resp.DecodeJSON(&out); err != nil {
		return TaskResult{}, fmt.Errorf("%w: task decode: %v", ErrAIUnavailable, err)
	}
	if out.Status != "completed" {
		c.logger.Warn("marketflow-ai task did not complete", zap.String("run_id", in.RunID), zap.String("status", out.Status))
		return TaskResult{}, fmt.Errorf("%w: task status %q", ErrAIUnavailable, out.Status)
	}
	answer := strings.TrimSpace(out.Answer)
	if answer == "" {
		return TaskResult{}, fmt.Errorf("%w: empty answer", ErrAIUnavailable)
	}
	return TaskResult{Answer: answer, StoppedEarly: out.StoppedEarly, StopReason: out.StopReason,
		ReviewNotes: out.ReviewNotes, Tainted: out.Tainted}, nil
}
