package grpcx_test

import (
	"context"
	"testing"

	bizerr "micro-scaffold/pkg/errors"
	"micro-scaffold/pkg/grpcx"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

func TestUnaryClientLangInterceptor_Set(t *testing.T) {
	ctx := bizerr.WithLang(context.Background(), "en-US")
	// 预置重复值，拦截器须 Set 覆盖为单值
	md := metadata.MD{}
	md.Append(grpcx.MetadataLang, "zh-CN")
	md.Append(grpcx.MetadataLang, "old")
	ctx = metadata.NewOutgoingContext(ctx, md)

	var gotMD metadata.MD
	invoker := func(ctx context.Context, method string, req, reply any, cc *grpc.ClientConn, opts ...grpc.CallOption) error {
		out, ok := metadata.FromOutgoingContext(ctx)
		require.True(t, ok)
		gotMD = out
		return nil
	}
	err := grpcx.UnaryClientLangInterceptor(ctx, "/scaffold.Service/Method", nil, nil, nil, invoker)
	require.NoError(t, err)
	require.Equal(t, []string{"en-US"}, gotMD.Get(grpcx.MetadataLang))
}
