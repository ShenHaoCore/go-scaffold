package errors

import (
	"context"
)

type langKey struct{}

// WithLang 将语言写入 context（由 Lang 中间件调用）。
func WithLang(ctx context.Context, lang string) context.Context {
	return context.WithValue(ctx, langKey{}, lang)
}

// LangFromContext 读取语言；空/非法回退 DefaultLang，并对 Accept-Language 主标签做与
// Translate 一致的规范化（zh-CN,zh;q=0.9 → zh-CN），避免两处重复实现漂移。
func LangFromContext(ctx context.Context) string {
	if ctx == nil {
		return DefaultLang
	}
	v, _ := ctx.Value(langKey{}).(string)
	return normalizeLang(v)
}
