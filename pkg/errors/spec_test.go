package errors_test

import (
	"testing"

	bizerr "micro-scaffold/pkg/errors"

	"github.com/stretchr/testify/require"
)

// 契约：错误码语义只有一个真源——注册表。
// 业务服务登记一次，HTTP 状态（以及基于它的 gRPC code）与文案都必须随之生效。
func TestRegister_BusinessCodeDrivesEverything(t *testing.T) {
	const code = "TST3007" // kind '3' = 业务错误 → 默认 422
	bizerr.Register(bizerr.Spec{
		Code: code,
		ZhCN: "订单 {{.No}} 状态不允许取消",
		EnUS: "Order {{.No}} cannot be cancelled",
	})

	require.Equal(t, 422, bizerr.HTTPStatus(code))
	require.Equal(t, bizerr.KindBiz, bizerr.KindOf(code))
	require.Contains(t, bizerr.Translate(code, "zh-CN", map[string]any{"No": "A1"}), "A1")
	require.Contains(t, bizerr.Translate(code, "en-US", nil), "cannot be cancelled")

	// 未登记但形合法的业务码：按类型位推导，不改注册表
	require.Equal(t, 422, bizerr.HTTPStatus("ZZZ3001"))
	require.Equal(t, "ZZZ3001", bizerr.Translate("ZZZ3001", "zh-CN", nil), "未登记码回退为原始 code")
}

// 契约：HTTP 覆盖项登记一次即全局生效——不再有 `if code == 某常量` 的特例分支。
func TestRegister_HTTPOverride(t *testing.T) {
	const code = "TST4009" // 默认鉴权位是 401
	bizerr.Register(bizerr.Spec{Code: code, HTTP: 403})

	require.Equal(t, 403, bizerr.HTTPStatus(code), "登记项的 HTTP 覆盖须优先生效")
	require.Equal(t, bizerr.KindAuth, bizerr.KindOf(code), "覆盖 HTTP 不改变类型位")
}

// 契约：形非法的错误码一律按系统错误（500）处理，不猜测类型位。
func TestHTTPStatus_MalformedCodeFailsClosed(t *testing.T) {
	require.Equal(t, 500, bizerr.HTTPStatus(""))
	require.Equal(t, 500, bizerr.HTTPStatus("COM"))
	require.Equal(t, 500, bizerr.HTTPStatus("COMX001"), "第 4 位非类型位")
	require.Equal(t, 500, bizerr.HTTPStatus("COM6001"), "第 4 位非已定义类型")
	require.Equal(t, bizerr.KindSystem, bizerr.ParseKind("COM6001"))
}

// 契约：Kind 与 Code 类型位不一致属初始化期编程错误，须尽早暴露而非静默登记。
func TestRegister_KindMismatchPanics(t *testing.T) {
	require.Panics(t, func() {
		bizerr.Register(bizerr.Spec{Code: "TST3001", Kind: bizerr.KindAuth})
	})
	require.Panics(t, func() {
		bizerr.Register(bizerr.Spec{Code: ""})
	})
}

// RegisterText 只动文案，不动语义。
func TestRegisterText_KeepsSemantics(t *testing.T) {
	const code = "TST2005"
	bizerr.Register(bizerr.Spec{Code: code, ZhCN: "旧文案"})
	require.Equal(t, 400, bizerr.HTTPStatus(code))

	bizerr.RegisterText(code, "新文案", "new text")
	require.Equal(t, 400, bizerr.HTTPStatus(code), "RegisterText 不得改动 Kind/HTTP")
	require.Equal(t, "新文案", bizerr.Translate(code, "zh-CN", nil))
	require.Equal(t, "new text", bizerr.Translate(code, "en-US", nil))
}

// 内置 COM* 全部经由注册表，可枚举核对（防止有人绕过注册表直接塞字典）。
func TestBuiltinCodesRegistered(t *testing.T) {
	codes := bizerr.RegisteredCodes()
	for _, want := range []string{
		bizerr.CodeInternal, bizerr.CodeRequired, bizerr.CodeInvalidParam,
		bizerr.CodeUnauthorized, bizerr.CodeForbidden, bizerr.CodeRateLimited,
	} {
		require.Contains(t, codes, want)
	}
}
