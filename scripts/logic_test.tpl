package {{API}}

import (
	"context"
	"testing"

	"micro-scaffold/internal/config"
	"micro-scaffold/internal/svc"
	"micro-scaffold/pkg/trace"

	"github.com/stretchr/testify/require"
)

func Test{{API}}Logic_Success(t *testing.T) {
	ctx := trace.WithTrace(context.Background(), "test-trace-id")
	svcCtx := &svc.ServiceContext{Config: config.Config{}}
	l := New{{API}}Logic(ctx, svcCtx)
	// TODO: 传入 types 中实际 Req，并断言 Resp 字段
	resp, err := l.Handle(nil)
	require.NoError(t, err)
	_ = resp
	require.Equal(t, "test-trace-id", trace.FromContext(ctx))
}
