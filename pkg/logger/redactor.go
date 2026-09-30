package logger

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/zeromicro/go-zero/core/logx"
)

// DefaultMaskRedactor 对敏感字段名做掩码（password/token/secret/authorization 等），
// 并对 message 中的常见密钥形态做轻量正则替换（覆盖 Infof 拼进正文的情况）。
type DefaultMaskRedactor struct{}

// 消息体里的敏感键名。键与值两侧的引号可选，因此 JSON（`"password":"x"`）、
// %v 打印结构体（`AccessKeySecret:x`）与 env 风格（`DB_PASS=x`）都能命中。
const secretKeyAlternation = `password|passwd|pass|pwd|secret|token|` +
	`access[_-]?token|refresh[_-]?token|access[_-]?key(?:[_-]?(?:id|secret))?|` +
	`api[_-]?key|private[_-]?key|credential|session[_-]?id`

var (
	reDSNPassword = regexp.MustCompile(`(?i)(postgres(?:ql)?://[^:/?\s]+:)([^@/\s]+)(@)`)
	reRedisURL    = regexp.MustCompile(`(?i)(rediss?://[^:/?\s]*:)([^@/\s]+)(@)`)
	reBearer      = regexp.MustCompile(`(?i)(bearer\s+)([A-Za-z0-9\-._~+/]+=*)`)

	// reSensitiveHeader：Authorization / Cookie 的值不止一个 token
	// （Basic <base64>、Bearer 之后的附加段、Cookie 的 name=value 列表），
	// 只掩第一个词会把可逆的 base64 留在日志里，因此掩到引号/方括号/行尾为止。
	reSensitiveHeader = regexp.MustCompile(`(?i)(["']?(?:authorization|proxy-authorization|cookie|set-cookie)["']?\s*[=:]\s*["']?)([^\r\n"'\]]+)`)

	// reAssignSecretQuoted 先处理引号包裹的值（允许含空格）：`password: "my secret"`。
	reAssignSecretQuoted = regexp.MustCompile(`(?i)(["']?\b(?:` + secretKeyAlternation + `)["']?\s*[=:]\s*["'])([^"'\r\n]*)`)
	// reAssignSecret 再处理裸值：值止于空白 / 逗号 / 分号 / 引号 / 括号。
	reAssignSecret = regexp.MustCompile(`(?i)(["']?\b(?:` + secretKeyAlternation + `)["']?\s*[=:]\s*)([^\s,;"'}\]]+)`)
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
		if s, ok := f.Value.(string); ok {
			if s == "" {
				out[i] = f
				continue
			}
			out[i] = logx.Field(f.Key, redactMessage(s))
			continue
		}
		// 非字符串值（error / struct / map / 配置对象）此前完全不过扫描，
		// 而 %+v 形态恰恰会带出 AccessKeySecret、DSN 口令等。
		// 这里只在**真的扫出敏感形态**时才降级为字符串，避免无谓地改变日志字段类型
		// （数字/布尔/无敏感内容的结构体原样保留）。
		if f.Value == nil {
			out[i] = f
			continue
		}
		rendered := safeSprint(f.Value)
		if rendered == "" {
			out[i] = f
			continue
		}
		if masked := redactMessage(rendered); masked != rendered {
			out[i] = logx.Field(f.Key, masked)
			continue
		}
		out[i] = f
	}
	return out
}

// safeSprint 渲染非字符串字段值。%+v 保留结构体字段名（否则无法判断哪个值敏感）；
// 日志路径不允许 panic，故兜底为「放弃渲染」。
//
// 边界：defer/recover 只能捕获值自身的 String()/Error()/Format() 抛出的 panic。
// **自引用结构导致的栈溢出是 fatal error（"goroutine stack exceeds ... limit"），
// recover 拦不住**——fmt 的 printValue 只递增 depth、不做上限判断。
// 因此不要把带环的对象交给本函数；需要时先自行裁剪。
func safeSprint(v any) (s string) {
	defer func() {
		if recover() != nil {
			s = ""
		}
	}()
	return fmt.Sprintf("%+v", v)
}

func redactMessage(s string) string {
	s = reDSNPassword.ReplaceAllString(s, `${1}***${3}`)
	s = reRedisURL.ReplaceAllString(s, `${1}***${3}`)
	s = reSensitiveHeader.ReplaceAllString(s, `${1}***`)
	s = reBearer.ReplaceAllString(s, `${1}***`)
	s = reAssignSecretQuoted.ReplaceAllString(s, `${1}***`)
	s = reAssignSecret.ReplaceAllString(s, `${1}***`)
	return s
}

// normalizeFieldKey 归一化字段名：小写并去掉 - _ . 空格分隔符，
// 使 access_key_secret / accessKeySecret / ACCESS-KEY-SECRET 落到同一形态。
func normalizeFieldKey(key string) string {
	k := strings.ToLower(strings.TrimSpace(key))
	if k == "" {
		return ""
	}
	return strings.NewReplacer("-", "", "_", "", ".", "", " ", "").Replace(k)
}

var sensitiveExactKeys = map[string]bool{
	"pass": true, "pwd": true, "pw": true, "passwd": true, "password": true,
	"secret": true, "token": true, "credential": true, "authorization": true,
	"cookie": true, "setcookie": true, "session": true, "sessionid": true,
	"dsn": true, "datasource": true, "apikey": true, "privatekey": true,
	"accesskey": true, "accesskeyid": true, "accesskeysecret": true,
}

// 子串命中项。归一化已剥掉分隔符，所以 `db_dsn` → `dbdsn`、`db_password` → `dbpassword`
// 这类复合名必须靠子串（或后缀）才能识别——精确表只覆盖裸名。
// `dsn` / `datasource` 此前只有精确项，导致 `DB_DSN` / `DB_DSN_SQL` 漏掩（见 TestDefaultMaskRedactor_KeyVariants）。
var sensitiveContainsKeys = []string{
	"password", "passwd", "pwd", "secret", "token", "credential",
	"authorization", "cookie", "apikey", "accesskey", "privatekey", "session",
	"dsn", "datasource",
}

func sensitiveFieldKey(key string) bool {
	k := normalizeFieldKey(key)
	if k == "" {
		return false
	}
	if sensitiveExactKeys[k] {
		return true
	}
	for _, kw := range sensitiveContainsKeys {
		if strings.Contains(k, kw) {
			return true
		}
	}
	// 后缀兜底用复合命名（db_pass / redis_pwd / dbpass）。
	// 代价是 compass / bypass 这类同后缀词会被误掩——宁可多掩，不可漏掩。
	return strings.HasSuffix(k, "pass") || strings.HasSuffix(k, "pwd")
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
