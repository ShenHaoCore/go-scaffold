package logger

import (
	"regexp"
	"strings"

	"github.com/zeromicro/go-zero/core/logx"
)

// DefaultMaskRedactor 对敏感字段名做掩码（password/token/secret/authorization 等），
// 并对 message 中的常见密钥形态做轻量正则替换（覆盖 Infof 拼进正文的情况）。
type DefaultMaskRedactor struct{}

var (
	reDSNPassword  = regexp.MustCompile(`(?i)(postgres(?:ql)?://[^:/?\s]+:)([^@/\s]+)(@)`)
	reRedisURL     = regexp.MustCompile(`(?i)(rediss?://[^:/?\s]*:)([^@/\s]+)(@)`)
	reBearer       = regexp.MustCompile(`(?i)(bearer\s+)([A-Za-z0-9\-._~+/]+=*)`)
	reAssignSecret = regexp.MustCompile(`(?i)\b(password|passwd|secret|token|access_token|refresh_token|access[_-]?key(?:_id|_secret)?|api[_-]?key|private[_-]?key)\b(\s*[=:]\s*)([^\s,;]+)`)
)

func (DefaultMaskRedactor) Redact(fields []logx.LogField) []logx.LogField {
	if len(fields) == 0 {
		return fields
	}
	out := make([]logx.LogField, len(fields))
	for i, f := range fields {
		if sensitiveFieldKey(f.Key) {
			out[i] = logx.Field(f.Key, "***")
			continue
		}
		if s, ok := f.Value.(string); ok && s != "" {
			out[i] = logx.Field(f.Key, redactMessage(s))
			continue
		}
		out[i] = f
	}
	return out
}

func redactMessage(s string) string {
	s = reDSNPassword.ReplaceAllString(s, `${1}***${3}`)
	s = reRedisURL.ReplaceAllString(s, `${1}***${3}`)
	s = reBearer.ReplaceAllString(s, `${1}***`)
	s = reAssignSecret.ReplaceAllString(s, `${1}${2}***`)
	return s
}

func sensitiveFieldKey(key string) bool {
	k := strings.ToLower(strings.TrimSpace(key))
	switch {
	case k == "password", k == "passwd", k == "secret", k == "token",
		k == "access_token", k == "refresh_token", k == "authorization",
		k == "api_key", k == "apikey", k == "private_key", k == "dsn", k == "data_source",
		k == "access_key", k == "access_key_id", k == "access_key_secret", k == "accesskeyid", k == "accesskeysecret":
		return true
	case strings.Contains(k, "password"), strings.Contains(k, "secret"),
		strings.Contains(k, "token"), strings.Contains(k, "authorization"),
		strings.Contains(k, "access_key"), strings.Contains(k, "accesskey"):
		return true
	default:
		return false
	}
}

// ApplyEnvRedactor 按环境注入脱敏器：test|prod 用 DefaultMaskRedactor，dev 用 Noop。
func ApplyEnvRedactor(appEnv string) {
	switch strings.ToLower(strings.TrimSpace(appEnv)) {
	case "prod", "production", "test":
		SetRedactor(DefaultMaskRedactor{})
	default:
		SetRedactor(NoopRedactor{})
	}
}

// RedactString 对自由文本做脱敏（供裸 logx 路径复用；Noop 时原样返回）。
func RedactString(s string) string {
	if r, ok := currentRedactor().(interface{ RedactMessage(string) string }); ok {
		return r.RedactMessage(s)
	}
	redacted := currentRedactor().Redact([]logx.LogField{logx.Field("_msg", s)})
	if len(redacted) == 1 {
		if out, ok := redacted[0].Value.(string); ok {
			return out
		}
	}
	return s
}

// RedactMessage 供 DefaultMaskRedactor 与 RedactString 统一调用。
func (DefaultMaskRedactor) RedactMessage(s string) string {
	return redactMessage(s)
}
