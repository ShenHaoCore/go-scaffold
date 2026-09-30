package env_test

import (
	"testing"

	"go-scaffold/pkg/env"

	"github.com/stretchr/testify/require"
)

func TestAppEnvAliases(t *testing.T) {
	t.Setenv("APP_ENV", "production")
	e, err := env.ResolveAppEnv()
	require.NoError(t, err)
	require.Equal(t, "prod", e)
	require.True(t, env.IsProd())
	require.True(t, env.IsNonDev())
	require.Equal(t, "prod", env.AppEnv())

	t.Setenv("APP_ENV", "PROD")
	e, err = env.ResolveAppEnv()
	require.NoError(t, err)
	require.Equal(t, "prod", e)

	t.Setenv("APP_ENV", "development")
	e, err = env.ResolveAppEnv()
	require.NoError(t, err)
	require.Equal(t, "dev", e)
	require.False(t, env.IsProd())
	require.False(t, env.IsNonDev())

	t.Setenv("APP_ENV", "")
	e, err = env.ResolveAppEnv()
	require.NoError(t, err)
	require.Equal(t, "dev", e)

	t.Setenv("APP_ENV", "test")
	require.True(t, env.IsNonDev())
	require.False(t, env.IsProd())
}

func TestResolveAppEnvRejectsUnknown(t *testing.T) {
	t.Setenv("APP_ENV", "staging")
	_, err := env.ResolveAppEnv()
	require.Error(t, err)
	require.True(t, env.IsProd(), "illegal APP_ENV must fail-closed as prod")
	require.True(t, env.IsNonDev())
	require.Empty(t, env.AppEnv(), "must not silently fall back to dev")
}
