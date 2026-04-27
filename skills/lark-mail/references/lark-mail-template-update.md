# mail +template-update

> **前置条件：** 先阅读 [`../../lark-shared/SKILL.md`](../../lark-shared/SKILL.md) 了解认证、全局参数和安全规则。

全量替换一个已有模板（PUT `/open-apis/mail/v1/user_mailboxes/<mailbox>/templates/<template_id>`）。

⚠️ **last-write-wins**：服务端**没有**乐观锁。两个客户端并发更新同一个模板，后写者的内容会完整覆盖先写者。命令在 DryRun 与 Execute 阶段都会向 stderr 输出警告。

## 三种入口

| 模式 | 触发 flag | 行为 |
|------|----------|------|
| 1. 检视 | `--inspect` | 只 GET 模板并打印投影；不 PUT |
| 2. 打印 patch 骨架 | `--print-patch-template` | 输出可编辑的 JSON 骨架 |
| 3. 应用 patch | `--patch-file <path>` 与/或 `--set-*` 任一 | 内部流：GET → 内存合并 patch + flat overrides → PUT 全量替换 |

三种模式互斥。优先级：`--set-*` flag > `--patch-file` > 现有内容。

## 命令

```bash
# 模式 1：检视
lark-cli mail +template-update --template-id 1234 --inspect

# 模式 2：打印 patch 骨架
lark-cli mail +template-update --template-id 1234 --print-patch-template

# 模式 3a：单字段扁平覆盖
lark-cli mail +template-update --template-id 1234 --set-subject '新主题'

# 模式 3b：patch-file + flat 覆盖（flat 优先）
lark-cli mail +template-update --template-id 1234 \
  --patch-file ./patch.json --set-content '<p>覆盖正文</p>'

# Dry Run
lark-cli mail +template-update --template-id 1234 --set-subject '...' --dry-run
```

## 参数

| 参数 | 必填 | 说明 |
|------|------|------|
| `--mailbox <email>` | 否 | 默认 me |
| `--template-id <int>` | 是 | 十进制 int64 模板 ID |
| `--inspect` | 否 | 模式 1 |
| `--print-patch-template` | 否 | 模式 2 |
| `--patch-file <path>` | 否 | 模式 3：JSON 补丁文件 |
| `--set-name/--set-subject/--set-content` | 否 | 模式 3 扁平覆盖 |
| `--set-to/--set-cc/--set-bcc` | 否 | 模式 3 收件人替换（PUT 全量替换语义） |
| `--set-attach` | 否 | 模式 3：附件本地路径 csv 替换；LARGE 分支拒绝 |
| `--set-inline` | 否 | 模式 3：JSON 数组替换 inline 图片 |
| `--plain-text` | 否 | 当 `--set-content` 提供时按纯文本模式包装 |

## 服务端硬约束（客户端镜像）

- `template_content` ≤ 3 MB（rune / byte 取严）
- `template_content + inline + 非 inline SMALL` 累计 ≤ 25 MB
- inline 图片**永远** `attachment_type=SMALL`

## 相关命令

- `lark-cli mail +template-create` — 创建模板
