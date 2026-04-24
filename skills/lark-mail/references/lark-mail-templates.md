# Templates — 邮件模板（个人）

个人邮件模板管理。CRUD 接口在 `mail.user_mailbox.templates.*`（`list` / `get` / `delete` 直接走 Meta API），`create` / `update` 由于要处理 HTML 内嵌图片的 Drive 上传，封装为 Shortcut。

> 模板类型：LMS USER 模板。单模板数据 ≤ 3 MB，用户模板数量 ≤ 20。不支持 TENANT。

## 调用方式速查

| 操作 | 命令 | 备注 |
|------|------|------|
| 创建 | `lark-cli mail +template-create ...` | Shortcut；自动处理 HTML `<img>` 本地路径 |
| 更新 | `lark-cli mail +template-update ...` | Shortcut；支持 `--inspect` / `--print-patch-template` / `--patch-file` / 扁平 `--set-*` flag |
| 列出 | `lark-cli mail user_mailbox.templates list --params '{"user_mailbox_id":"me"}'` | 直接调 Meta API；只返回 `template_id + name` |
| 获取 | `lark-cli mail user_mailbox.templates get --params '{"user_mailbox_id":"me","template_id":"<id>"}'` | 返回完整模板 |
| 删除 | `lark-cli mail user_mailbox.templates delete --params '{"user_mailbox_id":"me","template_id":"<id>"}'` | 删除不可恢复 |
| 附件下载链接 | `lark-cli mail user_mailbox.template_attachments download_url --params '{"user_mailbox_id":"me","template_id":"<tid>"}' --data '{"attachment_ids":["<aid>"]}'` | 下载模板中的附件 |

## `+template-create`

创建一个新模板。支持正文 HTML 内嵌图片 + 非 inline 附件。

**核心 flags**:
- `--mailbox`（可选，默认 `me`）
- `--name`（必填，≤100 字符）
- `--subject`（可选，模板默认主题）
- `--template-content` / `--template-content-file`（二选一，正文内容；HTML 首选）
- `--plain-text`（标为纯文本模式）
- `--to` / `--cc` / `--bcc`（逗号分隔，支持 `Name <email>` 格式）
- `--attach`（逗号分隔，非 inline 附件路径）

**HTML 内嵌图片**：

正文中所有 `<img src="./local.png">`（任何不带 URI scheme 的路径）都会被：
1. 上传到 Drive（≤20 MB 走 `medias/upload_all`，>20 MB 走 `upload_prepare + upload_part + upload_finish` 三步）
2. 生成 UUIDv4 CID
3. 原 HTML 改写为 `<img src="cid:<uuid>">`
4. 在 `attachments[]` 追加 `{id: <file_key>, cid, is_inline: true, filename, attachment_type}`

**LARGE 切换**：本地单文件 ≤ 20 MB 走单次上传、> 20 MB 走分块上传；`attachment_type` 则看累计 EML 投影（subject / to / cc / bcc / template_content + base64 附件体积），到 25 MB 后同批次剩余附件标 `LARGE`。两套判定相互独立。

**顺序约束**：
- `inline` 按正文 `<img>` 出现顺序处理
- 非 inline 按 `--attach` flag 书写顺序处理（多个 `,` 分隔保留顺序）

**示例**：

```bash
lark-cli mail +template-create --as user \
  --mailbox me --name '周报模板' \
  --subject '本周进展' \
  --template-content '<p>大家好，请见本周进展：</p><img src="./banner.png"><p>……</p>' \
  --attach './plan.xlsx,./budget.csv' \
  --to 'alice@example.com,bob@example.com'
```

## `+template-update`

全量替换式更新（后端无乐观锁 → **last-write-wins**，并发更新可能丢失）。

**入口 flag**：
- `--template-id <id>`（必填，除非只用 `--print-patch-template`）
- `--inspect`：打印模板 projection，不写
- `--print-patch-template`：打印 `--patch-file` 的 JSON 骨架
- `--patch-file <path>`：结构化 patch 文件，与扁平 flag 合并应用

**扁平 flag**：
- `--set-name` / `--set-subject` / `--set-template-content` / `--set-template-content-file`
- `--set-plain-text`（布尔置 true；不提供也不会置 false）
- `--set-to` / `--set-cc` / `--set-bcc`
- `--attach <path,path>`（追加非 inline 附件，书写顺序敏感）

**合并策略**：GET 当前模板 → 应用 flat flags 与 `--patch-file`（patch-file 字段非空即覆盖）→ 重新扫描 `--set-template-content*` 中的 `<img>` 本地路径并上传 / 改写 cid → PUT 整个模板。所有原有附件保留；`--attach` 新增附件以新的 `emlProjectedSize` 独立计算 LARGE / SMALL。

**示例**：

```bash
# 先看当前状态
lark-cli mail +template-update --as user --template-id 712345 --inspect

# 拿到 patch 骨架
lark-cli mail +template-update --as user --print-patch-template > /tmp/tpl-patch.json
# 编辑 /tmp/tpl-patch.json 后
lark-cli mail +template-update --as user --template-id 712345 --patch-file /tmp/tpl-patch.json

# 或者直接用扁平 flag
lark-cli mail +template-update --as user --template-id 712345 \
  --set-subject '每周五发布' \
  --set-cc 'manager@example.com'
```

> 每次成功更新 CLI 会在 stderr 打印 `warning: template endpoints have no optimistic locking; concurrent updates are last-write-wins.`。

## 在发信类 Shortcut 中套用模板：`--template-id`

以下 5 个 Shortcut 新增 `--template-id` 标志：`+send` / `+draft-create` / `+reply` / `+reply-all` / `+forward`。

**`--template-id` 必须是十进制整数字符串**，否则报 `ErrValidation`。

**合并规则（对齐 `lark/desktop`）**：

| # | 场景 | 合并策略 |
|---|------|----------|
| Q1 to/cc/bcc | `(草稿,模板)` 分别发送 4 种组合 | (正常,正常) 全量追加；(正常,分别) 模板 bcc→草稿 to；(分别,正常) 模板 to+cc+bcc→草稿 bcc；(分别,分别) 模板 bcc→草稿 bcc。**无去重**，用户显式 `--to/--cc/--bcc` 最先覆盖 draft-derived 侧再参与矩阵注入 |
| Q2 subject | `+send / +draft-create` | 用户 `--subject` > 草稿 subject（若已有）> 模板 subject |
|  | `+reply / +reply-all / +forward` | 用户 `--subject` 覆盖自动生成的 Re:/Fw: 前缀；否则保持 Re:/Fw: + 原邮件 subject |
| Q3 body | `+send / +draft-create` | 空草稿 body → 用模板；非空 → 追加 |
|  | `+reply / +reply-all / +forward` | 将模板内容注入 `<blockquote>` 之前（正则匹配 `history-quote-wrapper`），无 blockquote 则追加末尾；纯文本走 emlbuilder plain-text 追加 |
| Q4 附件 | 所有 5 个 Shortcut | 过期 / 封禁超大附件丢弃；`emlProjectedSize` 累计 > 25MB 则模板小附件改标 LARGE；去重键 = `Attachment.id`（Drive file_key）；顺序 = 草稿在前、模板在后 |
| Q5 cid 冲突 | inline 图片 | cid 由 UUID v4 生成（碰撞概率 ~ 2^-122），不显式检测 |

**IsSendSeparately**：模板不承载该字段；`+template-create` / `+template-update` / `--template-id` 合并 / `drafts.send` 均不涉及此字段。若需要按收件人分别发送，请显式使用 `+send --send-separately` 等 Shortcut 的预置开关。

**Warning**：`+reply` / `+reply-all` + 模板且模板有 to/cc/bcc 时，Execute 时 CLI 在 stderr 输出提示：

```text
warning: template to/cc/bcc are appended without de-duplication; you may see repeated recipients. Use --to/--cc/--bcc to override, or run +template-update to clear template addresses.
```

**示例**：

```bash
# 用模板发送，仅覆盖主要收件人
lark-cli mail +send --as user --template-id 712345 \
  --to alice@example.com --confirm-send

# 把模板套用到回复草稿；模板正文会插到 <blockquote> 之前
lark-cli mail +reply --as user --message-id <msg-id> --template-id 712345 \
  --body '<p>附上上次的约定：</p>'

# 转发并用模板覆盖默认 Fw: 前缀
lark-cli mail +forward --as user --message-id <msg-id> \
  --template-id 712345 --subject '请查阅：Q3 指标汇总' \
  --to alice@example.com
```

## DryRun 行为

- `--template-id` 场景：多一条 `GET /open-apis/mail/v1/user_mailboxes/:id/templates/:tid` 步骤，后接既有 compose 步骤。
- `+template-create` / `+template-update`：根据本地 `<img>` 与 `--attach` 文件逐个输出 Drive 上传步骤，依 20MB 阈值区分 `upload_all` 或 `upload_prepare + upload_part + upload_finish` 三条。
- `+template-update --print-patch-template` / `--inspect` 分别只打印 patch 骨架 / 不修改数据的 GET，不会触发任何写操作。

## 权限

| 方法 | 所需 scope |
|------|-----------|
| `user_mailbox.templates.create` | `mail:user_mailbox.message:modify` |
| `user_mailbox.templates.list` | `mail:user_mailbox.message:readonly,mail:user_mailbox.message:modify` |
| `user_mailbox.templates.get` | `mail:user_mailbox.message:readonly,mail:user_mailbox.message:modify` |
| `user_mailbox.templates.update` | `mail:user_mailbox.message:modify` |
| `user_mailbox.templates.delete` | `mail:user_mailbox.message:modify` |
| `user_mailbox.template_attachments.download_url` | `mail:user_mailbox.message:readonly` |

## 错误码速查

`15_08_02xx` 号段，与消息域 `15_08_00xx` 区段相邻：

| errno | HTTP | 触发 |
|-------|------|------|
| `15080201 InvalidTemplateName` | 400 | `name` 为空或超 100 字符 |
| `15080202 TemplateNumberLimit` | 400 | 已达 20 模板上限 |
| `15080203 TemplateContentSizeLimit` | 400 | 单模板 > 3 MB |
| `15080204 InvalidTemplateID` | 404 | `template_id` 不存在或不属于当前用户 |
| `15080205 TemplateBatchSizeLimit` | 400 | 批量接口批次过大 |
| `15080206 TemplateTotalSizeLimit` | 400 | 所有模板总大小 > 50 MB |
| `15080207 InvalidTemplateParam` | 400 | 其他参数错误（含 `template_id` 无法 parseInt） |
| `15080208 TemplateAttachmentNotFound` | 404 | 下载 URL 请求的 `attachment_id` 不属于该模板 |
| `15080209 TemplateAttachmentForbidden` | 403 | 当前用户无权访问指定附件 |
