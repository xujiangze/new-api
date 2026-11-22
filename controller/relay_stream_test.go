package controller

import (
	"bufio"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay/channel"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRelayKeepAliveDuringImagePreparation(t *testing.T) {
	settings := operation_setting.GetGeneralSetting()
	previousEnabled, previousInterval := settings.PingIntervalEnabled, settings.PingIntervalSeconds
	previousCount, previousMedia := constant.CountToken, constant.GetMediaToken
	fetchSettings := system_setting.GetFetchSetting()
	previousFetchSettings := *fetchSettings
	fetchSettings.AllowPrivateIp = true
	fetchSettings.AllowedPorts = []string{"1-65535"}
	settings.PingIntervalEnabled, settings.PingIntervalSeconds = true, 1
	constant.CountToken, constant.GetMediaToken = true, true
	t.Cleanup(func() {
		settings.PingIntervalEnabled, settings.PingIntervalSeconds = previousEnabled, previousInterval
		constant.CountToken, constant.GetMediaToken = previousCount, previousMedia
		*fetchSettings = previousFetchSettings
	})
	service.InitHttpClient()

	imageRequested := make(chan struct{})
	releaseImage := make(chan struct{})
	var releaseOnce sync.Once
	imageServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(imageRequested)
		<-releaseImage
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer imageServer.Close()
	engine := gin.New()
	engine.POST("/v1/messages", func(c *gin.Context) {
		common.SetContextKey(c, constant.ContextKeyOriginalModel, "claude-sonnet-4-20250514")
		Relay(c, types.RelayFormatClaude)
	})
	gateway := httptest.NewServer(engine)
	defer gateway.Close()
	defer releaseOnce.Do(func() { close(releaseImage) })

	body := `{"model":"claude-sonnet-4-20250514","max_tokens":16,"stream":true,"messages":[{"role":"user","content":[{"type":"image","source":{"type":"url","url":"` + imageServer.URL + `"}}]}]}`
	client := &http.Client{Timeout: 4 * time.Second}
	response, err := client.Post(gateway.URL+"/v1/messages", "application/json", strings.NewReader(body))
	require.NoError(t, err, "the gateway must send keepalive while its image download is blocked")
	defer response.Body.Close()
	require.Equal(t, http.StatusOK, response.StatusCode)
	require.Equal(t, "text/event-stream", response.Header.Get("Content-Type"))
	reader := bufio.NewReader(response.Body)
	for range 2 {
		line, readErr := reader.ReadString('\n')
		require.NoError(t, readErr)
		require.Equal(t, ": PING\n", line)
		line, readErr = reader.ReadString('\n')
		require.NoError(t, readErr)
		require.Equal(t, "\n", line)
	}
	select {
	case <-imageRequested:
	default:
		t.Fatal("the request never reached image preparation")
	}
	releaseOnce.Do(func() { close(releaseImage) })
	remaining, err := io.ReadAll(reader)
	require.NoError(t, err)
	require.Contains(t, string(remaining), "event: error\n")
	require.Contains(t, string(remaining), `"type":"error"`)
	require.Contains(t, string(remaining), "data: ")
}

func TestRelayKeepAliveAcrossUpstreamRetries(t *testing.T) {
	settings := operation_setting.GetGeneralSetting()
	previousEnabled, previousInterval := settings.PingIntervalEnabled, settings.PingIntervalSeconds
	settings.PingIntervalEnabled, settings.PingIntervalSeconds = true, 1
	t.Cleanup(func() {
		settings.PingIntervalEnabled, settings.PingIntervalSeconds = previousEnabled, previousInterval
	})
	service.InitHttpClient()
	var attempts atomic.Int32
	retryStarted, release := make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if attempts.Add(1) == 1 {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		close(retryStarted)
		select {
		case <-release:
			_, _ = io.WriteString(w, "data: ok\n\n")
		case <-r.Context().Done():
		}
	}))
	defer upstream.Close()
	engine := gin.New()
	engine.POST("/v1/messages", func(c *gin.Context) {
		stop := helper.StartStreamKeepAlive(c, 10*time.Millisecond)
		defer stop()
		if !assert.NoError(t, http.NewResponseController(c.Writer).SetWriteDeadline(time.Now().Add(5*time.Second))) {
			return
		}
		info := &relaycommon.RelayInfo{IsStream: true, ChannelMeta: &relaycommon.ChannelMeta{}}
		for range 2 {
			request, err := http.NewRequestWithContext(c.Request.Context(), http.MethodPost, upstream.URL, strings.NewReader("{}"))
			if !assert.NoError(t, err) {
				return
			}
			response, err := channel.DoRequest(c, request, info)
			if !assert.NoError(t, err) {
				return
			}
			if response.StatusCode == http.StatusTooManyRequests {
				response.Body.Close()
				continue
			}
			_, _ = io.Copy(c.Writer, response.Body)
			response.Body.Close()
		}
	})
	gateway := httptest.NewServer(engine)
	defer gateway.Close()
	defer releaseOnce.Do(func() { close(release) })
	client := &http.Client{Timeout: 4 * time.Second}
	response, err := client.Post(gateway.URL+"/v1/messages", "application/json", strings.NewReader("{}"))
	require.NoError(t, err)
	defer response.Body.Close()
	select {
	case <-retryStarted:
	case <-time.After(time.Second):
		t.Fatal("the upstream retry did not start")
	}
	reader := bufio.NewReader(response.Body)
	for range 2 {
		line, readErr := reader.ReadString('\n')
		require.NoError(t, readErr)
		require.Equal(t, ": PING\n", line)
		line, readErr = reader.ReadString('\n')
		require.NoError(t, readErr)
		require.Equal(t, "\n", line)
	}
	releaseOnce.Do(func() { close(release) })
	body, err := io.ReadAll(reader)
	require.NoError(t, err)
	require.Contains(t, string(body), "data: ok\n\n")
	require.Equal(t, int32(2), attempts.Load())
}

func TestRelayKeepAlivePreservesValidationErrors(t *testing.T) {
	settings := operation_setting.GetGeneralSetting()
	previousEnabled := settings.PingIntervalEnabled
	settings.PingIntervalEnabled = true
	t.Cleanup(func() { settings.PingIntervalEnabled = previousEnabled })
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(`{"model":"claude-sonnet-4-20250514","stream":true}`))
	c.Request.Header.Set("Content-Type", "application/json")
	Relay(c, types.RelayFormatClaude)
	require.Equal(t, http.StatusBadRequest, recorder.Code)
	require.Contains(t, recorder.Header().Get("Content-Type"), "application/json")
	require.NotContains(t, recorder.Body.String(), ": PING")
}

func TestStreamKeepAlivePreservesFastHTTPFailures(t *testing.T) {
	settings := operation_setting.GetGeneralSetting()
	previousEnabled, previousInterval := settings.PingIntervalEnabled, settings.PingIntervalSeconds
	settings.PingIntervalEnabled, settings.PingIntervalSeconds = true, 60
	t.Cleanup(func() {
		settings.PingIntervalEnabled, settings.PingIntervalSeconds = previousEnabled, previousInterval
	})
	service.InitHttpClient()
	for _, status := range []int{http.StatusUnauthorized, http.StatusPaymentRequired, http.StatusForbidden, http.StatusTooManyRequests} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(status)
				_, _ = io.WriteString(w, `{"error":"request rejected"}`)
			}))
			defer upstream.Close()
			engine := gin.New()
			engine.POST("/v1/messages", func(c *gin.Context) {
				stop := helper.StartStreamKeepAlive(c, time.Minute)
				defer stop()
				request, err := http.NewRequestWithContext(c.Request.Context(), http.MethodPost, upstream.URL, strings.NewReader("{}"))
				if !assert.NoError(t, err) {
					return
				}
				info := &relaycommon.RelayInfo{IsStream: true, ChannelMeta: &relaycommon.ChannelMeta{}}
				response, err := channel.DoRequest(c, request, info)
				if !assert.NoError(t, err) {
					return
				}
				defer response.Body.Close()
				body, err := io.ReadAll(response.Body)
				if assert.NoError(t, err) {
					c.Header("Content-Type", "application/json")
					c.Data(response.StatusCode, "application/json", body)
				}
			})
			gateway := httptest.NewServer(engine)
			defer gateway.Close()
			client := &http.Client{Timeout: 4 * time.Second}
			response, err := client.Post(gateway.URL+"/v1/messages", "application/json", strings.NewReader("{}"))
			require.NoError(t, err)
			defer response.Body.Close()
			body, err := io.ReadAll(response.Body)
			require.NoError(t, err)
			require.Equal(t, status, response.StatusCode)
			require.Equal(t, "application/json", response.Header.Get("Content-Type"))
			require.JSONEq(t, `{"error":"request rejected"}`, string(body))
		})
	}
}

func TestRelayKeepAliveHonorsStreamSettings(t *testing.T) {
	settings := operation_setting.GetGeneralSetting()
	previousEnabled := settings.PingIntervalEnabled
	previousCount, previousMedia, previousNonStreamMedia := constant.CountToken, constant.GetMediaToken, constant.GetMediaTokenNotStream
	constant.CountToken, constant.GetMediaToken, constant.GetMediaTokenNotStream = true, true, true
	t.Cleanup(func() {
		settings.PingIntervalEnabled = previousEnabled
		constant.CountToken, constant.GetMediaToken, constant.GetMediaTokenNotStream = previousCount, previousMedia, previousNonStreamMedia
	})
	for _, test := range []struct {
		name    string
		enabled bool
		stream  bool
	}{
		{name: "ping disabled", enabled: false, stream: true},
		{name: "non-streaming", enabled: true, stream: false},
		{name: "fast streaming failure", enabled: true, stream: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			settings.PingIntervalEnabled = test.enabled
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			body := `{"model":"claude-sonnet-4-20250514","max_tokens":16,"stream":` + strconv.FormatBool(test.stream) + `,"messages":[{"role":"user","content":[{"type":"image","source":{"type":"base64","media_type":"image/png","data":"!"}}]}]}`
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(body))
			c.Request.Header.Set("Content-Type", "application/json")
			common.SetContextKey(c, constant.ContextKeyOriginalModel, "claude-sonnet-4-20250514")
			Relay(c, types.RelayFormatClaude)
			require.Equal(t, http.StatusInternalServerError, recorder.Code)
			require.Contains(t, recorder.Header().Get("Content-Type"), "application/json")
			require.NotContains(t, recorder.Body.String(), ": PING")
			var response map[string]any
			require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
			require.Equal(t, "error", response["type"])
		})
	}
}

func TestRelayStreamErrorAfterKeepAlive(t *testing.T) {
	for _, format := range []types.RelayFormat{types.RelayFormatClaude, types.RelayFormatOpenAI} {
		t.Run(string(format), func(t *testing.T) {
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodPost, "/stream", nil)
			helper.SetEventStreamHeaders(c)
			require.NoError(t, helper.PingData(c))
			common.SetContextKey(c, constant.ContextKeyChannelBaseUrl, "https://provider.example")
			common.SetContextKey(c, constant.ContextKeyUpstreamRequestOrigin, "https://provider.example")
			apiError := types.NewErrorWithStatusCode(errors.New("upstream rejected input at https://provider.example/v1/messages"), types.ErrorCodeBadResponseStatusCode, http.StatusBadRequest)
			writeRelayStreamError(c, format, apiError)
			require.Equal(t, http.StatusOK, recorder.Code)
			require.Equal(t, "text/event-stream", recorder.Header().Get("Content-Type"))
			require.Contains(t, recorder.Body.String(), "upstream rejected input")
			require.NotContains(t, recorder.Body.String(), "provider.example")
			status, ok := common.GetContextKeyType[*relaycommon.StreamStatus](c, constant.ContextKeyResponseStreamStatus)
			require.True(t, ok)
			require.True(t, status.ResponseFailed(), "stream errors must not consume the successful-request rate limit")
			require.Equal(t, http.StatusBadRequest, status.OutcomeSnapshot().ErrorStatus)
			if format == types.RelayFormatClaude {
				require.Contains(t, recorder.Body.String(), "event: error\n")
			} else {
				require.Contains(t, recorder.Body.String(), "data: [DONE]")
			}
		})
	}
}

func TestStreamKeepAliveCancellation(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	requestContext, cancel := context.WithCancel(context.Background())
	defer cancel()
	c.Request = httptest.NewRequest(http.MethodPost, "/stream", nil).WithContext(requestContext)
	stop := helper.StartStreamKeepAlive(c, time.Hour)
	cancel()
	stopped := make(chan struct{})
	go func() {
		stop()
		close(stopped)
	}()
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("keepalive did not stop after client cancellation")
	}
	_, err := c.Writer.WriteString("data: done\n\n")
	require.NoError(t, err)
	require.True(t, strings.HasSuffix(recorder.Body.String(), "data: done\n\n"))
}

func TestRelayCancelUsageTimeoutConfiguration(t *testing.T) {
	for _, tc := range []struct {
		value string
		want  int
	}{
		{"", 15}, {"0", 0}, {"60", 60}, {"61", 15}, {"-1", 15}, {"invalid", 15},
	} {
		t.Run(tc.value, func(t *testing.T) {
			t.Setenv("RELAY_CANCEL_USAGE_TIMEOUT", tc.value)
			assert.Equal(t, tc.want, common.RelayCancelUsageTimeoutFromEnv())
		})
	}
}

func TestTextStreamCancellationBilling(t *testing.T) {
	const (
		modelName    = "gpt-cancel-billing-test"
		initialQuota = 100_000
		// 1024 uncached at $4/M, 3072 cached at $1/M, 73 output at $8/M.
		actualQuota = 3876
	)
	db, dialect := openTaskDialectDatabase(t, &model.User{}, &model.Token{}, &model.Channel{}, &model.UserSubscription{})
	logDSN := os.Getenv("TEST_RELAY_STREAM_LOG_DSN")
	if logDSN != "" {
		switch dialect {
		case common.DatabaseTypeMySQL:
			t.Setenv("TEST_MYSQL_DSN", logDSN)
		case common.DatabaseTypePostgreSQL:
			t.Setenv("TEST_POSTGRES_DSN", logDSN)
		}
	}
	logDB, _ := openTaskDialectDatabase(t, &model.Log{})
	previousDB, previousLogDB := model.DB, model.LOG_DB
	previousMain, previousLog := common.MainDatabaseType(), common.LogDatabaseType()
	redis, memory, batch, logs, exports := common.RedisEnabled, common.MemoryCacheEnabled, common.BatchUpdateEnabled, common.LogConsumeEnabled, common.DataExportEnabled
	streamingTimeout, cancelTimeout := constant.StreamingTimeout, constant.RelayCancelUsageTimeout
	countTokens := constant.CountToken
	oldRetries := common.RetryTimes
	model.DB, model.LOG_DB = db, logDB
	common.SetDatabaseTypes(dialect, dialect)
	common.RedisEnabled, common.MemoryCacheEnabled, common.BatchUpdateEnabled, common.LogConsumeEnabled, common.DataExportEnabled = false, false, false, true, false
	constant.StreamingTimeout = 5
	constant.CountToken = true
	common.RetryTimes = 1
	t.Cleanup(func() {
		model.DB, model.LOG_DB = previousDB, previousLogDB
		common.SetDatabaseTypes(previousMain, previousLog)
		common.RedisEnabled, common.MemoryCacheEnabled, common.BatchUpdateEnabled, common.LogConsumeEnabled, common.DataExportEnabled = redis, memory, batch, logs, exports
		constant.StreamingTimeout, constant.RelayCancelUsageTimeout = streamingTimeout, cancelTimeout
		constant.CountToken = countTokens
		common.RetryTimes = oldRetries
	})
	withTieredBillingConfig(t, map[string]string{modelName: "tiered_expr"}, map[string]string{modelName: `tier("text", p * 4 + cr + c * 8)`})
	service.InitHttpClient()

	user := model.User{Username: "stream-cancel", Quota: initialQuota, Status: common.UserStatusEnabled}
	require.NoError(t, db.Create(&user).Error)
	token := model.Token{UserId: user.Id, Key: "local-stream-cancel-test", Name: "stream-cancel", RemainQuota: initialQuota, Status: common.TokenStatusEnabled}
	require.NoError(t, db.Create(&token).Error)
	selected := model.Channel{Type: constant.ChannelTypeOpenAI, Key: "unused", Name: "stream-cancel", Status: common.ChannelStatusEnabled}
	require.NoError(t, db.Create(&selected).Error)

	chatRequest := `{"model":"` + modelName + `","messages":[{"role":"user","content":"hello"}],"stream":true}`
	chatStart := `{"id":"chat-test","model":"` + modelName + `","choices":[{"index":0,"delta":{"role":"assistant"}}]}`
	chatTail := `{"id":"chat-test","model":"` + modelName + `","choices":[],"usage":{"prompt_tokens":4096,"completion_tokens":73,"total_tokens":4169,"prompt_tokens_details":{"cached_tokens":3072}}}`
	responsesRequest := `{"model":"` + modelName + `","input":"hello","stream":true}`
	responsesStart := `{"type":"response.created","response":{"id":"resp_1","model":"` + modelName + `","status":"in_progress"}}`

	for _, tc := range []struct {
		name, path, request                                                            string
		format                                                                         types.RelayFormat
		channelType                                                                    int
		start, tail                                                                    []string
		wantInput, wantOutput, wantCache, wantQuota                                    int
		complete, estimated, cacheUnknown, noTail, preCancel, beforeHeaders, connected bool
		timeout                                                                        int
	}{
		{name: "chat", path: "/v1/chat/completions", request: chatRequest, format: types.RelayFormatOpenAI, channelType: constant.ChannelTypeOpenAI,
			start: []string{chatStart, chatStart}, tail: []string{chatTail, "[DONE]"}, wantInput: 4096, wantOutput: 73, wantCache: 3072, wantQuota: actualQuota, timeout: 15},
		{name: "responses", path: "/v1/responses", request: responsesRequest, format: types.RelayFormatOpenAIResponses, channelType: constant.ChannelTypeOpenAI,
			start: []string{responsesStart}, tail: []string{`{"type":"response.completed","response":{"id":"resp_1","status":"completed","usage":{"input_tokens":4096,"output_tokens":73,"total_tokens":4169,"input_tokens_details":{"cached_tokens":3072}}}}`},
			wantInput: 4096, wantOutput: 73, wantCache: 3072, wantQuota: actualQuota, complete: true, timeout: 60},
		{name: "claude", path: "/v1/messages", request: `{"model":"` + modelName + `","max_tokens":128,"messages":[{"role":"user","content":"hello"}],"stream":true}`, format: types.RelayFormatClaude, channelType: constant.ChannelTypeAnthropic,
			start:     []string{`{"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","model":"` + modelName + `","content":[],"usage":{"input_tokens":1024,"cache_read_input_tokens":3072,"output_tokens":0}}}`},
			tail:      []string{`{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":73}}`, `{"type":"message_stop"}`},
			wantInput: 1024, wantOutput: 73, wantCache: 3072, wantQuota: actualQuota, complete: true, timeout: 15},
		{name: "gemini", path: "/v1beta/models/" + modelName + ":streamGenerateContent", request: `{"generationConfig":{"candidateCount":2},"contents":[{"role":"user","parts":[{"text":"hello"}]}]}`, format: types.RelayFormatGemini, channelType: constant.ChannelTypeGemini,
			start: []string{`{"candidates":[{"index":0,"content":{"role":"model","parts":[{"text":"hello"}]}}]}`},
			tail: []string{
				`{"candidates":[{"index":0,"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":4096,"candidatesTokenCount":20,"totalTokenCount":4116,"cachedContentTokenCount":3072}}`,
				`{"candidates":[{"index":1,"finishReason":"STOP"}]}`,
				`{"usageMetadata":{"promptTokenCount":4096,"candidatesTokenCount":73,"totalTokenCount":4169,"cachedContentTokenCount":3072}}`,
			}, wantInput: 4096, wantOutput: 73, wantCache: 3072, wantQuota: actualQuota, complete: true, timeout: 15},
		{name: "gemini_reports_output_without_input", path: "/v1beta/models/" + modelName + ":streamGenerateContent", request: `{"contents":[{"role":"user","parts":[{"text":"hello"}]}]}`, format: types.RelayFormatGemini, channelType: constant.ChannelTypeGemini,
			start:      []string{`{"candidates":[{"index":0,"content":{"role":"model","parts":[{"text":"hello"}]}}]}`},
			tail:       []string{`{"candidates":[{"index":0,"finishReason":"STOP"}]}`, `{"usageMetadata":{"candidatesTokenCount":73}}`},
			wantOutput: 73, complete: true, estimated: true, cacheUnknown: true, timeout: 15},
		{name: "explicit_zero_replaces_earlier_cache", path: "/v1/chat/completions", request: chatRequest, format: types.RelayFormatOpenAI, channelType: constant.ChannelTypeOpenAI,
			start: []string{chatStart, chatTail, chatStart}, tail: []string{strings.Replace(chatTail, `"cached_tokens":3072`, `"cached_tokens":0`, 1), "[DONE]"},
			wantInput: 4096, wantOutput: 73, wantQuota: 8484, timeout: 15},
		{name: "partial_usage_survives_timeout", path: "/v1/chat/completions", request: chatRequest, format: types.RelayFormatOpenAI, channelType: constant.ChannelTypeOpenAI,
			start:     []string{chatStart, `{"model":"` + modelName + `","choices":[],"usage":{"prompt_tokens":4096,"prompt_tokens_details":{"cached_tokens":3072}}}`, chatStart, chatStart},
			wantInput: 4096, wantCache: 3072, wantQuota: 3584, noTail: true, estimated: true, timeout: 1},
		{name: "disabled_no_usage", path: "/v1/chat/completions", request: chatRequest, format: types.RelayFormatOpenAI, channelType: constant.ChannelTypeOpenAI,
			start: []string{chatStart, chatStart}, noTail: true, estimated: true, cacheUnknown: true},
		{name: "close_after_delivered_terminal", path: "/v1/responses", request: responsesRequest, format: types.RelayFormatOpenAIResponses, channelType: constant.ChannelTypeOpenAI,
			start:     []string{responsesStart, `{"type":"response.completed","response":{"id":"resp_1","status":"completed","usage":{"input_tokens":4096,"output_tokens":73,"total_tokens":4169,"input_tokens_details":{"cached_tokens":3072}}}}`},
			wantInput: 4096, wantOutput: 73, wantCache: 3072, wantQuota: actualQuota, complete: true, connected: true, timeout: 15},
		{name: "close_after_delivered_incomplete", path: "/v1/responses", request: responsesRequest, format: types.RelayFormatOpenAIResponses, channelType: constant.ChannelTypeOpenAI,
			start:     []string{responsesStart, `{"type":"response.done","response":{"id":"resp_1","status":"incomplete","incomplete_details":{"reason":"max_output_tokens"},"usage":{"input_tokens":4096,"output_tokens":73,"total_tokens":4169,"input_tokens_details":{"cached_tokens":3072}}}}`},
			wantInput: 4096, wantOutput: 73, wantCache: 3072, wantQuota: actualQuota, connected: true, timeout: 15},
		{name: "upstream_failure_while_connected", path: "/v1/responses", request: responsesRequest, format: types.RelayFormatOpenAIResponses, channelType: constant.ChannelTypeOpenAI,
			start: []string{responsesStart, `{"type":"response.failed","response":{"id":"resp_1","status":"failed"}}`}, connected: true, timeout: 15},
		{name: "cancel_before_send", path: "/v1/chat/completions", request: chatRequest, format: types.RelayFormatOpenAI, channelType: constant.ChannelTypeOpenAI, preCancel: true, timeout: 15},
		{name: "cancel_before_headers", path: "/v1/chat/completions", request: chatRequest, format: types.RelayFormatOpenAI, channelType: constant.ChannelTypeOpenAI, beforeHeaders: true, timeout: 15},
	} {
		t.Run(tc.name, func(t *testing.T) {
			constant.RelayCancelUsageTimeout = tc.timeout
			var attempts atomic.Int32
			clientGone, finished := make(chan struct{}), make(chan struct{})
			upstreamStarted, upstreamClosed := make(chan struct{}), make(chan struct{})
			releaseUpstream := make(chan struct{})
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if attempts.Add(1) != 1 {
					w.WriteHeader(http.StatusBadGateway)
					return
				}
				defer close(upstreamClosed)
				_, _ = io.Copy(io.Discard, r.Body)
				close(upstreamStarted)
				if tc.beforeHeaders {
					select {
					case <-r.Context().Done():
					case <-releaseUpstream:
					}
					return
				}
				w.Header().Set("Content-Type", "text/event-stream")
				for _, frame := range tc.start {
					_, _ = io.WriteString(w, "data: "+frame+"\n\n")
				}
				w.(http.Flusher).Flush()
				if tc.connected {
					return
				}
				if tc.noTail {
					<-r.Context().Done()
					return
				}
				select {
				case <-clientGone:
				case <-r.Context().Done():
					return
				}
				for _, frame := range tc.tail {
					_, _ = io.WriteString(w, "data: "+frame+"\n\n")
				}
			}))
			defer upstream.Close()
			engine := gin.New()
			engine.Use(middleware.BodyStorageCleanup())
			engine.POST(tc.path, func(c *gin.Context) {
				defer close(finished)
				if tc.preCancel {
					ctx, cancel := context.WithCancel(c.Request.Context())
					cancel()
					c.Request = c.Request.WithContext(ctx)
				}
				clientContext := c.Request.Context()
				stop := context.AfterFunc(clientContext, func() { close(clientGone) })
				defer stop()
				c.Set(common.RequestIdKey, tc.name)
				common.SetContextKey(c, constant.ContextKeyOriginalModel, modelName)
				common.SetContextKey(c, constant.ContextKeyUserId, user.Id)
				common.SetContextKey(c, constant.ContextKeyUserGroup, "default")
				common.SetContextKey(c, constant.ContextKeyUsingGroup, "default")
				common.SetContextKey(c, constant.ContextKeyTokenId, token.Id)
				common.SetContextKey(c, constant.ContextKeyTokenKey, token.Key)
				common.SetContextKey(c, constant.ContextKeyChannelId, selected.Id)
				common.SetContextKey(c, constant.ContextKeyChannelType, tc.channelType)
				common.SetContextKey(c, constant.ContextKeyChannelKey, selected.Key)
				common.SetContextKey(c, constant.ContextKeyChannelBaseUrl, upstream.URL)
				Relay(c, tc.format)
			})
			before, err := model.GetUserQuota(user.Id, true)
			require.NoError(t, err)
			gateway := httptest.NewServer(engine)
			defer gateway.Close()
			defer close(releaseUpstream)
			client := &http.Client{Timeout: 8 * time.Second}
			if tc.beforeHeaders {
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				req, err := http.NewRequestWithContext(ctx, http.MethodPost, gateway.URL+tc.path, strings.NewReader(tc.request))
				require.NoError(t, err)
				req.Header.Set("Content-Type", "application/json")
				requestDone := make(chan error, 1)
				go func() {
					response, err := client.Do(req)
					if response != nil {
						_ = response.Body.Close()
					}
					requestDone <- err
				}()
				select {
				case <-upstreamStarted:
				case <-time.After(5 * time.Second):
					t.Fatal("upstream request was not sent")
				}
				cancel()
				require.ErrorIs(t, <-requestDone, context.Canceled)
			} else {
				resp, err := client.Post(gateway.URL+tc.path, "application/json", strings.NewReader(tc.request))
				require.NoError(t, err)
				defer resp.Body.Close()
				require.Equal(t, http.StatusOK, resp.StatusCode)
				if !tc.preCancel {
					reader := bufio.NewReader(resp.Body)
					for {
						line, err := reader.ReadString('\n')
						require.NoError(t, err)
						if strings.HasPrefix(line, "data:") && (!tc.connected || strings.Contains(line, tc.start[len(tc.start)-1])) {
							break
						}
					}
				}
				require.NoError(t, resp.Body.Close())
			}
			select {
			case <-finished:
			case <-time.After(8 * time.Second):
				t.Fatal("relay did not settle")
			}

			var records []model.Log
			require.NoError(t, logDB.Where("request_id = ? AND type = ?", tc.name, model.LogTypeConsume).Find(&records).Error)
			quota, err := model.GetUserQuota(user.Id, true)
			require.NoError(t, err)
			require.NoError(t, db.First(&token, token.Id).Error)
			if tc.preCancel || tc.beforeHeaders {
				assert.Empty(t, records)
				if tc.preCancel {
					assert.Zero(t, attempts.Load())
					assert.Equal(t, before, quota)
					assert.Equal(t, before, token.RemainQuota)
				} else {
					assert.Equal(t, int32(1), attempts.Load())
					require.EventuallyWithT(t, func(c *assert.CollectT) {
						balance, err := model.GetUserQuota(user.Id, true)
						assert.NoError(c, err)
						assert.Equal(c, before, balance)
						var current model.Token
						assert.NoError(c, db.First(&current, token.Id).Error)
						assert.Equal(c, before, current.RemainQuota)
					}, 2*time.Second, 10*time.Millisecond)
				}
				return
			}
			assert.Equal(t, quota, token.RemainQuota)
			assert.Equal(t, int32(1), attempts.Load())
			select {
			case <-upstreamClosed:
			case <-time.After(time.Second):
				t.Fatal("upstream was not closed")
			}
			require.Len(t, records, 1)
			log := records[0]
			if tc.cacheUnknown {
				assert.Positive(t, log.PromptTokens)
				assert.Equal(t, log.PromptTokens*2+tc.wantOutput*4, log.Quota)
			} else {
				assert.Equal(t, tc.wantInput, log.PromptTokens)
				assert.Equal(t, tc.wantQuota, log.Quota)
			}
			assert.Equal(t, tc.wantOutput, log.CompletionTokens)
			assert.Equal(t, before-log.Quota, quota)
			var other struct {
				CacheTokens int `json:"cache_tokens"`
				Stream      struct {
					ClientCancelled bool   `json:"client_cancelled"`
					UsageEstimated  bool   `json:"usage_estimated"`
					CacheKnown      *bool  `json:"cache_usage_known"`
					ResponseStatus  string `json:"response_status"`
					EndReason       string `json:"end_reason"`
				} `json:"stream_status"`
			}
			require.NoError(t, common.UnmarshalJsonStr(log.Other, &other))
			assert.Equal(t, tc.wantCache, other.CacheTokens)
			assert.Equal(t, !tc.connected, other.Stream.ClientCancelled)
			assert.Equal(t, tc.estimated, other.Stream.UsageEstimated)
			if tc.complete {
				assert.Equal(t, "completed", other.Stream.ResponseStatus)
			}
			if tc.connected {
				assert.NotContains(t, log.Content, "User canceled")
			} else {
				assert.Contains(t, log.Content, "User canceled")
				require.NotNil(t, other.Stream.CacheKnown)
				assert.Equal(t, !tc.cacheUnknown, *other.Stream.CacheKnown)
				assert.Equal(t, tc.cacheUnknown, strings.Contains(log.Content, "Cache usage is unknown"))
				assert.Equal(t, tc.estimated, strings.Contains(log.Content, "Usage was estimated"))
			}
			if tc.noTail && tc.timeout != 0 {
				assert.Equal(t, "timeout", other.Stream.EndReason)
			}
		})
	}
}
