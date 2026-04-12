// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package mail

import (
	"context"
	"fmt"

	"github.com/larksuite/cli/internal/output"
	"github.com/larksuite/cli/shortcuts/common"
)

var MailCancelScheduledSend = common.Shortcut{
	Service:     "mail",
	Command:     "+cancel-scheduled-send",
	Description: "Cancel a scheduled email that has not been sent yet. The message will be moved back to drafts.",
	Risk:        "write",
	Scopes:      []string{"mail:user_mailbox.message:send"},
	AuthTypes:   []string{"user"},
	Flags: []common.Flag{
		{Name: "mailbox", Default: "me", Desc: "Mailbox ID (default: me)"},
		{Name: "message-id", Desc: "Required. The message ID (messageBizID) of the scheduled message to cancel.", Required: true},
	},
	DryRun: func(ctx context.Context, runtime *common.RuntimeContext) *common.DryRunAPI {
		mailboxID := resolveMailboxID(runtime)
		messageID := runtime.Str("message-id")
		return common.NewDryRunAPI().
			Desc("Cancel a scheduled send. The message must be in SCHEDULED state. Returns the message to DRAFT state.").
			POST(mailboxPath(mailboxID, "messages", messageID, "cancel_scheduled_send"))
	},
	Validate: func(ctx context.Context, runtime *common.RuntimeContext) error {
		if runtime.Str("message-id") == "" {
			return output.ErrValidation("--message-id is required")
		}
		return nil
	},
	Execute: func(ctx context.Context, runtime *common.RuntimeContext) error {
		mailboxID := resolveMailboxID(runtime)
		messageID := runtime.Str("message-id")

		apiPath := mailboxPath(mailboxID, "messages", messageID, "cancel_scheduled_send")
		_, err := runtime.CallAPI("POST", apiPath, nil, nil)
		if err != nil {
			return fmt.Errorf("failed to cancel scheduled send for message %s: %w", messageID, err)
		}

		runtime.Out(map[string]interface{}{
			"message_id": messageID,
			"status":     "cancelled",
			"tip":        "The scheduled email has been moved back to drafts.",
		}, nil)
		return nil
	},
}
