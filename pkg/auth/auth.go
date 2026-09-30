package auth

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"strings"

	"go-scaffold/pkg/env"
	bizerr "go-scaffold/pkg/errors"

	"github.com/zeromicro/go-zero/core/logx"
)

const (
	ModeDev     = "dev"     // 本地桩：无 Token 也放行
	ModeRequire = "require" // 强制 Bearer；Phase 1 仅为门闩，非真实鉴权
	ModeSDK     = "sdk"     // Phase 2 权限 SDK

	// Phase1AckValue prod 下除 AUTH_ALLOW_PHASE1_GATE 外须设置 AUTH_PHASE1_ACK 为此值，降低误开公网风险。
	Phase1AckValue = "I_UNDERSTAND_NOT_FOR_PUBLIC"
)

// Principal 鉴权后的主体（Phase 1 最小字段）。
type Principal struct {
	UserID string
	Roles  []string
}

type ctxKey struct{}

// WithPrincipal 将主体写入 context。
func WithPrincipal(ctx context.Context, p *Principal) context.Context {
	return context.WithValue(ctx, ctxKey{}, p)
}

// FromContext 读取主体；未鉴权返回 nil。
func FromContext(ctx context.Context) *Principal {
	if ctx == nil {
		return nil
	}
	p, _ := ctx.Value(ctxKey{}).(*Principal)
	return p
}

// Authenticator 鉴权接口；Phase 2 可换 SDK 实现（Mode=sdk）。
type Authenticator interface {
	Authenticate(ctx context.Context, token string) (*Principal, error)
}

// Authorizer 资源鉴权挂载点（Phase 2 Mode=sdk 注入；Phase 1 可为 nil）。
// 由 logic 或路由级中间件调用；无权限须返回 COM4002。
type Authorizer interface {
	Authorize(ctx context.Context, p *Principal, resource, action string) error
}

// Authorize 安全调用：Authorizer 为 nil 时直接放行（Phase 1 / 未接入 SDK）。
func Authorize(ctx context.Context, az Authorizer, p *Principal, resource, action string) error {
	if az == nil {
		return nil
	}
	return az.Authorize(ctx, p, resource, action)
}

// DevStub 本地开发桩：无 Token 也放行（假用户）；有 Token 时 UserID 为指纹（不落明文）。
// 生产禁止使用（见 NewAuthenticator）；logic 禁止直接解析 Token。
type DevStub struct{}

func (DevStub) Authenticate(ctx context.Context, token string) (*Principal, error) {
	token = strings.TrimSpace(token)
	if token == "" {
		return &Principal{UserID: "dev", Roles: []string{"admin"}}, nil
	}
	return &Principal{UserID: tokenFingerprint(token), Roles: []string{"admin"}}, nil
}

// Phase1Gate Phase 1「仅校验 Token 非空」门闩：不授 admin，不作 JWT/权限校验。
// UserID 为 token 的短哈希前缀，避免日志泄露完整 Bearer。
type Phase1Gate struct{}

func (Phase1Gate) Authenticate(ctx context.Context, token string) (*Principal, error) {
	token = strings.TrimSpace(token)
	if token == "" {
		lang := bizerr.LangFromContext(ctx)
		return nil, bizerr.New(bizerr.CodeUnauthorized, lang, nil)
	}
	return &Principal{UserID: tokenFingerprint(token), Roles: []string{"authenticated"}}, nil
}

func tokenFingerprint(token string) string {
	sum := sha256.Sum256([]byte(token))
	return "tok_" + hex.EncodeToString(sum[:8])
}

// RequireAuth 强制要求非空 Token；失败返回 COM4001。
type RequireAuth struct {
	Inner Authenticator
}

func (r RequireAuth) Authenticate(ctx context.Context, token string) (*Principal, error) {
	if strings.TrimSpace(token) == "" {
		lang := bizerr.LangFromContext(ctx)
		return nil, bizerr.New(bizerr.CodeUnauthorized, lang, nil)
	}
	inner := r.Inner
	if inner == nil {
		inner = Phase1Gate{}
	}
	return inner.Authenticate(ctx, token)
}

// NewAuthenticator 按 Mode 构造鉴权器。
// APP_ENV=test|prod 禁止 Mode=dev；Mode=require 须显式 AUTH_ALLOW_PHASE1_GATE=true（非真实鉴权，禁公网）。
func NewAuthenticator(mode string) (Authenticator, error) {
	if _, err := env.ResolveAppEnv(); err != nil {
		return nil, err
	}
	mode = strings.ToLower(strings.TrimSpace(mode))
	if mode == "" {
		mode = ModeDev
	}
	if env.IsNonDev() && mode == ModeDev {
		return nil, fmt.Errorf("auth: Mode=%s is forbidden when APP_ENV=%s; use Mode=%s (Phase1 gate) or %s", ModeDev, env.AppEnv(), ModeRequire, ModeSDK)
	}
	switch mode {
	case ModeDev:
		return DevStub{}, nil
	case ModeRequire:
		if env.IsNonDev() {
			if !phase1GateAllowed() {
				return nil, fmt.Errorf("auth: Mode=%s forbidden when APP_ENV=%s without AUTH_ALLOW_PHASE1_GATE=true (Phase-1 gate only; do not expose publicly; prefer Mode=%s)", ModeRequire, env.AppEnv(), ModeSDK)
			}
			if env.IsProd() && !phase1AckOK() {
				return nil, fmt.Errorf("auth: prod Mode=%s also requires AUTH_PHASE1_ACK=%s (explicit ack: not for public ingress; prefer Mode=%s)", ModeRequire, Phase1AckValue, ModeSDK)
			}
			msg := "SECURITY: Auth.Mode=require is Phase-1 gate only (any non-empty Bearer → Roles=authenticated, NOT real JWT/RBAC). Do NOT expose via public Ingress; switch to Mode=sdk when permission service is ready."
			if env.IsProd() {
				logx.Severe(msg)
			} else {
				logx.Infof("[warn] %s", msg)
			}
		}
		return RequireAuth{Inner: Phase1Gate{}}, nil
	case ModeSDK:
		return nil, fmt.Errorf("auth: Mode=%s not ready; permission SDK not wired yet, meanwhile use %s (or %s locally)", ModeSDK, ModeRequire, ModeDev)
	default:
		return nil, fmt.Errorf("auth: unknown Mode=%q (want %s|%s|%s)", mode, ModeDev, ModeRequire, ModeSDK)
	}
}

func phase1GateAllowed() bool {
	v := strings.TrimSpace(os.Getenv("AUTH_ALLOW_PHASE1_GATE"))
	return strings.EqualFold(v, "true") || v == "1"
}

func phase1AckOK() bool {
	return strings.TrimSpace(os.Getenv("AUTH_PHASE1_ACK")) == Phase1AckValue
}
