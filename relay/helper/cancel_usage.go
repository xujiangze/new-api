package helper

import (
	"context"
	"errors"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/MoeForge/moego/ojson"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/gin-gonic/gin"
)

// Do not let cancelled clients retain an unbounded number of upstream streams.
var cancelledUsageSlots = make(chan struct{}, 32)

func IsTextStreamCancelCandidate(info *relaycommon.RelayInfo, path string) bool {
	if info == nil || !info.IsStream || info.ReplyCheckActive {
		return false
	}
	switch info.RelayFormat {
	case types.RelayFormatOpenAI:
		return info.RelayMode == relayconstant.RelayModeChatCompletions && path == "/v1/chat/completions"
	case types.RelayFormatOpenAIResponses:
		return info.RelayMode == relayconstant.RelayModeResponses && path == "/v1/responses"
	case types.RelayFormatClaude:
		return path == "/v1/messages"
	case types.RelayFormatGemini:
		return strings.HasSuffix(path, ":streamGenerateContent")
	}
	return false
}

// WatchStreamDisconnect also covers the HTTP keepalive handoff, which can be
// waiting for a write when the client disconnects. Stop joins the callback
// rather than letting it access a writer after the handler returns.
func WatchStreamDisconnect(c *gin.Context) func() {
	writer := findStreamResponseWriter(c.Writer)
	if writer == nil {
		return func() {}
	}
	ctx := c.Request.Context()
	writer.deadlineMu.Lock()
	writer.client = ctx
	writer.deadlineMu.Unlock()
	done := make(chan struct{})
	stop := context.AfterFunc(ctx, func() {
		defer close(done)
		if ctx.Err() == context.Canceled {
			writer.interruptWrites()
		}
	})
	return func() {
		if !stop() {
			<-done
		}
		writer.deadlineMu.Lock()
		writer.client = nil
		writer.deadlineMu.Unlock()
	}
}

// StartCancelledUsage opts in only from native parsers of a successful SSE.
// Keep it active through any final downstream frames sent after the scanner.
func StartCancelledUsage(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response) func() {
	if !info.CancelUsageEligible || !IsSuccessfulEventStream(resp) {
		return func() {}
	}
	SetEventStreamHeaders(c)
	writer := findStreamResponseWriter(c.Writer)
	if writer == nil {
		return func() {}
	}
	stop := WatchStreamDisconnect(c)
	return func() {
		stop()
		if info.StreamStatus != nil {
			info.StreamStatus.ClientCancelled = c.Request.Context().Err() == context.Canceled && !writer.terminalDelivered && !writer.localWriteTimeout.Load()
		}
	}
}

func IsSuccessfulEventStream(resp *http.Response) bool {
	return resp != nil && resp.StatusCode == http.StatusOK && strings.HasPrefix(strings.ToLower(resp.Header.Get("Content-Type")), "text/event-stream")
}

func TracksCancelledUsage(c *gin.Context) bool {
	writer := findStreamResponseWriter(c.Writer)
	return writer != nil && writer.watchedClient() != nil
}

// StreamClientGone is only true on the opt-in path, never for a Reply check.
func StreamClientGone(c *gin.Context) bool {
	writer := findStreamResponseWriter(c.Writer)
	if writer == nil {
		return false
	}
	ctx := writer.watchedClient()
	return ctx != nil && ctx.Err() == context.Canceled
}

func (w *streamResponseWriter) noteWriteError(err error) {
	if err == nil {
		return
	}
	w.deadlineMu.Lock()
	defer w.deadlineMu.Unlock()
	var timeout net.Error
	if w.client != nil && !w.writeDeadline.IsZero() && !time.Now().Before(w.writeDeadline) &&
		(w.cancelledAt.IsZero() || !w.cancelledAt.Before(w.writeDeadline)) && errors.As(err, &timeout) && timeout.Timeout() {
		// net/http can cancel the connection context because our own write
		// deadline fired, not because the client actively closed the stream.
		if w.localWriteTimeout.CompareAndSwap(false, true) {
			close(w.writeTimedOut)
		}
	}
}

func (w *streamResponseWriter) watchedClient() context.Context {
	w.deadlineMu.Lock()
	defer w.deadlineMu.Unlock()
	return w.client
}

// MarkStreamTerminalDelivered follows a successful, error-aware flush. A
// parser observing a terminal event (or skipping a write) isn't a delivery.
func MarkStreamTerminalDelivered(c *gin.Context) {
	writer := findStreamResponseWriter(c.Writer)
	if writer != nil && writer.watchedClient() != nil && writer.confirmedFlush {
		writer.terminalDelivered = true
	}
}

func findStreamResponseWriter(w http.ResponseWriter) *streamResponseWriter {
	for w != nil {
		if stream, ok := w.(*streamResponseWriter); ok {
			return stream
		}
		wrapper, ok := w.(interface{ Unwrap() http.ResponseWriter })
		if !ok {
			break
		}
		w = wrapper.Unwrap()
	}
	return nil
}

func (w *streamResponseWriter) interruptWrites() (time.Time, bool) {
	w.deadlineMu.Lock()
	defer w.deadlineMu.Unlock()
	if w.cancelledAt.IsZero() {
		w.cancelledAt = time.Now()
		// No write lock: a write can be waiting for the peer. HTTP/2 requires
		// a deadline in the past to reset just the affected stream immediately.
		_ = http.NewResponseController(w.ResponseWriter).SetWriteDeadline(w.cancelledAt.Add(-time.Second))
	}
	return w.cancelledAt, w.localWriteTimeout.Load()
}

// ReportedTokenCount tests presence on the wire, since most normalized usage
// DTOs cannot distinguish a missing counter from a real, explicit zero.
func ReportedTokenCount(value ojson.Result) bool {
	if value.Type != ojson.Number {
		return false
	}
	n, err := strconv.ParseInt(value.Raw, 10, 32)
	return err == nil && n >= 0
}
