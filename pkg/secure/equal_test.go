package secure_test

import (
	"testing"

	"micro-scaffold/pkg/secure"

	"github.com/stretchr/testify/require"
)

func TestEqualString(t *testing.T) {
	require.True(t, secure.EqualString("abc", "abc"))
	require.False(t, secure.EqualString("abc", "ab"))
	require.False(t, secure.EqualString("abc", "abd"))
	require.True(t, secure.EqualString("", ""))
}
