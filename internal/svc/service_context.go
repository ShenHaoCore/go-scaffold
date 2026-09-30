package svc

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"sync"
	"time"

	"go-scaffold/internal/config"
	"go-scaffold/internal/model"
	"go-scaffold/internal/repo"
	"go-scaffold/pkg/auth"
	"go-scaffold/pkg/env"
	"go-scaffold/pkg/health"
	"go-scaffold/pkg/logger"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/core/stores/sqlx"
)

const startupDBTimeout = 5 * time.Second

// 可测钩子：默认走标准库 / model；单测可替换（见 export_test.go）。
var (
	sqlOpen           = sql.Open
	checkSoftDeleteFn = model.CheckSoftDeleteColumn
)

// ServiceContext 依赖注入；字段类型为接口（repo / auth）。
// SqlConn / DB 仅供 WithTx / 组装层；业务 logic 禁止直连手写 SQL。
type ServiceContext struct {
	Config     config.Config
	SqlConn    sqlx.SqlConn // 可为 nil；跨 Repo 事务用 model.WithTx(ctx, SqlConn, ...)
	DB         *sql.DB      // RawDB；软删校验 / Close；logic 禁止手写 SQL
	Auth       auth.Authenticator
	Authorizer auth.Authorizer // Phase 2 Mode=sdk 注入；Phase 1 为 nil，调用须 auth.Authorize
	DBPing     repo.DBPinger
	RedisPing  repo.RedisPinger
	OSSPing    repo.OSSPinger
	MQ         repo.MQProducer
	SLS        repo.SLSClient
	RpcClients RpcClients // 下游 gRPC 连接（RpcClient.Targets 为空时为 nil）
	Health     *health.Runner
	closeOnce  sync.Once
	// TODO(Phase2): 注入权限 SDK（Auth.Mode=sdk + Authorizer）
}

// NewServiceContext 组装具体实现。
// DB 规则：已配置 DataSource → 连库/缺软删列失败一律 fail-fast；
// 仅非 prod 且 DataSource 为空时允许显式 Memory（启动日志可见）。
func NewServiceContext(c config.Config) (*ServiceContext, error) {
	if _, err := env.ResolveAppEnv(); err != nil {
		return nil, err
	}
	authn, err := auth.NewAuthenticator(c.Auth.Mode)
	if err != nil {
		return nil, err
	}

	strict := env.IsProd()

	var dbPing repo.DBPinger
	var db *sql.DB
	var sqlConn sqlx.SqlConn
	dbConfigured := false

	if c.DB.DataSource != "" {
		driver := strings.TrimSpace(c.DB.Driver)
		if driver == "" {
			driver = "postgres"
		}
		switch strings.ToLower(driver) {
		case "postgres", "postgresql", "pgx":
			// ok：stdlib 注册名固定为 pgx
		default:
			return nil, fmt.Errorf("unsupported DB.Driver %q (want postgres/pgx)", c.DB.Driver)
		}
		d, err := sqlOpen("pgx", c.DB.DataSource)
		if err != nil {
			return nil, fmt.Errorf("open postgres: %w", err)
		}
		d.SetMaxOpenConns(20)
		d.SetMaxIdleConns(5)
		d.SetConnMaxLifetime(time.Hour)
		d.SetConnMaxIdleTime(10 * time.Minute)

		pingCtx, pingCancel := context.WithTimeout(context.Background(), startupDBTimeout)
		pingErr := d.PingContext(pingCtx)
		pingCancel()
		if pingErr != nil {
			_ = d.Close()
			return nil, fmt.Errorf("postgres ping: %w", pingErr)
		}

		softCtx, softCancel := context.WithTimeout(context.Background(), startupDBTimeout)
		softErr := checkSoftDeleteFn(softCtx, d)
		softCancel()
		if softErr != nil {
			_ = d.Close()
			return nil, fmt.Errorf("soft-delete schema incomplete (run make migrate-up; register tables in SoftDeleteRequiredTables): %w", softErr)
		}

		db = d
		sqlConn = sqlx.NewSqlConnFromDB(d)
		dbConfigured = true
		dbPing = &model.SQLDB{DB: d}
	} else {
		if strict {
			return nil, fmt.Errorf("db DataSource is required when APP_ENV=prod")
		}
		logx.Infof("db DataSource empty: using Memory / no business repo (APP_ENV=%s)", env.AppEnv())
		dbPing = &failPing{err: fmt.Errorf("db not configured")}
	}

	redisPing := model.NewRedisPing(c.RedisConf.Host, c.RedisConf.Type, c.RedisConf.User, c.RedisConf.Pass, c.RedisConf.Tls)
	redisConfigured := redisPing.Configured()
	ossPing := model.NewOSSPing(c.OSS.Endpoint, c.OSS.Bucket, c.OSS.AccessKeyID, c.OSS.AccessKeySecret)
	ossConfigured := ossPing.Configured()
	var mqProd *model.MQProducer
	var slsWriter *model.SLSWriter
	var rpcClients RpcClients

	cleanup := func() {
		if rpcClients != nil {
			rpcClients.CloseAll()
		}
		if mqProd != nil {
			_ = mqProd.Close()
		}
		if slsWriter != nil {
			_ = slsWriter.Close()
		}
		if ossPing != nil {
			_ = ossPing.Close()
		}
		if redisPing != nil {
			_ = redisPing.Close()
		}
		if db != nil {
			_ = db.Close()
		}
	}
	fail := func(format string, args ...any) (*ServiceContext, error) {
		cleanup()
		return nil, fmt.Errorf(format, args...)
	}

	if redisConfigured {
		rctx, rcancel := context.WithTimeout(context.Background(), startupDBTimeout)
		rerr := redisPing.Ping(rctx)
		rcancel()
		if rerr != nil {
			return fail("redis ping (REDIS_HOST configured): %w", rerr)
		}
	}

	if ossConfigured {
		octx, ocancel := context.WithTimeout(context.Background(), startupDBTimeout)
		oerr := ossPing.Ping(octx)
		ocancel()
		if oerr != nil {
			return fail("oss ping (OSS_* configured): %w", oerr)
		}
	}

	mqProd = model.NewMQProducer(model.MQOptions{
		Endpoint:        c.MQ.Endpoint,
		InstanceID:      c.MQ.InstanceID,
		Topic:           c.MQ.Topic,
		Group:           c.MQ.Group,
		AccessKey:       c.MQ.AccessKey,
		AccessKeySecret: c.MQ.AccessKeySecret,
	})
	mqConfigured := mqProd.Configured()
	if mqConfigured {
		mctx, mcancel := context.WithTimeout(context.Background(), startupDBTimeout)
		merr := mqProd.Ping(mctx)
		mcancel()
		if merr != nil {
			if strict {
				return fail("mq ping (MQ_ENDPOINT configured): %w", merr)
			}
			logx.Errorf("mq ping failed: %v (health mq=degraded until VPC reachable / ACL OK)", logger.RedactString(merr.Error()))
		}
	}

	slsWriter = model.NewSLSWriter(model.SLSOptions{
		Endpoint:        c.SLS.Endpoint,
		Project:         c.SLS.Project,
		Logstore:        c.SLS.Logstore,
		AccessKeyID:     c.SLS.AccessKeyID,
		AccessKeySecret: c.SLS.AccessKeySecret,
		Topic:           c.SLS.Topic,
		Source:          c.SLS.Source,
	})
	slsConfigured := slsWriter.Configured()
	if slsConfigured {
		sctx, scancel := context.WithTimeout(context.Background(), startupDBTimeout)
		serr := slsWriter.Ping(sctx)
		scancel()
		if serr != nil {
			if strict {
				return fail("sls ping (SLS_* configured): %w", serr)
			}
			logx.Errorf("sls ping failed: %v (health sls=degraded until VPC reachable / ACL OK)", logger.RedactString(serr.Error()))
		} else if lx := model.NewSLSLogxWriter(slsWriter); lx != nil {
			// 额外 Writer：保留 Log.Mode=file，同时双写 SLS
			logx.AddWriter(lx)
			logx.Infof("sls: logx writer attached project=%s logstore=%s", c.SLS.Project, c.SLS.Logstore)
		}
	}

	// 下游 gRPC 客户端。RpcClient.Targets 为空时一个连接都不建——
	// HTTP 网关不填则不依赖任何 RPC 服务，RPC 服务不填则自身不外呼，二者可独立启动。
	clients, err := dialRpcClients(c)
	if err != nil {
		return fail("%w", err)
	}
	rpcClients = clients

	timeout := c.HealthCheckTimeout
	if timeout <= 0 {
		timeout = 3 * time.Second
	}

	checkers := map[string]health.Checker{
		"db": func(ctx context.Context) health.CheckResult {
			if !dbConfigured {
				return health.CheckResult{Name: "db", Status: health.StatusSkipped, Message: "db not configured"}
			}
			if err := dbPing.Ping(ctx); err != nil {
				return health.CheckResult{Name: "db", Status: health.StatusDegraded, Message: "ping failed"}
			}
			return health.CheckResult{Name: "db", Status: health.StatusOK}
		},
		"redis": func(ctx context.Context) health.CheckResult {
			if !redisConfigured {
				return health.CheckResult{Name: "redis", Status: health.StatusSkipped, Message: "redis not configured"}
			}
			if err := redisPing.Ping(ctx); err != nil {
				return health.CheckResult{Name: "redis", Status: health.StatusDegraded, Message: "ping failed"}
			}
			return health.CheckResult{Name: "redis", Status: health.StatusOK}
		},
		"oss": func(ctx context.Context) health.CheckResult {
			if !ossConfigured {
				return health.CheckResult{Name: "oss", Status: health.StatusSkipped, Message: "oss not configured"}
			}
			if err := ossPing.Ping(ctx); err != nil {
				return health.CheckResult{Name: "oss", Status: health.StatusDegraded, Message: "ping failed"}
			}
			return health.CheckResult{Name: "oss", Status: health.StatusOK}
		},
		"mq": func(ctx context.Context) health.CheckResult {
			if !mqConfigured {
				return health.CheckResult{Name: "mq", Status: health.StatusSkipped, Message: "mq not configured"}
			}
			if err := mqProd.Ping(ctx); err != nil {
				return health.CheckResult{Name: "mq", Status: health.StatusDegraded, Message: "ping failed"}
			}
			return health.CheckResult{Name: "mq", Status: health.StatusOK}
		},
		"sls": func(ctx context.Context) health.CheckResult {
			if !slsConfigured {
				return health.CheckResult{Name: "sls", Status: health.StatusSkipped, Message: "sls not configured"}
			}
			if err := slsWriter.Ping(ctx); err != nil {
				return health.CheckResult{Name: "sls", Status: health.StatusDegraded, Message: "ping failed"}
			}
			return health.CheckResult{Name: "sls", Status: health.StatusOK}
		},
		"process": func(ctx context.Context) health.CheckResult {
			return health.CheckResult{Name: "process", Status: health.StatusOK}
		},
	}
	for name, ck := range rpcCheckers(rpcClients) {
		checkers[name] = ck
	}

	runner := &health.Runner{Timeout: timeout, Checkers: checkers}

	return &ServiceContext{
		Config:     c,
		SqlConn:    sqlConn,
		DB:         db,
		Auth:       authn,
		Authorizer: nil, // Phase 2 Mode=sdk 接入
		DBPing:     dbPing,
		RedisPing:  redisPing,
		OSSPing:    ossPing,
		MQ:         mqProd,
		SLS:        slsWriter,
		RpcClients: rpcClients,
		Health:     runner,
	}, nil
}

// Close 释放进程级资源（下游 gRPC 连接 / DB / Redis / OSS / MQ / SLS 等）；
// Start 返回后由 defer 调用。幂等且并发安全。
//
// 与 NewServiceContext 失败路径的 cleanup 保持同一份清单——两条路径都必须收干净，
// 否则成功启动后走 Close 会漏掉只在 cleanup 里关过的资源（下游 gRPC 连接曾如此）。
func (s *ServiceContext) Close() {
	if s == nil {
		return
	}
	s.closeOnce.Do(func() {
		if s.RpcClients != nil {
			s.RpcClients.CloseAll()
			s.RpcClients = nil
		}
		if s.DB != nil {
			if err := s.DB.Close(); err != nil {
				logx.Errorf("close db: %v", err)
			}
			s.DB = nil
			s.SqlConn = nil
		}
		if closer, ok := s.RedisPing.(interface{ Close() error }); ok && closer != nil {
			if err := closer.Close(); err != nil {
				logx.Errorf("close redis: %v", err)
			}
			s.RedisPing = nil
		}
		if closer, ok := s.OSSPing.(interface{ Close() error }); ok && closer != nil {
			if err := closer.Close(); err != nil {
				logx.Errorf("close oss: %v", err)
			}
			s.OSSPing = nil
		}
		if closer, ok := s.MQ.(interface{ Close() error }); ok && closer != nil {
			if err := closer.Close(); err != nil {
				logx.Errorf("close mq: %v", err)
			}
			s.MQ = nil
		}
		if s.SLS != nil {
			if err := s.SLS.Close(); err != nil {
				logx.Errorf("close sls: %v", err)
			}
			s.SLS = nil
		}
	})
}

type failPing struct{ err error }

func (f *failPing) Ping(ctx context.Context) error { return f.err }
