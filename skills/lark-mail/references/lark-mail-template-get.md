# mail +template-get

> **前置条件：** 先阅读 [`../../lark-shared/SKILL.md`](../../lark-shared/SKILL.md) 了解认证、全局参数和安全规则。

按 ID 读取个人邮件模板（subject / body / 收件人 / 附件元信息）。

本 skill 对应 shortcut `lark-cli mail +template-get`，内部步骤：
1. `GET /open-apis/mail/v1/user_mailboxes/{mailbox}/templates/{template_id}` — 获取模板详情

## 命令

```bash
# 读取一个模板
lark-cli mail +template-get --template-id <template-id>

# 指定邮箱
lark-cli mail +template-get --mailbox user@example.com --template-id <template-id>

# JSON 输出（直出原始响应，含完整 template_content）
lark-cli mail +template-get --template-id <template-id> --format json

# Dry Run
lark-cli mail +template-get --template-id <template-id> --dry-run
```

## 参数

| 参数 | 必填 | 默认值 | 说明 |
|------|------|--------|------|
| `--template-id <id>` | 是 | — | 模板 ID（十进制整数字符串） |
| `--mailbox <email>` | 否 | 当前用户 | 邮箱地址（`user_mailbox_id`） |
| `--format <mode>` | 否 | json | 输出格式：`json`（默认）/ `pretty` / `table` / `ndjson` / `csv` |
| `--dry-run` | 否 | — | 仅打印请求，不执行 |

## 返回值

`--format json`（默认）直出 OAPI 原始响应，便于脚本使用：

```json
{
  "ok": true,
  "data": {
    "code": 0,
    "data": {
      "template_id":      "12345",
      "name":             "季度汇报",
      "subject":          "Q1 Status",
      "template_content": "<p>...</p>",
      "to":               [{"mail_address": "boss@example.com"}],
      "cc":               [],
      "bcc":              [],
      "attachments":      [{"id": "att_1", "filename": "report.pdf"}]
    }
  }
}
```

`--format pretty` 仅展示概览：`template_id` / `name` / `subject` / `template_content`（截断到 200 字符）/ `attachments`（数量）/ `recipients`（to + cc + bcc 总数）。

## 注意事项

- `template_content` 可能是 HTML 且体积较大（最大 3 MB）。pretty 模式只展示前 200 字符；查看完整内容请用 `--format json | jq -r '.data.data.template_content'`。
- 仅查看模板内容，不会创建 / 编辑 / 删除模板。
- `template_id` 服务端为 int64，传输使用十进制字符串；CLI Validate 阶段对格式做前置校验，避免无效请求。

## 相关命令

- `lark-cli mail +send` / `+draft-create` — 基于模板发送或起草邮件（参见各自的 skill 文档）
- `lark-cli mail +signature` — 列出 / 查看邮件签名
