// Hand-written, copied in by scripts/generate-sdks.sh (see wait.go for why).
//
// This is the SDK's public face; the generated client is its internals. A
// generated call answers with a struct holding one pointer per HTTP status
// (JSON200, JSON201, JSON401...), because Go has no union type to hold "this
// body or that one". That is the generator's workaround, not something a caller
// should have to learn, so every method here answers the way Go code does: the
// value, or an error that says what went wrong.

package r4m

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	openapi_types "github.com/oapi-codegen/runtime/types"
)

// ApiError means the API refused the request. Reach it with errors.As:
//
//	var apiErr *r4m.ApiError
//	if errors.As(err, &apiErr) && apiErr.Code == "INSUFFICIENT_FUNDS" { ... }
//
// A request that never got an answer returns the underlying network error.
type ApiError struct {
	// Status is the HTTP status.
	Status int
	// Code is machine-readable: an API error code such as INSUFFICIENT_FUNDS,
	// or UNEXPECTED_RESPONSE when the body was not the API's error format.
	Code string
	// Message is the server's own explanation.
	Message string
	// Details is optional machine-readable context. Message stands alone
	// without it.
	Details map[string]any
	// RetryAfter is how long the server asked us to wait, on a 429.
	RetryAfter time.Duration
	// Body is the response body as sent, for fields outside the error envelope.
	Body []byte
}

func (e *ApiError) Error() string {
	return fmt.Sprintf("r4m: %s (HTTP %d): %s", e.Code, e.Status, e.Message)
}

const errorBodyPreviewBytes = 160

// apiError reads a response that did not carry the expected payload.
func apiError(resp *http.Response, body []byte) error {
	e := &ApiError{Body: body, Code: "UNEXPECTED_RESPONSE"}

	var parsed struct {
		Error struct {
			Code    string         `json:"code"`
			Message string         `json:"message"`
			Details map[string]any `json:"details"`
		} `json:"error"`
	}
	if json.Unmarshal(body, &parsed) == nil && parsed.Error.Code != "" {
		e.Code = parsed.Error.Code
		e.Message = parsed.Error.Message
		e.Details = parsed.Error.Details
	}

	if resp != nil {
		e.Status = resp.StatusCode
		if resp.StatusCode == http.StatusTooManyRequests {
			e.RetryAfter = retryAfter(resp)
		}
	}
	if e.Code == "UNEXPECTED_RESPONSE" {
		preview := body
		if len(preview) > errorBodyPreviewBytes {
			preview = preview[:errorBodyPreviewBytes]
		}
		contentType := ""
		if resp != nil {
			contentType = resp.Header.Get("Content-Type")
		}
		e.Message = fmt.Sprintf("expected JSON from the API but received %q: %s", contentType, preview)
	}
	return e
}

// Ptr returns a pointer to v, because every optional field is a pointer:
//
//	r4m.Address{Address: "Depot", IsDepot: r4m.Ptr(true)}
func Ptr[T any](v T) *T { return &v }

// CreateOptimization submits an optimization and returns the receipt (status
// pending). A 429 comes back as an *ApiError; SubmitOptimization waits it out.
func (c *API) CreateOptimization(ctx context.Context, body CreateOptimizationRequest) (*Optimization, error) {
	resp, err := c.raw.CreateOptimizationWithResponse(ctx, body)
	if err != nil {
		return nil, err
	}
	if resp.JSON202 == nil {
		return nil, apiError(resp.HTTPResponse, resp.Body)
	}
	return &resp.JSON202.Data, nil
}

// GetOptimization fetches one optimization.
func (c *API) GetOptimization(ctx context.Context, id openapi_types.UUID) (*Optimization, error) {
	resp, err := c.raw.GetOptimizationWithResponse(ctx, id)
	if err != nil {
		return nil, err
	}
	if resp.JSON200 == nil {
		return nil, apiError(resp.HTTPResponse, resp.Body)
	}
	return &resp.JSON200.Data, nil
}

// OptimizationPage is one page of ListOptimizations.
type OptimizationPage struct {
	Items []Optimization
	// NextCursor is the cursor for the next page; nil on the last one.
	NextCursor *openapi_types.UUID
}

// ListOptimizations lists optimizations, newest first. params may be nil.
func (c *API) ListOptimizations(ctx context.Context, params *ListOptimizationsParams) (*OptimizationPage, error) {
	resp, err := c.raw.ListOptimizationsWithResponse(ctx, params)
	if err != nil {
		return nil, err
	}
	if resp.JSON200 == nil {
		return nil, apiError(resp.HTTPResponse, resp.Body)
	}
	page := OptimizationPage(resp.JSON200.Data)
	return &page, nil
}

// CancelOptimization stops waiting for an optimization still pending or
// processing: it turns cancelled and frees its concurrency slot at once. The
// charge is not refunded and no result is ever recorded for it.
func (c *API) CancelOptimization(ctx context.Context, id openapi_types.UUID) error {
	resp, err := c.raw.CancelOptimizationWithResponse(ctx, id)
	if err != nil {
		return err
	}
	if resp.JSON200 == nil {
		return apiError(resp.HTTPResponse, resp.Body)
	}
	return nil
}

// DeleteOptimization permanently deletes an optimization and its result.
func (c *API) DeleteOptimization(ctx context.Context, id openapi_types.UUID) error {
	resp, err := c.raw.DeleteOptimizationWithResponse(ctx, id)
	if err != nil {
		return err
	}
	if resp.JSON200 == nil {
		return apiError(resp.HTTPResponse, resp.Body)
	}
	return nil
}

// ForwardGeocoding is the answer to ForwardGeocode.
type ForwardGeocoding struct {
	PriceMicros int
	Result      GeocodeResult
}

// ForwardGeocode converts one address to coordinates.
func (c *API) ForwardGeocode(ctx context.Context, body ForwardGeocodeRequest) (*ForwardGeocoding, error) {
	resp, err := c.raw.ForwardGeocodeWithResponse(ctx, body)
	if err != nil {
		return nil, err
	}
	if resp.JSON200 == nil {
		return nil, apiError(resp.HTTPResponse, resp.Body)
	}
	geocoding := ForwardGeocoding(resp.JSON200.Data)
	return &geocoding, nil
}

// BulkGeocoding is the answer to BulkGeocode.
type BulkGeocoding struct {
	PriceMicros int
	Results     []GeocodeResult
}

// BulkGeocode converts many addresses to coordinates in one request: one result
// per distinct address, to be matched by Original, never by position.
func (c *API) BulkGeocode(ctx context.Context, body BulkGeocodeRequest) (*BulkGeocoding, error) {
	resp, err := c.raw.BulkGeocodeWithResponse(ctx, body)
	if err != nil {
		return nil, err
	}
	if resp.JSON200 == nil {
		return nil, apiError(resp.HTTPResponse, resp.Body)
	}
	geocoding := BulkGeocoding(resp.JSON200.Data)
	return &geocoding, nil
}

// CreateApiKey generates an API key. The raw key is in this answer only.
func (c *API) CreateApiKey(ctx context.Context, body CreateApiKeyRequest) (*ApiKeyCreated, error) {
	resp, err := c.raw.CreateApiKeyWithResponse(ctx, body)
	if err != nil {
		return nil, err
	}
	if resp.JSON201 == nil {
		return nil, apiError(resp.HTTPResponse, resp.Body)
	}
	return &resp.JSON201.Data, nil
}

// ListApiKeys lists your API keys; raw keys are never returned.
func (c *API) ListApiKeys(ctx context.Context) ([]ApiKey, error) {
	resp, err := c.raw.ListApiKeysWithResponse(ctx)
	if err != nil {
		return nil, err
	}
	if resp.JSON200 == nil {
		return nil, apiError(resp.HTTPResponse, resp.Body)
	}
	return resp.JSON200.Data, nil
}

// RevokeApiKey revokes an API key; it stops working immediately.
func (c *API) RevokeApiKey(ctx context.Context, id openapi_types.UUID) error {
	resp, err := c.raw.RevokeApiKeyWithResponse(ctx, id)
	if err != nil {
		return err
	}
	if resp.JSON200 == nil {
		return apiError(resp.HTTPResponse, resp.Body)
	}
	return nil
}

// GetUsage returns the prepaid balance and recent activity.
func (c *API) GetUsage(ctx context.Context) (*Usage, error) {
	resp, err := c.raw.GetUsageWithResponse(ctx)
	if err != nil {
		return nil, err
	}
	if resp.JSON200 == nil {
		return nil, apiError(resp.HTTPResponse, resp.Body)
	}
	return &resp.JSON200.Data, nil
}

// GetAccount returns your account and plan.
func (c *API) GetAccount(ctx context.Context) (*Account, error) {
	resp, err := c.raw.GetAccountWithResponse(ctx)
	if err != nil {
		return nil, err
	}
	if resp.JSON200 == nil {
		return nil, apiError(resp.HTTPResponse, resp.Body)
	}
	return &resp.JSON200.Data, nil
}

// WebhookSecrets holds the secrets webhook deliveries are signed with.
type WebhookSecrets struct {
	Previous WebhookSecret
	Primary  WebhookSecret
}

// GetWebhookSecret returns the account's webhook signing secrets.
func (c *API) GetWebhookSecret(ctx context.Context) (*WebhookSecrets, error) {
	resp, err := c.raw.GetWebhookSecretWithResponse(ctx)
	if err != nil {
		return nil, err
	}
	if resp.JSON200 == nil {
		return nil, apiError(resp.HTTPResponse, resp.Body)
	}
	secrets := WebhookSecrets(resp.JSON200.Data)
	return &secrets, nil
}

// TopupCredits requests a machine-payment top-up. Without a payment receipt the
// answer is a 402: an *ApiError whose Body holds the payment challenge.
func (c *API) TopupCredits(ctx context.Context, body TopupCreditsJSONRequestBody) (*TopupReceipt, error) {
	resp, err := c.raw.TopupCreditsWithResponse(ctx, body)
	if err != nil {
		return nil, err
	}
	if resp.JSON200 == nil {
		return nil, apiError(resp.HTTPResponse, resp.Body)
	}
	return &resp.JSON200.Data, nil
}
