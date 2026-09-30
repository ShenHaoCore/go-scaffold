package {{API}}

import (
	"context"

	"micro-scaffold/internal/svc"
	"micro-scaffold/pkg/logger"
)

// {{API}}Logic 由 make newlogic 生成的骨架；请按业务补全。
type {{API}}Logic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func New{{API}}Logic(ctx context.Context, svcCtx *svc.ServiceContext) *{{API}}Logic {
	return &{{API}}Logic{ctx: ctx, svcCtx: svcCtx}
}

// Handle 为占位方法。补全步骤：
// 1. 在 api/desc 定义请求/响应类型并 make gen
// 2. 将下方 any 换成 internal/types 中生成的 Req/Resp
// 3. 将方法名改为与 handler 一致（如 ListXxx / GetXxx）
func (l *{{API}}Logic) Handle(req any) (any, error) {
	logger.WithContext(l.ctx).Infof("{{API}} logic stub")
	_ = req
	return nil, nil
}
