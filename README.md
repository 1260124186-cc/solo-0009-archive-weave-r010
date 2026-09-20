# ArchiveWeave

ArchiveWeave 是 `solo-0009-archive-weave` 的 Go 后端基线。它面向地方档案馆、整理团队和研究使用者，提供档案元数据受理、修订、提交审核、审核决定、公开筛选和结构化导出能力。服务只使用本机磁盘数据，不依赖数据库或外部接口。

## 构建与运行

要求 Go 1.26 或更高版本。

```bash
go build ./...
go run ./cmd/archiveweave --addr 127.0.0.1:8080 \
  --data ./var/artifacts.json --audit ./var/history.json \
  --snapshots ./var/snapshots.json --comparisons ./var/comparisons.json
```

服务启动后可访问 `GET /healthz`。写入接口默认开启，可通过 `--read-only` 或 `ARCHIVE_WEAVE_READ_ONLY=true` 只提供读取能力。

## 目录结构

- `cmd/archiveweave`：HTTP 服务入口。
- `cmd/archiveweave-check`：本地工作流检查入口。
- `internal/domain`：档案实体、生命周期、审计事件和校验规则。
- `internal/catalog`：受理、修订、审核、检索、导出、摘要和组合编排。
- `internal/storage`：内存存储、JSON 持久化、原子文件替换、审计保存和版本快照。
- `internal/comparison`：档案版本比较任务、版本固化解析、字段级差异、有界并发调度、结果复用与重启恢复。
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
- `POST /comparisons`：提交一次比较任务（一条引用 + 多条目标，最多 256 项）。
- `GET /comparisons/{id}`：读取比较任务及逐项状态、结论与失败原因。
- `GET /comparisons`：列出最近的比较任务。

写接口可以读取 `X-Archive-Actor` 请求头记录操作者。档案创建、修订、提交、批准和退回都会生成不可覆盖的审计事件。

## 档案版本比较

一次提交包含一条引用和多条目标，服务为每个目标生成独立项目并逐项给出结论：

- **逐项隔离**：某项目引用/目标档案不存在或版本号非法时，只把该项目标记为 `failed` 并保留稳定原因代码（`reference_missing`、`reference_version_invalid`、`target_not_found`、`target_version_invalid`），其余项目照常完成；仅引用本身无法解析时整请求以 4xx 拒绝。
- **版本固化**：提交时把引用与目标的"当前版本"立即解析为具体版本号并写入任务；每次档案保存都会冻结该版本的不可变快照。比较只针对冻结快照，之后档案修订不会改写已保存的结论。
- **结果复用**：相同的引用+目标（含固化版本）以 SHA-256 指纹去重，重复提交直接返回既有任务（响应中 `reused=true`，HTTP 200）。
- **有界并发**：比较项目由固定大小的 worker 池执行（默认 4），超出上限的项目保持 `pending`，大批量提交不会拖垮服务；提交通道有界，溢出由周期重扫兜底。
- **重启恢复**：任务、逐项状态和版本快照都做原子 JSON 持久化。服务重启后重扫会把残留的 `running` 项目退回 `pending` 并重新执行，已完成项目保持原样，可随时通过 `GET /comparisons/{id}` 查看。

提交示例：

```json
{
  "reference": { "artifact_id": "artifact-a" },
  "targets": [
    { "artifact_id": "artifact-a", "version": 1 },
    { "artifact_id": "artifact-b" },
    { "artifact_id": "artifact-b", "version": 3 }
  ]
}
```

`version` 省略或为 0 表示提交时的当前版本，提交后即固化。比较结论包含字段级差异（`title`、`summary`、`source`、`year`、`status`、标签集合增删）与 `equal` 判定。

## 工作流检查

每条生产工作流都有独立的本机检查命令：

```bash
go run ./cmd/archiveweave-check --workflow intake-artifact
go run ./cmd/archiveweave-check --workflow update-metadata
go run ./cmd/archiveweave-check --workflow submit-review
go run ./cmd/archiveweave-check --workflow decide-review
go run ./cmd/archiveweave-check --workflow search-export
go run ./cmd/archiveweave-check --workflow import-batch
go run ./cmd/archiveweave-check --workflow archive-comparison
go run ./cmd/archiveweave-check --workflow comparison-recovery
```

也可以执行 `go run ./cmd/archiveweave-check --workflow all` 顺序检查全部场景。检查工具会启动真实 HTTP handler，创建临时数据文件并在结束时清理。比较检查覆盖逐项失败原因、字段差异、版本固化、结果复用、并发上限以及跨"进程"重启恢复。

## 配置

| 环境变量 | 作用 | 默认值 |
| --- | --- | --- |
| `ARCHIVE_WEAVE_ADDR` | HTTP 监听地址 | `:8080` |
| `ARCHIVE_WEAVE_DATA` | 档案 JSON 路径 | `./archive-weave-data.json` |
| `ARCHIVE_WEAVE_AUDIT` | 审计 JSON 路径 | `./archive-weave-history.json` |
| `ARCHIVE_WEAVE_SNAPSHOTS` | 档案版本快照 JSON 路径 | `./archive-weave-snapshots.json` |
| `ARCHIVE_WEAVE_COMPARISONS` | 比较任务 JSON 路径 | `./archive-weave-comparisons.json` |
| `ARCHIVE_WEAVE_READ_ONLY` | 是否禁用写接口 | `false` |
| `ARCHIVE_WEAVE_SHUTDOWN_TIMEOUT` | 优雅停机时限 | `5s` |
| `ARCHIVE_WEAVE_COMPARISON_CONCURRENCY` | 比较项目最大并发数 | `4` |

对应的命令行参数为 `--snapshots`、`--comparisons` 和 `--comparison-concurrency`。四个持久化文件路径必须互不相同。

## 测试边界

本阶段按 healthy baseline 规则刻意不生成单元测试、测试夹具或浏览器测试，也不提供 `test_command`。后续工程任务阶段负责加入红绿验证测试。当前 `workflow_checks` 是生产级的本机 smoke 路径，用于验证构建、状态迁移、错误传播、持久化和导出行为。
