package binding

import (
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
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

// resumingBody continues a dropped transfer with a ranged request instead of
// failing it.
//
// Removing the wall-clock bound made a slow link survivable, but not an unstable
// one: the provider's read loop turns any mid-body transport error into a failed
// update, so a single reset after 6 MB of a 7 MB asset discarded all of it and
// the retry started from zero. On the links where OTA actually hurts, a transfer
// long enough to finish is also long enough to be interrupted, which is why the
// stall guard alone was not enough -- it ends a dead transfer, and this one was
// not dead.
//
// The contract it has to honour is that the updater hashes the stream as it
// flows past (updater.download's io.MultiWriter), so the bytes handed out across
// a resume must be exactly the ones a single uninterrupted read would have
// produced. That is why a resume is accepted only on a 206 whose Content-Range
// starts precisely where this reader stopped: a server that ignores Range and
// replays the whole body from zero would otherwise duplicate everything read so
// far, and the digest would fail after the download rather than here.
type resumingBody struct {
	base    http.RoundTripper
	request *http.Request
	url     string
	body    io.ReadCloser

	// offset counts the bytes handed to the caller, which is where a resume has to
	// pick up. advanced records whether any arrived since the last resume, so a
	// repair has to be earned by progress rather than retried in a loop.
	offset   int64
	advanced bool
	closed   bool
}

func (b *resumingBody) Read(buffer []byte) (int, error) {
	for {
		n, err := b.body.Read(buffer)
		if n > 0 {
			b.offset += int64(n)
			b.advanced = true
		}
		if err == nil || errors.Is(err, io.EOF) {
			return n, err
		}
		// Bytes in hand are worth more than the error: hand them over and let the
		// next Read decide whether to resume, so nothing already transferred is
		// dropped on the way out.
		if n > 0 {
			return n, nil
		}
		if !b.resumable(err) {
			return 0, err
		}
		if resumeErr := b.resume(); resumeErr != nil {
			// The original error is what happened to the transfer; the resume
			// failure only says the repair did not work.
			return 0, fmt.Errorf("%w (resume from %d failed: %v)", err, b.offset, resumeErr)
		}
	}
}

// resumable reports whether err is worth a ranged retry.
//
// A stall or a cancellation is never resumed. Both arrive here as a read error
// because the guard above closes the body to unblock a parked read, and resuming
// would undo exactly the interruption that was asked for.
func (b *resumingBody) resumable(err error) bool {
	if b.closed || errors.Is(err, process.ErrStalled) {
		return false
	}
	if b.request.Context().Err() != nil {
		return false
	}
	// One resume per stretch of progress. Without this an endpoint that accepts
	// Range and then immediately drops would spin here; with it, every attempt
	// after the first has to be paid for by bytes actually delivered, which is
	// also the condition the stall guard is measuring.
	if !b.advanced {
		return false
	}
	return b.offset > 0
}

func (b *resumingBody) resume() error {
	target, err := url.Parse(b.url)
	if err != nil {
		return err
	}
	// Cloned rather than rebuilt so the headers the provider set are kept -- most
	// importantly the Accept it uses to ask for the raw asset, and the
	// Authorization it has already stripped if the redirect crossed hosts.
	request := b.request.Clone(b.request.Context())
	request.URL = target
	// Cleared because a clone carries the first hop's Host, which no longer
	// matches the presigned URL a resume goes to.
	request.Host = ""
	request.Header.Set("Range", fmt.Sprintf("bytes=%d-", b.offset))
	response, err := b.base.RoundTrip(request)
	if err != nil {
		return err
	}
	if response.StatusCode != http.StatusPartialContent {
		_ = response.Body.Close()
		return fmt.Errorf("resume expected HTTP 206, got %d", response.StatusCode)
	}
	start, err := contentRangeStart(response.Header.Get("Content-Range"))
	if err != nil {
		_ = response.Body.Close()
		return err
	}
	if start != b.offset {
		_ = response.Body.Close()
		return fmt.Errorf("resume returned range from %d, want %d", start, b.offset)
	}
	_ = b.body.Close()
	b.body = response.Body
	b.advanced = false
	return nil
}

func (b *resumingBody) Close() error {
	b.closed = true
	return b.body.Close()
}

// contentRangeStart reads the first-byte position out of a Content-Range header,
// which is the only part that has to be verified: it is what says the server
// honoured the offset rather than restarting the body.
func contentRangeStart(header string) (int64, error) {
	value := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(header), "bytes"))
	value = strings.TrimSpace(value)
	slash := strings.Index(value, "/")
	if slash >= 0 {
		value = value[:slash]
	}
	dash := strings.Index(value, "-")
	if dash <= 0 {
		return 0, fmt.Errorf("malformed Content-Range %q", header)
	}
	start, err := strconv.ParseInt(strings.TrimSpace(value[:dash]), 10, 64)
	if err != nil {
		return 0, fmt.Errorf("malformed Content-Range %q", header)
	}
	return start, nil
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
	// Resume sits under the stall guard, not over it: the guard has to observe the
	// reads that resumption produces, or a transfer it repaired would still look
	// idle and be abandoned. Only a 200 to a GET can be resumed -- a 206 is
	// already someone else's ranged request, and this is the OTA client's only
	// caller shape.
	if request.Method == http.MethodGet && response.StatusCode == http.StatusOK {
		response.Body = &resumingBody{
			base:    t.base,
			request: request,
			url:     responseURL(response, request),
			body:    response.Body,
		}
	}
	response.Body = process.StallGuardedBody(request.Context(), response.Body, t.timeout)
	return response, nil
}

// responseURL names the location to resume from. After a redirect chain that is
// the presigned URL the last hop landed on, not the original asset URL, which
// would start the redirect dance over on every resume.
func responseURL(response *http.Response, request *http.Request) string {
	if response.Request != nil && response.Request.URL != nil {
		return response.Request.URL.String()
	}
	return request.URL.String()
}
