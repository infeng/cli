// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package mail

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/larksuite/cli/internal/output"
	"github.com/larksuite/cli/shortcuts/common"
	draftpkg "github.com/larksuite/cli/shortcuts/mail/draft"
)

// Mail template attachment-type enum values. Inline images are ALWAYS
// AttachmentTypeSMALL — see standard-mail-shortcut.md §1 invariant. The LARGE
// branch is rejected client-side in this scope (templates must be
// self-contained EML, KB §2).
const (
	AttachmentTypeSMALL = "SMALL"
	AttachmentTypeLARGE = "LARGE"
)

// Template-write-side hard caps mirrored from larkmail/open-access
// biz/mailtemplate/template_service.go (spec §2 + KB §2).
const (
	// MaxTemplateContentBytes is the server cap on template_content (HTML body).
	// Both byte length AND rune length are checked; the stricter one trips
	// first (see validateTemplateContentCap).
	MaxTemplateContentBytes = 3 * 1024 * 1024 // 3 MB

	// MaxTemplateCumulativeBytes mirrors `templateLargeSwitchThreshold`
	// (template_content + inline images + SMALL non-inline). Same value as
	// the draft/send EML SMALL-attachment 25 MB cap so apply-into-draft never
	// silently degrades attachment_type.
	MaxTemplateCumulativeBytes = 25 * 1024 * 1024 // 25 MB

	// templateBaseEMLOverhead is a generous budget for headers / MIME
	// scaffolding so emlProjectedSize starts realistic (~5-10 KB per spec
	// §4.2 pseudocode).
	templateBaseEMLOverhead = 8 * 1024
)

// TemplateAttachment is one entry of `attachments[]` in the mail template
// POST/PUT body. Field names mirror the registry meta_data.json snake_case
// expected by the OAPI server.
type TemplateAttachment struct {
	ID             string `json:"id"`
	Filename       string `json:"filename"`
	Cid            string `json:"cid,omitempty"`
	IsInline       bool   `json:"is_inline"`
	AttachmentType string `json:"attachment_type"`
}

// templateRequestBody is the JSON shape for POST templates / PUT
// templates/<id>. Optional recipient slices are emitted with omitempty so
// the server-side optional fields don't carry empty arrays.
type templateRequestBody struct {
	Name        string               `json:"name"`
	Subject     string               `json:"subject"`
	Content     string               `json:"content"`
	To          []string             `json:"to,omitempty"`
	Cc          []string             `json:"cc,omitempty"`
	Bcc         []string             `json:"bcc,omitempty"`
	Attachments []TemplateAttachment `json:"attachments,omitempty"`
}

// templateAttachmentBuilder accumulates attachment entries while enforcing
// the §2 invariants:
//   - inline ALWAYS SMALL regardless of largeBucket;
//   - non-inline switches to LARGE once emlProjectedSize+base64Size ≥ 25MB
//     and largeBucket stays true for the rest of the batch (order-sensitive).
//
// This sprint ALSO rejects any non-inline crossing the 25MB cumulative cap
// (`output.ErrValidation`) — templates must be self-contained EML, see
// standard-mail-shortcut.md §2 / contract S1 §"Server-mirrored constraints".
type templateAttachmentBuilder struct {
	emlProjectedSize int64
	largeBucket      bool
	rejectLarge      bool // true for template-create/update; LARGE not allowed
	out              []TemplateAttachment
}

// newTemplateAttachmentBuilder seeds the accumulator with the body / headers
// base + already-counted body size (post buildBodyDiv wrapping).
func newTemplateAttachmentBuilder(bodyBytes int64) *templateAttachmentBuilder {
	return &templateAttachmentBuilder{
		emlProjectedSize: int64(templateBaseEMLOverhead) + estimateBase64EMLSize(bodyBytes),
		rejectLarge:      true,
	}
}

// AppendInline classifies an inline attachment and appends the entry. inline
// is always AttachmentTypeSMALL (KB §1 / spec §2 risk row 3); its base64 size
// still counts toward emlProjectedSize so it can trigger LARGE switch for
// later non-inline entries in the same batch.
func (b *templateAttachmentBuilder) AppendInline(fileKey, filename, cid string, fileSize int64) (TemplateAttachment, error) {
	base64Size := estimateBase64EMLSize(fileSize)
	if b.emlProjectedSize+base64Size > MaxTemplateCumulativeBytes {
		return TemplateAttachment{}, output.ErrValidation(
			"template inline image %q would push template_content + inline + attachments over the 25 MB cumulative cap",
			filename)
	}
	b.emlProjectedSize += base64Size
	att := TemplateAttachment{
		ID:             fileKey,
		Filename:       filename,
		Cid:            cid,
		IsInline:       true,
		AttachmentType: AttachmentTypeSMALL,
	}
	b.out = append(b.out, att)
	return att, nil
}

// AppendAttachment classifies a non-inline attachment. Once
// emlProjectedSize+base64(fileSize) crosses 25 MB the accumulator latches
// largeBucket=true and the rest of the batch is LARGE — but in this sprint
// rejectLarge=true short-circuits with ErrValidation since the spec excludes
// the LARGE branch for templates entirely.
func (b *templateAttachmentBuilder) AppendAttachment(fileKey, filename string, fileSize int64) (TemplateAttachment, error) {
	base64Size := estimateBase64EMLSize(fileSize)
	if b.largeBucket || b.emlProjectedSize+base64Size >= MaxTemplateCumulativeBytes {
		b.largeBucket = true
		if b.rejectLarge {
			return TemplateAttachment{}, output.ErrValidation(
				"attachment %q would push the template over the 25 MB cumulative cap; templates must fit entirely in the EML (LARGE attachments are not supported on this endpoint)",
				filename)
		}
		att := TemplateAttachment{
			ID:             fileKey,
			Filename:       filename,
			IsInline:       false,
			AttachmentType: AttachmentTypeLARGE,
		}
		b.out = append(b.out, att)
		return att, nil
	}
	b.emlProjectedSize += base64Size
	att := TemplateAttachment{
		ID:             fileKey,
		Filename:       filename,
		IsInline:       false,
		AttachmentType: AttachmentTypeSMALL,
	}
	b.out = append(b.out, att)
	return att, nil
}

// Result returns the accumulated attachments slice.
func (b *templateAttachmentBuilder) Result() []TemplateAttachment {
	return b.out
}

// preflightAttachmentCap checks whether appending a non-inline attachment of
// raw size `fileSize` would push the builder past the 25 MB cumulative cap,
// without mutating the accumulator. Used to reject BEFORE Drive upload so we
// don't waste the round-trip when the LARGE branch will be denied.
func preflightAttachmentCap(b *templateAttachmentBuilder, fileSize int64) error {
	base64Size := estimateBase64EMLSize(fileSize)
	if b.largeBucket || b.emlProjectedSize+base64Size >= MaxTemplateCumulativeBytes {
		if b.rejectLarge {
			return output.ErrValidation(
				"attachment would push template over the 25 MB cumulative cap; templates must fit entirely in the EML (LARGE attachments are not supported on this endpoint)")
		}
	}
	return nil
}

// EMLSize returns the running emlProjectedSize accumulator for tests / logs.
func (b *templateAttachmentBuilder) EMLSize() int64 { return b.emlProjectedSize }

// validateTemplateContentCap enforces the 3 MB cap on template_content.
// Both rune count and byte count are checked; the stricter one wins
// (per spec §4.2 "rune/byte stricter"). Mirror of the server constant in
// larkmail/open-access biz/mailtemplate/template_service.go:1064.
func validateTemplateContentCap(content string) error {
	bytes := len(content)
	runes := utf8.RuneCountInString(content)
	if bytes > MaxTemplateContentBytes || runes > MaxTemplateContentBytes {
		return output.ErrValidation(
			"template_content exceeds the 3 MB cap (%d bytes / %d runes); reduce HTML body size before saving",
			bytes, runes)
	}
	return nil
}

// applyTemplateBodyWrap wraps plain-text body via the mail compose
// `buildBodyDiv` helper (HTML escape + \n→<br> + <div>) so the stored
// template_content matches the rendering pipeline of `+send` / `+draft-create`
// (KB §3, spec §2 risk row 4 + §4.2). Pass plainText=false for HTML mode —
// content is returned verbatim.
func applyTemplateBodyWrap(content string, plainText bool) string {
	if plainText {
		return buildBodyDiv(content, false)
	}
	return content
}

// templateComposeInput collects the five compose-side flag values shared by
// +template-create and +template-update (post patch-merge for update).
type templateComposeInput struct {
	Name      string
	Subject   string
	Content   string
	To        string
	CC        string
	BCC       string
	Attach    string
	Inline    string
	PlainText bool
}

// composedTemplate is the result of running compose: it bundles the
// final wrapped HTML, parsed/normalized recipients, and the classified
// attachment slice ready to drop into the OAPI request.
type composedTemplate struct {
	Name        string
	Subject     string
	Content     string
	To          []string
	Cc          []string
	Bcc         []string
	Attachments []TemplateAttachment
	// driveSteps is the per-file Drive upload step list captured for
	// DryRun output (1 step per ≤20MB file, 3 steps per >20MB file).
	driveSteps []driveUploadStep
}

// driveUploadStep is one row of the DryRun "what we'd upload" table.
type driveUploadStep struct {
	Method string // "POST"
	Path   string // "/open-apis/drive/v1/medias/upload_all" etc.
	File   string // basename
}

// composeTemplate runs the full compose pipeline for a template write:
// validate→wrap→HTML scan→inline upload→attach upload→classify→build body.
// All Drive uploads happen here (NOT in DryRun); for DryRun the caller must
// instead invoke buildTemplateDryRunSteps below.
func composeTemplate(ctx context.Context, runtime *common.RuntimeContext, in templateComposeInput) (*composedTemplate, error) {
	wrapped := applyTemplateBodyWrap(in.Content, in.PlainText)
	if err := validateTemplateContentCap(wrapped); err != nil {
		return nil, err
	}

	inlineSpecs, err := parseInlineSpecs(in.Inline)
	if err != nil {
		return nil, output.ErrValidation("%v", err)
	}

	// HTML inline <img src="local"> scan — only meaningful for HTML body
	// mode. Plain-text body has no <img> tags.
	var (
		resolvedHTML  = wrapped
		autoRefs      []draftpkg.LocalImageRef
	)
	if !in.PlainText {
		resolvedHTML, autoRefs, err = draftpkg.ResolveLocalImagePaths(wrapped)
		if err != nil {
			return nil, err
		}
	}

	// Cap re-check on the resolved (cid-rewritten) HTML before any upload —
	// rewriting can grow the body slightly when paths are longer than the
	// generated cid: URI; the cap is enforced on what's actually stored.
	if err := validateTemplateContentCap(resolvedHTML); err != nil {
		return nil, err
	}

	builder := newTemplateAttachmentBuilder(int64(len(resolvedHTML)))

	// Process order (spec §4.2): HTML inline imgs (HTML appearance order,
	// preserved by ResolveLocalImagePaths' refs slice) THEN --attach values
	// (flag input order). Both share one emlProjectedSize accumulator.
	steps := make([]driveUploadStep, 0)

	// 1) auto-resolved inline <img src="local"> images.
	for _, ref := range autoRefs {
		fileKey, fileSize, stepRows, upErr := uploadAttachmentToDrive(ctx, runtime, ref.FilePath)
		if upErr != nil {
			return nil, upErr
		}
		steps = append(steps, stepRows...)
		if _, err := builder.AppendInline(fileKey, filepath.Base(ref.FilePath), ref.CID, fileSize); err != nil {
			return nil, err
		}
	}

	// 2) explicit --inline JSON specs.
	for _, spec := range inlineSpecs {
		fileKey, fileSize, stepRows, upErr := uploadAttachmentToDrive(ctx, runtime, spec.FilePath)
		if upErr != nil {
			return nil, upErr
		}
		steps = append(steps, stepRows...)
		if _, err := builder.AppendInline(fileKey, filepath.Base(spec.FilePath), spec.CID, fileSize); err != nil {
			return nil, err
		}
	}

	// 3) --attach values (comma-sep, flag input order). Pre-flight stat
	// each file so the 25 MB cumulative cap can reject BEFORE the upload
	// happens (spec §2 risk row 4: "do not wait for errno 15180203").
	for _, p := range splitByComma(in.Attach) {
		info, statErr := runtime.FileIO().Stat(p)
		if statErr != nil {
			return nil, output.ErrValidation("cannot stat attachment %s: %v", p, statErr)
		}
		// Pre-flight cap check using a side-channel builder snapshot.
		if err := preflightAttachmentCap(builder, info.Size()); err != nil {
			return nil, err
		}
		fileKey, fileSize, stepRows, upErr := uploadAttachmentToDrive(ctx, runtime, p)
		if upErr != nil {
			return nil, upErr
		}
		steps = append(steps, stepRows...)
		if _, err := builder.AppendAttachment(fileKey, filepath.Base(p), fileSize); err != nil {
			return nil, err
		}
	}

	// Validate cid bidirectional consistency on the final HTML.
	if !in.PlainText && (len(autoRefs) > 0 || len(inlineSpecs) > 0) {
		var allCIDs []string
		for _, r := range autoRefs {
			allCIDs = append(allCIDs, r.CID)
		}
		for _, s := range inlineSpecs {
			allCIDs = append(allCIDs, s.CID)
		}
		if err := validateInlineCIDs(resolvedHTML, allCIDs, nil); err != nil {
			return nil, err
		}
	}

	return &composedTemplate{
		Name:        in.Name,
		Subject:     in.Subject,
		Content:     resolvedHTML,
		To:          parseRecipientList(in.To),
		Cc:          parseRecipientList(in.CC),
		Bcc:         parseRecipientList(in.BCC),
		Attachments: builder.Result(),
		driveSteps:  steps,
	}, nil
}

// buildTemplateDryRunSteps enumerates the Drive upload steps that compose
// would issue WITHOUT actually touching the network. Used to populate the
// DryRunAPI .GET/.POST/.PUT chain (spec §4.2 + contract S1 §Header/RPC):
// 1 step per ≤20MB file (`upload_all`), 3 steps per >20MB file
// (`upload_prepare` + `upload_part` + `upload_finish`).
func buildTemplateDryRunSteps(runtime *common.RuntimeContext, in templateComposeInput) ([]driveUploadStep, error) {
	wrapped := applyTemplateBodyWrap(in.Content, in.PlainText)
	if err := validateTemplateContentCap(wrapped); err != nil {
		return nil, err
	}
	inlineSpecs, err := parseInlineSpecs(in.Inline)
	if err != nil {
		return nil, output.ErrValidation("%v", err)
	}

	var steps []driveUploadStep
	if !in.PlainText {
		_, refs, err := draftpkg.ResolveLocalImagePaths(wrapped)
		if err != nil {
			return nil, err
		}
		for _, ref := range refs {
			ss, sErr := dryRunStepsForFile(runtime, ref.FilePath)
			if sErr != nil {
				return nil, sErr
			}
			steps = append(steps, ss...)
		}
	}
	for _, s := range inlineSpecs {
		ss, sErr := dryRunStepsForFile(runtime, s.FilePath)
		if sErr != nil {
			return nil, sErr
		}
		steps = append(steps, ss...)
	}
	for _, p := range splitByComma(in.Attach) {
		ss, sErr := dryRunStepsForFile(runtime, p)
		if sErr != nil {
			return nil, sErr
		}
		steps = append(steps, ss...)
	}
	return steps, nil
}

// dryRunStepsForFile classifies a single file into the 1-step / 3-step Drive
// upload sequence by file size only (independent of attachment_type).
func dryRunStepsForFile(runtime *common.RuntimeContext, path string) ([]driveUploadStep, error) {
	info, err := runtime.FileIO().Stat(path)
	if err != nil {
		return nil, output.ErrValidation("cannot stat attachment %s: %v", path, err)
	}
	name := filepath.Base(path)
	if info.Size() <= common.MaxDriveMediaUploadSinglePartSize {
		return []driveUploadStep{{
			Method: "POST",
			Path:   "/open-apis/drive/v1/medias/upload_all",
			File:   name,
		}}, nil
	}
	return []driveUploadStep{
		{Method: "POST", Path: "/open-apis/drive/v1/medias/upload_prepare", File: name},
		{Method: "POST", Path: "/open-apis/drive/v1/medias/upload_part", File: name},
		{Method: "POST", Path: "/open-apis/drive/v1/medias/upload_finish", File: name},
	}, nil
}

// uploadAttachmentToDrive dispatches Drive upload by file size only (≤20MB
// → upload_all; >20MB → upload_prepare+upload_part+upload_finish), per spec
// §2 risk row 2: dispatch is INDEPENDENT of attachment_type. Returns the
// file_key, raw file size, and the step rows for DryRun symmetry.
func uploadAttachmentToDrive(ctx context.Context, runtime *common.RuntimeContext, path string) (string, int64, []driveUploadStep, error) {
	info, err := runtime.FileIO().Stat(path)
	if err != nil {
		return "", 0, nil, output.ErrValidation("cannot stat attachment %s: %v", path, err)
	}
	name := filepath.Base(path)
	userOpenId := runtime.UserOpenId()
	if userOpenId == "" {
		return "", 0, nil, output.ErrValidation(
			"Drive upload requires user identity (--as user); current identity has no user open_id")
	}
	var (
		fileKey string
		upErr   error
		steps   []driveUploadStep
	)
	if info.Size() <= common.MaxDriveMediaUploadSinglePartSize {
		fileKey, upErr = common.UploadDriveMediaAll(runtime, common.DriveMediaUploadAllConfig{
			FilePath:   path,
			FileName:   name,
			FileSize:   info.Size(),
			ParentType: "email",
			ParentNode: &userOpenId,
		})
		steps = []driveUploadStep{{
			Method: "POST",
			Path:   "/open-apis/drive/v1/medias/upload_all",
			File:   name,
		}}
	} else {
		fileKey, upErr = common.UploadDriveMediaMultipart(runtime, common.DriveMediaMultipartUploadConfig{
			FilePath:   path,
			FileName:   name,
			FileSize:   info.Size(),
			ParentType: "email",
			ParentNode: userOpenId,
		})
		steps = []driveUploadStep{
			{Method: "POST", Path: "/open-apis/drive/v1/medias/upload_prepare", File: name},
			{Method: "POST", Path: "/open-apis/drive/v1/medias/upload_part", File: name},
			{Method: "POST", Path: "/open-apis/drive/v1/medias/upload_finish", File: name},
		}
	}
	if upErr != nil {
		return "", 0, nil, output.Errorf(output.ExitAPI, "api_error",
			"failed to upload template attachment %s: %v", name, upErr)
	}
	_ = ctx // ctx threaded for future timeout/cancel; helpers don't accept it yet.
	return fileKey, info.Size(), steps, nil
}

// parseRecipientList splits the comma-separated raw recipient string,
// trims whitespace, drops empties. Templates allow zero recipients.
func parseRecipientList(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	out := splitByComma(raw)
	if len(out) == 0 {
		return nil
	}
	return out
}

// templateBodyFromBuild builds the OAPI POST/PUT request body. Marshalled
// here so the JSON shape is owned by one helper and the test suite has a
// single anchor for field-name regressions.
func templateBodyFromBuild(c *composedTemplate) map[string]interface{} {
	body := templateRequestBody{
		Name:        c.Name,
		Subject:     c.Subject,
		Content:     c.Content,
		To:          c.To,
		Cc:          c.Cc,
		Bcc:         c.Bcc,
		Attachments: c.Attachments,
	}
	// Round-trip via JSON so callers can pass the result directly into
	// runtime.CallAPI (which expects a marshallable interface{}); also lets
	// tests inspect the wire shape via map keys.
	raw, _ := json.Marshal(body)
	var out map[string]interface{}
	_ = json.Unmarshal(raw, &out)
	return out
}

// emitConcurrencyWarning writes the +template-update last-write-wins warning
// to stderr (DryRun preview AND Execute, per spec §2 risk row 6).
func emitConcurrencyWarning(runtime *common.RuntimeContext) {
	fmt.Fprintln(runtime.IO().ErrOut,
		"warning: mail template update is last-write-wins; concurrent updates may overwrite each other")
}
