package grpcx

import (
	"context"

	bizerr "micro-scaffold/pkg/errors"

	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

const MetadataLang = "lang"

// UnaryClientLangInterceptor 将 context 中的语言写入 outgoing metadata「lang」（对键 Set，避免重复 Append）。
func UnaryClientLangInterceptor(ctx context.Context, method string, req, reply any, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
	lang := bizerr.LangFromContext(ctx)
	if lang != "" {
		md, ok := metadata.FromOutgoingContext(ctx)
		if !ok {
			md = metadata.MD{}
		}
		md = md.Copy()
		md.Set(MetadataLang, lang)
		ctx = metadata.NewOutgoingContext(ctx, md)
	}
	return invoker(ctx, method, req, reply, cc, opts...)
}

// StreamClientLangInterceptor 流式版语言透传（与 Unary 同口径）。
func StreamClientLangInterceptor(ctx context.Context, desc *grpc.StreamDesc, cc *grpc.ClientConn, method string, streamer grpc.Streamer, opts ...grpc.CallOption) (grpc.ClientStream, error) {
	lang := bizerr.LangFromContext(ctx)
	if lang != "" {
		md, ok := metadata.FromOutgoingContext(ctx)
		if !ok {
			md = metadata.MD{}
		}
		md = md.Copy()
		md.Set(MetadataLang, lang)
		ctx = metadata.NewOutgoingContext(ctx, md)
	}
	return streamer(ctx, desc, cc, method, opts...)
}
