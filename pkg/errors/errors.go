package errors

import (
	"context"
	stderrors "errors"
	"fmt"
)

// BizError 业务错误，可携带错误码、文案与可选 detail。
type BizError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Detail  any    `json:"detail,omitempty"`
	HTTP    int    `json:"-"`

	// cause 保留底层错误（如客户端侧的 gRPC status），使 errors.Is/As 与
	// status.FromError 在包装后依然可用。不参与 JSON、不对外暴露。
	cause error `json:"-"`
}

func (e *BizError) Error() string {
	if e == nil {
		return ""
	}
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

// Unwrap 暴露底层错误，供 errors.Is / errors.As / status.FromError 下钻。
// 注意：调用方若要判断"错误本身是不是 gRPC status"，不能用 status.FromError
// （它会顺着 cause 找到原始 status），须直接做类型断言，见 grpcx.isPassthroughErr。
func (e *BizError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.cause
}

// WithCause 附加底层错误并返回自身。
func (e *BizError) WithCause(cause error) *BizError {
	if e == nil {
		return nil
	}
	e.cause = cause
	return e
}

// 通用错误码（COM）
const (
	CodeInternal     = "COM1001"
	CodeRequired     = "COM2001"
	CodeInvalidParam = "COM2002"
	CodeUnauthorized = "COM4001"
	CodeForbidden    = "COM4002"
	// CodeRateLimited 限流预留码；Phase 1 未实现限流中间件，勿当作已具备限流能力。
	CodeRateLimited = "COM5001"
)

// 业务服务错误码前缀由各服务登记；脚手架仅保留 COM*。

// New 按错误码 + 语言构造 BizError；params 供 i18n 模板使用。
func New(code, lang string, params map[string]any) *BizError {
	msg := Translate(code, lang, params)
	return &BizError{
		Code:    code,
		Message: msg,
		HTTP:    HTTPStatus(code),
	}
}

// NewFromContext 从 context 读取语言（Accept-Language / WithLang），构造 BizError。
func NewFromContext(ctx context.Context, code string, params map[string]any) *BizError {
	return New(code, LangFromContext(ctx), params)
}

// WithDetail 附加可选 detail。
func (e *BizError) WithDetail(detail any) *BizError {
	if e == nil {
		return nil
	}
	e.Detail = detail
	return e
}

// AsBizError 尝试断言为 BizError（支持 errors.As 解包）。
func AsBizError(err error) (*BizError, bool) {
	if err == nil {
		return nil, false
	}
	var be *BizError
	if stderrors.As(err, &be) {
		return be, true
	}
	return nil, false
}
