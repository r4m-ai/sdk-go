// Hand-written, copied in by scripts/generate-sdks.sh. It is NOT generated —
// the generator wipes sdks/go/ on every run, so this file lives in
// scripts/sdk-templates/go/ and is copied back afterwards.
//
// Everything here sits on top of the generated calls; nothing is shadowed.
// CreateOptimizationWithResponse keeps returning a receipt, so setting a
// WebhookUrl and waiting, or setting one and polling yourself, all stay legal
// — the delivery modes are deliberately not mutually exclusive.

package r4m

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	openapi_types "github.com/oapi-codegen/runtime/types"
)

// DefaultBaseURL is baked in at generation time so NewClientWithKey needs
// nothing but an API key.
const DefaultBaseURL = "https://r4m.ai"

// NewClientWithKey builds a client for your account. Nothing is sent — this
// only holds your credentials until the first call.
//
// Named for its neighbours (NewClient, NewClientWithResponses) rather than to
// match the Python and TypeScript helpers: plain NewClient is already taken by
// the generated low-level constructor.
//
// NewClientWithResponses does the same thing, but wants a server URL every
// time even though there is only one host worth talking to, and has no auth
// helper — so every caller writes the same request editor. Pass extra options
// to reach a staging deployment (r4m.WithBaseURL).
func NewClientWithKey(apiKey string, opts ...ClientOption) (*ClientWithResponses, error) {
	bearer := func(ctx context.Context, req *http.Request) error {
		req.Header.Set("Authorization", "Bearer "+apiKey)
		return nil
	}
	return NewClientWithResponses(
		DefaultBaseURL,
		append([]ClientOption{WithRequestEditorFn(bearer)}, opts...)...,
	)
}

const (
	DefaultWaitTimeout  = 5 * time.Minute
	DefaultInitialDelay = 1 * time.Second
	DefaultMaxDelay     = 5 * time.Second
	// DefaultBusyTimeout is how long "busy" is acceptable before a call fails.
	DefaultBusyTimeout = 2 * time.Minute
	// DefaultActivePoll is how often WaitForActiveOptimizations looks.
	DefaultActivePoll = 5 * time.Second

	// Used when a 429 arrives without a usable Retry-After header.
	fallbackRetryAfter = 5 * time.Second
)

// --- Busy -------------------------------------------------------------------
//
// The API says "busy" with a 429 in two situations, and a client should treat
// them the same way: the per-minute rate limit (RATE_LIMITED) and the plan's
// optimizations-in-flight allowance (CONCURRENCY_LIMIT_EXCEEDED). Both carry
// Retry-After; the second also names the optimizations holding the slots.

// BusyReason says which limit answered 429.
type BusyReason string

const (
	BusyRateLimited              BusyReason = "RATE_LIMITED"
	BusyConcurrencyLimitExceeded BusyReason = "CONCURRENCY_LIMIT_EXCEEDED"
)

// BusyDecision is what to do about a busy answer: wait it out, or give up now.
type BusyDecision string

const (
	BusyWait BusyDecision = "wait"
	BusyFail BusyDecision = "fail"
)

// BusyEvent describes one busy answer.
type BusyEvent struct {
	Reason BusyReason
	// Message is the server's own explanation.
	Message string
	// RetryAfter is how long the server asked us to wait before trying again.
	RetryAfter time.Duration
	// Attempt is 1 for the first busy answer of this call, counting up.
	Attempt int
	// Elapsed is the time spent busy-waiting in this call so far.
	Elapsed time.Duration
	// ActiveOptimizationIDs names the optimizations holding the slots
	// (concurrency only; empty otherwise).
	ActiveOptimizationIDs []string
	// TimedOut reports that BusyTimeout has elapsed. From here on only
	// BusyFail is honoured: a BusyWait returned after the deadline is treated
	// as BusyFail, because the caller already said how long busy is acceptable.
	TimedOut bool
}

// OnBusy is called on every busy answer, synchronously: decide, and the SDK
// does the waiting (honouring Retry-After and polling a named optimization
// first, so a slot whose deadline passed is released server-side). Do not
// sleep here. Without a callback the SDK waits until BusyTimeout, then fails.
type OnBusy func(BusyEvent) BusyDecision

// BusyError means the API was busy and the call was not allowed to keep
// waiting.
type BusyError struct {
	Event BusyEvent
}

func (e *BusyError) Error() string {
	if e.Event.TimedOut {
		return fmt.Sprintf("still busy (%s) after %s: %s", e.Event.Reason, e.Event.Elapsed.Round(time.Second), e.Event.Message)
	}
	return fmt.Sprintf("busy (%s): %s", e.Event.Reason, e.Event.Message)
}

// WaitOptions tunes how long and how eagerly to poll, and how busy answers
// are handled. The zero value is usable: every field falls back to its
// Default* constant, and a nil OnBusy waits until BusyTimeout, then fails.
type WaitOptions struct {
	Timeout      time.Duration
	InitialDelay time.Duration
	MaxDelay     time.Duration
	OnBusy       OnBusy
	BusyTimeout  time.Duration
}

func (o WaitOptions) withDefaults() WaitOptions {
	if o.Timeout <= 0 {
		o.Timeout = DefaultWaitTimeout
	}
	if o.InitialDelay <= 0 {
		o.InitialDelay = DefaultInitialDelay
	}
	if o.MaxDelay <= 0 {
		o.MaxDelay = DefaultMaxDelay
	}
	if o.BusyTimeout <= 0 {
		o.BusyTimeout = DefaultBusyTimeout
	}
	return o
}

// OptimizationFailedError means the optimization finished, but the solver
// could not produce a plan.
type OptimizationFailedError struct {
	Optimization Optimization
}

func (e *OptimizationFailedError) Error() string {
	msg := "optimization failed"
	if e.Optimization.ErrorMessage != nil && *e.Optimization.ErrorMessage != "" {
		msg = *e.Optimization.ErrorMessage
	}
	return msg
}

// OptimizationTimeoutError means the wait ran out of time. The optimization
// is still running server-side.
type OptimizationTimeoutError struct {
	OptimizationID openapi_types.UUID
	Timeout        time.Duration
}

func (e *OptimizationTimeoutError) Error() string {
	return fmt.Sprintf(
		"timed out after %s waiting for optimization %s; it is still being "+
			"processed — fetch it later with GetOptimizationWithResponse",
		e.Timeout, e.OptimizationID,
	)
}

// ActiveOptimizationsTimeoutError means the account still had optimizations
// running when the wait ran out.
type ActiveOptimizationsTimeoutError struct {
	ActiveOptimizationIDs []string
	Timeout               time.Duration
}

func (e *ActiveOptimizationsTimeoutError) Error() string {
	return fmt.Sprintf(
		"timed out after %s with %d optimization(s) still running: %s",
		e.Timeout, len(e.ActiveOptimizationIDs), strings.Join(e.ActiveOptimizationIDs, ", "),
	)
}

// retryAfter reads the Retry-After header (RFC 9110), falling back when it is
// absent or unparseable. Honouring it matters: the limiter protects the
// backend, so ignoring the one header that says "stop" turns a busy moment
// into an outage.
func retryAfter(resp *http.Response) time.Duration {
	if resp == nil {
		return fallbackRetryAfter
	}
	seconds, err := strconv.Atoi(resp.Header.Get("Retry-After"))
	if err != nil || seconds <= 0 {
		return fallbackRetryAfter
	}
	return time.Duration(seconds) * time.Second
}

// sleepCtx waits for d, or returns early if ctx is cancelled.
func sleepCtx(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// errorBody is the error envelope, read defensively: a 429 may arrive with
// any body, so it is decoded here rather than through the generated type.
type errorBody struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
		Details struct {
			ActiveOptimizationIDs []string `json:"activeOptimizationIds"`
		} `json:"details"`
	} `json:"error"`
}

// busyState is per-call busy bookkeeping: when waiting started and how often
// it happened.
type busyState struct {
	startedAt time.Time
	attempt   int
}

func newBusyState() *busyState { return &busyState{startedAt: time.Now()} }

// onBusyResponse handles one busy answer: build the event, ask the callback,
// then either wait as the server asked or return a *BusyError. Shared by
// every helper so a 429 means the same thing whichever call met it.
func (c *ClientWithResponses) onBusyResponse(
	ctx context.Context,
	body []byte,
	resp *http.Response,
	state *busyState,
	opts WaitOptions,
) error {
	state.attempt++
	var parsed errorBody
	_ = json.Unmarshal(body, &parsed)
	elapsed := time.Since(state.startedAt)

	event := BusyEvent{
		Reason:                BusyRateLimited,
		Message:               parsed.Error.Message,
		RetryAfter:            retryAfter(resp),
		Attempt:               state.attempt,
		Elapsed:               elapsed,
		ActiveOptimizationIDs: parsed.Error.Details.ActiveOptimizationIDs,
		TimedOut:              elapsed >= opts.BusyTimeout,
	}
	if parsed.Error.Code == string(BusyConcurrencyLimitExceeded) {
		event.Reason = BusyConcurrencyLimitExceeded
	}
	if event.Message == "" {
		event.Message = "too many requests"
	}
	if event.ActiveOptimizationIDs == nil {
		event.ActiveOptimizationIDs = []string{}
	}

	decision := BusyWait
	if opts.OnBusy != nil {
		decision = opts.OnBusy(event)
	}
	if decision == BusyFail || event.TimedOut {
		return &BusyError{Event: event}
	}

	// Reading one of the optimizations holding a slot is not idle curiosity:
	// that GET is what lets the server notice a slot whose deadline has passed
	// and release it. Then wait exactly as long as the server asked.
	if len(event.ActiveOptimizationIDs) > 0 {
		var id openapi_types.UUID
		if err := id.UnmarshalText([]byte(event.ActiveOptimizationIDs[0])); err == nil {
			_, _ = c.GetOptimizationWithResponse(ctx, id)
		}
	}
	return sleepCtx(ctx, event.RetryAfter)
}

// SubmitOptimization submits an optimization, waiting out "busy" answers,
// and returns the receipt (status pending). Use this when you will collect
// the plan some other way — a webhook, your own poller — but still want the
// 429s handled. Only OnBusy and BusyTimeout of opts are read.
func (c *ClientWithResponses) SubmitOptimization(
	ctx context.Context,
	body CreateOptimizationJSONRequestBody,
	opts WaitOptions,
) (*Optimization, error) {
	opts = opts.withDefaults()
	state := newBusyState()

	for {
		created, err := c.CreateOptimizationWithResponse(ctx, body)
		if err != nil {
			return nil, err
		}
		if created.StatusCode() == http.StatusTooManyRequests {
			if berr := c.onBusyResponse(ctx, created.Body, created.HTTPResponse, state, opts); berr != nil {
				return nil, berr
			}
			continue
		}
		if created.JSON202 == nil {
			return nil, fmt.Errorf(
				"could not create the optimization (HTTP %d): %s",
				created.StatusCode(), string(created.Body),
			)
		}
		return &created.JSON202.Data, nil
	}
}

// WaitForRoutes polls an optimization until it is finished.
//
// Use this when the id came from somewhere else — your own queue, a database
// row, an earlier process. If you are submitting and waiting in one place,
// CreateOptimizationAndWaitForRoutes does both.
//
// It backs off 1s, 2s, 4s, then every 5s; treats a 429 as busy (see OnBusy);
// and keeps going through transient network errors and 5xx, since the
// optimization runs whether or not you stay connected.
func (c *ClientWithResponses) WaitForRoutes(
	ctx context.Context,
	id openapi_types.UUID,
	opts WaitOptions,
) (*Optimization, error) {
	opts = opts.withDefaults()
	deadline := time.Now().Add(opts.Timeout)
	delay := opts.InitialDelay
	busy := newBusyState()

	for time.Now().Before(deadline) {
		resp, err := c.GetOptimizationWithResponse(ctx, id)

		switch {
		case err != nil:
			// Transient: a network blip should not abandon a result that has
			// already been paid for.

		case resp.StatusCode() == http.StatusTooManyRequests:
			// Not counted against the backoff: the server named its interval.
			if berr := c.onBusyResponse(ctx, resp.Body, resp.HTTPResponse, busy, opts); berr != nil {
				return nil, berr
			}
			continue

		case resp.JSON200 != nil:
			switch resp.JSON200.Data.Status {
			case OptimizationStatusCompleted:
				return &resp.JSON200.Data, nil
			case OptimizationStatusFailed:
				return nil, &OptimizationFailedError{Optimization: resp.JSON200.Data}
			}

		case resp.StatusCode() >= 400 && resp.StatusCode() < 500:
			// 401/404 and friends are final — retrying cannot change the answer.
			return nil, fmt.Errorf(
				"could not fetch optimization %s (HTTP %d)", id, resp.StatusCode(),
			)
		}

		if werr := sleepCtx(ctx, delay); werr != nil {
			return nil, werr
		}
		if delay < opts.MaxDelay {
			delay *= 2
			if delay > opts.MaxDelay {
				delay = opts.MaxDelay
			}
		}
	}

	return nil, &OptimizationTimeoutError{OptimizationID: id, Timeout: opts.Timeout}
}

// CreateOptimizationAndWaitForRoutes submits an optimization and waits for
// the finished plan.
//
// Equivalent to SubmitOptimization followed by WaitForRoutes, for the common
// case where both happen in the same place. A WebhookUrl may still be set —
// you will simply be notified as well.
func (c *ClientWithResponses) CreateOptimizationAndWaitForRoutes(
	ctx context.Context,
	body CreateOptimizationJSONRequestBody,
	opts WaitOptions,
) (*Optimization, error) {
	receipt, err := c.SubmitOptimization(ctx, body, opts)
	if err != nil {
		return nil, err
	}
	return c.WaitForRoutes(ctx, receipt.Id, opts)
}

// ActiveWaitEvent is one look at what the account still has running.
type ActiveWaitEvent struct {
	// Active is how many optimizations are still pending or processing.
	Active                int
	ActiveOptimizationIDs []string
	Elapsed               time.Duration
	// Attempt is 1 for the first look, counting up.
	Attempt int
}

// ActiveWaitOptions tunes WaitForActiveOptimizations. The zero value is
// usable.
type ActiveWaitOptions struct {
	// OnWait is called after every look while something is still running,
	// for display.
	OnWait       func(ActiveWaitEvent)
	Timeout      time.Duration
	PollInterval time.Duration
}

// WaitForActiveOptimizations returns once the account has nothing pending or
// processing — the moment a SubmitOptimization is guaranteed a free slot.
// Useful before a batch, or when OnBusy told you to come back later.
func (c *ClientWithResponses) WaitForActiveOptimizations(
	ctx context.Context,
	opts ActiveWaitOptions,
) error {
	if opts.Timeout <= 0 {
		opts.Timeout = DefaultWaitTimeout
	}
	if opts.PollInterval <= 0 {
		opts.PollInterval = DefaultActivePoll
	}
	startedAt := time.Now()
	attempt := 0
	lastSeen := []string{}
	limit := 100

	for time.Since(startedAt) < opts.Timeout {
		attempt++
		ids := []string{}
		var busyFor time.Duration
		busy := false

		for _, status := range []ListOptimizationsParamsStatus{"pending", "processing"} {
			s := status
			resp, err := c.ListOptimizationsWithResponse(ctx, &ListOptimizationsParams{Status: &s, Limit: &limit})
			if err != nil {
				return err
			}
			if resp.StatusCode() == http.StatusTooManyRequests {
				busy, busyFor = true, retryAfter(resp.HTTPResponse)
				break
			}
			if resp.JSON200 != nil {
				for _, item := range resp.JSON200.Data.Items {
					ids = append(ids, item.Id.String())
				}
			}
		}

		wait := opts.PollInterval
		if busy {
			wait = busyFor
		} else {
			if len(ids) == 0 {
				return nil
			}
			lastSeen = ids
			if opts.OnWait != nil {
				opts.OnWait(ActiveWaitEvent{
					Active:                len(ids),
					ActiveOptimizationIDs: ids,
					Elapsed:               time.Since(startedAt),
					Attempt:               attempt,
				})
			}
		}
		if err := sleepCtx(ctx, wait); err != nil {
			return err
		}
	}

	return &ActiveOptimizationsTimeoutError{ActiveOptimizationIDs: lastSeen, Timeout: opts.Timeout}
}
