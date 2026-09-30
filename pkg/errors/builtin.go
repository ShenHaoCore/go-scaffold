package errors

import "net/http"

// 脚手架内置的 COM* 通用错误码。
//
// 一处定义、三处生效：Kind（由码位决定）+ HTTP（此处或 Kind 默认）+ 中英文文案。
// HTTP 与 gRPC 两侧的映射都从本表推导，因此不会出现「HTTP 改了 403、gRPC 还是 401」的漂移。
//
// 业务服务：在自己的 init 中 Register 业务前缀的码，勿改本文件。
// 前缀登记见 error-code-spec.md。
func init() {
	reg := func(spec Spec) { Register(spec) }

	reg(Spec{
		Code: CodeInternal,
		ZhCN: "服务内部错误",
		EnUS: "Internal server error",
	})
	reg(Spec{
		Code: CodeRequired,
		ZhCN: "参数 {{.Field}} 为必填",
		EnUS: "Field {{.Field}} is required",
	})
	reg(Spec{
		Code: CodeInvalidParam,
		ZhCN: "参数 {{.Field}} 格式不合法",
		EnUS: "Field {{.Field}} is invalid",
	})
	reg(Spec{
		Code: CodeUnauthorized,
		ZhCN: "未登录或 Token 已过期",
		EnUS: "Unauthorized or token expired",
	})
	// 鉴权位默认 401；403 属该位的细分，在登记处声明一次即可，
	// HTTP 与 gRPC（PermissionDenied）都会随之生效。
	reg(Spec{
		Code: CodeForbidden,
		HTTP: http.StatusForbidden,
		ZhCN: "无操作权限",
		EnUS: "Permission denied",
	})
	reg(Spec{
		Code: CodeRateLimited,
		ZhCN: "请求频率超限",
		EnUS: "Too many requests",
	})
}
