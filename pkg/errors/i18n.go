package errors

import (
	"bytes"
	"strings"
	"sync"
	"text/template"

	"github.com/zeromicro/go-zero/core/logx"
)

const DefaultLang = "zh-CN"

var tplCache sync.Map // key: tpl string → *template.Template

// Translate 取错误码在指定语言下的文案。
// 回退链：指定语言 → zh-CN → en-US → 原始 code（未登记时至少不丢码）。
// 文案唯一来源是注册表（见 spec.go / builtin.go），本包不再另存字典。
func Translate(code, lang string, params map[string]any) string {
	lang = normalizeLang(lang)
	msg := textFor(lang, code)
	if msg == "" && lang != DefaultLang {
		msg = textFor(DefaultLang, code)
	}
	if msg == "" && lang != "en-US" {
		msg = textFor("en-US", code)
	}
	if msg == "" {
		return code
	}
	return render(msg, code, params)
}

// textFor 按语言取登记文案；该语言无文案或码未登记时返回空串，由 Translate 继续回退。
func textFor(lang, code string) string {
	s, ok := SpecOf(code)
	if !ok {
		return ""
	}
	switch lang {
	case DefaultLang:
		return s.ZhCN
	case "en-US":
		return s.EnUS
	default:
		return ""
	}
}

// normalizeLang 规范化 Accept-Language 主标签：空→DefaultLang，zh*→zh-CN，en*→en-US。
func normalizeLang(lang string) string {
	lang = strings.TrimSpace(lang)
	if lang == "" {
		return DefaultLang
	}
	if i := strings.IndexByte(lang, ','); i >= 0 {
		lang = lang[:i]
	}
	if i := strings.IndexByte(lang, ';'); i >= 0 {
		lang = lang[:i]
	}
	lang = strings.TrimSpace(lang)
	switch {
	case strings.HasPrefix(strings.ToLower(lang), "zh"):
		return "zh-CN"
	case strings.HasPrefix(strings.ToLower(lang), "en"):
		return "en-US"
	default:
		return lang
	}
}

// render 执行模板。契约：模板非法或占位缺失时**不得 panic**，回退模板原文。
func render(tpl, code string, params map[string]any) string {
	if !strings.Contains(tpl, "{{") {
		return tpl
	}
	t, err := cachedTemplate(tpl)
	if err != nil {
		logx.Infow("i18n template parse failed, fallback to raw",
			logx.Field("level_hint", "warn"),
			logx.Field("code", code),
			logx.Field("err", err.Error()))
		return tpl
	}
	if params == nil {
		params = map[string]any{}
	}
	var buf bytes.Buffer
	if err := t.Execute(&buf, params); err != nil {
		logx.Infow("i18n template execute failed, fallback to raw",
			logx.Field("level_hint", "warn"),
			logx.Field("code", code),
			logx.Field("err", err.Error()))
		return tpl
	}
	return buf.String()
}

func cachedTemplate(tpl string) (*template.Template, error) {
	if v, ok := tplCache.Load(tpl); ok {
		return v.(*template.Template), nil
	}
	t, err := template.New("i18n").Option("missingkey=error").Parse(tpl)
	if err != nil {
		return nil, err
	}
	actual, _ := tplCache.LoadOrStore(tpl, t)
	return actual.(*template.Template), nil
}
