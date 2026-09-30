# migrations

业务团队在此目录新增标准 golang-migrate 迁移文件。

本脚手架**无占位迁移**，也**不提供**业务表迁移；目录可为空，由业务自行添加文件。推荐步骤：

1. 新增 `NNNNNN_xxx.up.sql` / `NNNNNN_xxx.down.sql`
2. 若表使用软删除（`deleted_at`），在 `internal/model.SoftDeleteRequiredTables` 中登记表名
3. 本地：`export DB_DSN='postgres://scaffold:scaffold123@127.0.0.1:5432/scaffold?sslmode=disable'` 后 `make migrate-up`

无迁移文件时可跳过 `make migrate-up`（空目录一般可直接成功）。非 prod 且未配置 DSN 时，可直接 `make run-api`。
