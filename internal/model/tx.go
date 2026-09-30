package model

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/zeromicro/go-zero/core/stores/sqlx"
)

// SoftDeleteRequiredTables 已配置 DSN 时启动须存在 deleted_at 的表。
// 业务服务在此登记软删表；脚手架无业务表，保持为空。
var SoftDeleteRequiredTables = []string{}

// WithTx 跨 Repo 事务编排（sqlx.TransactCtx）：回调内用业务 ModelTx(session)。
func WithTx(ctx context.Context, conn sqlx.SqlConn, fn func(ctx context.Context, session sqlx.Session) error) error {
	if conn == nil {
		return sql.ErrConnDone
	}
	return conn.TransactCtx(ctx, fn)
}

// CheckSoftDeleteColumn 校验 SoftDeleteRequiredTables 均含 deleted_at。
func CheckSoftDeleteColumn(ctx context.Context, db *sql.DB) error {
	return CheckSoftDeleteColumns(ctx, db, SoftDeleteRequiredTables...)
}

// CheckSoftDeleteColumns 校验指定表均含 deleted_at；tables 为空时用 SoftDeleteRequiredTables。
func CheckSoftDeleteColumns(ctx context.Context, db *sql.DB, tables ...string) error {
	if db == nil {
		return sql.ErrConnDone
	}
	if len(tables) == 0 {
		tables = SoftDeleteRequiredTables
	}
	for _, table := range tables {
		var tableCount int
		err := db.QueryRowContext(ctx, `
			SELECT COUNT(*) FROM information_schema.tables
			WHERE table_schema = current_schema()
			  AND table_name = $1`, table).Scan(&tableCount)
		if err != nil {
			return err
		}
		if tableCount == 0 {
			return fmt.Errorf("table %s not found (require migrate)", table)
		}
		var colCount int
		err = db.QueryRowContext(ctx, `
			SELECT COUNT(*) FROM information_schema.columns
			WHERE table_schema = current_schema()
			  AND table_name = $1
			  AND column_name = 'deleted_at'`, table).Scan(&colCount)
		if err != nil {
			return err
		}
		if colCount == 0 {
			return fmt.Errorf("table %s missing deleted_at (require soft-delete migrate)", table)
		}
	}
	return nil
}
