# ArchiveWeave

ArchiveWeave 是 `solo-0009-archive-weave` 的 Go 后端基线。它面向地方档案馆、整理团队和研究使用者，提供档案元数据受理、修订、提交审核、审核决定、公开筛选和结构化导出能力。服务只使用本机磁盘数据，不依赖数据库或外部接口。

## 构建与运行

要求 Go 1.26 或更高版本。

```bash
go build ./...
go run ./cmd/archiveweave --addr 127.0.0.1:8080 --data ./var/artifacts.json --audit ./var/history.json --snapshots ./var/snapshots.json --comparisons ./var/comparisons.json
```

服务启动后可访问 `GET /healthz`。写入接口默认开启，可通过 `--read-only` 或 `ARCHIVE_WEAVE_READ_ONLY=true` 只提供读取能力。

## 目录结构

- `cmd/archiveweave`：HTTP 服务入口。
- `cmd/archiveweave-check`：本地工作流检查入口。
- `internal/domain`：档案实体、生命周期、审计事件、版本快照、比较作业实体与字段级比对规则。
- `internal/catalog`：受理、修订、审核、检索、导出、摘要、版本比较调度和组合编排。
- `internal/storage`：内存存储、JSON 持久化、原子文件替换、版本快照与比较作业存储和审计保存。
- `internal/httpapi`：HTTP 路由、请求解析、错误映射和只读边界。
- `internal/observability`：结构化日志和运行计数。
- `internal/workflowcheck`：通过真实 HTTP 路径执行验收场景。

## HTTP 接口

- `GET /healthz`：服务状态。
- `GET /metrics`：进程内请求、写入和失败计数。
- `GET /catalog/summary`：档案总量、状态分布、年份范围和常用标签。
- `POST /artifacts`：受理单条草稿档案。
- `POST /artifacts/batch`：批量受理草稿档案。
- `GET /artifacts`：检索档案，支持关键词、标签、年份、状态、视图、排序和分页。
- `GET /artifacts/{id}`：读取单条档案。
- `PUT /artifacts/{id}/metadata`：修订草稿元数据。
- `POST /artifacts/{id}/submit`：提交审核。
- `POST /artifacts/{id}/review`：批准或退回。
- `GET /artifacts/{id}/history`：读取按版本排序的审计轨迹。
- `GET /artifacts/{id}/export`：导出单条已批准档案。
- `GET /collections/export`：导出带校验和的公开集合。
- `POST /comparisons`：提交一次"一条参考档案版本 + 多个目标档案版本"的比较作业。
- `GET /comparisons`：列出全部比较作业。
- `GET /comparisons/{id}`：查看作业及逐项目状态、失败原因和不可变比较结果。
- `POST /comparisons/{id}/resume`：把未结束条目重置为待处理并重新排队，终态结论保持不变。

写接口可以读取 `X-Archive-Actor` 请求头记录操作者。档案创建、修订、提交、批准和退回都会生成不可覆盖的审计事件。

## 档案版本比较

`POST /comparisons` 接受一条参考和最多 50 条目标，每条引用形如 `{"artifact_id":"...","version":3}`；`version` 省略或传 `0` 表示提交时的最新版本。

- **逐项隔离**：目标不存在记为 `artifact_not_found`，版本越界记为 `invalid_version`，其余项目照常完成；参考本身不合法时整单拒绝。
- **历史版本绑定**：每次档案写入都会固化该版本快照；作业在提交时解析并嵌入参考快照，条目完成时嵌入目标快照。档案后续修订不会改变已保存的结论。
- **结果复用**：按解析后的实际版本集合计算 SHA-256 指纹，相同输入直接返回既有作业（HTTP 200 且带 `X-Comparison-Reused: true` 响应头）。
- **重启恢复**：作业持久化到独立 JSON 文件；服务启动时把残留 `pending`/`running` 条目重新入队，后台 reaper 还会回收租约过期的条目。`POST /comparisons/{id}/resume` 可手动恢复。
- **有界并发**：固定数量 worker（默认 2）通过带租约的原子领取处理条目，大队列由后台兜底，避免大批比较拖垮服务。

请求示例：

```bash
curl -X POST localhost:8080/comparisons -H 'Content-Type: application/json' -d '{
  "reference": {"artifact_id": "ref-id", "version": 1},
  "targets": [
    {"artifact_id": "target-a"},
    {"artifact_id": "target-b", "version": 2},
    {"artifact_id": "missing-id"}
  ]
}'
```

成功条目在 `items[].result` 中返回 `identical`、`changed_fields` 和每个字段的 `before/after`；失败条目只保留 `failure_code` 与 `failure_reason`。

## 工作流检查

每条生产工作流都有独立的本机检查命令：

```bash
go run ./cmd/archiveweave-check --workflow intake-artifact
go run ./cmd/archiveweave-check --workflow update-metadata
go run ./cmd/archiveweave-check --workflow submit-review
go run ./cmd/archiveweave-check --workflow decide-review
go run ./cmd/archiveweave-check --workflow search-export
go run ./cmd/archiveweave-check --workflow import-batch
go run ./cmd/archiveweave-check --workflow compare-versions
```

也可以执行 `go run ./cmd/archiveweave-check --workflow all` 顺序检查全部场景。检查工具会启动真实 HTTP handler，创建临时数据文件并在结束时清理。

## 配置

| 环境变量 | 作用 | 默认值 |
| --- | --- | --- |
| `ARCHIVE_WEAVE_ADDR` | HTTP 监听地址 | `:8080` |
| `ARCHIVE_WEAVE_DATA` | 档案 JSON 路径 | `./archive-weave-data.json` |
| `ARCHIVE_WEAVE_AUDIT` | 审计 JSON 路径 | `./archive-weave-history.json` |
| `ARCHIVE_WEAVE_SNAPSHOTS` | 版本快照 JSON 路径 | `./archive-weave-snapshots.json` |
| `ARCHIVE_WEAVE_COMPARISONS` | 比较作业 JSON 路径 | `./archive-weave-comparisons.json` |
| `ARCHIVE_WEAVE_COMPARISON_WORKERS` | 比较 worker 数量（1-64） | `2` |
| `ARCHIVE_WEAVE_READ_ONLY` | 是否禁用写接口 | `false` |
| `ARCHIVE_WEAVE_SHUTDOWN_TIMEOUT` | 优雅停机时限 | `5s` |

## 测试边界

本阶段按 healthy baseline 规则刻意不生成单元测试、测试夹具或浏览器测试，也不提供 `test_command`。后续工程任务阶段负责加入红绿验证测试。当前 `workflow_checks` 是生产级的本机 smoke 路径，用于验证构建、状态迁移、错误传播、持久化和导出行为。
