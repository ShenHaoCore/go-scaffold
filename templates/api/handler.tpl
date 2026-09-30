package {{.PkgName}}

// import 与用法必须对称：httpx / bizerr 只在有 request 的分支里被用到，
// 无 request 的路由（如 get /ping returns (PingResp)）若仍无条件 import，
// 生成物会报 imported and not used 而编译失败。
import (
	"net/http"
{{if .HasRequest}}	"github.com/zeromicro/go-zero/rest/httpx"
{{end}}	{{.ImportPackages}}
{{if .HasRequest}}	bizerr "micro-scaffold/pkg/errors"
{{end}}	"micro-scaffold/pkg/response"
)

// 自定义：统一走 pkg/response（改 Success/Error 签名须同步本模板）
{{if .HasDoc}}{{.Doc}}{{end}}
func {{.HandlerName}}(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		{{if .HasRequest}}var req types.{{.RequestType}}
		if err := httpx.Parse(r, &req); err != nil {
			response.Error(r.Context(), w, bizerr.NewFromContext(r.Context(), bizerr.CodeInvalidParam, map[string]any{"Field": "request"}))
			return
		}

		{{end}}l := {{.LogicName}}.New{{.LogicType}}(r.Context(), svcCtx)
		{{if .HasResp}}resp, err := l.{{.Call}}({{if .HasRequest}}&req{{end}})
		if err != nil {
			response.Error(r.Context(), w, err)
		} else {
			response.Success(r.Context(), w, resp)
		}{{else}}err := l.{{.Call}}({{if .HasRequest}}&req{{end}})
		if err != nil {
			response.Error(r.Context(), w, err)
		} else {
			response.Success(r.Context(), w, nil)
		}{{end}}
	}
}
