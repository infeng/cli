
# mail +watch

> **前置条件：** 先阅读 [`../lark-shared/SKILL.md`](../../lark-shared/SKILL.md) 了解认证、全局参数和安全规则。

实时监听新邮件事件（`mail.user_mailbox.event.message_received_v1`）。

**权限要求：** 应用需要 `mail:event`、`mail:user_mailbox.message:readonly` 权限，以及字段权限 `mail:user_mailbox.message.address:read`、`mail:user_mailbox.message.subject:read`、`mail:user_mailbox.message.body:read`，且机器人需订阅事件 `mail.user_mailbox.event.message_received_v1`。按需权限（缺失时会提示申请）：使用 `--folders` / `--folder-ids` 筛选自定义文件夹时需要 `mail:user_mailbox.folder:read`；使用 `--labels` / `--label-ids` 筛选自定义标签时需要 `mail:user_mailbox.message:modify`。

## 命令

```bash
# 默认：表格输出 message 元数据
lark-cli mail +watch

# 仅输出 message 数据（jq 友好）
lark-cli mail +watch --msg-format metadata --format data

# 输出精简元数据（message_id / thread_id / folder_id / label_ids / internal_date / message_state）
lark-cli mail +watch --msg-format minimal --format data

# 输出纯文本全文
lark-cli mail +watch --msg-format plain_text_full --format data

# 输出完整 message（含正文相关字段）
lark-cli mail +watch --msg-format full --format data

# 输出原始事件体
lark-cli mail +watch --msg-format event --format data

# 监听指定邮箱
lark-cli mail +watch --mailbox alice@company.com

# 按文件夹/标签过滤（客户端过滤，支持名称或 ID）
lark-cli mail +watch --folders '["收件箱项目"]' --label-ids '["FLAGGED"]'

# 写入文件
lark-cli mail +watch --msg-format metadata --output-dir ./mail-events

# 查看各 --msg-format 的输出字段说明（解析前先运行）
lark-cli mail +watch --print-output-schema
```

## 参数

| 参数 | 默认 | 说明 |
|------|------|------|
| `--mailbox <id>` | `me` | 订阅目标邮箱 |
| `--msg-format <mode>` | `metadata` | 输出模式：`metadata` / `minimal` / `plain_text_full` / `full` / `event` |
| `--format <mode>` | `table` | 输出样式：`table` / `json` / `data` |
| `--folder-ids <json-array>` | — | 文件夹 ID 过滤，如 `["INBOX","SENT"]` |
| `--folders <json-array>` | — | 文件夹名称过滤（与 `--folder-ids` 取并集） |
| `--label-ids <json-array>` | — | 标签 ID 过滤，如 `["FLAGGED","IMPORTANT"]` |
| `--labels <json-array>` | — | 标签名称过滤（与 `--label-ids` 取并集） |

> **过滤逻辑：** `--folder-ids`/`--folders` 与 `--label-ids`/`--labels` 之间是 **AND** 关系，即邮件必须**同时**匹配指定的文件夹和标签才会输出。同类参数内部是 **OR** 关系（匹配其中任一即可）。新收到的邮件通常只有系统标签（如 `UNREAD`、`IMPORTANT`），不会自动带有自定义标签。
| `--output-dir <dir>` | — | 每条事件写入单独 JSON 文件 |
| `--print-output-schema` | — | 打印各 `--msg-format` 的输出字段说明（解析输出前先运行此命令） |
| `--dry-run` | — | 仅预览订阅请求，不实际连接 |

## --msg-format 输出结构（--format json）

每条事件输出为一行 NDJSON。

**`metadata`**（默认，适合分拣/通知）
```json
{"ok":true,"data":{"message":{"message_id":"...","thread_id":"...","subject":"...","head_from":{"name":"Alice","mail_address":"alice@example.com"},"to":[{"name":"Bob","mail_address":"bob@example.com"}],"folder_id":"INBOX","label_ids":["IMPORTANT"],"internal_date":"1742800000000","message_state":1,"body_preview":"Please find attached..."}}}
```

**`minimal`**（仅 ID 和状态，适合追踪已读/文件夹变更）
```json
{"ok":true,"data":{"message":{"message_id":"...","thread_id":"...","folder_id":"INBOX","label_ids":["IMPORTANT"],"internal_date":"1742800000000","message_state":1}}}
```

**`plain_text_full`**（metadata 全部字段 + 完整纯文本正文）
```json
{"ok":true,"data":{"message":{"message_id":"...","subject":"...","head_from":{...},"folder_id":"INBOX","label_ids":[...],"body_preview":"...","body_plain_text":"<base64url>"}}}
```

**`event`**（原始 WebSocket 事件，不发起 API 请求，适合调试）
```json
{"ok":true,"data":{"header":{"event_id":"abc123","event_type":"mail.user_mailbox.event.message_received_v1","create_time":"1742800000000"},"event":{"message_id":"...","mail_address":"user@example.com"}}}
```

**`full`**（全部字段，含 HTML 正文和附件）
```json
{"ok":true,"data":{"message":{"message_id":"...","subject":"...","head_from":{...},"body_preview":"...","body_plain_text":"<base64url>","body_html":"<base64url>","attachments":[{"name":"report.pdf","size":102400}]}}}
```

## ⛔ 空结果处理

`+watch` 持续监听 WebSocket 事件流，"长时间没有事件"或"连接异常"分三种语义，必须区分上报。**禁止**为了让流程看起来"有进展"而通过 `+send` 自发邮件给自己以制造一个收信事件。

| 情形 | 表现 | 正确做法 |
|------|------|----------|
| 长时间无事件（业务上的"空数组"） | 连接正常但收件箱在监听窗口内确实没有新邮件 | 如实回报"监听窗口内没有新邮件"，附监听时长 / 过滤条件；不要当作错误，也不要主动发信凑事件 |
| 404 / 订阅缺失 | 启动时 WebSocket 握手成功但服务端返回订阅未注册（`mail.user_mailbox.event.message_received_v1` 未在应用 event 列表中） | 回报"事件订阅未注册"，提示用户在飞书开发者后台为应用添加 `mail.user_mailbox.event.message_received_v1`，并确保已申请 `mail:event` scope |
| 网络 / 权限错误（5xx / 401 / 403 / WebSocket 断开） | 鉴权失败、scope 不足、网络中断、服务端 5xx | 回报错误类别并建议用户重试或检查身份；连接断开后**不要**改用 `+send` 自给自足，要先恢复监听 |

> 详见 SKILL.md 「## ⛔ Non-goals（不应做的事）」第 1 条 与 「## ⚠️ 安全规则」第 9 条。

## 参考

- [lark-mail](../SKILL.md) — 邮箱域总览
- [lark-mail-triage](lark-mail-triage.md) — 邮件摘要列表
- [lark-event-subscribe](../../lark-event/references/lark-event-subscribe.md) — 通用事件订阅
