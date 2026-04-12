// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package mail

import (
	"strings"
	"testing"

	"github.com/larksuite/cli/internal/httpmock"
)

func TestMailCancelScheduledSend_Success(t *testing.T) {
	f, stdout, _, reg := mailShortcutTestFactory(t)

	cancelStub := &httpmock.Stub{
		Method: "POST",
		URL:    "/open-apis/mail/v1/user_mailboxes/me/messages/MSG_001/cancel_scheduled_send",
		Body:   `{"code":0,"data":{}}`,
	}
	reg.Register(cancelStub)

	err := runMountedMailShortcut(t, MailCancelScheduledSend, []string{
		"+cancel-scheduled-send", "--mailbox", "me", "--message-id", "MSG_001",
	}, f, stdout)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	data := decodeShortcutEnvelopeData(t, stdout)
	if data["message_id"] != "MSG_001" {
		t.Errorf("expected message_id=MSG_001, got %v", data["message_id"])
	}
	if data["status"] != "cancelled" {
		t.Errorf("expected status=cancelled, got %v", data["status"])
	}
}

func TestMailCancelScheduledSend_MissingMessageID(t *testing.T) {
	f, stdout, _, _ := mailShortcutTestFactory(t)

	err := runMountedMailShortcut(t, MailCancelScheduledSend, []string{
		"+cancel-scheduled-send", "--mailbox", "me",
	}, f, stdout)
	if err == nil {
		t.Fatal("expected error for missing --message-id")
	}
	if !strings.Contains(err.Error(), "message-id") {
		t.Fatalf("expected error about message-id, got: %v", err)
	}
}

func TestMailCancelScheduledSend_CustomMailbox(t *testing.T) {
	f, stdout, _, reg := mailShortcutTestFactory(t)

	cancelStub := &httpmock.Stub{
		Method: "POST",
		URL:    "user_mailboxes/user@example.com/messages/MSG_002/cancel_scheduled_send",
		Body:   `{"code":0,"data":{}}`,
	}
	reg.Register(cancelStub)

	err := runMountedMailShortcut(t, MailCancelScheduledSend, []string{
		"+cancel-scheduled-send", "--mailbox", "user@example.com", "--message-id", "MSG_002",
	}, f, stdout)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	data := decodeShortcutEnvelopeData(t, stdout)
	if data["message_id"] != "MSG_002" {
		t.Errorf("expected message_id=MSG_002, got %v", data["message_id"])
	}
}
