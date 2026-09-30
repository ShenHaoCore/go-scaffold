package errors

import (
	pkgerrors "github.com/pkg/errors"
)

// Wrap 为普通 error 增加上下文与堆栈；保留 cause。不改变为业务错误码。
// 实现依赖 github.com/pkg/errors v0.9.1，日志用 %+v 输出 stack。
func Wrap(err error, msg string) error {
	if err == nil {
		return nil
	}
	return pkgerrors.Wrap(err, msg)
}

// Wrapf 格式化上下文后 Wrap。
func Wrapf(err error, format string, args ...any) error {
	if err == nil {
		return nil
	}
	return pkgerrors.Wrapf(err, format, args...)
}
