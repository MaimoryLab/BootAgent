package binding

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/MaimoryLab/BootAgent/internal/process"
)

// The defect this guards: as an http.Client.Timeout, 30 seconds bounded the body
// transfer too, so the OTA download died on any link slower than about
// 242 KB/s -- and identically on every retry, since each attempt restarted the
// same budget. A client that sets Timeout at all reintroduces that.
func TestUpdateClientSetsNoWallClockTimeout(t *testing.T) {
	if got := NewUpdateHTTPClient().Timeout; got != 0 {
		t.Fatalf("update client Timeout = %v, want 0 so a slow body is not cut off", got)
	}
}

// The phases that should be quick even on a slow link stay bounded. Without
// these, removing Client.Timeout would leave a dead host hanging on connect.
func TestUpdateClientBoundsTheFastPhases(t *testing.T) {
	transport, ok := NewUpdateHTTPClient().Transport.(*stallGuardTransport)
	if !ok {
		t.Fatalf("update client transport = %T, want *stallGuardTransport", NewUpdateHTTPClient().Transport)
	}
	base, ok := transport.base.(*http.Transport)
	if !ok {
		t.Fatalf("guard base = %T, want *http.Transport", transport.base)
	}
	if base.TLSHandshakeTimeout != updateDialTimeout {
		t.Errorf("TLSHandshakeTimeout = %v, want %v", base.TLSHandshakeTimeout, updateDialTimeout)
	}
	if base.ResponseHeaderTimeout != updateResponseHeaderTimeout {
		t.Errorf("ResponseHeaderTimeout = %v, want %v", base.ResponseHeaderTimeout, updateResponseHeaderTimeout)
	}
	if base.DialContext == nil {
		t.Error("DialContext is nil, so the dial is unbounded")
	}
	if transport.timeout != UpdateStallTimeout {
		t.Errorf("stall timeout = %v, want %v", transport.timeout, UpdateStallTimeout)
	}
}

// The point of the change: a transfer that keeps arriving, however slowly, runs
// to completion. This one dribbles bytes for longer than its own stall window
// and well past the interval between writes, which is what the old wall-clock
// bound would have killed.
func TestUpdateClientCompletesASlowButMovingTransfer(t *testing.T) {
	const chunks = 12
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		flusher, _ := writer.(http.Flusher)
		for range chunks {
			_, _ = writer.Write([]byte("x"))
			if flusher != nil {
				flusher.Flush()
			}
			time.Sleep(20 * time.Millisecond)
		}
	}))
	defer server.Close()

	// A stall window far shorter than the whole transfer: 12 chunks 20ms apart
	// runs ~240ms in total, so a wall-clock bound of 60ms would fail this while
	// stall detection lets it through -- no single gap ever reaches 60ms.
	client := &http.Client{Transport: &stallGuardTransport{base: http.DefaultTransport, timeout: 60 * time.Millisecond}}
	response, err := client.Get(server.URL)
	if err != nil {
		t.Fatalf("slow transfer failed to start: %v", err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("slow-but-moving transfer was abandoned: %v", err)
	}
	if len(body) != chunks {
		t.Fatalf("read %d bytes, want %d", len(body), chunks)
	}
}

// The complement: removing the wall-clock bound must not make a dead socket hang
// forever. A body that sends headers and then never another byte still ends.
func TestUpdateClientAbandonsAStalledTransfer(t *testing.T) {
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Length", "4096")
		writer.WriteHeader(http.StatusOK)
		if flusher, ok := writer.(http.Flusher); ok {
			flusher.Flush()
		}
		<-release
	}))
	defer func() {
		close(release)
		server.Close()
	}()

	client := &http.Client{Transport: &stallGuardTransport{base: http.DefaultTransport, timeout: 150 * time.Millisecond}}
	response, err := client.Get(server.URL)
	if err != nil {
		t.Fatalf("request failed before the body: %v", err)
	}
	defer response.Body.Close()
	started := time.Now()
	if _, err := io.ReadAll(response.Body); !errors.Is(err, process.ErrStalled) {
		t.Fatalf("stalled body error = %v, want ErrStalled", err)
	}
	if elapsed := time.Since(started); elapsed > 10*time.Second {
		t.Fatalf("stall detection took %v", elapsed)
	}
}

// Cancelling has to reach a transfer that is already streaming, not just one
// between phases -- the task centre's cancel button is the user's only way out
// of a download that is moving too slowly to be worth waiting for.
func TestUpdateClientCancelReachesTheBodyTransfer(t *testing.T) {
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Length", "4096")
		writer.WriteHeader(http.StatusOK)
		if flusher, ok := writer.(http.Flusher); ok {
			flusher.Flush()
		}
		<-release
	}))
	defer func() {
		close(release)
		server.Close()
	}()

	ctx, cancel := context.WithCancel(context.Background())
	// Long enough that the stall watchdog cannot be what ends this.
	client := &http.Client{Transport: &stallGuardTransport{base: http.DefaultTransport, timeout: time.Hour}}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	response, err := client.Do(request)
	if err != nil {
		t.Fatalf("request failed before the body: %v", err)
	}
	defer response.Body.Close()
	go func() {
		time.Sleep(80 * time.Millisecond)
		cancel()
	}()
	if _, err := io.ReadAll(response.Body); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled body error = %v, want context.Canceled", err)
	}
}

// The defect this guards: the provider's read loop turns any mid-body transport
// error into a failed update, so one reset near the end of a 7 MB asset discarded
// everything and the retry began at zero. The bytes handed out across the resume
// have to equal the whole payload exactly -- the updater hashes the stream as it
// flows, so a duplicated or missing byte fails verification after the download.
func TestUpdateClientResumesAfterTheConnectionDrops(t *testing.T) {
	payload := []byte(strings.Repeat("bootagent", 400))
	var ranges []string
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		header := request.Header.Get("Range")
		ranges = append(ranges, header)
		if header == "" {
			// Announce the whole length, then cut the connection partway to
			// reproduce a drop rather than a clean short body.
			writer.Header().Set("Content-Length", strconv.Itoa(len(payload)))
			writer.WriteHeader(http.StatusOK)
			_, _ = writer.Write(payload[:1000])
			if flusher, ok := writer.(http.Flusher); ok {
				flusher.Flush()
			}
			panic(http.ErrAbortHandler)
		}
		start, err := strconv.Atoi(strings.TrimSuffix(strings.TrimPrefix(header, "bytes="), "-"))
		if err != nil || start > len(payload) {
			writer.WriteHeader(http.StatusRequestedRangeNotSatisfiable)
			return
		}
		writer.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, len(payload)-1, len(payload)))
		writer.WriteHeader(http.StatusPartialContent)
		_, _ = writer.Write(payload[start:])
	}))
	defer server.Close()

	client := &http.Client{Transport: &stallGuardTransport{base: http.DefaultTransport, timeout: time.Minute}}
	response, err := client.Get(server.URL)
	if err != nil {
		t.Fatalf("first request failed: %v", err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("interrupted transfer was not resumed: %v", err)
	}
	if !bytes.Equal(body, payload) {
		t.Fatalf("resumed body is %d bytes, want %d identical bytes", len(body), len(payload))
	}
	if len(ranges) != 2 || ranges[0] != "" || ranges[1] != "bytes=1000-" {
		t.Fatalf("requests = %q, want an unranged attempt then a resume from 1000", ranges)
	}
}

// A server that ignores Range and replays the body from zero must fail rather
// than resume: appending a second copy of what was already read would corrupt
// the artifact, and the digest would only catch it after the whole download.
func TestUpdateClientRefusesAResumeThatRestartsTheBody(t *testing.T) {
	payload := []byte(strings.Repeat("x", 2000))
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Range") == "" {
			writer.Header().Set("Content-Length", strconv.Itoa(len(payload)))
			writer.WriteHeader(http.StatusOK)
			_, _ = writer.Write(payload[:500])
			if flusher, ok := writer.(http.Flusher); ok {
				flusher.Flush()
			}
			panic(http.ErrAbortHandler)
		}
		// 200 to a ranged request: the whole body again, from the start.
		writer.WriteHeader(http.StatusOK)
		_, _ = writer.Write(payload)
	}))
	defer server.Close()

	client := &http.Client{Transport: &stallGuardTransport{base: http.DefaultTransport, timeout: time.Minute}}
	response, err := client.Get(server.URL)
	if err != nil {
		t.Fatalf("first request failed: %v", err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err == nil {
		t.Fatalf("a restarted body was accepted, yielding %d bytes", len(body))
	}
	if !strings.Contains(err.Error(), "206") {
		t.Fatalf("error = %v, want it to name the missing 206", err)
	}
}

// Resumption must not defeat the stall guard. A transfer that delivers a little,
// resumes, and then goes quiet still has to end -- otherwise the repair would
// turn a dead transfer into a permanent one.
func TestUpdateClientStillAbandonsAStalledResume(t *testing.T) {
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Range") == "" {
			writer.Header().Set("Content-Length", "4096")
			writer.WriteHeader(http.StatusOK)
			_, _ = writer.Write([]byte("start"))
			if flusher, ok := writer.(http.Flusher); ok {
				flusher.Flush()
			}
			panic(http.ErrAbortHandler)
		}
		writer.Header().Set("Content-Range", "bytes 5-4095/4096")
		writer.WriteHeader(http.StatusPartialContent)
		if flusher, ok := writer.(http.Flusher); ok {
			flusher.Flush()
		}
		<-release
	}))
	defer func() {
		close(release)
		server.Close()
	}()

	client := &http.Client{Transport: &stallGuardTransport{base: http.DefaultTransport, timeout: 150 * time.Millisecond}}
	response, err := client.Get(server.URL)
	if err != nil {
		t.Fatalf("first request failed: %v", err)
	}
	defer response.Body.Close()
	if _, err := io.ReadAll(response.Body); !errors.Is(err, process.ErrStalled) {
		t.Fatalf("stalled resume error = %v, want ErrStalled", err)
	}
}

// A disabled stall bound must hand the body back untouched, so a caller that
// already has its own bound is not double-guarded.
func TestStallGuardedBodyPassesThroughWhenDisabled(t *testing.T) {
	body := io.NopCloser(io.LimitReader(nil, 0))
	if got := process.StallGuardedBody(context.Background(), body, 0); got != body {
		t.Error("zero stall timeout should return the body unchanged")
	}
	if got := process.StallGuardedBody(context.Background(), nil, time.Second); got != nil {
		t.Error("a nil body should stay nil")
	}
}
