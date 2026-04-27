// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package mail

import (
	"context"
	"fmt"
	"io"
	"regexp"

	"github.com/larksuite/cli/internal/output"
	"github.com/larksuite/cli/internal/util"
	"github.com/larksuite/cli/shortcuts/common"
)

// templateIDPattern matches a non-empty decimal integer string (no sign, no
// leading zero requirement). Validation runs in the Validate stage before
// any API call, so the user gets a structured errno instead of a server
// 4xx round-trip.
var templateIDPattern = regexp.MustCompile(`^\d+$`)

// MailTemplateGet is the `+template-get` shortcut: fetch a personal mail
// template by ID. Returns subject / body / recipient and attachment metadata
// for a single template under a user mailbox.
var MailTemplateGet = common.Shortcut{
	Service:     "mail",
	Command:     "+template-get",
	Description: "Use when reading a single personal mail template by ID. Returns subject, body, recipient lists and attachment metadata.",
	Risk:        "read",
	Scopes:      []string{"mail:user_mailbox.template:readonly"},
	AuthTypes:   []string{"user", "bot"},
	HasFormat:   true,
	Flags: []common.Flag{
		{Name: "mailbox", Default: "me", Desc: "email address (default: me)"},
		{Name: "template-id", Desc: "Required. Personal mail template ID (decimal integer string)", Required: true},
	},
	Validate: func(ctx context.Context, runtime *common.RuntimeContext) error {
		tid := runtime.Str("template-id")
		if tid == "" {
			return output.ErrValidation("--template-id is required")
		}
		if !templateIDPattern.MatchString(tid) {
			return output.ErrValidation("--template-id: must be a decimal integer string")
		}
		return nil
	},
	DryRun: func(ctx context.Context, runtime *common.RuntimeContext) *common.DryRunAPI {
		mailboxID := resolveMailboxID(runtime)
		templateID := runtime.Str("template-id")
		return common.NewDryRunAPI().
			Desc("Fetch one mail template").
			GET(mailboxPath(mailboxID, "templates", templateID))
	},
	Execute: func(ctx context.Context, runtime *common.RuntimeContext) error {
		mailboxID := resolveMailboxID(runtime)
		hintIdentityFirst(runtime, mailboxID)
		templateID := runtime.Str("template-id")

		raw, err := runtime.RawAPI("GET", mailboxPath(mailboxID, "templates", templateID), nil, nil)
		if err != nil {
			return fmt.Errorf("failed to fetch mail template: %w", err)
		}
		respMap, ok := raw.(map[string]interface{})
		if !ok {
			return output.Errorf(output.ExitAPI, "api_error", "unexpected response shape from mail template API")
		}
		if code, hasCode := respMap["code"]; hasCode {
			if codeFloat, ok := util.ToFloat64(code); ok && codeFloat != 0 {
				msg, _ := respMap["msg"].(string)
				return output.ErrAPI(int(codeFloat), msg, respMap["error"])
			}
		}

		runtime.OutFormat(raw, &output.Meta{Count: 1}, func(w io.Writer) {
			data := extractTemplateData(respMap)
			renderTemplatePretty(w, data)
		})
		return nil
	},
}

// extractTemplateData unwraps the OAPI envelope's data field. Falls back to
// the response itself when no `data` wrapper is present (for unit tests that
// pass through a plain object).
func extractTemplateData(resp map[string]interface{}) map[string]interface{} {
	if data, ok := resp["data"].(map[string]interface{}); ok {
		return data
	}
	return resp
}

// recipientCount sums the lengths of to/cc/bcc lists, defensively handling
// both `to` and `to_recipients` style keys to match whichever the actual
// OAPI response uses.
func recipientCount(data map[string]interface{}) int {
	total := 0
	for _, key := range []string{"to", "to_recipients", "cc", "cc_recipients", "bcc", "bcc_recipients"} {
		if list, ok := data[key].([]interface{}); ok {
			total += len(list)
		}
	}
	return total
}

// attachmentCount returns len(data.attachments) defensively.
func attachmentCount(data map[string]interface{}) int {
	if list, ok := data["attachments"].([]interface{}); ok {
		return len(list)
	}
	return 0
}

// renderTemplatePretty writes the human-readable summary of a mail template
// for `--format pretty`. `template_content` is rune-truncated to 200 chars
// to keep the output single-screen even when templates carry multi-MB HTML.
func renderTemplatePretty(w io.Writer, data map[string]interface{}) {
	templateID, _ := data["template_id"].(string)
	name, _ := data["name"].(string)
	subject, _ := data["subject"].(string)
	content, _ := data["template_content"].(string)

	fmt.Fprintf(w, "template_id:      %s\n", templateID)
	fmt.Fprintf(w, "name:             %s\n", name)
	fmt.Fprintf(w, "subject:          %s\n", subject)
	fmt.Fprintf(w, "template_content: %s\n", common.TruncateStr(content, 200))
	fmt.Fprintf(w, "attachments:      %d\n", attachmentCount(data))
	fmt.Fprintf(w, "recipients:       %d\n", recipientCount(data))
}
