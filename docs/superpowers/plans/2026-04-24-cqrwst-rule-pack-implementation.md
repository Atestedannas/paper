# 重庆人文科技学院规则包 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build the Chongqing College of Humanities, Science & Technology undergraduate thesis rule pack into the v2 DOCX closed loop so only verified final DOCX files can be downloaded.

**Architecture:** Keep the new v2 route only: `paper_workflow_service -> OOXML rule pack -> verifier -> workflow status`. The first implementation slice is deterministic and safe: text normalization plus machine-checkable issues. Later slices extend the same package to paragraph styles, section headers/footers, pagination, tables, references, appendix, and TOC verification without reviving the old Python/AI fallback chain.

**Tech Stack:** Go, DOCX zip+xml OOXML, `internal/core/ooxmlpkg`, `internal/core/verify`, `internal/core/workflow`, Gin/GORM v2 paper workflow.

---

## File Structure

- Create: `internal/core/cqrwst/rulepack.go`
  - Owns the Chongqing Humanities rule pack for machine-checkable rules.
  - Exposes `FixDOCX(ctx, docxPath)` and `CheckDOCX(ctx, docxPath)`.
- Create: `internal/core/cqrwst/rulepack_test.go`
  - Tests deterministic text fixes and issue classification against minimal DOCX fixtures.
- Modify: `internal/core/verify/verifier.go`
  - Calls the CQRWST checker and maps rule-pack issues into workflow verifier issues.
- Modify: `internal/core/verify/verifier_test.go`
  - Verifies CQRWST repairable issues block `verified_pass`.
- Modify: `internal/service/paper_workflow_service.go`
  - Applies `cqrwst.FixDOCX` before independent verification in `RunJob`.

## Task 1: CQRWST Rule Pack Slice 1

**Files:**
- Create: `internal/core/cqrwst/rulepack.go`
- Create: `internal/core/cqrwst/rulepack_test.go`

- [ ] **Step 1: Write failing tests**

Create tests proving:

```go
func TestFixDOCXNormalizesDeterministicCQRWSTTextRules(t *testing.T) {
	docxPath := writeCQRWSTDocx(t, `<w:p><w:r><w:t>2026年 3 月</w:t></w:r></w:p>`+
		`<w:p><w:r><w:t>1.1研究背景</w:t></w:r></w:p>`+
		`<w:p><w:r><w:t>1.3 国内外研究现状：</w:t></w:r></w:p>`+
		`<w:p><w:r><w:t>5 结论/总结</w:t></w:r></w:p>`+
		`<w:p><w:r><w:t>2/Z</w:t></w:r></w:p>`+
		`<w:p><w:r><w:t>Wald2</w:t></w:r></w:p>`)

	result, err := FixDOCX(context.Background(), docxPath)

	if err != nil {
		t.Fatalf("FixDOCX() error = %v", err)
	}
	if result.FixCount != 6 {
		t.Fatalf("FixCount = %d, want 6", result.FixCount)
	}
	documentXML := readDocumentXML(t, docxPath)
	for _, want := range []string{
		"2026年3月",
		"1.1 研究背景",
		"1.4 国内外研究现状",
		"5 结论",
		"χ²/Z",
		"Wald χ²",
	} {
		if !strings.Contains(documentXML, want) {
			t.Fatalf("document XML missing %q: %s", want, documentXML)
		}
	}
}
```

- [ ] **Step 2: Run failing tests**

Run:

```powershell
go test -count=1 ./internal/core/cqrwst -run TestFixDOCXNormalizesDeterministicCQRWSTTextRules -v
```

Expected: FAIL because package/functions are not implemented.

- [ ] **Step 3: Implement minimal rule pack**

Implement:

```go
type Issue struct {
	RuleID   string
	Kind     string
	Severity string
	Message  string
	Target   string
}

type Result struct {
	Passed   bool
	FixCount int
	Issues   []Issue
}

func FixDOCX(ctx context.Context, docxPath string) (Result, error)
func CheckDOCX(ctx context.Context, docxPath string) (Result, error)
```

Initial deterministic rules:

- `2026年 3 月` -> `2026年3月`
- `1.1研究背景` -> `1.1 研究背景`
- `1.3 国内外研究现状：` -> `1.4 国内外研究现状`
- `1.3.1` -> `1.4.1` and `1.3.2` -> `1.4.2` when under the duplicated research-status heading
- `5 结论/总结` -> `5 结论`
- `2/Z` -> `χ²/Z`
- `Wald2` -> `Wald χ²`
- `[D].石河子大学,2016.` -> `[D].石河子: 石河子大学,2016.`
- DOI trailing punctuation when it is outside the DOI token

- [ ] **Step 4: Run green tests**

Run:

```powershell
go test -count=1 ./internal/core/cqrwst -v
```

Expected: PASS.

## Task 2: Verifier Integration

**Files:**
- Modify: `internal/core/verify/verifier.go`
- Modify: `internal/core/verify/verifier_test.go`

- [ ] **Step 1: Write failing verifier test**

```go
func TestVerifierReportsCQRWSTRepairableIssues(t *testing.T) {
	docxPath := writeMinimalDocx(t, `<w:document><w:body><w:p><w:r><w:t>1.1研究背景</w:t></w:r></w:p></w:body></w:document>`)

	result, err := NewVerifier().Verify(context.Background(), docxPath)

	if err != nil {
		t.Fatalf("Verify() error = %v", err)
	}
	if result.Passed {
		t.Fatal("Verify() Passed = true, want false")
	}
	if len(result.RepairableIssues) == 0 {
		t.Fatal("RepairableIssues is empty")
	}
}
```

- [ ] **Step 2: Run failing verifier test**

Run:

```powershell
go test -count=1 ./internal/core/verify -run TestVerifierReportsCQRWSTRepairableIssues -v
```

Expected: FAIL until verifier calls the CQRWST checker.

- [ ] **Step 3: Map CQRWST issues**

Append CQRWST checker issues to `RepairableIssues` for safe text/format fixes and `Warnings` for manual-only checks.

- [ ] **Step 4: Run verifier tests**

Run:

```powershell
go test -count=1 ./internal/core/verify -v
```

Expected: PASS.

## Task 3: V2 Workflow Integration

**Files:**
- Modify: `internal/service/paper_workflow_service.go`
- Modify: `internal/service/paper_workflow_service_test.go`

- [ ] **Step 1: Write failing service test**

Add a test proving `RunJob` outputs a final DOCX with deterministic CQRWST fixes applied before download path is stored.

- [ ] **Step 2: Run failing service test**

Run:

```powershell
go test -count=1 .\internal\service\paper_workflow_service.go .\internal\service\paper_workflow_service_test.go -run TestPaperWorkflowServiceRunJobAppliesCQRWSTFixesBeforeVerification -v
```

Expected: FAIL because `RunJob` currently copies the file and verifies without fixing.

- [ ] **Step 3: Apply rule pack before verification**

After `copyFile(job.Paper.FilePath, outputPath)`, call:

```go
if _, err := cqrwst.FixDOCX(ctx, outputPath); err != nil {
	return nil, err
}
```

- [ ] **Step 4: Run service tests**

Run:

```powershell
go test -count=1 .\internal\service\paper_workflow_service.go .\internal\service\paper_workflow_service_test.go -v
```

Expected: PASS.

## Task 4: Full Verification

- [ ] **Step 1: Run focused backend tests**

```powershell
go test -count=1 ./internal/core/cqrwst ./internal/core/verify ./internal/core/workflow
go test -count=1 .\internal\service\paper_workflow_service.go .\internal\service\paper_workflow_service_test.go -v
go test -count=1 ./cmd/server
```

Expected: all PASS.

- [ ] **Step 2: Manual smoke**

Run backend:

```powershell
cd "C:\Users\user\.config\superpowers\worktrees\paper\docx-closed-loop-task1"
go run .\cmd\server
```

Upload a CQRWST test DOCX through `POST /api/v2/papers`, run job, and download final DOCX only after `verified_pass`.

## Follow-up Rule Pack Slices

- Slice 2: paragraph/run style writer for title levels, abstract, keywords, body, references, acknowledgements, appendix.
- Slice 3: section properties for A4, margins, header/footer, summary roman page numbering, body arabic numbering.
- Slice 4: table formatting to three-line tables and table caption spacing.
- Slice 5: formula, TOC, cross-page table continuation checks as manual-review gates where Word pagination cannot be safely derived from raw OOXML.

## Self-Review

- Spec coverage: the plan covers the new CQRWST route and starts with deterministic fixes. Full school spec is split into follow-up slices because page layout, headers/footers, pagination, and table continuation are independent subsystems.
- Placeholder scan: no `TBD`/`TODO` remains.
- Type consistency: `FixDOCX`, `CheckDOCX`, `Result`, and `Issue` are defined before use.
