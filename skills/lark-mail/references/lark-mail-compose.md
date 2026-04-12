# mail compose reference (scheduled send)

> **前置条件：** 先阅读 [`../../lark-shared/SKILL.md`](../../lark-shared/SKILL.md) 了解认证、全局参数和安全规则。

## +draft-send (发送草稿 / 定时发送)

发送已有草稿，支持立即发送和定时发送。

```bash
# 立即发送草稿
lark-cli mail +draft-send --draft-id DR_xxx

# 定时发送 — 绝对时间（Unix 秒）
lark-cli mail +draft-send --draft-id DR_xxx --send-time 1775846400

# 定时发送 — 相对时间
lark-cli mail +draft-send --draft-id DR_xxx --send-after 30m
lark-cli mail +draft-send --draft-id DR_xxx --send-after 2h
lark-cli mail +draft-send --draft-id DR_xxx --send-after 1d
```

### 参数

| 参数 | 必填 | 说明 |
|------|------|------|
| `--draft-id` | 是 | 要发送的草稿 ID |
| `--mailbox` | 否 | 邮箱 ID（默认 me） |
| `--send-time` | 否 | Unix 时间戳（秒），定时发送的绝对时间。至少 5 分钟后。 |
| `--send-after` | 否 | 相对时间（如 30m、2h、1d），从现在起多久后发送。至少 5 分钟后。 |

- `--send-time` 优先级高于 `--send-after`。同时设置时使用 `--send-time` 并打印 warning。
- 不设置任何定时参数时，草稿立即发送。

## +send (新邮件 + 定时发送)

`+send` 也支持 `--send-time` / `--send-after`，但仅在 `--confirm-send` 模式下生效：

```bash
# 创建草稿并定时发送
lark-cli mail +send --to alice@example.com --subject '周报' \
  --body '<p>本周进展</p>' --confirm-send --send-after 1h
```

## +cancel-scheduled-send (取消定时发送)

取消处于 SCHEDULED 状态的邮件，将其退回 DRAFT 状态。

```bash
lark-cli mail +cancel-scheduled-send --message-id MSG_xxx
lark-cli mail +cancel-scheduled-send --mailbox me --message-id MSG_xxx
```

### 参数

| 参数 | 必填 | 说明 |
|------|------|------|
| `--message-id` | 是 | 定时发送邮件的 message ID (messageBizID) |
| `--mailbox` | 否 | 邮箱 ID（默认 me） |
