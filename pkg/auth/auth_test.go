package auth_test

import (
	"context"
	"os"
	"strings"
	"testing"

	"micro-scaffold/pkg/auth"
	bizerr "micro-scaffold/pkg/errors"

	"github.com/stretchr/testify/require"
)

func TestAuthorize_NilPassThrough(t *testing.T) {
	err := auth.Authorize(context.Background(), nil, &auth.Principal{UserID: "u"}, "product", "read")
	require.NoError(t, err)
}

type denyAuthorizer struct{}

func (denyAuthorizer) Authorize(ctx context.Context, p *auth.Principal, resource, action string) error {
	return bizerr.New(bizerr.CodeForbidden, "zh-CN", nil)
}

func TestAuthorize_Delegates(t *testing.T) {
	err := auth.Authorize(context.Background(), denyAuthorizer{}, &auth.Principal{UserID: "u"}, "product", "write")
	require.Error(t, err)
	be, ok := bizerr.AsBizError(err)
	require.True(t, ok)
	require.Equal(t, bizerr.CodeForbidden, be.Code)
}

func TestDevStub(t *testing.T) {
	p, err := auth.DevStub{}.Authenticate(context.Background(), "")
	require.NoError(t, err)
	require.Equal(t, "dev", p.UserID)

	p2, err := auth.DevStub{}.Authenticate(context.Background(), "u1")
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(p2.UserID, "tok_"))
	require.NotEqual(t, "u1", p2.UserID)
	require.Contains(t, p2.Roles, "admin")
}

func TestPhase1Gate(t *testing.T) {
	_, err := auth.Phase1Gate{}.Authenticate(context.Background(), "")
	require.Error(t, err)

	p, err := auth.Phase1Gate{}.Authenticate(context.Background(), "tok")
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(p.UserID, "tok_"))
	require.NotEqual(t, "tok", p.UserID)
	require.Equal(t, []string{"authenticated"}, p.Roles)
	require.NotContains(t, p.Roles, "admin")
}

func TestRequireAuth(t *testing.T) {
	_, err := auth.RequireAuth{}.Authenticate(context.Background(), "")
	require.Error(t, err)
	be, ok := bizerr.AsBizError(err)
	require.True(t, ok)
	require.Equal(t, bizerr.CodeUnauthorized, be.Code)

	p, err := auth.RequireAuth{}.Authenticate(context.Background(), "tok")
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(p.UserID, "tok_"))
	require.Equal(t, []string{"authenticated"}, p.Roles)
}

func TestNewAuthenticatorProdForbidsDev(t *testing.T) {
	t.Setenv("APP_ENV", "prod")
	t.Setenv("AUTH_ALLOW_PHASE1_GATE", "")
	_, err := auth.NewAuthenticator(auth.ModeDev)
	require.Error(t, err)

	_, err = auth.NewAuthenticator(auth.ModeRequire)
	require.Error(t, err, "prod+require 须 AUTH_ALLOW_PHASE1_GATE")

	t.Setenv("AUTH_ALLOW_PHASE1_GATE", "true")
	_, err = auth.NewAuthenticator(auth.ModeRequire)
	require.Error(t, err, "prod+require 还须 AUTH_PHASE1_ACK")

	t.Setenv("AUTH_PHASE1_ACK", auth.Phase1AckValue)
	a, err := auth.NewAuthenticator(auth.ModeRequire)
	require.NoError(t, err)
	require.NotNil(t, a)
	p, err := a.Authenticate(context.Background(), "x")
	require.NoError(t, err)
	require.NotContains(t, p.Roles, "admin")
}

func TestNewAuthenticatorTestRequiresPhase1Gate(t *testing.T) {
	t.Setenv("APP_ENV", "test")
	t.Setenv("AUTH_ALLOW_PHASE1_GATE", "")
	_, err := auth.NewAuthenticator(auth.ModeRequire)
	require.Error(t, err, "test+require 须 AUTH_ALLOW_PHASE1_GATE")

	t.Setenv("AUTH_ALLOW_PHASE1_GATE", "true")
	a, err := auth.NewAuthenticator(auth.ModeRequire)
	require.NoError(t, err)
	require.NotNil(t, a)
}

func TestNewAuthenticatorTestForbidsDev(t *testing.T) {
	t.Setenv("APP_ENV", "test")
	_, err := auth.NewAuthenticator(auth.ModeDev)
	require.Error(t, err)
}

func TestNewAuthenticatorProductionAliasForbidsDev(t *testing.T) {
	t.Setenv("APP_ENV", "production")
	_, err := auth.NewAuthenticator(auth.ModeDev)
	require.Error(t, err)
}

func TestNewAuthenticatorRejectsUnknownAppEnv(t *testing.T) {
	t.Setenv("APP_ENV", "staging")
	_, err := auth.NewAuthenticator(auth.ModeDev)
	require.Error(t, err)
}

func TestNewAuthenticatorDev(t *testing.T) {
	_ = os.Unsetenv("APP_ENV")
	t.Setenv("APP_ENV", "dev")
	a, err := auth.NewAuthenticator(auth.ModeDev)
	require.NoError(t, err)
	_, ok := a.(auth.DevStub)
	require.True(t, ok)
}

func TestPrincipalContext(t *testing.T) {
	ctx := auth.WithPrincipal(context.Background(), &auth.Principal{UserID: "x"})
	require.Equal(t, "x", auth.FromContext(ctx).UserID)
}
