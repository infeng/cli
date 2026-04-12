// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package mail

import (
	"context"
	"fmt"
	"strconv"

	"github.com/larksuite/cli/internal/output"
	"github.com/larksuite/cli/shortcuts/common"
	draftpkg "github.com/larksuite/cli/shortcuts/mail/draft"
)

var MailDraftSend = common.Shortcut{
	Service:     "mail",
	Command:     "+draft-send",
	Description: "Send an existing draft immediately or schedule it for later. Use --send-time or --send-after to schedule.",
	Risk:        "write",
	Scopes:      []string{"mail:user_mailbox.message:send", "mail:user_mailbox:readonly"},
	AuthTypes:   []string{"user"},
	Flags: []common.Flag{
		{Name: "mailbox", Default: "me", Desc: "Mailbox email address (default: me)"},
		{Name: "draft-id", Desc: "Required. The draft ID to send.", Required: true},
		{Name: "send-time", Desc: "Optional. Unix timestamp in seconds for scheduled sending. If empty or 0, the draft is sent immediately. Must be at least 5 minutes from now."},
		{Name: "send-after", Desc: "Optional. Relative duration from now (e.g. 30m, 2h, 1d). Converted to absolute Unix timestamp internally. Must result in at least 5 minutes from now."},
	},
	DryRun: func(ctx context.Context, runtime *common.RuntimeContext) *common.DryRunAPI {
		mailboxID := resolveMailboxID(runtime)
		draftID := runtime.Str("draft-id")
		desc := "Send draft immediately"
		if runtime.Str("send-time") != "" || runtime.Str("send-after") != "" {
			desc = "Schedule draft for later sending"
		}
		return common.NewDryRunAPI().
			Desc(desc).
			POST(mailboxPath(mailboxID, "drafts", draftID, "send"))
	},
	Validate: func(ctx context.Context, runtime *common.RuntimeContext) error {
		if runtime.Str("draft-id") == "" {
			return output.ErrValidation("--draft-id is required")
		}
		return nil
	},
	Execute: func(ctx context.Context, runtime *common.RuntimeContext) error {
		mailboxID := resolveMailboxID(runtime)
		draftID := runtime.Str("draft-id")

		sendTime, err := resolveScheduledSendTime(runtime)
		if err != nil {
			return err
		}

		var body map[string]interface{}
		if sendTime > 0 {
			body = map[string]interface{}{
				"send_time": strconv.FormatInt(sendTime, 10),
			}
		}

		resData, err := draftpkg.SendWithBody(runtime, mailboxID, draftID, body)
		if err != nil {
			return fmt.Errorf("failed to send draft %s: %w", draftID, err)
		}

		out := map[string]interface{}{}
		if msgID, ok := resData["message_id"]; ok {
			out["message_id"] = msgID
		}
		if threadID, ok := resData["thread_id"]; ok {
			out["thread_id"] = threadID
		}
		if sendTime > 0 {
			out["scheduled"] = true
			out["send_time"] = strconv.FormatInt(sendTime, 10)
		}
		runtime.Out(out, nil)
		return nil
	},
}
