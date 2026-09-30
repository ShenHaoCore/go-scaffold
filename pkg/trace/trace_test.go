package trace_test

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"go-scaffold/pkg/trace"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/metadata"
)

func TestWithTraceWritesValueAndOutgoingMD(t *testing.T) {
	ctx := trace.WithTrace(context.Background(), "demo-trace")
	require.Equal(t, "demo-trace", trace.FromContext(ctx))

	md, ok := metadata.FromOutgoingContext(ctx)
	require.True(t, ok)
	vals := md.Get(trace.MetadataKey)
	require.Len(t, vals, 1)
	require.Equal(t, "demo-trace", vals[0])
}

func TestFromContextPrefersIncomingMD(t *testing.T) {
	md := metadata.Pairs(trace.MetadataKey, "from-incoming")
	ctx := metadata.NewIncomingContext(context.Background(), md)
	ctx = context.WithValue(ctx, struct{}{}, "ignored")
	// Inject via WithTrace after incoming — FromContext should still read incoming first
	require.Equal(t, "from-incoming", trace.FromContext(ctx))
}

func TestInjectIncomingFromMD(t *testing.T) {
	md := metadata.Pairs(trace.MetadataKey, "rpc-trace")
	ctx := metadata.NewIncomingContext(context.Background(), md)
	ctx = trace.InjectIncomingFromMD(ctx)
	require.Equal(t, "rpc-trace", trace.FromContext(ctx))
	out, ok := metadata.FromOutgoingContext(ctx)
	require.True(t, ok)
	require.Equal(t, []string{"rpc-trace"}, out.Get(trace.MetadataKey))
}

func TestInjectIncomingPrefersLastMeta(t *testing.T) {
	md := metadata.MD{}
	md.Append(trace.MetadataKey, "old")
	md.Append(trace.MetadataKey, "new")
	ctx := metadata.NewIncomingContext(context.Background(), md)
	ctx = trace.InjectIncomingFromMD(ctx)
	require.Equal(t, "new", trace.FromContext(ctx))
}

func TestWithTraceDedupSet(t *testing.T) {
	ctx := trace.WithTrace(context.Background(), "a")
	ctx = trace.WithTrace(ctx, "a")
	out, ok := metadata.FromOutgoingContext(ctx)
	require.True(t, ok)
	require.Equal(t, []string{"a"}, out.Get(trace.MetadataKey), "Set must not accumulate duplicate values")
}

func TestWithTraceDoesNotOverwriteRequestID(t *testing.T) {
	const reqIDKey = "x-request-id"
	md := metadata.MD{}
	md.Set(reqIDKey, "go-zero-req-id")
	ctx := metadata.NewOutgoingContext(context.Background(), md)
	ctx = trace.WithTrace(ctx, "our-trace")

	out, ok := metadata.FromOutgoingContext(ctx)
	require.True(t, ok)
	require.Equal(t, []string{"go-zero-req-id"}, out.Get(reqIDKey), "must not overwrite go-zero x-request-id")
	require.Equal(t, []string{"our-trace"}, out.Get(trace.MetadataKey))
}

func TestSanitizeTraceID(t *testing.T) {
	require.Equal(t, "abc-123", trace.SanitizeTraceID("abc-123"))
	require.Empty(t, trace.SanitizeTraceID("bad id with space"))
	require.Empty(t, trace.SanitizeTraceID("<script>"))
	require.Empty(t, trace.SanitizeTraceID(strings.Repeat("a", 200)))
	require.NotEmpty(t, trace.ParseHTTPHeaders(func(k string) string {
		if k == trace.HeaderTraceID {
			return "evil\ninjection"
		}
		return ""
	}))
}

func TestParseHTTPHeadersCloud(t *testing.T) {
	id := trace.ParseHTTPHeaders(func(k string) string {
		if k == trace.HeaderEagleEyeTraceID {
			return "213e3c1a16727357601711075e1055"
		}
		return ""
	})
	require.Equal(t, "213e3c1a16727357601711075e1055", id)

	id = trace.ParseHTTPHeaders(func(k string) string {
		if k == trace.HeaderB3TraceID {
			return "abc123def456"
		}
		return ""
	})
	require.Equal(t, "abc123def456", id)

	id = trace.ParseHTTPHeaders(func(k string) string {
		if k == trace.HeaderTraceParent {
			return "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01"
		}
		return ""
	})
	require.Equal(t, "4bf92f3577b34da6a3ce929d0e0e4736", id)
}

func TestInjectHTTPHeaders(t *testing.T) {
	ctx := trace.WithTrace(context.Background(), "out-tid")
	h := make(http.Header)
	trace.InjectHTTPHeaders(h, ctx)
	require.Equal(t, "out-tid", h.Get(trace.HeaderTraceID))
	require.Equal(t, "out-tid", h.Get(trace.HeaderEagleEyeTraceID))
}
