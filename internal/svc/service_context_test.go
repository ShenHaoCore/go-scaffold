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
	"github.com/zeromicro/go-zero/zrpc"
	"google.golang.org/grpc/connectivity"
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

// 契约：Close 必须和 NewServiceContext 失败路径的 cleanup 收同一份清单。
// 曾经漏掉下游 gRPC 连接——cleanup 关了、Close 没关，成功启动的进程退出时连接不释放。
func TestServiceContext_CloseReleasesRpcClients(t *testing.T) {
	t.Setenv("APP_ENV", "dev")

	ctx, err := svc.NewServiceContext(config.Config{
		Auth: config.AuthConf{Mode: "dev"},
		DB:   config.DBConf{},
		RpcClient: config.RpcClientConf{
			// 非阻塞拨号：指向无人监听的端口也能建出连接对象（首调才 Unavailable）
			Targets: map[string]zrpc.RpcClientConf{
				"downstream": {Endpoints: []string{"127.0.0.1:1"}},
			},
		},
	})
	require.NoError(t, err)
	require.NotNil(t, ctx.RpcClients)

	conn, ok := ctx.RpcClients.Conn("downstream")
	require.True(t, ok)
	require.NotEqual(t, connectivity.Shutdown, conn.GetState())

	ctx.Close()
	require.Nil(t, ctx.RpcClients, "Close 须清空 RpcClients 字段")
	require.Equal(t, connectivity.Shutdown, conn.GetState(), "Close 后连接须真正关闭，不得只置空字段")

	ctx.Close() // 幂等：第二次不得 panic
}
