// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package mail

import (
	"context"
	"strings"

	"github.com/larksuite/cli/shortcuts/common"
)

const threadBatchMethod = "POST"

type threadBatchRequest struct {
	Method string
	Path   string
	Body   map[string]interface{}
}

// MailThreadModify is the `+thread-modify` shortcut: apply label changes or a
// folder move to existing mail threads with one batch_modify request.
var MailThreadModify = common.Shortcut{
	Service:     "mail",
	Command:     "+thread-modify",
	Description: "Modify existing mail threads in one batch request by adding/removing label IDs or moving them to a folder.",
	Risk:        "write",
	Scopes:      []string{"mail:user_mailbox.message:modify"},
	AuthTypes:   []string{"user", "bot"},
	HasFormat:   true,
	Flags: []common.Flag{
		{Name: "mailbox", Default: "me", Desc: "Mailbox email address that owns the threads (default: me)."},
		{Name: "thread-id", Type: "string_slice", Required: true, Desc: "Thread ID to modify; comma-separated or repeat the flag."},
		{Name: "add-label-id", Type: "string_slice", Desc: "Label ID to add; comma-separated or repeat the flag."},
		{Name: "remove-label-id", Type: "string_slice", Desc: "Label ID to remove; cannot overlap with --add-label-id."},
		{Name: "folder-id", Desc: "Folder ID to move the threads to; sent only as add_folder."},
	},
	Validate: validateThreadModify,
	DryRun:   dryRunThreadModify,
	Execute:  executeThreadModify,
}

// MailThreadTrash is the `+thread-trash` shortcut: soft-delete existing mail
// threads with one batch_trash request. Risk is high-risk-write, so the runner
// requires --yes before Execute.
var MailThreadTrash = common.Shortcut{
	Service:     "mail",
	Command:     "+thread-trash",
	Description: "Soft-delete existing mail threads with one batch request. Requires --yes.",
	Risk:        "high-risk-write",
	Scopes:      []string{"mail:user_mailbox.message:modify"},
	AuthTypes:   []string{"user", "bot"},
	HasFormat:   true,
	Flags: []common.Flag{
		{Name: "mailbox", Default: "me", Desc: "Mailbox email address that owns the threads (default: me)."},
		{Name: "thread-id", Type: "string_slice", Required: true, Desc: "Thread ID to soft-delete; comma-separated or repeat the flag."},
	},
	Validate: validateThreadTrash,
	DryRun:   dryRunThreadTrash,
	Execute:  executeThreadTrash,
}

func validateThreadModify(ctx context.Context, rt *common.RuntimeContext) error {
	if err := validateBotMailboxNotMe(rt); err != nil {
		return err
	}
	_, err := buildThreadModifyRequest(rt)
	return err
}

func dryRunThreadModify(ctx context.Context, rt *common.RuntimeContext) *common.DryRunAPI {
	req, _ := buildThreadModifyRequest(rt)
	return common.NewDryRunAPI().
		Desc("Modify threads with one batch_modify request").
		POST(req.Path).
		Body(req.Body)
}

func executeThreadModify(ctx context.Context, rt *common.RuntimeContext) error {
	req, err := buildThreadModifyRequest(rt)
	if err != nil {
		return err
	}
	data, err := rt.CallAPITyped(req.Method, req.Path, nil, req.Body)
	if err != nil {
		return err
	}
	rt.Out(data, nil)
	return nil
}

func validateThreadTrash(ctx context.Context, rt *common.RuntimeContext) error {
	if err := validateBotMailboxNotMe(rt); err != nil {
		return err
	}
	_, err := buildThreadTrashRequest(rt)
	return err
}

func dryRunThreadTrash(ctx context.Context, rt *common.RuntimeContext) *common.DryRunAPI {
	req, _ := buildThreadTrashRequest(rt)
	return common.NewDryRunAPI().
		Desc("Soft-delete threads with one batch_trash request").
		POST(req.Path).
		Body(req.Body)
}

func executeThreadTrash(ctx context.Context, rt *common.RuntimeContext) error {
	req, err := buildThreadTrashRequest(rt)
	if err != nil {
		return err
	}
	data, err := rt.CallAPITyped(req.Method, req.Path, nil, req.Body)
	if err != nil {
		return err
	}
	rt.Out(data, nil)
	return nil
}

func buildThreadModifyRequest(rt *common.RuntimeContext) (threadBatchRequest, error) {
	threadIDs, err := normalizeThreadValues(rt.StrSlice("thread-id"), "--thread-id", true)
	if err != nil {
		return threadBatchRequest{}, err
	}
	addLabelIDs, err := normalizeThreadValues(rt.StrSlice("add-label-id"), "--add-label-id", false)
	if err != nil {
		return threadBatchRequest{}, err
	}
	removeLabelIDs, err := normalizeThreadValues(rt.StrSlice("remove-label-id"), "--remove-label-id", false)
	if err != nil {
		return threadBatchRequest{}, err
	}
	if err := validateThreadLabelIntersection(addLabelIDs, removeLabelIDs); err != nil {
		return threadBatchRequest{}, err
	}

	folderID := ""
	if rt.Changed("folder-id") {
		folderID = strings.TrimSpace(rt.Str("folder-id"))
		if folderID == "" {
			return threadBatchRequest{}, mailValidationParamError("--folder-id", "--folder-id must not be empty")
		}
	}
	if len(addLabelIDs) == 0 && len(removeLabelIDs) == 0 && folderID == "" {
		return threadBatchRequest{}, mailValidationParamError("--thread-modify", "provide at least one of --add-label-id, --remove-label-id, or --folder-id")
	}

	body := map[string]interface{}{"thread_ids": threadIDs}
	if len(addLabelIDs) > 0 {
		body["add_label_ids"] = addLabelIDs
	}
	if len(removeLabelIDs) > 0 {
		body["remove_label_ids"] = removeLabelIDs
	}
	if folderID != "" {
		body["add_folder"] = folderID
	}
	return threadBatchRequest{
		Method: threadBatchMethod,
		Path:   mailboxPath(resolveMailboxID(rt), "threads", "batch_modify"),
		Body:   body,
	}, nil
}

func buildThreadTrashRequest(rt *common.RuntimeContext) (threadBatchRequest, error) {
	threadIDs, err := normalizeThreadValues(rt.StrSlice("thread-id"), "--thread-id", true)
	if err != nil {
		return threadBatchRequest{}, err
	}
	return threadBatchRequest{
		Method: threadBatchMethod,
		Path:   mailboxPath(resolveMailboxID(rt), "threads", "batch_trash"),
		Body:   map[string]interface{}{"thread_ids": threadIDs},
	}, nil
}

func normalizeThreadValues(raw []string, flagName string, required bool) ([]string, error) {
	values := make([]string, 0, len(raw))
	seen := make(map[string]struct{}, len(raw))
	for _, token := range raw {
		for _, part := range strings.Split(token, ",") {
			value := strings.TrimSpace(part)
			if value == "" {
				return nil, mailValidationParamError(flagName, "%s contains an empty ID", flagName)
			}
			if _, ok := seen[value]; ok {
				continue
			}
			seen[value] = struct{}{}
			values = append(values, value)
		}
	}
	if required && len(values) == 0 {
		return nil, mailValidationParamError(flagName, "%s is required", flagName)
	}
	return values, nil
}

func validateThreadLabelIntersection(addLabelIDs, removeLabelIDs []string) error {
	added := make(map[string]struct{}, len(addLabelIDs))
	for _, id := range addLabelIDs {
		added[id] = struct{}{}
	}
	for _, id := range removeLabelIDs {
		if _, ok := added[id]; ok {
			return mailValidationParamError("--add-label-id", "label ID %q cannot be both added and removed", id)
		}
	}
	return nil
}
