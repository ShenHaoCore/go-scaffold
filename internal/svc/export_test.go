package svc

import (
	"context"
	"database/sql"

	"go-scaffold/internal/config"
	"go-scaffold/pkg/health"
)

// SetSQLOpenForTest 仅供单测替换 sql.Open；返回 restore。
func SetSQLOpenForTest(fn func(driverName, dataSourceName string) (*sql.DB, error)) (restore func()) {
	prev := sqlOpen
	sqlOpen = fn
	return func() { sqlOpen = prev }
}

// SetCheckSoftDeleteForTest 仅供单测替换软删列检查；返回 restore。
func SetCheckSoftDeleteForTest(fn func(ctx context.Context, db *sql.DB) error) (restore func()) {
	prev := checkSoftDeleteFn
	checkSoftDeleteFn = fn
	return func() { checkSoftDeleteFn = prev }
}

// DialRpcClientsForTest 暴露 dialRpcClients（外部测试包用）。
func DialRpcClientsForTest(c config.Config) (RpcClients, error) { return dialRpcClients(c) }

// RpcCheckersForTest 暴露 rpcCheckers（外部测试包用）。
func RpcCheckersForTest(clients RpcClients) map[string]health.Checker {
	return rpcCheckers(clients)
}
