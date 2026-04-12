# mail list reference (SCHEDULED label)

> **前置条件：** 先阅读 [`../../lark-shared/SKILL.md`](../../lark-shared/SKILL.md) 了解认证、全局参数和安全规则。

## 列出定时发送邮件

使用 `+triage` 的 `--filter` 参数并指定 `folder: scheduled` 可列出所有处于 SCHEDULED 状态的邮件：

```bash
# 列出所有定时发送中的邮件
lark-cli mail +triage --filter '{"folder":"scheduled"}'

# 也可以通过原生 API 使用 label_id=SCHEDULED
lark-cli mail user_mailbox message list \
    --user_mailbox_id me \
    --label_id SCHEDULED \
    --page_size 20
```

## SCHEDULED 标签说明

| 标签/文件夹 | 说明 |
|------------|------|
| `SCHEDULED` | 已设置定时发送但尚未到达发送时间的邮件。通过 `+draft-send --send-time` 或 `--send-after` 设置。 |

- 定时发送的邮件从 DRAFT 文件夹移到 SCHEDULED 文件夹
- 取消定时发送后，邮件从 SCHEDULED 退回 DRAFT
- 到达发送时间并成功发送后，邮件从 SCHEDULED 移到 SENT
