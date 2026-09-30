package middleware

import (
	"net/http"

	bizerr "micro-scaffold/pkg/errors"
)

// LangMiddleware 将 Accept-Language 写入 context，供 i18n / NewFromContext 使用。
// 读语言请用 bizerr.LangFromContext / NewFromContext，勿在此包另造 API。
func LangMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		lang := r.Header.Get("Accept-Language")
		ctx := bizerr.WithLang(r.Context(), lang)
		next(w, r.WithContext(ctx))
	}
}
