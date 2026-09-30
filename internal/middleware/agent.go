package middleware

import (
	"context"
	"net/http"
	"regexp"
	"unicode/utf8"
)

type agentKey struct{}

var agentIDPattern = regexp.MustCompile(`^[A-Za-z0-9._:@-]{1,128}$`)

// AgentMiddleware 强制链占位：可解析 X-Agent-Id 入 ctx；业务 logic 仍禁止依赖。
// 非法/过长 ID 忽略（不写入 ctx），避免任意 header 值进入 ctx 后被日志/业务输出。
// Phase 2 / MCP 在此替换为真实实现。
func AgentMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		agentID := r.Header.Get("X-Agent-Id")
		if agentID != "" && utf8.RuneCountInString(agentID) <= 128 && agentIDPattern.MatchString(agentID) {
			ctx := context.WithValue(r.Context(), agentKey{}, agentID)
			r = r.WithContext(ctx)
		}
		next(w, r)
	}
}

// AgentIDFromContext 读取 Agent 占位。
func AgentIDFromContext(ctx context.Context) string {
	v, _ := ctx.Value(agentKey{}).(string)
	return v
}
