package model

import (
	"context"
	"database/sql"
)

// SQLDB wraps *sql.DB for ping（实现 repo.DBPinger）。
type SQLDB struct {
	DB *sql.DB
}

func (s *SQLDB) Ping(ctx context.Context) error {
	if s == nil || s.DB == nil {
		return sql.ErrConnDone
	}
	return s.DB.PingContext(ctx)
}
