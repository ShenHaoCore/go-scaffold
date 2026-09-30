package middleware

import (
	"fmt"
	"net"
	"net/http"
	"os"
	"path"
	"strings"

	"go-scaffold/pkg/auth"
	"go-scaffold/pkg/env"
	bizerr "go-scaffold/pkg/errors"
	"go-scaffold/pkg/response"
	"go-scaffold/pkg/secure"
)

const healthProbeTokenHeader = "X-Health-Probe-Token"

// AuthMiddleware 鉴权：Recovery→Trace→Lang→Auth→Agent。
// skipPaths 默认仅精确匹配 /health/live（存活探针；不含子路径）。
// /health（readiness）：
//   - 来自本机回环：允许（供 Pod 内 exec 探针，避免 Token 进 argv；
//     若同 Pod sidecar 也会表现为 127.0.0.1，须依赖 NetworkPolicy 限制谁能连业务端口）
//   - 否则须 X-Health-Probe-Token 匹配（已配置时）或走正常 Bearer
//   - 未配置 Token：non-prod 放行；prod 启动 CheckHealthProbeTokenForProd 强制配置
func AuthMiddleware(authenticator auth.Authenticator, skipPaths ...string) func(http.HandlerFunc) http.HandlerFunc {
	if authenticator == nil {
		panic(fmt.Sprintf("AuthMiddleware: authenticator is nil (APP_ENV=%s); inject via svc.NewServiceContext", os.Getenv("APP_ENV")))
	}
	if len(skipPaths) == 0 {
		skipPaths = []string{"/health/live"}
	}

	return func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if pathSkipped(r.URL.Path, skipPaths) {
				next(w, r)
				return
			}
			if healthProbeAllowed(r) {
				next(w, r)
				return
			}
			token := bearerToken(r.Header.Get("Authorization"))
			principal, err := authenticator.Authenticate(r.Context(), token)
			if err != nil {
				response.Error(r.Context(), w, err)
				return
			}
			if principal == nil {
				response.Error(r.Context(), w, bizerr.NewFromContext(r.Context(), bizerr.CodeUnauthorized, nil))
				return
			}
			ctx := auth.WithPrincipal(r.Context(), principal)
			next(w, r.WithContext(ctx))
		}
	}
}

// CheckHealthProbeTokenForProd：prod 必须配置 HEALTH_PROBE_TOKEN；
// 若 HEALTH_PROBE_REQUIRE_TOKEN=true（任意环境）亦须已配置 Token。
func CheckHealthProbeTokenForProd() error {
	if healthProbeRequireToken() && strings.TrimSpace(os.Getenv("HEALTH_PROBE_TOKEN")) == "" {
		return fmt.Errorf("health: HEALTH_PROBE_TOKEN is required when HEALTH_PROBE_REQUIRE_TOKEN=true")
	}
	if !env.IsProd() {
		return nil
	}
	if strings.TrimSpace(os.Getenv("HEALTH_PROBE_TOKEN")) == "" {
		return fmt.Errorf("health: HEALTH_PROBE_TOKEN is required when APP_ENV=prod (set Secret; in-pod probes may use loopback without header)")
	}
	return nil
}

func healthProbeAllowed(r *http.Request) bool {
	cleaned := path.Clean("/" + strings.TrimPrefix(r.URL.Path, "/"))
	if cleaned != "/health" {
		return false
	}
	want := strings.TrimSpace(os.Getenv("HEALTH_PROBE_TOKEN"))
	// HEALTH_PROBE_REQUIRE_TOKEN=true：强制比对 Token（含本机回环）；未配置 Token 则一律拒绝
	if healthProbeRequireToken() {
		if want == "" {
			return false
		}
		got := strings.TrimSpace(r.Header.Get(healthProbeTokenHeader))
		return secure.EqualString(got, want)
	}
	// 默认：本机回环放行（Pod 内探针免 argv 泄密）
	if isLoopbackAddr(r.RemoteAddr) {
		return true
	}
	if want == "" {
		return !env.IsProd()
	}
	got := strings.TrimSpace(r.Header.Get(healthProbeTokenHeader))
	return secure.EqualString(got, want)
}

func healthProbeRequireToken() bool {
	v := strings.TrimSpace(os.Getenv("HEALTH_PROBE_REQUIRE_TOKEN"))
	return strings.EqualFold(v, "true") || v == "1"
}

func isLoopbackAddr(remoteAddr string) bool {
	host := remoteAddr
	if h, _, err := net.SplitHostPort(remoteAddr); err == nil {
		host = h
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// pathSkipped：/health/live 仅精确匹配；其它 skip 前缀仍支持子路径。
func pathSkipped(raw string, prefixes []string) bool {
	cleaned := path.Clean("/" + strings.TrimPrefix(raw, "/"))
	if cleaned != "/" {
		cleaned = strings.TrimSuffix(cleaned, "/")
	}
	for _, p := range prefixes {
		if p == "" {
			continue
		}
		p = path.Clean("/" + strings.TrimPrefix(p, "/"))
		if p != "/" {
			p = strings.TrimSuffix(p, "/")
		}
		if p == "/health/live" {
			if cleaned == p {
				return true
			}
			continue
		}
		if cleaned == p || strings.HasPrefix(cleaned, p+"/") {
			return true
		}
	}
	return false
}

func bearerToken(h string) string {
	h = strings.TrimSpace(h)
	if h == "" {
		return ""
	}
	const prefix = "Bearer "
	if len(h) > len(prefix) && strings.EqualFold(h[:len(prefix)], prefix) {
		return strings.TrimSpace(h[len(prefix):])
	}
	return ""
}
