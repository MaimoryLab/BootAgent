package provider

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"time"
)

// Doer is the seam every outbound request goes through. Injectable for the same
// reason the subprocess runner is: a test must be able to exercise a rejected key
// or an unreachable endpoint without a network, and the regular CI run reaches no
// provider at all.
type Doer interface {
	Do(request *http.Request) (*http.Response, error)
}

// Client sends the probe and discovery requests.
type Client struct {
	HTTP Doer
	// Timeout bounds a single request. Applied per call rather than on the
	// transport so a caller can pass its own budget.
	Timeout time.Duration
}

// NewClient builds a Client over the default transport.
//
// Redirects are followed but credentials are not carried across a host change:
// the Authorization header would otherwise be replayed to whatever host a
// redirect names, which is a credential leak an endpoint could trigger.
func NewClient(timeout time.Duration) *Client {
	return &Client{
		HTTP: &http.Client{
			Timeout: timeout,
			CheckRedirect: func(request *http.Request, via []*http.Request) error {
				if len(via) >= 10 {
					return errors.New("stopped after 10 redirects")
				}
				if request.URL.Host != via[0].URL.Host {
					request.Header.Del("Authorization")
					request.Header.Del("X-Api-Key")
				}
				return nil
			},
		},
		Timeout: timeout,
	}
}

// send performs one request and reads its body.
//
// The body is read regardless of status because the failure classification needs
// it: an endpoint that does not serve a protocol answers 400 with a message
// saying so, and that is what tells "wrong protocol" from "wrong key".
func (c *Client) send(request *http.Request) (status int, body string, err error) {
	timeout := c.Timeout
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	ctx, cancel := context.WithTimeout(request.Context(), timeout)
	defer cancel()

	client := c.HTTP
	if client == nil {
		client = NewClient(timeout).HTTP
	}
	response, err := client.Do(request.WithContext(ctx))
	if err != nil {
		return 0, "", err
	}
	defer response.Body.Close()
	// Bounded: a provider that streams an unbounded error page must not be able
	// to exhaust memory, and nothing here needs more than the first few KB.
	raw, readErr := io.ReadAll(io.LimitReader(response.Body, 64*1024))
	if readErr != nil && len(raw) == 0 {
		return response.StatusCode, "", readErr
	}
	return response.StatusCode, string(raw), nil
}

// isTimeout reports whether a transport error was a deadline rather than a
// refusal, which decides between TIMEOUT and PROVIDER_UNREACHABLE.
func isTimeout(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return true
	}
	// Python decides partly on the text of the reason, and some transports only
	// report the deadline that way.
	return strings.Contains(strings.ToLower(err.Error()), "timed out") ||
		strings.Contains(strings.ToLower(err.Error()), "timeout")
}

// reason renders a transport failure the way Python renders URLError.reason,
// which is what reaches the user.
func reason(err error) string {
	var urlErr interface{ Unwrap() error }
	if errors.As(err, &urlErr) {
		if inner := urlErr.Unwrap(); inner != nil {
			return inner.Error()
		}
	}
	return err.Error()
}
