package response_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	bizerr "go-scaffold/pkg/errors"
	"go-scaffold/pkg/response"
	"go-scaffold/pkg/trace"

	"github.com/stretchr/testify/require"
)

func TestSuccessAndBizError(t *testing.T) {
	ctx := trace.WithTrace(context.Background(), "tid-1")
	w := httptest.NewRecorder()
	response.Success(ctx, w, map[string]string{"ok": "1"})
	require.Equal(t, http.StatusOK, w.Code)
	var body response.Body
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	require.Equal(t, float64(0), body.Code)
	require.Equal(t, "tid-1", body.TraceID)
	require.Equal(t, "tid-1", w.Header().Get("X-Trace-Id"))

	w2 := httptest.NewRecorder()
	response.Error(ctx, w2, bizerr.New(bizerr.CodeRequired, "zh-CN", map[string]any{"Field": "id"}))
	require.Equal(t, http.StatusBadRequest, w2.Code)
	var errBody response.Body
	require.NoError(t, json.Unmarshal(w2.Body.Bytes(), &errBody))
	require.Equal(t, "COM2001", errBody.Code)
}

func TestErrorMapsPlainAndWrappedError(t *testing.T) {
	ctx := trace.WithTrace(context.Background(), "tid-2")

	w := httptest.NewRecorder()
	response.Error(ctx, w, context.DeadlineExceeded)
	require.Equal(t, http.StatusInternalServerError, w.Code)
	var body response.Body
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	require.Equal(t, "COM1001", body.Code)
	require.NotContains(t, body.Message, "deadline")

	w2 := httptest.NewRecorder()
	wrapped := bizerr.Wrap(fmt.Errorf("sql: connection refused"), "list resources")
	response.Error(ctx, w2, wrapped)
	require.Equal(t, http.StatusInternalServerError, w2.Code)
	var body2 response.Body
	require.NoError(t, json.Unmarshal(w2.Body.Bytes(), &body2))
	require.Equal(t, "COM1001", body2.Code)
	require.NotContains(t, body2.Message, "connection refused")
	require.NotContains(t, body2.Message, "list resources")
}
