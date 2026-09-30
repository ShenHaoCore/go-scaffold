package model

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestComposeAliyunRedisPass(t *testing.T) {
	require.Equal(t, "secret", ComposeAliyunRedisPass("", "secret"))
	require.Equal(t, "pms_user:s3cret", ComposeAliyunRedisPass("pms_user", "s3cret"))
	require.Equal(t, "pms_user:already", ComposeAliyunRedisPass("pms_user", "pms_user:already"))
	require.Equal(t, "pms_user:x", ComposeAliyunRedisPass("  pms_user  ", "  x  "))
}
