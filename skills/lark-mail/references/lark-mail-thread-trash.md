# mail +thread-trash

> **前置条件：** 先阅读 [`../../lark-shared/SKILL.md`](../../lark-shared/SKILL.md) 了解认证、全局参数和安全规则。

使用 `mail +thread-trash` 在一次批量请求中软删除整个邮件会话。如果操作对象是单封邮件的 `message_id`，请使用 [`mail +message-trash`](./lark-mail-message-trash.md)。

## 命令

```bash
# 先用 dry-run 查看将发送的请求
lark-cli mail +thread-trash --thread-id <thread_id_1>,<thread_id_2> --dry-run

# 用户确认后执行；同一 flag 可重复
lark-cli mail +thread-trash --thread-id <thread_id_1> --thread-id <thread_id_2> --yes

# 使用 bot 身份时必须显式指定邮箱
lark-cli mail +thread-trash --as bot --mailbox user@example.com --thread-id <thread_id> --yes
```

## 参数

| 参数 | 必填 | 说明 |
|------|------|------|
| `--mailbox <email>` | 否 | 会话所属邮箱，默认 `me`；使用 `--as bot` 时必须显式传邮箱地址 |
| `--thread-id <id>` | 是 | 会话 ID；支持逗号分隔和重复传参，trim 后按首次出现顺序去重 |
| `--yes` | 执行时必填 | 高风险写操作确认；dry-run 不需要 |

空 ID 会在发请求前返回参数错误。示例中的 ID 都是占位值；实际执行前应通过 `+triage`、`+message`、`+thread` 或会话列表取得真实 `thread_id`。

## 副作用与返回值

- 软删除作用于整个 thread，不是单封邮件；执行前先向用户展示 dry-run 预览并取得确认。
- 所有规范化后的 ID 在一次 `threads.batch_trash` 请求中提交；请求体只含 `thread_ids`。
- CLI 不拆分请求，也不自动重试写操作。
- 成功时原样输出底层接口的 `data`；CLI 不根据输入数量生成 `trashed_count`、成功 ID 或逐项结果。

## 相关命令

- `lark-cli mail +triage` — 浏览邮件摘要并获取真实 `thread_id`
- `lark-cli mail +thread` — 读取完整会话
- `lark-cli mail +message-trash` — 按 `message_id` 软删除单封邮件
- `lark-cli mail +thread-modify` — 按 `thread_id` 修改会话标签或文件夹
