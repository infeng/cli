# mail +template-create

> **前置条件：** 先阅读 [`../../lark-shared/SKILL.md`](../../lark-shared/SKILL.md) 了解认证、全局参数和安全规则。

创建一个个人邮件模板（POST `/open-apis/mail/v1/user_mailboxes/<mailbox>/templates`）。模板存储 name / subject / content（HTML 或纯文本）/ 收件人 / 附件，可在 `+send` / `+draft-create` 等链路里通过模板 id 应用。

模板的 LARGE 附件分支客户端**直接拒绝**——模板内容必须能内嵌进 EML（25 MB 累计上限），保证 apply 进 draft 时不被迫降级。

## 安全约束

- 模板正文（HTML 或经 `--plain-text` 包装后的 plain）**字节 / rune 任一**超过 3 MB 就拒绝（镜像服务端 `template_service.go:1064`），不要等 errno 15180203。
- `template_content + inline + 非 inline SMALL` 累计超过 25 MB 直接拒绝，不会改判 LARGE。
- inline 图片**永远** `attachment_type=SMALL`：cid 引用必须能 resolve 到内嵌 MIME part；LARGE 是下载 URL，无法 cid embed。

## 命令

```bash
# 最简：纯 HTML 模板
lark-cli mail +template-create \
  --name '周报模板' --subject '本周进展' \
  --body '<p>本周完成：</p><ul><li>...</li></ul>'

# 带本地内嵌图片（自动扫描 <img src="./...">）
lark-cli mail +template-create \
  --name '签名模板' --subject 'Hello' \
  --content '<p>Hi <img src="./banner.png" /></p>'

# 纯文本模式（content 会经 buildBodyDiv 包装为 HTML）
lark-cli mail +template-create \
  --name '简短通知' --subject '提醒' \
  --content $'第一行\n第二行' --plain-text

# Dry Run（仅打印 Drive upload 步骤 + POST 请求，不执行）
lark-cli mail +template-create --name n --subject s --content '<p>hi</p>' --dry-run
```

## 参数

| 参数 | 必填 | 说明 |
|------|------|------|
| `--mailbox <email>` | 否 | 模板所属邮箱（默认 me） |
| `--name <text>` | 是 | 模板显示名 |
| `--subject <text>` | 是 | 默认主题 |
| `--content <text>` | 是 | HTML 或纯文本正文（`--plain-text` 时会经 buildBodyDiv 包装） |
| `--plain-text` | 否 | 强制纯文本模式，存储前经 `\n→<br>` + HTML escape + `<div>` 包装 |
| `--to/--cc/--bcc <emails>` | 否 | 默认收件人列表（逗号分隔），允许全空 |
| `--attach <paths>` | 否 | 普通附件本地相对路径（逗号分隔），按文件大小自动分发 Drive ≤20MB / >20MB 上传 |
| `--inline <json>` | 否 | 内嵌图片 JSON 数组：`[{"cid":"<id>","file_path":"<rel-path>"}]`。inline 永远 SMALL；与 `--plain-text` 互斥 |
| `--format <mode>` | 否 | 输出格式（json / pretty / table / ndjson / csv） |
| `--dry-run` | 否 | 仅打印请求 |

## 返回值

```json
{
  "ok": true,
  "data": {
    "template_id": "...",
    "name": "...",
    "attachments": <count>
  }
}
```

## 相关命令

- `lark-cli mail +template-update` — 更新模板（last-write-wins，无乐观锁）
