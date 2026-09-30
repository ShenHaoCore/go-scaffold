package response_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	bizerr "micro-scaffold/pkg/errors"
	"micro-scaffold/pkg/response"
	"micro-scaffold/pkg/trace"

	"github.com/stretchr/testify/require"
	"github.com/zeromicro/go-zero/rest/httpx"
)

// 契约：框架内部走 httpx.Error 的失败也必须产出统一响应体。
// 回归点：不装钩子时 go-zero 回退 http.Error(w, err.Error(), 400) —— 原文进 body、状态码恒 400。
func TestInstallFrameworkErrorHandler_UnifiesHttpxError(t *testing.T) {
	response.InstallFrameworkErrorHandler()
	ctx := trace.WithTrace(context.Background(), "fw-tid")

	t.Run("BizError 保留错误码与状态码", func(t *testing.T) {
		w := httptest.NewRecorder()
		httpx.ErrorCtx(ctx, w, bizerr.New(bizerr.CodeRequired, "zh-CN", map[string]any{"Field": "id"}))

		require.Equal(t, http.StatusBadRequest, w.Code)
		var body response.Body
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
		require.Equal(t, "COM2001", body.Code)
		require.Equal(t, "fw-tid", body.TraceID)
	})

	t.Run("普通 error 映射 COM1001 且原文不泄漏", func(t *testing.T) {
		w := httptest.NewRecorder()
		httpx.ErrorCtx(ctx, w, errors.New("dial tcp 10.0.0.1:5432: connection refused"))

		require.Equal(t, http.StatusInternalServerError, w.Code)
		var body response.Body
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
		require.Equal(t, "COM1001", body.Code)
		// 契约：原始 error 绝不进响应体（整段 body 一起断言，避免只查 Message 漏掉别的字段）
		require.NotContains(t, w.Body.String(), "connection refused")
		require.NotContains(t, w.Body.String(), "10.0.0.1")
	})

	t.Run("鉴权位下 403 的细分仍生效", func(t *testing.T) {
		w := httptest.NewRecorder()
		httpx.ErrorCtx(ctx, w, bizerr.New(bizerr.CodeForbidden, "zh-CN", nil))
		require.Equal(t, http.StatusForbidden, w.Code)
	})
}
