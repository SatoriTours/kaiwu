# GitHub Actions REST 契约核对记录

核对日期：2026-10-17。来源：官方文档页面（本次直接抓取成功，此前记录中的 fake-IP 阻塞已解除）：

- https://docs.github.com/en/rest/actions/workflow-runs?apiVersion=2022-11-28
- https://docs.github.com/en/rest/actions/workflow-jobs?apiVersion=2022-11-28

两页官方说明（原样摘录）："Most endpoints use `Authorization: Bearer <YOUR-TOKEN>` and
`Accept: application/vnd.github+json` headers, plus `X-GitHub-Api-Version: 2026-03-10`."
因此本包默认 `APIVersion = "2026-03-10"`，可经 `ClientConfig.APIVersion` 覆盖；请求同时发送
`Accept: application/vnd.github+json`。`apiVersion=2022-11-28` 的参数化文档也正常返回内容，
说明旧版本仍可用，但默认值以页面现行提示为准。

## 已核对的端点（只读，本包不提供 dispatch/rerun/cancel 方法）

| 端点 | 用途 | 要点 |
| --- | --- | --- |
| `GET /repos/{owner}/{repo}` | Inspect 仓库身份 | 返回 `id`(int64)、`default_branch`；ID 与授权不一致必须拒绝 |
| `GET /repos/{owner}/{repo}/actions/workflows` | Inspect workflow 定义 | 项含 `id`、`state`(active/disabled/manually_disabled 等)、`path`、`name`；分页 |
| `GET /repos/{owner}/{repo}/actions/workflows/{workflow_id}/runs` | 按 workflow 列 run | 支持 `actor`、`branch`、`event`、`head_sha`、`status`、`per_page`(≤100)、`page` 过滤 |
| `GET /repos/{owner}/{repo}/actions/runs` | 仓库级 run 列表（备用） | 同样支持 `branch`/`event`/`head_sha` 过滤 |
| `GET /repos/{owner}/{repo}/actions/runs/{run_id}` | 单 run | schema 同列表项 |
| `GET /repos/{owner}/{repo}/actions/runs/{run_id}/attempts/{attempt_number}` | 单 run 指定 attempt | "Same response schema as Get a workflow run" |
| `GET /repos/{owner}/{repo}/actions/runs/{run_id}/attempts/{attempt_number}/jobs` | 按 attempt 列 job | 支持 `per_page`/`page` |
| `GET /repos/{owner}/{repo}/actions/runs/{run_id}/jobs` | 列 job（`filter=latest\|all`，默认 latest） | 本包固定使用 attempt 端点 |
| `GET /repos/{owner}/{repo}/actions/jobs/{job_id}/logs` | job 文本日志 | 302 跳转，`Location` 1 分钟过期 |
| `GET /repos/{owner}/{repo}/actions/runs/{run_id}/logs`、`.../attempts/{n}/logs` | run 日志压缩包 | 302；本包只取 job 文本日志 |

## run 行关键字段（官方 schema 摘录）

`id` int64；`run_attempt` int；`event`；`status`（列表参数枚举：`completed, action_required,
cancelled, failure, neutral, skipped, stale, success, timed_out, in_progress, queued,
requested, waiting, pending`）；`conclusion`；`head_branch`；`head_sha`；`workflow_id`；
`created_at`/`updated_at`（date-time）；`html_url`；`url`。

## job 行关键字段（官方 schema 摘录）

`id` int64；`run_id`；`run_attempt`；`head_sha`；`status` 枚举
`queued, in_progress, completed, waiting, requested, pending`；`conclusion` 枚举
`success, failure, neutral, cancelled, skipped, timed_out, action_required, null`；
`name`；`steps[]`；`labels`；`html_url`。

## 门禁判定（本包固定语义）

- 只接受 `event=push`、`head_sha` 与候选一致、`head_branch` 与目标分支一致的 run；行数据再逐条
  复核，不信任服务端过滤。
- 未完成 status → `waiting/not_completed`；无匹配 run → `waiting/no_matching_run`。
- `completed`+`success` 且（配置了必需 job 时）对应 job 在该 attempt 分页全集中 `success` → 通过。
- conclusion `skipped`/`neutral` → `blocked`（配置性问题，不可自动修复）。
- conclusion `failure` → `failed/failure`（可修复）；`cancelled`/`timed_out`/`action_required`/
  `stale` → `failed/<conclusion>`（不可自动修复）。
- 必需 job 缺失 → `failed/required_job_missing`；被跳过 → `blocked/required_job_skipped`。
- 分页超过 20 页×100 条未遍历完 → `blocked/pagination_incomplete`，不假通过。
- 401→`auth_failed`、403→`forbidden`、404→`not_found`、429→`rate_limited`（遵循 `Retry-After`）、
  5xx→`server_error`；等待类观察的调度建议 10 秒（`RetryAt`）。
- ETag 条件请求：`If-None-Match`，304 复用缓存响应体（`not_modified`），缓存不当作新成功。
- 日志：单次读取上限 8 MiB（超出标记截断）；交给模型的脱敏摘要上限 64 KiB；持久阶段摘要上限
  256 KiB（由存储层二次裁剪）。跳转到日志主机时不携带 `Authorization`；临时 URL 不进入证据。

## 未验证项

- 以上为官方文档文本核对；未调用真实 GitHub API，未验证真实响应样例与限流头行为。
- 真实凭据、真实 Actions 运行联调另行授权。
