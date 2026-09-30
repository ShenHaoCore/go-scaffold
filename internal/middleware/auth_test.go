package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"micro-scaffold/internal/middleware"
	"micro-scaffold/pkg/auth"
	bizerr "micro-scaffold/pkg/errors"

	"github.com/stretchr/testify/require"
)

func TestAuthMiddleware_SkipHealthLive(t *testing.T) {
	t.Setenv("APP_ENV", "dev")
	t.Setenv("HEALTH_PROBE_TOKEN", "")
	called := false
	h := middleware.AuthMiddleware(auth.Phase1Gate{}, "/health/live")(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})
	req := httptest.NewRequest(http.MethodGet, "/health/live", nil)
	rr := httptest.NewRecorder()
	h(rr, req)
	require.True(t, called)
	require.Equal(t, http.StatusOK, rr.Code)
}

func TestAuthMiddleware_HealthProbeToken(t *testing.T) {
	t.Setenv("APP_ENV", "dev")
	t.Setenv("HEALTH_PROBE_TOKEN", "probe-secret")
	called := false
	h := middleware.AuthMiddleware(auth.Phase1Gate{}, "/health/live")(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rr := httptest.NewRecorder()
	h(rr, req)
	require.False(t, called)
	require.Equal(t, http.StatusUnauthorized, rr.Code)

	req2 := httptest.NewRequest(http.MethodGet, "/health", nil)
	req2.Header.Set("X-Health-Probe-Token", "probe-secret")
	rr2 := httptest.NewRecorder()
	h(rr2, req2)
	require.True(t, called)
	require.Equal(t, http.StatusOK, rr2.Code)
}

func TestAuthMiddleware_HealthLoopbackWithoutToken(t *testing.T) {
	t.Setenv("APP_ENV", "prod")
	t.Setenv("HEALTH_PROBE_TOKEN", "probe-secret")
	t.Setenv("HEALTH_PROBE_REQUIRE_TOKEN", "")
	called := false
	h := middleware.AuthMiddleware(auth.Phase1Gate{}, "/health/live")(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	req.RemoteAddr = "127.0.0.1:45678"
	rr := httptest.NewRecorder()
	h(rr, req)
	require.True(t, called)
	require.Equal(t, http.StatusOK, rr.Code)
}

func TestAuthMiddleware_RequireTokenWithoutSecretRejects(t *testing.T) {
	t.Setenv("APP_ENV", "dev")
	t.Setenv("HEALTH_PROBE_TOKEN", "")
	t.Setenv("HEALTH_PROBE_REQUIRE_TOKEN", "true")
	called := false
	h := middleware.AuthMiddleware(auth.Phase1Gate{}, "/health/live")(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	req.RemoteAddr = "127.0.0.1:45678"
	rr := httptest.NewRecorder()
	h(rr, req)
	require.False(t, called)
	require.Equal(t, http.StatusUnauthorized, rr.Code)

	err := middleware.CheckHealthProbeTokenForProd()
	require.Error(t, err)
	require.Contains(t, err.Error(), "HEALTH_PROBE_REQUIRE_TOKEN")
}

func TestAuthMiddleware_HealthLoopbackRequireToken(t *testing.T) {
	t.Setenv("APP_ENV", "prod")
	t.Setenv("HEALTH_PROBE_TOKEN", "probe-secret")
	t.Setenv("HEALTH_PROBE_REQUIRE_TOKEN", "true")
	called := false
	h := middleware.AuthMiddleware(auth.Phase1Gate{}, "/health/live")(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	req.RemoteAddr = "127.0.0.1:45678"
	rr := httptest.NewRecorder()
	h(rr, req)
	require.False(t, called)
	require.Equal(t, http.StatusUnauthorized, rr.Code)

	req2 := httptest.NewRequest(http.MethodGet, "/health", nil)
	req2.RemoteAddr = "127.0.0.1:45678"
	req2.Header.Set("X-Health-Probe-Token", "probe-secret")
	rr2 := httptest.NewRecorder()
	h(rr2, req2)
	require.True(t, called)
	require.Equal(t, http.StatusOK, rr2.Code)
}

func TestAuthMiddleware_HealthLiveExactOnly(t *testing.T) {
	t.Setenv("APP_ENV", "dev")
	t.Setenv("HEALTH_PROBE_TOKEN", "")
	h := middleware.AuthMiddleware(auth.RequireAuth{Inner: auth.Phase1Gate{}}, "/health/live")(
		func(w http.ResponseWriter, r *http.Request) {
			t.Fatal("should not skip /health/live/extra")
		})
	req := httptest.NewRequest(http.MethodGet, "/health/live/extra", nil)
	rr := httptest.NewRecorder()
	h(rr, req)
	require.Equal(t, http.StatusUnauthorized, rr.Code)
}

func TestAuthMiddleware_RejectEmpty(t *testing.T) {
	t.Setenv("APP_ENV", "dev")
	h := middleware.AuthMiddleware(auth.Phase1Gate{}, "/health/live")(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("should not reach handler")
	})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/hello", nil)
	rr := httptest.NewRecorder()
	h(rr, req)
	require.Equal(t, http.StatusUnauthorized, rr.Code)
}

func TestAuthMiddleware_AcceptBearer(t *testing.T) {
	t.Setenv("APP_ENV", "dev")
	var got *auth.Principal
	h := middleware.AuthMiddleware(auth.Phase1Gate{}, "/health/live")(func(w http.ResponseWriter, r *http.Request) {
		got = auth.FromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/resource", nil)
	req.Header.Set("Authorization", "Bearer user-1")
	rr := httptest.NewRecorder()
	h(rr, req)
	require.Equal(t, http.StatusOK, rr.Code)
	require.NotNil(t, got)
	require.True(t, strings.HasPrefix(got.UserID, "tok_"))
}

func TestAuthMiddleware_RejectNonBearer(t *testing.T) {
	t.Setenv("APP_ENV", "dev")
	h := middleware.AuthMiddleware(auth.Phase1Gate{}, "/health/live")(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("should not reach handler")
	})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/hello", nil)
	req.Header.Set("Authorization", "raw-token-without-bearer")
	rr := httptest.NewRecorder()
	h(rr, req)
	require.Equal(t, http.StatusUnauthorized, rr.Code)
}

func TestAuthMiddleware_NilPanics(t *testing.T) {
	require.Panics(t, func() {
		_ = middleware.AuthMiddleware(nil)
	})
}

func TestAuthMiddleware_RejectPathTraversalSkip(t *testing.T) {
	t.Setenv("APP_ENV", "dev")
	h := middleware.AuthMiddleware(auth.Phase1Gate{}, "/health/live")(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("should not skip auth via /health/../ traversal")
	})
	req := httptest.NewRequest(http.MethodGet, "/health/../api/v1/hello", nil)
	rr := httptest.NewRecorder()
	h(rr, req)
	require.Equal(t, http.StatusUnauthorized, rr.Code)
}

func TestAuthMiddleware_BizErrorBody(t *testing.T) {
	t.Setenv("APP_ENV", "dev")
	h := middleware.AuthMiddleware(auth.RequireAuth{Inner: auth.Phase1Gate{}}, "/health/live")(
		func(w http.ResponseWriter, r *http.Request) {},
	)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/x", nil)
	rr := httptest.NewRecorder()
	h(rr, req)
	require.Contains(t, rr.Body.String(), bizerr.CodeUnauthorized)
}
