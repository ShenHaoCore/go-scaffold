package trace

import (
	"context"
	"net/http"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/zeromicro/go-zero/core/logx"
	"google.golang.org/grpc/metadata"
)

type ctxKey struct{}

const (
	HeaderTraceID         = "X-Trace-Id"
	HeaderRequestID       = "X-Request-Id"
	HeaderTraceParent     = "traceparent"
	HeaderEagleEyeTraceID = "EagleEye-TraceID"
	HeaderB3TraceID       = "X-B3-TraceId"
	MetadataKey           = "x-trace-id"
	MetadataEagleEyeKey   = "eagleeye-traceid"
	maxTraceIDLen         = 128
)

var traceIDPattern = regexp.MustCompile(`^[A-Za-z0-9._\-:/]+$`)

// SanitizeTraceID 校验客户端传入的 trace id；非法则返回空（调用方应 NewID）。
func SanitizeTraceID(id string) string {
	id = strings.TrimSpace(id)
	if id == "" || utf8.RuneCountInString(id) > maxTraceIDLen {
		return ""
	}
	if !traceIDPattern.MatchString(id) {
		return ""
	}
	return id
}

// WithTrace 同时写入 context.Value 与 gRPC outgoing metadata，保证 HTTP→RPC 透传。
func WithTrace(ctx context.Context, id string) context.Context {
	if id = SanitizeTraceID(id); id == "" {
		id = uuid.NewString()
	}
	ctx = context.WithValue(ctx, ctxKey{}, id)
	md, ok := metadata.FromOutgoingContext(ctx)
	if !ok {
		md = metadata.MD{}
	}
	md = md.Copy()
	// 对本键使用 Set，避免 interceptor 链重复追加多值；不触碰 x-request-id。
	md.Set(MetadataKey, id)
	md.Set(MetadataEagleEyeKey, id) // 云侧 / MSE 兼容
	return metadata.NewOutgoingContext(ctx, md)
}

// FromContext 优先入站 metadata，再出站 metadata，再 Value；metadata 多值取最后一个（append 语义）。
// 返回值均经 Sanitize，非法客户端值不会进入日志/响应。
func FromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	if md, ok := metadata.FromIncomingContext(ctx); ok {
		if id := SanitizeTraceID(lastMeta(md.Get(MetadataKey))); id != "" {
			return id
		}
		if id := SanitizeTraceID(lastMeta(md.Get(MetadataEagleEyeKey))); id != "" {
			return id
		}
	}
	if md, ok := metadata.FromOutgoingContext(ctx); ok {
		if id := SanitizeTraceID(lastMeta(md.Get(MetadataKey))); id != "" {
			return id
		}
		if id := SanitizeTraceID(lastMeta(md.Get(MetadataEagleEyeKey))); id != "" {
			return id
		}
	}
	if v, ok := ctx.Value(ctxKey{}).(string); ok {
		return SanitizeTraceID(v)
	}
	return ""
}

func lastMeta(vals []string) string {
	for i := len(vals) - 1; i >= 0; i-- {
		if vals[i] != "" {
			return vals[i]
		}
	}
	return ""
}

// NewID 生成新的 trace id。
func NewID() string {
	return uuid.NewString()
}

// Ensure 若 context 无 trace 则注入新 id。
func Ensure(ctx context.Context) context.Context {
	if FromContext(ctx) != "" {
		return ctx
	}
	return WithTrace(ctx, NewID())
}

// InjectIncomingFromMD 从入站 gRPC metadata 恢复 trace 到 Value + outgoing。
func InjectIncomingFromMD(ctx context.Context) context.Context {
	if md, ok := metadata.FromIncomingContext(ctx); ok {
		if id := SanitizeTraceID(lastMeta(md.Get(MetadataKey))); id != "" {
			return WithTrace(ctx, id)
		}
		if id := SanitizeTraceID(lastMeta(md.Get(MetadataEagleEyeKey))); id != "" {
			return WithTrace(ctx, id)
		}
	}
	id := SanitizeTraceID(FromContext(ctx))
	if id == "" {
		id = NewID()
	}
	return WithTrace(ctx, id)
}

// InjectOutgoing 确保 ctx 的出站 metadata 带 x-trace-id 与 eagleeye-traceid：
// 已有（HTTP 入站解析过，或上游 gRPC 传入）则复用，没有则新建。
// gRPC 客户端拦截器调用，保证跨服务调用不断链、上下游同一个 tid。
func InjectOutgoing(ctx context.Context) context.Context {
	return WithTrace(ctx, FromContext(ctx))
}

// ParseHTTPHeaders 云服务兼容：X-Trace-Id → EagleEye-TraceID → X-Request-Id → X-B3-TraceId → traceparent；皆无则 UUID。
func ParseHTTPHeaders(getHeader func(string) string) string {
	for _, key := range []string{
		HeaderTraceID,
		HeaderEagleEyeTraceID,
		HeaderRequestID,
		HeaderB3TraceID,
	} {
		if id := SanitizeTraceID(getHeader(key)); id != "" {
			return id
		}
	}
	if tp := getHeader(HeaderTraceParent); tp != "" {
		if id := SanitizeTraceID(splitTraceParent(tp)); id != "" {
			return id
		}
	}
	return NewID()
}

func splitTraceParent(tp string) string {
	parts := strings.Split(tp, "-")
	if len(parts) >= 2 && len(parts[1]) >= 16 {
		return parts[1]
	}
	return ""
}

// SetResponseHeaders 写出站 HTTP 响应头（业务 X-Trace-Id + 云 EagleEye）。
func SetResponseHeaders(w http.ResponseWriter, id string) {
	if id = SanitizeTraceID(id); id == "" {
		return
	}
	w.Header().Set(HeaderTraceID, id)
	w.Header().Set(HeaderEagleEyeTraceID, id)
}

// InjectHTTPHeaders 出站请求注入 Trace（Adapter / 调云 API 前调用）。
func InjectHTTPHeaders(h http.Header, ctx context.Context) {
	if h == nil {
		return
	}
	id := FromContext(ctx)
	if id == "" {
		return
	}
	h.Set(HeaderTraceID, id)
	h.Set(HeaderEagleEyeTraceID, id)
}

// Field 供 logger 使用。
func Field(ctx context.Context) logx.LogField {
	return logx.Field("trace_id", FromContext(ctx))
}
