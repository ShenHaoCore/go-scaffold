package middleware_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"go-scaffold/internal/middleware"
	bizerr "go-scaffold/pkg/errors"
	"go-scaffold/pkg/response"
	"go-scaffold/pkg/trace"

	"github.com/stretchr/testify/require"
	"github.com/zeromicro/go-zero/core/logx"
)

func TestRecoveryMiddleware_PanicReturnsCOM1001AndTraceID(t *testing.T) {
	logx.Disable()

	h := middleware.RecoveryMiddleware(func(w http.ResponseWriter, r *http.Request) {
		panic("scaffold-http-panic")
	})

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	req.Header.Set(trace.HeaderTraceID, "http-panic-tid")
	rr := httptest.NewRecorder()
	h(rr, req)

	require.Equal(t, http.StatusInternalServerError, rr.Code)
	require.Equal(t, "http-panic-tid", rr.Header().Get(trace.HeaderTraceID))

	var body response.Body
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &body))
	require.Equal(t, bizerr.CodeInternal, body.Code)
	require.Equal(t, "http-panic-tid", body.TraceID)
	require.NotEmpty(t, body.Message)
}

// Recovery→Trace 全链：Trace 写入的 header/body tid 与 Recovery panic 出口一致。
func TestRecoveryTraceChain_PanicKeepsSameTraceID(t *testing.T) {
	logx.Disable()

	h := middleware.RecoveryMiddleware(middleware.TraceMiddleware(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "chain-http-tid", trace.FromContext(r.Context()))
		panic("chain-panic")
	}))

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	req.Header.Set(trace.HeaderTraceID, "chain-http-tid")
	rr := httptest.NewRecorder()
	h(rr, req)

	require.Equal(t, http.StatusInternalServerError, rr.Code)
	require.Equal(t, "chain-http-tid", rr.Header().Get(trace.HeaderTraceID))
	var body response.Body
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &body))
	require.Equal(t, bizerr.CodeInternal, body.Code)
	require.Equal(t, "chain-http-tid", body.TraceID)
}
