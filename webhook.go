// Hand-written, copied in by scripts/generate-sdks.sh. See wait.go for why
// this cannot live in the generated tree.
//
// Webhook verification is the one piece of integration code where a mistake is
// a security hole rather than a bug, so we ship it rather than describing it.
// The two usual ways to get it wrong by hand: comparing digests with == or
// bytes.Equal, which leaks the answer through timing, and verifying
// re-serialised JSON instead of the bytes we signed — key order and whitespace
// differ, so it never matches, and the usual "fix" is to stop verifying.

package r4m

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
)

// WebhookSignatureHeader carries the HMAC-SHA256 hex digest of the raw body.
const WebhookSignatureHeader = "X-Webhook-Signature"

// ErrWebhookSignature reports a delivery whose signature did not match. Treat
// as hostile: reject with a 401.
var ErrWebhookSignature = errors.New("webhook signature did not match")

// The event names carried in WebhookEvent.Type.
const (
	EventOptimizationCompleted = "optimization.completed"
	EventOptimizationFailed    = "optimization.failed"
)

// WebhookError says why an optimization failed. Code is NO_ROUTES (the engine
// produced no routes), PROCESSING_FAILED (the job threw) or TIMED_OUT (it did
// not finish within 15 minutes and its concurrency slot was released); the
// the charge is refunded in every case. More codes may be added, so handle the
// ones you know and fall through on the rest.
type WebhookError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// WebhookEvent is the body we POST to your WebhookUrl.
//
// Go has no sum types, so the two outcomes share one struct and Type says
// which you have: Result is non-nil on EventOptimizationCompleted, Error is
// non-nil on EventOptimizationFailed. Switch on Type rather than nil-checking
// the fields — an added event would otherwise look like a completed one with
// nothing in it.
type WebhookEvent struct {
	Type           string              `json:"type"`
	OptimizationID string              `json:"optimizationId"`
	Result         *OptimizationResult `json:"result,omitempty"`
	Error          *WebhookError       `json:"error,omitempty"`
}

// VerifyWebhook verifies a delivery and returns its parsed body.
//
// Pass the raw request body, exactly the bytes you received — not a struct you
// have already decoded and re-encoded. That is what was signed.
//
// It returns ErrWebhookSignature on a mismatch rather than a bool, so an
// unverified payload cannot reach your handler by accident: a bool is easy to
// ignore, an error the compiler nags about is not.
func VerifyWebhook(body []byte, signature, secret string) (*WebhookEvent, error) {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	expected := hex.EncodeToString(mac.Sum(nil))

	// hmac.Equal is constant-time and safe on unequal lengths.
	if !hmac.Equal([]byte(expected), []byte(signature)) {
		return nil, ErrWebhookSignature
	}

	var event WebhookEvent
	if err := json.Unmarshal(body, &event); err != nil {
		return nil, err
	}
	return &event, nil
}

// VerifyWebhookRequest verifies a delivery straight from an *http.Request: it
// reads the raw body and finds the signature header itself, so neither the
// header name nor the read-before-parse rule is yours to remember.
//
//	event, err := r4m.VerifyWebhookRequest(r, os.Getenv("R4M_WEBHOOK_SECRET"))
//	if errors.Is(err, r4m.ErrWebhookSignature) {
//	        http.Error(w, "invalid signature", http.StatusUnauthorized)
//	        return
//	}
//
// The body is consumed; read it yourself and call VerifyWebhook if you need
// the bytes afterwards.
func VerifyWebhookRequest(r *http.Request, secret string) (*WebhookEvent, error) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		return nil, err
	}
	return VerifyWebhook(body, r.Header.Get(WebhookSignatureHeader), secret)
}
