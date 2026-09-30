package middleware

import (
	"net/http"

	"micro-scaffold/pkg/trace"
)

// TraceMiddleware 解析入站 Trace Header（含云 EagleEye / B3 / W3C）并写入 context + metadata。
func TraceMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := trace.ParseHTTPHeaders(r.Header.Get)
		ctx := trace.WithTrace(r.Context(), id)
		trace.SetResponseHeaders(w, id)
		next(w, r.WithContext(ctx))
	}
}
