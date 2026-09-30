# Phase 2 SDK 接入说明

> 本文件跟踪 **micro-scaffold** 侧的 SDK 挂载契约。
>
> **脚手架：** 框架能力 + `api/sdk-interfaces` + `pkg/auth` 适配器预留。  
> **独立 Module：** 埋点等能力由业务侧自行 `go get`；**权限 SDK 由独立仓开发、待合并**，勿在本工作区另起一套。

## 安全与权限

| 项 | 状态 |
|----|------|
| 挂载点 | `Authenticator` / `Authorizer`；`SDKAuthenticator` / `SDKAuthorizer` 预留 |
| 权限 SDK | **独立仓待合并**；合并前 `Mode=sdk` 不可用 |
| Phase 1 | `dev` / `require` + Phase1Gate |

## 可观测

| 项 | 状态 |
|----|------|
| 埋点 | 由业务服务自行挂载（独立 Module，脚手架不引入） |
| Writer/Redactor | 脚手架 `pkg/logger` |

## 消息队列 / Adapter / 云资源

| 项 | 状态 |
|----|------|
| 契约 | `api/sdk-interfaces` 占位 |
| 实现 | 业务仓内提供；不强制独立 Module |

## 依赖与发版摘要

- 脚手架 `go.mod` 不引入业务向 SDK
- 权限 SDK 以独立仓合并结果为准，禁止本仓重复实现
