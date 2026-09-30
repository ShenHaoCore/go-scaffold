package errors_test

import (
	"context"
	"fmt"
	"testing"

	bizerr "micro-scaffold/pkg/errors"

	"github.com/stretchr/testify/require"
)

func TestI18nFallback(t *testing.T) {
	zh := bizerr.New(bizerr.CodeRequired, "zh-CN", map[string]any{"Field": "name"})
	require.Equal(t, "COM2001", zh.Code)
	require.Contains(t, zh.Message, "name")

	en := bizerr.New(bizerr.CodeRequired, "en-US", map[string]any{"Field": "name"})
	require.Contains(t, en.Message, "required")

	fallback := bizerr.New(bizerr.CodeInternal, "fr-FR", nil)
	require.Equal(t, "服务内部错误", fallback.Message)
}

func TestI18nMissingFieldNoPanic(t *testing.T) {
	require.NotPanics(t, func() {
		msg := bizerr.Translate(bizerr.CodeRequired, "zh-CN", map[string]any{})
		require.Contains(t, msg, "{{.Field}}")
	})
	require.NotPanics(t, func() {
		msg := bizerr.Translate(bizerr.CodeRequired, "zh-CN", nil)
		require.Contains(t, msg, "{{.Field}}")
	})
}

func TestHTTPStatus(t *testing.T) {
	require.Equal(t, 500, bizerr.HTTPStatus(bizerr.CodeInternal))
	require.Equal(t, 400, bizerr.HTTPStatus(bizerr.CodeRequired))
	require.Equal(t, 400, bizerr.HTTPStatus(bizerr.CodeInvalidParam)) // COM2002 → 第4位=2 → 400
	require.Equal(t, 422, bizerr.HTTPStatus("XXX3001"))               // 类型位 3 → 422（业务码由各服务自定）
	require.Equal(t, 401, bizerr.HTTPStatus(bizerr.CodeUnauthorized))
	require.Equal(t, 403, bizerr.HTTPStatus(bizerr.CodeForbidden))
	require.Equal(t, 429, bizerr.HTTPStatus(bizerr.CodeRateLimited))
}

func TestWrap(t *testing.T) {
	root := fmt.Errorf("db down")
	err := bizerr.Wrap(root, "query resource")
	require.Error(t, err)
	require.Contains(t, err.Error(), "query resource")
	require.Contains(t, err.Error(), "db down")
	require.ErrorIs(t, err, root)

	_, ok := bizerr.AsBizError(err)
	require.False(t, ok)

	be := bizerr.New(bizerr.CodeInvalidParam, "zh-CN", map[string]any{"Field": "id"})
	wrappedBiz := bizerr.Wrap(be, "get resource")
	got, ok := bizerr.AsBizError(wrappedBiz)
	require.True(t, ok)
	require.Equal(t, bizerr.CodeInvalidParam, got.Code)
}

func TestNewFromContext(t *testing.T) {
	ctx := bizerr.WithLang(context.Background(), "en-US,en;q=0.9")
	be := bizerr.NewFromContext(ctx, bizerr.CodeUnauthorized, nil)
	require.Equal(t, bizerr.CodeUnauthorized, be.Code)
	require.Contains(t, be.Message, "Unauthorized")
}
