// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package mail

import (
	"bytes"
	"context"
	"os"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/larksuite/cli/internal/cmdutil"
	"github.com/larksuite/cli/internal/core"
	"github.com/larksuite/cli/internal/httpmock"
	"github.com/larksuite/cli/shortcuts/common"
	draftpkg "github.com/larksuite/cli/shortcuts/mail/draft"
	"github.com/spf13/cobra"
)

var templateTestSeq atomic.Int64

func newTemplateTestRuntime(t *testing.T) (*common.RuntimeContext, *httpmock.Registry) {
	t.Helper()
	t.Setenv("LARKSUITE_CLI_CONFIG_DIR", t.TempDir())
	cfg := &core.CliConfig{
		AppID:      "template-test-" + itoaSeq(templateTestSeq.Add(1)),
		AppSecret:  "secret",
		Brand:      core.BrandFeishu,
		UserOpenId: "ou_test_user",
	}
	f, _, _, reg := cmdutil.TestFactory(t, cfg)
	cmd := &cobra.Command{Use: "test"}
	rt := common.TestNewRuntimeContextForAPI(context.Background(), cmd, cfg, f, core.AsUser)
	return rt, reg
}

func itoaSeq(i int64) string {
	const digits = "0123456789"
	if i == 0 {
		return "0"
	}
	var b [20]byte
	pos := len(b)
	for i > 0 {
		pos--
		b[pos] = digits[i%10]
		i /= 10
	}
	return string(b[pos:])
}

func writeTempFile(t *testing.T, name string, size int) string {
	t.Helper()
	if err := os.WriteFile(name, bytes.Repeat([]byte("a"), size), 0o644); err != nil {
		t.Fatalf("WriteFile(%q): %v", name, err)
	}
	return name
}

func writeSizedFile(t *testing.T, name string, size int64) string {
	t.Helper()
	fh, err := os.Create(name)
	if err != nil {
		t.Fatalf("Create(%q): %v", name, err)
	}
	if err := fh.Truncate(size); err != nil {
		t.Fatalf("Truncate(%q): %v", name, err)
	}
	if err := fh.Close(); err != nil {
		t.Fatalf("Close(%q): %v", name, err)
	}
	return name
}

// TestUploadAttachmentToDrive_SinglePartDispatch covers the spec §2 risk row
// 2 dispatch rule: ≤20 MB → upload_all (one POST). Drive dispatch is by
// file size only, INDEPENDENT of attachment_type.
func TestUploadAttachmentToDrive_SinglePartDispatch(t *testing.T) {
	rt, reg := newTemplateTestRuntime(t)
	dir := t.TempDir()
	cwd, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(cwd) })

	uploadAllStub := &httpmock.Stub{
		Method: "POST",
		URL:    "/open-apis/drive/v1/medias/upload_all",
		Body:   map[string]interface{}{"code": 0, "data": map[string]interface{}{"file_token": "fk_small"}},
	}
	reg.Register(uploadAllStub)

	path := writeTempFile(t, ("./small.bin"), 1024)
	fileKey, size, steps, err := uploadAttachmentToDrive(context.Background(), rt, path)
	if err != nil {
		t.Fatalf("uploadAttachmentToDrive: %v", err)
	}
	if fileKey != "fk_small" {
		t.Errorf("fileKey = %q want fk_small", fileKey)
	}
	if size != 1024 {
		t.Errorf("size = %d want 1024", size)
	}
	if len(steps) != 1 {
		t.Fatalf("steps len = %d want 1 (single-part)", len(steps))
	}
	if !strings.HasSuffix(steps[0].Path, "/medias/upload_all") {
		t.Errorf("step path = %q, want upload_all", steps[0].Path)
	}
}

// TestUploadAttachmentToDrive_MultipartDispatch covers >20 MB → 3 steps
// (upload_prepare + upload_part + upload_finish) per spec §2 risk row 2.
func TestUploadAttachmentToDrive_MultipartDispatch(t *testing.T) {
	rt, reg := newTemplateTestRuntime(t)
	dir := t.TempDir()
	cwd, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(cwd) })

	prepareStub := &httpmock.Stub{
		Method: "POST",
		URL:    "/open-apis/drive/v1/medias/upload_prepare",
		Body: map[string]interface{}{
			"code": 0,
			"data": map[string]interface{}{"upload_id": "uid", "block_size": 4 * 1024 * 1024, "block_num": 6},
		},
	}
	reg.Register(prepareStub)
	for i := 0; i < 6; i++ {
		reg.Register(&httpmock.Stub{
			Method: "POST",
			URL:    "/open-apis/drive/v1/medias/upload_part",
			Body:   map[string]interface{}{"code": 0, "data": map[string]interface{}{}},
		})
	}
	reg.Register(&httpmock.Stub{
		Method: "POST",
		URL:    "/open-apis/drive/v1/medias/upload_finish",
		Body:   map[string]interface{}{"code": 0, "data": map[string]interface{}{"file_token": "fk_big"}},
	})

	bigPath := writeSizedFile(t, ("./big.bin"), common.MaxDriveMediaUploadSinglePartSize+1)
	fileKey, size, steps, err := uploadAttachmentToDrive(context.Background(), rt, bigPath)
	if err != nil {
		t.Fatalf("uploadAttachmentToDrive: %v", err)
	}
	if fileKey != "fk_big" {
		t.Errorf("fileKey = %q want fk_big", fileKey)
	}
	if size != common.MaxDriveMediaUploadSinglePartSize+1 {
		t.Errorf("size = %d want %d", size, common.MaxDriveMediaUploadSinglePartSize+1)
	}
	if len(steps) != 3 {
		t.Fatalf("steps len = %d want 3 (chunked)", len(steps))
	}
	wantSuffixes := []string{"/medias/upload_prepare", "/medias/upload_part", "/medias/upload_finish"}
	for i, w := range wantSuffixes {
		if !strings.HasSuffix(steps[i].Path, w) {
			t.Errorf("step[%d].Path = %q, want suffix %q", i, steps[i].Path, w)
		}
	}
}

// TestComposeTemplate_CIDRewrite covers spec §4.2: HTML <img src="local">
// scanned, uploaded, and rewritten to <img src="cid:...">. The compose
// pipeline must produce a body where cid: appears and the original local
// path no longer does.
func TestComposeTemplate_CIDRewrite(t *testing.T) {
	rt, reg := newTemplateTestRuntime(t)
	dir := t.TempDir()
	cwd, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(cwd) })

	if err := os.WriteFile("logo.png", []byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A}, 0o644); err != nil {
		t.Fatal(err)
	}
	reg.Register(&httpmock.Stub{
		Method: "POST",
		URL:    "/open-apis/drive/v1/medias/upload_all",
		Body:   map[string]interface{}{"code": 0, "data": map[string]interface{}{"file_token": "fk_logo"}},
	})

	in := templateComposeInput{
		Name:    "weekly",
		Subject: "report",
		Content: `<p>see <img src="./logo.png" /></p>`,
	}
	composed, err := composeTemplate(context.Background(), rt, in)
	if err != nil {
		t.Fatalf("composeTemplate: %v", err)
	}
	if strings.Contains(composed.Content, `src="./logo.png"`) {
		t.Errorf("expected local path rewritten, got: %q", composed.Content)
	}
	if !strings.Contains(composed.Content, "cid:") {
		t.Errorf("expected cid: rewrite in body, got: %q", composed.Content)
	}
	if len(composed.Attachments) != 1 {
		t.Fatalf("attachments = %d, want 1 inline", len(composed.Attachments))
	}
	if !composed.Attachments[0].IsInline {
		t.Error("expected attachment IsInline=true after HTML scan")
	}
	if composed.Attachments[0].AttachmentType != AttachmentTypeSMALL {
		t.Errorf("inline attachment_type = %q, want SMALL", composed.Attachments[0].AttachmentType)
	}
	if composed.Attachments[0].ID != "fk_logo" {
		t.Errorf("attachment id = %q, want fk_logo", composed.Attachments[0].ID)
	}
}

// TestComposeTemplate_PlainTextWraps covers spec §2 risk row 4: plain-text
// content must be wrapped via buildBodyDiv before being stored. End-to-end
// from compose: the resulting Content must contain the <div> + <br>.
func TestComposeTemplate_PlainTextWraps(t *testing.T) {
	rt, _ := newTemplateTestRuntime(t)
	composed, err := composeTemplate(context.Background(), rt, templateComposeInput{
		Name:      "n",
		Subject:   "s",
		Content:   "line1\nline2",
		PlainText: true,
	})
	if err != nil {
		t.Fatalf("composeTemplate: %v", err)
	}
	if !strings.Contains(composed.Content, "<br>") {
		t.Errorf("plain-text body not wrapped with <br>: %q", composed.Content)
	}
	if !strings.Contains(composed.Content, "<div") {
		t.Errorf("plain-text body not wrapped with <div>: %q", composed.Content)
	}
}

// TestComposeTemplate_RejectsLargeAttachment covers the §2 invariant: the
// LARGE branch is not allowed for templates. A non-inline attachment that
// would push the cumulative cap over 25 MB must surface as ErrValidation
// BEFORE the Drive upload happens (no upload stubs are registered — the
// preflight check must reject first).
func TestComposeTemplate_RejectsLargeAttachment(t *testing.T) {
	rt, _ := newTemplateTestRuntime(t)
	dir := t.TempDir()
	cwd, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(cwd) })

	bigPath := writeSizedFile(t, ("./huge.bin"), 24*1024*1024)

	// Body close to 3 MB pushes the 24 MB attachment past 25 MB.
	in := templateComposeInput{
		Name:    "n",
		Subject: "s",
		Content: strings.Repeat("a", 2*1024*1024),
		Attach:  bigPath,
	}
	_, err := composeTemplate(context.Background(), rt, in)
	if err == nil {
		t.Fatal("expected ErrValidation rejecting LARGE attachment, got nil")
	}
	if !strings.Contains(err.Error(), "25 MB") {
		t.Fatalf("expected error to mention 25 MB cap, got: %v", err)
	}
}

// TestBuildTemplateDryRunSteps_Counts covers the DryRun enumeration
// requirement: 1 step per ≤20MB file, 3 steps per >20MB file (spec §4.2 +
// contract S1 §"Header / RPC contract").
func TestBuildTemplateDryRunSteps_Counts(t *testing.T) {
	rt, _ := newTemplateTestRuntime(t)
	dir := t.TempDir()
	cwd, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(cwd) })

	smallPath := writeTempFile(t, ("./small.txt"), 1024)
	bigPath := writeSizedFile(t, ("./big.bin"), common.MaxDriveMediaUploadSinglePartSize+1)

	steps, err := buildTemplateDryRunSteps(rt, templateComposeInput{
		Name:    "n",
		Subject: "s",
		Content: "<p>hello</p>",
		Attach:  smallPath + "," + bigPath,
	})
	if err != nil {
		t.Fatalf("buildTemplateDryRunSteps: %v", err)
	}
	// 1 (small) + 3 (big) = 4 steps total.
	if len(steps) != 4 {
		t.Fatalf("step count = %d, want 4 (1 single-part + 3 chunked)", len(steps))
	}
	if !strings.HasSuffix(steps[0].Path, "/medias/upload_all") {
		t.Errorf("first step = %q, want upload_all", steps[0].Path)
	}
	if !strings.HasSuffix(steps[1].Path, "/medias/upload_prepare") {
		t.Errorf("second step = %q, want upload_prepare", steps[1].Path)
	}
}

// TestProjectExistingTemplate_RoundTrip ensures the GET-projection helper
// flattens both raw {"template": ...} and bare-data shapes.
func TestProjectExistingTemplate_RoundTrip(t *testing.T) {
	cases := map[string]map[string]interface{}{
		"bare":           {"name": "n1", "subject": "s1", "content": "c1", "to": []interface{}{"a@x"}},
		"data wrapped":   {"data": map[string]interface{}{"name": "n2", "subject": "s2", "content": "c2"}},
		"template inner": {"data": map[string]interface{}{"template": map[string]interface{}{"name": "n3"}}},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got := projectExistingTemplate(tc)
			if got.Name == "" {
				t.Errorf("Name empty after projection: %+v", got)
			}
		})
	}
}

// Ensure ResolveLocalImagePaths is referenced (compile-time anchor for the
// rewritten path the body test depends on).
var _ = draftpkg.ResolveLocalImagePaths
