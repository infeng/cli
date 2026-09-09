# mail +thread-modify

> **前置条件：** 先阅读 [`../../lark-shared/SKILL.md`](../../lark-shared/SKILL.md) 了解认证、全局参数和安全规则。

使用 `mail +thread-modify` 在一次批量请求中修改整个邮件会话的标签或所在文件夹。如果操作对象是单封邮件的 `message_id`，请使用 [`mail +message-modify`](./lark-mail-message-modify.md)。

## 命令

```bash
# 给多个会话添加标签；同一 flag 可重复，也可用逗号分隔
lark-cli mail +thread-modify --thread-id <thread_id_1>,<thread_id_2> --add-label-id <label_id>

# 同时移除标签并移动文件夹
lark-cli mail +thread-modify --thread-id <thread_id> --remove-label-id <label_id> --folder-id <folder_id>

# 使用 bot 身份时必须显式指定邮箱
lark-cli mail +thread-modify --as bot --mailbox user@example.com --thread-id <thread_id> --folder-id <folder_id>

# Dry Run：查看实际 method、path 和 body，不发送请求
lark-cli mail +thread-modify --thread-id <thread_id> --add-label-id <label_id> --dry-run
```

## 参数

| 参数 | 必填 | 说明 |
|------|------|------|
| `--mailbox <email>` | 否 | 会话所属邮箱，默认 `me`；使用 `--as bot` 时必须显式传邮箱地址 |
| `--thread-id <id>` | 是 | 会话 ID；支持逗号分隔和重复传参，trim 后按首次出现顺序去重 |
| `--add-label-id <id>` | 否 | 要添加的标签 ID；支持逗号分隔和重复传参 |
| `--remove-label-id <id>` | 否 | 要移除的标签 ID；不能与 `--add-label-id` 的规范化结果有交集 |
| `--folder-id <id>` | 否 | 目标文件夹 ID；仅映射到请求体的 `add_folder` 字段 |

`--add-label-id`、`--remove-label-id`、`--folder-id` 至少传一个。所有 ID 会去除首尾空白；空 ID 会在发请求前返回参数错误。

本 shortcut 不提供 `--data` 或 `--add-folder`。`--folder-id` 不会进入 URL，也不会成为任意 JSON key。

## 副作用与返回值

- 修改作用于整个 thread，不是单封邮件。
- 所有规范化后的 `thread_id` 在一次 `threads.batch_modify` 请求中提交；CLI 不拆分请求，也不自动重试写操作。
- 成功时原样输出底层接口的 `data`；CLI 不根据输入数量生成 `updated_count`、成功 ID 或逐项结果。
- 如果底层返回空 `data`，保持标准空成功输出，不推断哪些会话已经生效。

## 相关命令

- `lark-cli mail +triage` — 浏览邮件摘要并获取真实 `thread_id`
- `lark-cli mail +thread` — 读取完整会话
- `lark-cli mail +message-modify` — 按 `message_id` 修改单封邮件
- `lark-cli mail +thread-trash` — 按 `thread_id` 软删除整个会话
