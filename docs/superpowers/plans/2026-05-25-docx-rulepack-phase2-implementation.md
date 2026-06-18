# DOCX RulePack Phase 2 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add advanced configurable DOCX validation for odd/even headers, complex page numbering, heading numbering systems, caption position/numbering, and reference styles.

**Architecture:** Extend `templateprofile.RulePack`, then keep all logic in generic cqrwst processors. The processors never branch on school names; they only interpret rule values from template profile or sidecar JSON.

**Tech Stack:** Go, existing OOXML package reader/writer, existing `cqrwst` template profile workflow, `go test`.

---

### Task 1: RulePack Schema

**Files:**
- Modify: `internal/core/templateprofile/profile.go`
- Modify: `internal/core/templateprofile/profile_test.go`

- [ ] Add fields for odd/even headers, page formats, heading levels, caption positions, and reference style.
- [ ] Extend `mergeRulePack` so sidecar JSON can override every new field.
- [ ] Add assertions to `TestBuildMergesRulePackSidecar`.
- [ ] Run `go test ./internal/core/templateprofile`.

### Task 2: Processor Split

**Files:**
- Modify: `internal/core/cqrwst/template_profile.go`

- [ ] Add processor structs: `HeaderFooterPolicyProcessor`, `PageNumberingProcessor`, `HeadingNumberingProcessor`, `FigureTableCaptionProcessor`, `ReferenceStyleProcessor`.
- [ ] Register them in `templateProfileProcessors`.
- [ ] Keep old `RulePackValidationProcessor` for existing scalar checks.
- [ ] Each new processor returns count-only validation failures and does not write the DOCX in this phase.

### Task 3: Header And Page Number Tests

**Files:**
- Modify: `internal/core/cqrwst/template_profile_test.go`

- [ ] Test `header_policy=none` fails when document has header references.
- [ ] Test `header_policy=odd_even` fails without odd/even header references.
- [ ] Test `front_page_format=lowerRoman` and `body_page_format=decimal` require matching `pgNumType`.
- [ ] Test `body_page_wrapper=dash` requires footer text around the page field.

### Task 4: Heading And Caption Tests

**Files:**
- Modify: `internal/core/cqrwst/template_profile_test.go`

- [ ] Test `heading_levels=["第1章","1.1","1.1.1"]`.
- [ ] Test `heading_levels=["第一章","1.1","1.1.1"]`.
- [ ] Test `heading_levels=["一","(一)","1"]`.
- [ ] Test `figure_numbering=continuous` accepts `图1` and rejects `图1.1`.
- [ ] Test `table_caption_position=above` fails when a table caption appears after the table.

### Task 5: Reference Style Tests

**Files:**
- Modify: `internal/core/cqrwst/template_profile_test.go`

- [ ] Test `reference_style=gb_t_7714_sequence` requires `[1]` entries with reference type markers.
- [ ] Test `reference_style=author_year` requires author-year shape.
- [ ] Test `reference_style=sample_book_journal_basic` accepts `[M]` and `[J]` examples and rejects entries without type/source data.

### Task 6: Implementation And Verification

**Files:**
- Modify: `internal/core/cqrwst/template_profile.go`

- [ ] Implement helpers for OOXML header/footer entries, section references, heading pattern validation, caption neighborhood scanning, and reference style validation.
- [ ] Run focused cqrwst tests.
- [ ] Run `go test ./internal/core/templateprofile ./internal/core/cqrwst ./internal/core/transplant ./internal/core/verify ./internal/service`.
- [ ] Run `go build ./cmd/server` and delete generated `server.exe`.
