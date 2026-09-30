package svc_test

import (
	"context"
	"database/sql"
	"testing"

	"micro-scaffold/internal/config"
	"micro-scaffold/internal/svc"
	"micro-scaffold/pkg/auth"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"
)

func TestNewServiceContext_ProdRequiresDB(t *testing.T) {
	t.Setenv("APP_ENV", "prod")
	t.Setenv("AUTH_ALLOW_PHASE1_GATE", "true")
	t.Setenv("AUTH_PHASE1_ACK", auth.Phase1AckValue)
	_, err := svc.NewServiceContext(config.Config{
		Auth: config.AuthConf{Mode: "require"},
		DB:   config.DBConf{},
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "DataSource")
}

func TestNewServiceContext_UnsupportedDriver(t *testing.T) {
	t.Setenv("APP_ENV", "dev")
	_, err := svc.NewServiceContext(config.Config{
		Auth: config.AuthConf{Mode: "dev"},
		DB: config.DBConf{
			Driver:     "mysql",
			DataSource: "postgres://scaffold:scaffold@127.0.0.1:5432/scaffold?sslmode=disable",
		},
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "unsupported DB.Driver")
}

func TestNewServiceContext_ConfiguredDSNPingFailNoMemory(t *testing.T) {
	t.Setenv("APP_ENV", "dev")
	_, err := svc.NewServiceContext(config.Config{
		Auth: config.AuthConf{Mode: "dev"},
		DB: config.DBConf{
			// 不可达地址：须 fail-fast，禁止回退 Memory
			DataSource: "postgres://scaffold:scaffold@127.0.0.1:1/scaffold?sslmode=disable&connect_timeout=1",
		},
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "postgres ping")
}

func TestNewServiceContext_EmptyDSNAllowsMemory(t *testing.T) {
	t.Setenv("APP_ENV", "dev")
	ctx, err := svc.NewServiceContext(config.Config{
		Auth: config.AuthConf{Mode: "dev"},
		DB:   config.DBConf{},
	})
	require.NoError(t, err)
	require.NotNil(t, ctx)
	require.NotNil(t, ctx.Health)
	t.Cleanup(ctx.Close)
}

func TestNewServiceContext_SoftDeleteMissingFailFast(t *testing.T) {
	t.Setenv("APP_ENV", "dev")

	db, mock, err := sqlmock.New(sqlmock.MonitorPingsOption(true))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	mock.ExpectPing()
	mock.ExpectClose()

	restoreOpen := svc.SetSQLOpenForTest(func(driverName, dataSourceName string) (*sql.DB, error) {
		require.Equal(t, "pgx", driverName)
		require.NotEmpty(t, dataSourceName)
		return db, nil
	})
	t.Cleanup(restoreOpen)

	restoreSoft := svc.SetCheckSoftDeleteForTest(func(ctx context.Context, d *sql.DB) error {
		require.Same(t, db, d)
		return sql.ErrNoRows
	})
	t.Cleanup(restoreSoft)

	_, err = svc.NewServiceContext(config.Config{
		Auth: config.AuthConf{Mode: "dev"},
		DB:   config.DBConf{DataSource: "postgres://user:pass@127.0.0.1:5432/scaffold?sslmode=disable"},
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "soft-delete")
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestServiceContext_CloseNilRedisPing(t *testing.T) {
	t.Setenv("APP_ENV", "dev")
	ctx, err := svc.NewServiceContext(config.Config{
		Auth: config.AuthConf{Mode: "dev"},
		DB:   config.DBConf{},
	})
	require.NoError(t, err)
	ctx.Close()
	ctx.Close() // 第二次须安全（RedisPing/DB 已清空）
}
