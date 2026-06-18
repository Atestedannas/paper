# DOCX Rule Pack Expanded Validation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add template-driven validation for document structure, required metadata fields, title length, keywords, heading numbering, body length, figure/table/formula numbering, reference count/foreign ratio, header/page-number strategy, and blind-review requirements.

**Architecture:** Keep school differences in `templateprofile.RulePack`; processors read only the profile and never branch on school name. Rules that cannot be safely auto-repaired become profile verification failures and drive manual review instead of pretending the DOCX is compliant.

**Tech Stack:** Go, OOXML string-level inspection through existing `ooxmlpkg`, existing `cqrwst` template profile processor chain, `go test`.

---

### File Structure

- Modify `internal/core/templateprofile/profile.go`
  - Extend `RulePack` with structured validation fields.
  - Merge sidecar JSON overrides for those fields.
- Modify `internal/core/templatecontract/contract.go`
  - Persist the expanded `RulePack` in workflow rule JSON.
- Modify `internal/core/cqrwst/template_profile.go`
  - Add a `RulePackValidationProcessor`.
  - Add reusable paragraph extraction helpers and profile-driven checks.
  - Keep auto-repair limited to already-safe rules; most new rules are check-only.
- Modify `internal/core/cqrwst/template_profile_test.go`
  - Add failing tests first for each rule family, then implement.
- Modify `internal/core/templateprofile/profile_test.go`
  - Verify sidecar JSON can configure expanded rules.
- Run existing workflow/service tests to ensure persisted rule JSON still works.

### Task 1: Expand RulePack Schema

**Files:**
- Modify: `internal/core/templateprofile/profile.go`
- Modify: `internal/core/templatecontract/contract.go`
- Test: `internal/core/templateprofile/profile_test.go`

- [ ] **Step 1: Add failing sidecar test**

Add a test that writes `template.docx.rules.json` with:

```json
{
  "required_sections": ["cover", "title_page", "abstract_cn", "abstract_en", "toc", "body", "references", "acknowledgements"],
  "required_fields": ["分类号", "学校代码", "UDC", "密级", "作者", "指导教师"],
  "title_max_cn_chars": 25,
  "title_max_en_words": 10,
  "keyword_min": 3,
  "keyword_max": 5,
  "heading_numbering": "arabic",
  "body_min_chars": 30000,
  "figure_numbering": "chapter",
  "table_numbering": "chapter",
  "formula_numbering": "chapter",
  "reference_min_count": 20,
  "reference_foreign_ratio_min": 0.3333,
  "header_policy": "template",
  "page_numbering": "body_arabic_footer_center",
  "blind_review": true
}
```

Expected: `Build` returns these values in `profile.RulePack`.

- [ ] **Step 2: Implement schema and merge**

Add fields to `RulePack` and extend `mergeRulePack`.

- [ ] **Step 3: Run profile tests**

Run:

```powershell
go test ./internal/core/templateprofile
```

Expected: PASS.

### Task 2: Structure And Field Validation

**Files:**
- Modify: `internal/core/cqrwst/template_profile.go`
- Test: `internal/core/cqrwst/template_profile_test.go`

- [ ] **Step 1: Add failing tests**

Add tests where `required_sections` includes `cover`, `abstract_cn`, `abstract_en`, `toc`, `body`, `references`, `acknowledgements`, but the DOCX lacks one section. Add another test where `required_fields` includes `学校代码` and the DOCX has `学校代码：` with no value.

Expected: `CheckDOCXWithTemplateProfile` fails.

- [ ] **Step 2: Implement check-only processor**

Add `RulePackValidationProcessor` and count missing/empty required sections and fields using extracted visible paragraph text.

- [ ] **Step 3: Run targeted tests**

Run:

```powershell
go test ./internal/core/cqrwst -run "RequiredSections|RequiredFields"
```

Expected: PASS.

### Task 3: Title, Keyword, Heading, Body-Length Validation

**Files:**
- Modify: `internal/core/cqrwst/template_profile.go`
- Test: `internal/core/cqrwst/template_profile_test.go`

- [ ] **Step 1: Add failing tests**

Cover:
- Chinese title exceeds `title_max_cn_chars`.
- English title exceeds `title_max_en_words`.
- Keywords count outside `keyword_min`/`keyword_max` or uses comma instead of semicolon.
- Heading uses `第一章` or `一、` when `heading_numbering` is `arabic`.
- Body text is shorter than `body_min_chars`.

- [ ] **Step 2: Implement validators**

Use conservative heuristics:
- title paragraphs are lines immediately after labels `题名`, `中文题名`, `英文题名`, `Title`.
- keyword lines start with `关键词` or `Key words`.
- heading violations match `^第.+章` or Chinese numeral list prefixes.
- body length counts visible text after the first arabic chapter heading and before references.

- [ ] **Step 3: Run targeted tests**

Run:

```powershell
go test ./internal/core/cqrwst -run "TitleLength|KeywordRules|HeadingNumbering|BodyLength"
```

Expected: PASS.

### Task 4: Figure/Table/Formula And Reference Quantitative Validation

**Files:**
- Modify: `internal/core/cqrwst/template_profile.go`
- Test: `internal/core/cqrwst/template_profile_test.go`

- [ ] **Step 1: Add failing tests**

Cover:
- `图1` fails when `figure_numbering` is `chapter`; `图2.1` passes.
- `表1` fails when `table_numbering` is `chapter`; `表3.2` passes.
- `式(1)` fails when `formula_numbering` is `chapter`; `式(3.5)` passes.
- fewer than `reference_min_count` references fails.
- foreign references below `reference_foreign_ratio_min` fails.

- [ ] **Step 2: Implement validators**

Scan visible paragraphs and reference entries. Treat references containing mostly ASCII letters before the first period as foreign references.

- [ ] **Step 3: Run targeted tests**

Run:

```powershell
go test ./internal/core/cqrwst -run "FigureTableFormulaNumbering|ReferenceCount|ReferenceForeignRatio"
```

Expected: PASS.

### Task 5: Header/Page Number Strategy And Blind Review

**Files:**
- Modify: `internal/core/cqrwst/template_profile.go`
- Test: `internal/core/cqrwst/template_profile_test.go`

- [ ] **Step 1: Add failing tests**

Cover:
- `header_policy=template` fails when the profile requires a header but document has no `headerReference`.
- `page_numbering=body_arabic_footer_center` fails when there is no `PAGE` field or no `pgNumType start="1"`.
- `blind_review=true` fails when visible text contains author or tutor labels with values.

- [ ] **Step 2: Implement validators**

Use document XML for section references and visible text. Header/footer part-level visual fidelity stays outside this task; exact header/footer content remains template-transplant responsibility.

- [ ] **Step 3: Run targeted tests**

Run:

```powershell
go test ./internal/core/cqrwst -run "HeaderPolicy|PageNumbering|BlindReview"
```

Expected: PASS.

### Task 6: Full Verification

**Files:**
- No extra source edits unless tests reveal integration defects.

- [ ] **Step 1: Run rule and workflow tests**

Run:

```powershell
go test ./internal/core/templateprofile ./internal/core/cqrwst ./internal/core/transplant ./internal/core/verify ./internal/service
```

Expected: PASS.

- [ ] **Step 2: Build server locally**

Run:

```powershell
go build ./cmd/server
```

Expected: exit code 0. Delete generated `server.exe` afterward.

### Self-Review

- Spec coverage: all requested rule families are represented as `RulePack` fields and profile-driven checks.
- Repair policy: only safe OOXML formatting repairs remain automatic; semantic/quantitative requirements become verification failures for manual review.
- No school binding: processors inspect `RulePack` values only and do not branch on school name.
- Known limitation: title/section detection is heuristic unless the uploaded template or sidecar names the exact labels; ambiguous documents should fail review rather than pass.
