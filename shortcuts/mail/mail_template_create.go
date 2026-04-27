// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package mail

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/larksuite/cli/internal/output"
	"github.com/larksuite/cli/shortcuts/common"
)

// MailTemplateCreate is the `+template-create` shortcut: create a new
// personal mail template via POST
// /open-apis/mail/v1/user_mailboxes/<mailbox>/templates.
//
// Compose pipeline lives in template_compose.go and is shared with
// +template-update. See sibling contract S1-contract.md for the transport
// decisions this implements.
var MailTemplateCreate = common.Shortcut{
	Service:     "mail",
	Command:     "+template-create",
	Description: "Create a personal mail template (subject + HTML/plain body + recipients + attachments). The body and any local <img src> images / --attach files are validated and uploaded before POSTing.",
	Risk:        "write",
	Scopes:      []string{"mail:user_mailbox.message:modify", "mail:user_mailbox:readonly"},
	AuthTypes:   []string{"user", "bot"},
	HasFormat:   true,
	Flags: []common.Flag{
		{Name: "mailbox", Desc: "Optional. Mailbox email or open_id that owns the template (default: me)."},
		{Name: "name", Desc: "Required. Template display name.", Required: true},
		{Name: "subject", Desc: "Required. Default subject stored on the template.", Required: true},
		{Name: "content", Desc: "Required. Template HTML or plain-text body. Use --plain-text to force plain mode (body will be HTML-escaped + <br>-wrapped via buildBodyDiv before storage). Server cap: 3 MB byte/rune.", Required: true},
		{Name: "plain-text", Type: "bool", Desc: "Wrap --content via the mail compose buildBodyDiv helper (HTML escape + \\n→<br> + <div>) so plain-text bodies render with line breaks. Cannot be combined with --inline."},
		{Name: "to", Desc: "Optional default To recipients (comma-separated). Templates may have zero recipients."},
		{Name: "cc", Desc: "Optional default Cc recipients (comma-separated)."},
		{Name: "bcc", Desc: "Optional default Bcc recipients (comma-separated)."},
		{Name: "attach", Desc: "Optional. Comma-separated relative paths of regular attachments. Each is uploaded to Drive (≤20MB upload_all, >20MB chunked) and registered as attachment_type=SMALL. Templates reject the LARGE branch entirely (server-mirrored 25 MB cumulative cap)."},
		{Name: "inline", Desc: "Optional. JSON array '[{\"cid\":\"<id>\",\"file_path\":\"<rel-path>\"}]' for inline images. Inline is always SMALL — see standard-mail-shortcut.md §1. Cannot be combined with --plain-text."},
	},
	DryRun: func(ctx context.Context, runtime *common.RuntimeContext) *common.DryRunAPI {
		input := readTemplateCreateInput(runtime)
		mailboxID := resolveComposeMailboxID(runtime)
		api := common.NewDryRunAPI().
			Desc("Upload local <img> images + --attach files to Drive (POST upload_all for ≤20MB, upload_prepare+upload_part+upload_finish for >20MB), then POST the assembled template body. Drive upload dispatch depends on file size only and is independent of attachment_type.")
		steps, err := buildTemplateDryRunSteps(runtime, input)
		if err != nil {
			return api.Set("error", err.Error())
		}
		for _, s := range steps {
			api = api.POST(s.Path).Body(map[string]interface{}{"file": s.File})
		}
		api = api.POST(mailboxPath(mailboxID, "templates")).Body(map[string]interface{}{
			"name":        input.Name,
			"subject":     input.Subject,
			"content":     "<post-buildBodyDiv body, see Execute>",
			"to":          parseRecipientList(input.To),
			"cc":          parseRecipientList(input.CC),
			"bcc":         parseRecipientList(input.BCC),
			"attachments": "<populated post-upload by templateAttachmentBuilder>",
		})
		return api
	},
	Validate: func(ctx context.Context, runtime *common.RuntimeContext) error {
		if strings.TrimSpace(runtime.Str("name")) == "" {
			return output.ErrValidation("--name is required")
		}
		if strings.TrimSpace(runtime.Str("subject")) == "" {
			return output.ErrValidation("--subject is required")
		}
		content := runtime.Str("content")
		if strings.TrimSpace(content) == "" {
			return output.ErrValidation("--content is required")
		}
		plainText := runtime.Bool("plain-text")
		// Body cap is enforced AFTER buildBodyDiv wrapping so the cap
		// reflects what's actually stored (S1 contract §"Validate vs Execute split").
		if err := validateTemplateContentCap(applyTemplateBodyWrap(content, plainText)); err != nil {
			return err
		}
		return validateComposeInlineAndAttachments(runtime.FileIO(),
			runtime.Str("attach"), runtime.Str("inline"), plainText, content)
	},
	Execute: func(ctx context.Context, runtime *common.RuntimeContext) error {
		input := readTemplateCreateInput(runtime)
		mailboxID := resolveComposeMailboxID(runtime)

		composed, err := composeTemplate(ctx, runtime, input)
		if err != nil {
			return err
		}

		body := templateBodyFromBuild(composed)
		resp, err := runtime.CallAPI("POST", mailboxPath(mailboxID, "templates"), nil, body)
		if err != nil {
			return fmt.Errorf("create template failed: %w", err)
		}

		out := map[string]interface{}{
			"template_id": extractTemplateID(resp),
			"name":        composed.Name,
			"attachments": len(composed.Attachments),
		}
		runtime.OutFormat(out, nil, func(w io.Writer) {
			fmt.Fprintln(w, "Template created.")
			if tid, ok := out["template_id"].(string); ok && tid != "" {
				fmt.Fprintf(w, "template_id: %s\n", tid)
			}
			fmt.Fprintf(w, "attachments: %d\n", len(composed.Attachments))
		})
		return nil
	},
}

// readTemplateCreateInput packs the cobra flag values into the shared
// templateComposeInput struct. Kept tiny on purpose so DryRun and Execute
// agree on what user input looks like.
func readTemplateCreateInput(runtime *common.RuntimeContext) templateComposeInput {
	return templateComposeInput{
		Name:      runtime.Str("name"),
		Subject:   runtime.Str("subject"),
		Content:   runtime.Str("content"),
		To:        runtime.Str("to"),
		CC:        runtime.Str("cc"),
		BCC:       runtime.Str("bcc"),
		Attach:    runtime.Str("attach"),
		Inline:    runtime.Str("inline"),
		PlainText: runtime.Bool("plain-text"),
	}
}

// extractTemplateID pulls template_id from the POST response, tolerating both
// the bare-data shape and the {"data": {...}} envelope that some open-apis
// endpoints wrap their payloads in.
func extractTemplateID(resp map[string]interface{}) string {
	if id, ok := resp["template_id"].(string); ok && id != "" {
		return id
	}
	if data, ok := resp["data"].(map[string]interface{}); ok {
		if id, ok := data["template_id"].(string); ok && id != "" {
			return id
		}
	}
	if id, ok := resp["id"].(string); ok && id != "" {
		return id
	}
	return ""
}
