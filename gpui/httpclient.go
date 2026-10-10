package gpui

// This file is the port of the pinned HTTP client seam,
// crates/gpui/src/http_client.rs: the HttpClient Get(url,
// follow_redirects) operation, HttpResponse{status, body},
// NullHttpClient (the application default — zero network attempts, "No
// HttpClient available"), BlockedHttpClient (the permission error),
// the fixed-status fake clients, and the net/http adapter.
//
// The pinned trait returns a boxed future; the Go port calls Get
// synchronously on the image-load worker under the deterministic
// scheduler (blocking there is the worker's job, exactly where the pin
// awaits the future).
//
// The net/http adapter is explicit application configuration with its
// own redirect and timeout policy (distribution contract: "Any
// convenience net/http adapter is explicit caller configuration ... but
// must not silently replace the default with http.DefaultClient"). The
// default stays NullHttpClient; this adapter never builds on
// http.DefaultClient.

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"time"
)

// HttpResponse is a simple HTTP response (http_client.rs lines 8-13):
// the status code and the full body bytes.
type HttpResponse struct {
	// Status is the HTTP status code (the pinned http::StatusCode).
	Status int
	// Body is the response body bytes.
	Body []byte
}

// HttpClient is the trait for making HTTP requests (http_client.rs
// lines 16-25): one GET returning the full response.
type HttpClient interface {
	// Get performs a GET request and returns the full response
	// (HttpClient::get). followRedirects selects the redirect policy.
	Get(url string, followRedirects bool) (HttpResponse, error)
}

// NullHttpClient is the HTTP client that always returns an error
// (http_client.rs lines 27-38) — the application default (app.rs line
// 187 constructs it), so an unconfigured app makes zero network
// attempts.
type NullHttpClient struct{}

// Get implements HttpClient: "No HttpClient available" (the pinned
// anyhow::bail! message).
func (NullHttpClient) Get(url string, followRedirects bool) (HttpResponse, error) {
	return HttpResponse{}, errors.New("No HttpClient available")
}

// BlockedHttpClient is the HTTP client that blocks all requests
// (http_client.rs lines 40-63) with a permission error.
type BlockedHttpClient struct{}

// NewBlockedHttpClient creates a BlockedHttpClient (the pinned
// BlockedHttpClient::new).
func NewBlockedHttpClient() *BlockedHttpClient { return &BlockedHttpClient{} }

// Get implements HttpClient: the pinned std::io::Error with
// ErrorKind::PermissionDenied, whose Go mapping is an error that
// satisfies errors.Is(err, fs.ErrPermission).
func (BlockedHttpClient) Get(url string, followRedirects bool) (HttpResponse, error) {
	return HttpResponse{}, &blockedHTTPClientError{}
}

// blockedHTTPClientError is the pinned permission-denied error: the
// message text with an fs.ErrPermission unwrap chain.
type blockedHTTPClientError struct{}

// Error renders the pinned message.
func (e *blockedHTTPClientError) Error() string {
	return "BlockedHttpClient disallowed request"
}

// Unwrap maps the error kind to fs.ErrPermission.
func (e *blockedHTTPClientError) Unwrap() error { return fs.ErrPermission }

// FakeHTTPClient is the pinned test-support fake (http_client.rs lines
// 66-123): fixed-status responses with empty bodies.
type FakeHTTPClient struct {
	status int
}

// With200Response returns a fake client that returns 200 responses
// (FakeHttpClient::with_200_response).
func With200Response() HttpClient { return &FakeHTTPClient{status: http.StatusOK} }

// With404Response returns a fake client that returns 404 responses
// (FakeHttpClient::with_404_response).
func With404Response() HttpClient { return &FakeHTTPClient{status: http.StatusNotFound} }

// Get implements HttpClient with the fixed status and empty body.
func (c *FakeHTTPClient) Get(url string, followRedirects bool) (HttpResponse, error) {
	return HttpResponse{Status: c.status}, nil
}

// GoHTTPClient is the net/http adapter for the HttpClient seam
// (explicit application configuration, never the default and never
// http.DefaultClient). It carries its own redirect and timeout policy:
// the caller's timeout is applied per request, and followRedirects
// switches between following redirects (Go's default policy, up to 10
// hops) and returning the redirect response itself
// (http.ErrUseLastResponse) so a 3xx surfaces to the caller exactly
// like the pinned non-following behavior.
type GoHTTPClient struct {
	base    http.Client
	timeout time.Duration
}

// NewGoHTTPClient creates the adapter with the caller's timeout policy
// (non-positive disables the per-request timeout).
func NewGoHTTPClient(timeout time.Duration) *GoHTTPClient {
	return &GoHTTPClient{timeout: timeout}
}

// Get performs the GET (HttpClient::get). The transport is the shared
// process pool; the redirect policy is set on a per-call shallow copy
// of the client so concurrent calls with different redirect settings
// never race.
func (c *GoHTTPClient) Get(url string, followRedirects bool) (HttpResponse, error) {
	ctx := context.Background()
	if c.timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, c.timeout)
		defer cancel()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return HttpResponse{}, err
	}
	client := c.base
	if !followRedirects {
		client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		}
	}
	response, err := client.Do(req)
	if err != nil {
		return HttpResponse{}, err
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		return HttpResponse{}, fmt.Errorf("gpui: reading the response body: %w", err)
	}
	return HttpResponse{Status: response.StatusCode, Body: body}, nil
}
