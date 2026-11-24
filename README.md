# gemini-web2api-go（分支）

把 [gemini.google.com](https://gemini.google.com) 网页端反代成 OpenAI 兼容 / Gemini 原生接口的单二进制服务。
不需要 Google API Key，也不需要付费配额。

本仓库是 [zexadev/gemini-web2api-go](https://github.com/zexadev/gemini-web2api-go) 的一个分支，按自己的部署需求维护。
这里只写本分支实际用到的内容；上游的完整功能说明、截图和 FAQ 请看[原仓库](https://github.com/zexadev/gemini-web2api-go)。

## 本分支的改动

基于上游 v4.30.0，主要增加了：

- **自动续写 `auto_continue`**：上游把长回答截在半句时，自动在同一会话里补一条「继续」，把剩余内容拼上；
  跳数用尽仍未收尾时回 `finish_reason=length` / `finishReason=MAX_TOKENS` / `status=incomplete`，不再假装正常结束。
- **`gemini-4-neon`**：2026-11 网页端新增的模型（hex `e6fa609c3fa255c0`，Mode 3）。
- **引用标记清理**：去掉回答正文里的 `[cite: N]` 角标。

其余行为与上游 v4.30.0 一致。上游 v4.31.x 尚未合并，`upstream` 远端保留着，随时可以对比。

## 快速开始

要求 Go 1.26+。

```bash
go build -o gemini-web2api .
ADMIN_TOKEN=change-me ./gemini-web2api --port 8083 --db ./data/gemini.db
```

启动后：

- 管理面板：<http://127.0.0.1:8083/admin>
- OpenAI 兼容：`/v1/chat/completions`、`/v1/models`、`/v1/responses`
- Gemini 原生：`/v1beta/models/{model}:generateContent` 和 `:streamGenerateContent`
- 视频：`/v1/videos`（异步任务，需要登录态）
- MCP：`/mcp`，工具 `web_search`

首次启动会自动生成 API key，在面板「API Key」里查看或轮换；也可以用 `--api-key` 或环境变量 `API_KEY` 固定。
`ADMIN_TOKEN` 为空时面板不鉴权，只适合绑在 127.0.0.1。

Docker 部署用仓库根目录的 `Dockerfile` / `docker-compose.yml`。容器里记得把数据目录挂出来，
并用 `--db` 指到挂载点上（例如 `--db /data/gemini.db`），否则重建容器会丢 cookie 池和面板设置。

## 认证

| 接口 | 方式 |
|---|---|
| `/v1/*`、`/mcp` | `Authorization: Bearer <key>`、`x-api-key` 或 `x-goog-api-key` |
| `/admin`、`/admin/api/*` | `ADMIN_TOKEN`，或 `Authorization: Bearer <token>` |

## 模型

以面板和 `/v1/models` 为准；免费号和付费号能看到的档位不同。加 `-thinking` 后缀会开网页端的扩展思考。

| 名称 | 说明 |
|---|---|
| `gemini-3.5-flash-lite` | 最轻量 |
| `gemini-3.6-flash` / `gemini-3.7-flash` / `gemini-3.8-flash` | 日常档，默认模型是 `gemini-3.6-flash` |
| `gemini-4-hydrogen` | 网页端「日常帮助」档 |
| `gemini-4-neon` | 本分支补充的 4 Neon |
| `gemini-4-carbon` | 原 4 Argon，付费号 |
| `gemini-4-argon` | 旧名称，仍可用 |
| `gemini-image` / `gemini-music` / `gemini-video` / `gemini-canvas` | 媒体生成，需要登录态 |

## 关键配置

面板「设置」里改，保存即生效；值存在数据库里，优先级高于 `config.json` 和命令行参数。

| 项 | 说明 |
|---|---|
| `default_model` | 客户端没传 `model` 时用哪个 |
| `multi_turn` | 服务端多轮续接：历史留 Gemini 侧，每轮只发最新一句，长会话不撞单请求字节墙 |
| `auto_continue` / `auto_continue_max_hops` | 本分支的自动续写，默认关、最多 2 跳 |
| `max_prompt_bytes` | 单次 prompt 的 UTF-8 字节上限，默认 128000；超了报 400 `context_length_exceeded` |
| `request_timeout_sec` | 整条请求的总超时，默认 180，面板上限 600 |
| `stream_idle_timeout_sec` | 上游开流后多久没 chunk 就掐断重试，0 = 关 |
| `web_channel` | 对话改走 `processSession`（WebChannel），老 `StreamGenerate` 出问题时再开 |
| `strict_model` | 上游静默降级换模型时不算成功，换号重试 |
| `anon_first` | 不需要登录态能力的请求走匿名，省账号额度 |
| `temporary_chat` / `auto_delete_conversation` | 网页端临时对话 / 出完结果删会话 |
| `per_ip_concurrent` / `per_ip_rpm` / `per_ip_rph` | 按出口 IP 的限流 |

## 限制

- 单请求 prompt 约 13 万 UTF-8 字节封顶；挂 cookie 后超长对话会转成文本附件，但附件可读总量约 16 万字节。
  超了直接报 `context_length_exceeded`，不会静默丢历史。多轮模式下，新建会话的首轮完整历史同样走这条兜底。
- 网页端的输入和输出共用同一个上下文窗口，长对话里留给输出的空间会变小。`auto_continue` 只是把被截断的尾巴补回来，
  并不会放大窗口。
- 带 `tools` 的请求不启用自动续写——中途续写会把 `tool_call` 围栏拼断，这种情况交给客户端自己重试。
- 自动续写需要保留会话，因此与 `auto_delete_conversation` 互斥；开启后这类文本会话会留在账号里。
- 匿名可以跑纯文本，但 4 Carbon / 扩展思考 / 生图 / 传文件需要登录 cookie。

## 上游与许可

- 上游项目：<https://github.com/zexadev/gemini-web2api-go>
- 完整功能文档、已知问题、协议细节：以上游 README 为准
- 许可证：MIT，见 [LICENSE](LICENSE)
