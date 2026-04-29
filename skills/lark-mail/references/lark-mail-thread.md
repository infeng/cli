# mail +thread

> **前置条件：** 先阅读 [`../../lark-shared/SKILL.md`](../../lark-shared/SKILL.md) 了解认证、全局参数和安全规则。

读取指定会话中的所有邮件，按发送时间升序排列。每条邮件结构与 `+message` 相同。

在实现上，每个 `messages[]` 项与 `mail +message` 的构建方式一致：安全元数据字段直接透传，正文/附件辅助字段由 shortcut 派生。每条邮件使用统一的 `attachments[]` 列表，涵盖普通附件和内嵌图片。

本 skill 对应 shortcut `lark-cli mail +thread`，内部调用：
- `GET /open-apis/mail/v1/user_mailboxes/{mailbox}/threads/{thread_id}` — 获取会话中所有邮件的完整内容

## 命令

```bash
# 读取完整会话
lark-cli mail +thread --thread-id <thread-id>

# 仅纯文本正文（更小的负载，适合 AI 处理）
lark-cli mail +thread --thread-id <thread-id> --html=false

# 指定邮箱
lark-cli mail +thread --mailbox user@example.com --thread-id <thread-id>

# JSON 输出
lark-cli mail +thread --thread-id <thread-id> --format json

# Dry Run
lark-cli mail +thread --thread-id <thread-id> --dry-run
```

## 参数

| 参数 | 必填 | 默认值 | 说明 |
|------|------|--------|------|
| `--thread-id <id>` | 是 | — | 会话 ID（`thread_id`） |
| `--mailbox <email>` | 否 | 当前用户 | 邮箱地址（`user_mailbox_id`） |
| `--html` | 否 | true | 是否返回 HTML 正文（`false` 仅返回纯文本，减少带宽） |
| `--format <mode>` | 否 | json | 输出格式：`json`（默认）/ `pretty` / `table` / `ndjson` / `csv` |
| `--dry-run` | 否 | — | 仅打印请求，不执行 |

## 返回值

成功时返回 `{"ok": true, "data": ...}` 结构，`data` 字段包含：

```json
{
  "thread_id":     "会话 ID",
  "message_count": 2,
  "messages": [
    { "...与 +message 输出结构相同（最早的在前）..." },
    { "......" }
  ]
}
```

顶层字段：

| 字段 | 说明 |
|------|------|
| `thread_id` | `--thread-id` 请求的会话 ID |
| `message_count` | 成功获取的邮件数量 |
| `messages` | 按 `internal_date` 升序排列的邮件列表（最早的在前） |

每个 `messages[]` 项使用与 [`mail +message`](./lark-mail-message.md#返回值) 相同的结构。完整字段列表参见 [`+message` 字段说明](./lark-mail-message.md#字段说明) 和 [`+message` security_level](./lark-mail-message.md#security_level)。

> 注意：使用 `--format json` 获取结构化输出。所有 JSON 输出统一包裹在 `{"ok": true, "data": ...}` 结构中。

## 注意事项

- **JSON 输出可直接使用**，可直接读取，无需额外编码转换。
- JSON 输出中 `messages[].body_html` 里的 `<` / `>` 可能显示为 `\u003c` / `\u003e`（JSON 安全转义，内容不变，`jq -r` 可还原）。
- `mail +thread` 不再在读取会话时获取附件/图片下载 URL。如后续步骤需要 URL，请针对特定的 `message_id` 和 `attachment_ids` 调用原生附件 URL API。
- 与 `+message` 一样，普通附件和内嵌图片都出现在 `messages[].attachments[]` 中，使用同一个 `user_mailbox.message.attachments download_url` API。
- 查看某条邮件的原始 HTML：

```bash
lark-cli mail +thread --thread-id <thread_id> --format json | jq -r '.data.messages[0].body_html'
```

## 典型场景

### 查看会话时间线 → 生成摘要

```bash
# 1. 从某封邮件获取 thread_id
lark-cli mail +message --message-id <id> --html=false --format json | jq '.data.thread_id'

# 2. 读取完整会话（仅纯文本）
lark-cli mail +thread --thread-id <thread_id> --html=false --format json

# 3. 让 LLM 分析 messages[].body_plain_text 并生成会话摘要
```

### 回复会话中最新一封邮件

```bash
# 获取最新一封邮件的 message_id
lark-cli mail +thread --thread-id <thread_id> --html=false --format json | \
  jq '.data.messages[-1].message_id'

# 回复
lark-cli mail +reply --message-id <last_message_id> --body "..."
```

## ⛔ 空结果处理

按 `--thread-id` 取整个会话时，"取不到"分三种语义，必须区分上报，**禁止**通过新建草稿、自发邮件等写操作凭空构造一个会话来满足后续动作（如"回复会话最新一封"）。

| 情形 | 表现 | 正确做法 |
|------|------|----------|
| 200 OK 但 `messages` 为空 | 极少出现；若 `data.messages` 为空数组或 `data.thread` 缺失，视同会话不存在或已被全部删除 | 回报"该会话目前没有可读邮件（可能整个会话已被删除）"，请用户确认 `thread_id` 是否正确 |
| 404（资源不存在） | 接口返回 `NOT_FOUND`，会话已被删除 / `thread_id` 错误 / 跨账号无权访问 | 回报"该会话不存在或已被删除"，附原 `thread_id`；可建议用 `+triage` 重新定位 |
| 网络 / 权限错误（5xx / 401 / 403） | 5xx、401、403 或网络超时 | 回报错误类别（鉴权 / 权限 / 网络）并提示用户重试或检查 `--as` 身份与 scope；**不要**当作"会话不存在"继续推进 |

> 详见 SKILL.md 「## ⛔ Non-goals（不应做的事）」第 1 条 与 「## ⚠️ 安全规则」第 9 条。

## 相关命令

- `lark-cli mail +message` — 读取单封邮件
- `lark-cli mail +reply` — 回复邮件
- `lark-cli mail +forward` — 转发邮件
- `lark-cli mail user_mailbox.message.attachments download_url` — 按需获取邮件附件/图片下载 URL
- `lark-cli mail user_mailbox.messages list` — 列出收件箱邮件（获取 `thread_id`）
