package errors

import (
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
)

// Kind 错误类型位：错误码的第 4 位，唯一决定该码的默认语义。
//
// 契约（不可破坏）：错误码形如 {3 位服务前缀}{1 位类型}{3 位编号}。
// 只有第 4 位参与语义推导；形非法的码一律按 KindSystem 处理，**不猜测**类型位。
type Kind byte

const (
	KindSystem Kind = '1' // 系统错误
	KindParam  Kind = '2' // 参数错误
	KindBiz    Kind = '3' // 业务错误
	KindAuth   Kind = '4' // 鉴权错误
	KindLimit  Kind = '5' // 限流错误
)

// defaultHTTP 各 Kind 的默认 HTTP 状态码。
// 需要更细的区分（如鉴权位下的 403）时，用 Spec.HTTP 在注册处覆盖——
// 不再在映射函数里写 `if code == 某常量` 这类特例。
var defaultHTTP = map[Kind]int{
	KindSystem: http.StatusInternalServerError,
	KindParam:  http.StatusBadRequest,
	KindBiz:    http.StatusUnprocessableEntity,
	KindAuth:   http.StatusUnauthorized,
	KindLimit:  http.StatusTooManyRequests,
}

// Spec 一个错误码的登记项。只需登记「与默认不同」的部分：
// 零值字段按 Code 的类型位推导，因此新增业务码通常只写 Code + 文案。
type Spec struct {
	Code string // 必填。{3 位前缀}{1 位类型}{3 位编号}
	Kind Kind   // 可选。非空时须与 Code 第 4 位一致，用于在登记处自证意图
	HTTP int    // 可选。覆盖 Kind 默认 HTTP 状态码
	ZhCN string // 可选。中文文案，占位用 text/template 语法 {{.Field}}
	EnUS string // 可选。英文文案
}

var (
	registryMu sync.RWMutex
	registry   = map[string]Spec{}
)

// Register 登记或覆盖一个错误码。并发安全，可在任意 init 中调用。
//
// 业务服务登记自身前缀的错误码时用它；脚手架只登记 COM*（见 builtin.go）。
// Code 为空、或 Kind 与 Code 类型位不一致时 panic——两者都是初始化期的编程错误，
// 静默忽略只会把问题推到运行时的错误响应的类型上。
func Register(spec Spec) {
	if strings.TrimSpace(spec.Code) == "" {
		panic("errors.Register: empty Code")
	}
	if spec.Kind != 0 {
		if got := ParseKind(spec.Code); got != spec.Kind {
			panic(fmt.Sprintf(
				"errors.Register: Code=%s 的类型位是 %c，与声明的 Kind=%c 不一致",
				spec.Code, byte(got), byte(spec.Kind)))
		}
	}
	registryMu.Lock()
	registry[spec.Code] = spec
	registryMu.Unlock()
}

// RegisterText 仅登记/覆盖文案，保留已有的 Kind/HTTP。
// 适用于文案由翻译流程产出、与码语义解耦的场景。
func RegisterText(code, zhCN, enUS string) {
	registryMu.Lock()
	s := registry[code]
	s.Code = code
	s.ZhCN, s.EnUS = zhCN, enUS
	registry[code] = s
	registryMu.Unlock()
}

// SpecOf 查询登记项。
func SpecOf(code string) (Spec, bool) {
	registryMu.RLock()
	s, ok := registry[code]
	registryMu.RUnlock()
	return s, ok
}

// ParseKind 取错误码的类型位；码形非法（长度不足或第 4 位不是已定义类型）时返回 KindSystem。
func ParseKind(code string) Kind {
	if len(code) < 4 {
		return KindSystem
	}
	k := Kind(code[3])
	if _, ok := defaultHTTP[k]; !ok {
		return KindSystem
	}
	return k
}

// KindOf 返回错误码的类型位：优先登记项声明的 Kind，否则按码形推导。
func KindOf(code string) Kind {
	if s, ok := SpecOf(code); ok && s.Kind != 0 {
		return s.Kind
	}
	return ParseKind(code)
}

// HTTPStatus 按「登记项覆盖 → Kind 默认 → 500」三级推导。
// 这是全仓唯一的错误码 → HTTP 状态映射点；gRPC 侧由 pkg/response 基于本结果一一对照。
func HTTPStatus(code string) int {
	if s, ok := SpecOf(code); ok && s.HTTP != 0 {
		return s.HTTP
	}
	if st, ok := defaultHTTP[KindOf(code)]; ok {
		return st
	}
	return http.StatusInternalServerError
}

// RegisteredCodes 返回已登记的错误码（升序），供启动自检与文档核对。
func RegisteredCodes() []string {
	registryMu.RLock()
	out := make([]string, 0, len(registry))
	for c := range registry {
		out = append(out, c)
	}
	registryMu.RUnlock()
	sort.Strings(out)
	return out
}
