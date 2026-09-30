package repo_test

import (
	"math"
	"testing"

	"go-scaffold/internal/repo"

	"github.com/stretchr/testify/require"
)

func TestNormalizePage(t *testing.T) {
	_, _, err := repo.NormalizePage(0, 0)
	require.ErrorIs(t, err, repo.ErrInvalidPageSize)

	p, s, err := repo.NormalizePage(3, 50)
	require.NoError(t, err)
	require.Equal(t, 3, p)
	require.Equal(t, 50, s)

	_, _, err = repo.NormalizePage(-1, 101)
	require.ErrorIs(t, err, repo.ErrInvalidPageSize)

	_, _, err = repo.NormalizePage(0, 20)
	require.ErrorIs(t, err, repo.ErrInvalidPage)

	p, s, err = repo.NormalizePage(1, 20)
	require.NoError(t, err)
	require.Equal(t, 1, p)
	require.Equal(t, 20, s)
}

func TestNormalizePage_NoOverflow(t *testing.T) {
	p, s, err := repo.NormalizePage(math.MaxInt, 20)
	require.NoError(t, err)
	require.Equal(t, 20, s)
	off := repo.PageOffset(p, s)
	require.GreaterOrEqual(t, off, 0)
	require.LessOrEqual(t, off, math.MaxInt-s)
}
