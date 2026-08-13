package binding

import (
	"net"
	"net/http"
	"time"

	"github.com/MaimoryLab/BootAgent/internal/process"
)

// UpdateStallTimeout is how long the OTA download may receive nothing before it
// is abandoned. It matches install.DownloadStallTimeout: a stalled socket is
// unambiguous, and an HTTP body has no reason to go quiet for two minutes and
// then recover.
const UpdateStallTimeout = 120 * time.Second

// updateDialTimeout and updateResponseHeaderTimeout bound the phases that should
// be fast even on a slow link. Only the body transfer is left unbounded, and
// stall detection covers that.
const (
	updateDialTimeout           = 30 * time.Second
	updateResponseHeaderTimeout = 60 * time.Second
)

// NewUpdateHTTPClient returns the client the GitHub release provider should use.
//
// It deliberately sets no Client.Timeout. That field bounds the whole request
// including reading the body, so the provider's own default of 30 seconds was a
// wall-clock budget for the entire OTA transfer: the largest asset is around
// 7 MB, which made the update impossible below roughly 242 KB/s and identical on
// every retry, since each attempt restarts the same budget from zero. A user on
// a working-but-slow link was told the update had failed when nothing was wrong.
//
// What replaces it is the split internal/install already uses: phase timeouts
// for the parts that should be quick, and stall detection for the body, which
// ends a dead transfer without ever penalising a slow one.
func NewUpdateHTTPClient() *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.DialContext = (&net.Dialer{Timeout: updateDialTimeout}).DialContext
	transport.TLSHandshakeTimeout = updateDialTimeout
	transport.ResponseHeaderTimeout = updateResponseHeaderTimeout
	return &http.Client{Transport: &stallGuardTransport{base: transport, timeout: UpdateStallTimeout}}
}

// stallGuardTransport applies the stall bound to responses whose body is read by
// code we do not own.
//
// The provider streams the asset inside its own Download method, so there is no
// copy loop here to wrap -- the response body on the way out is the only hook.
// The request's context is what the wrapper watches, which is also what makes
// the task centre's cancel button work during the body transfer rather than only
// between phases.
type stallGuardTransport struct {
	base    http.RoundTripper
	timeout time.Duration
}

func (t *stallGuardTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	response, err := t.base.RoundTrip(request)
	if err != nil || response == nil || response.Body == nil {
		return response, err
	}
	response.Body = process.StallGuardedBody(request.Context(), response.Body, t.timeout)
	return response, nil
}
