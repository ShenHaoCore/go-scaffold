package model_test

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"go-scaffold/internal/model"
	"go-scaffold/internal/repo"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"
	"github.com/zeromicro/go-zero/core/stores/sqlx"
)

func TestWithTx_NilConn(t *testing.T) {
	err := model.WithTx(context.Background(), nil, func(ctx context.Context, session sqlx.Session) error {
		return nil
	})
	require.ErrorIs(t, err, sql.ErrConnDone)
}

func TestWithTx_RollbackOnError(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	mock.ExpectBegin()
	mock.ExpectRollback()

	want := errors.New("biz")
	conn := sqlx.NewSqlConnFromDB(db)
	err = model.WithTx(context.Background(), conn, func(ctx context.Context, session sqlx.Session) error {
		_ = session
		return want
	})
	require.ErrorIs(t, err, want)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestWithTx_Commit(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	mock.ExpectBegin()
	mock.ExpectCommit()

	conn := sqlx.NewSqlConnFromDB(db)
	err = model.WithTx(context.Background(), conn, func(ctx context.Context, session sqlx.Session) error {
		return nil
	})
	require.NoError(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestNormalizePage_ViaRepo(t *testing.T) {
	_, _, err := repo.NormalizePage(0, 999)
	require.ErrorIs(t, err, repo.ErrInvalidPageSize)
}

func TestCheckSoftDeleteColumn_EmptyTablesOK(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	// SoftDeleteRequiredTables 为空时不应查库
	err = model.CheckSoftDeleteColumn(context.Background(), db)
	require.NoError(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestCheckSoftDeleteColumn_Missing(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	mock.ExpectQuery("SELECT COUNT\\(\\*\\) FROM information_schema.tables").
		WithArgs("items").
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))
	mock.ExpectQuery("SELECT COUNT\\(\\*\\) FROM information_schema.columns").
		WithArgs("items").
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))

	err = model.CheckSoftDeleteColumns(context.Background(), db, "items")
	require.Error(t, err)
	require.Contains(t, err.Error(), "items")
	require.Contains(t, err.Error(), "missing deleted_at")
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestCheckSoftDeleteColumn_TableNotFound(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	mock.ExpectQuery("SELECT COUNT\\(\\*\\) FROM information_schema.tables").
		WithArgs("items").
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))

	err = model.CheckSoftDeleteColumns(context.Background(), db, "items")
	require.Error(t, err)
	require.Contains(t, err.Error(), "not found")
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestCheckSoftDeleteColumn_Present(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	mock.ExpectQuery("SELECT COUNT\\(\\*\\) FROM information_schema.tables").
		WithArgs("items").
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))
	mock.ExpectQuery("SELECT COUNT\\(\\*\\) FROM information_schema.columns").
		WithArgs("items").
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))

	err = model.CheckSoftDeleteColumns(context.Background(), db, "items")
	require.NoError(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestCheckSoftDeleteColumns_MultiTable(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	mock.ExpectQuery("SELECT COUNT\\(\\*\\) FROM information_schema.tables").
		WithArgs("items").
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))
	mock.ExpectQuery("SELECT COUNT\\(\\*\\) FROM information_schema.columns").
		WithArgs("items").
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))
	mock.ExpectQuery("SELECT COUNT\\(\\*\\) FROM information_schema.tables").
		WithArgs("t_soft").
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))
	mock.ExpectQuery("SELECT COUNT\\(\\*\\) FROM information_schema.columns").
		WithArgs("t_soft").
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))

	err = model.CheckSoftDeleteColumns(context.Background(), db, "items", "t_soft")
	require.Error(t, err)
	require.Contains(t, err.Error(), "t_soft")
	require.Contains(t, err.Error(), "missing deleted_at")
	require.NoError(t, mock.ExpectationsWereMet())
}
