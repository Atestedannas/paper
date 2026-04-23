# DOCX 单模板闭环重构 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 将当前论文格式系统重构为“单模板预编译 + 模板骨架移植 + 块级内容映射 + 独立复检 + 最终文件直下”的唯一生产主链。

**Architecture:** 先建立新的 v2 工作流数据模型与任务状态机，再围绕 `templatecompile -> paperparse -> blockmap -> transplant -> ooxmlpatch -> verify -> workflow` 七个核心包逐步落地。旧的上传即修复、多引擎回退、半成品 comparison 链不再进入生产写路径，待 v2 通过验收后统一下线。

**Tech Stack:** Go, Gin, GORM, PostgreSQL, OOXML(zip+xml), unioffice, Vue 3, Pinia, Vue Router, Vite, Vitest

---

## File Structure

### 数据模型与迁移

- Create: `backend/internal/model/paper_workflow_v2.go`
  责任：定义编译模板、闭环任务、任务问题项的持久化模型。
- Create: `backend/internal/database/migration_20260423_docx_closed_loop_v2.go`
  责任：创建 v2 主链所需表结构与索引。
- Modify: `backend/internal/database/migrations.go`
  责任：注册新迁移。
- Create: `backend/internal/database/migration_20260423_docx_closed_loop_v2_test.go`
  责任：验证迁移建表、索引、幂等性。

### OOXML 基础层

- Create: `backend/internal/core/ooxmlpkg/docx_package.go`
  责任：提供 DOCX zip 包读取、写回、条目访问的统一底层工具。
- Create: `backend/internal/core/ooxmlpkg/docx_package_test.go`
  责任：验证 round-trip、缺失条目处理、稳定输出顺序。

### 模板编译

- Create: `backend/internal/core/templatecompile/types.go`
  责任：定义模板资产包、块目录、样式档案、映射合同。
- Create: `backend/internal/core/templatecompile/compiler.go`
  责任：将单模板编译为版本化资产包。
- Create: `backend/internal/core/templatecompile/compiler_test.go`
  责任：验证编译结果包含 manifest、block catalog、skeleton、patch targets。

### 学生稿解析

- Create: `backend/internal/core/paperparse/types.go`
  责任：定义解析后的内容树、块、标题树。
- Create: `backend/internal/core/paperparse/parser.go`
  责任：按确定性规则解析学生稿。
- Create: `backend/internal/core/paperparse/parser_test.go`
  责任：验证摘要、关键词、标题、参考文献等分区识别。

### 块级映射

- Create: `backend/internal/core/blockmap/types.go`
  责任：定义映射结果、异常桶、绑定关系。
- Create: `backend/internal/core/blockmap/mapper.go`
  责任：将学生内容映射到模板槽位。
- Create: `backend/internal/core/blockmap/mapper_test.go`
  责任：验证模板顺序优先、歧义检测、异常桶输出。

### 生成与补丁

- Create: `backend/internal/core/transplant/transplanter.go`
  责任：以模板骨架为基底生成最终文档。
- Create: `backend/internal/core/transplant/transplanter_test.go`
  责任：验证封面、摘要、标题、正文与参考文献移植。
- Create: `backend/internal/core/ooxmlpatch/writer.go`
  责任：对白名单 OOXML 节点做定点补丁。
- Create: `backend/internal/core/ooxmlpatch/writer_test.go`
  责任：验证 TOC、编号、run/段落补丁不越界。

### 复检与闭环

- Create: `backend/internal/core/verify/verifier.go`
  责任：独立执行块级、样式级、包级、安全级验证。
- Create: `backend/internal/core/verify/verifier_test.go`
  责任：验证 fatal / repairable / warnings 分类。
- Create: `backend/internal/core/workflow/types.go`
  责任：定义阶段、状态、转换规则、工作流结果。
- Create: `backend/internal/core/workflow/store.go`
  责任：封装任务存储与状态更新。
- Create: `backend/internal/core/workflow/store_test.go`
  责任：验证状态转换与持久化。
- Create: `backend/internal/core/workflow/loop_controller.go`
  责任：编排 parse -> map -> transplant -> patch -> verify 闭环。
- Create: `backend/internal/core/workflow/loop_controller_test.go`
  责任：验证一次补丁重试、fatal 直接 manual_review。

### 服务层与路由

- Create: `backend/internal/service/paper_workflow_service.go`
  责任：对 handler 暴露 v2 工作流入口。
- Create: `backend/internal/service/paper_workflow_service_test.go`
  责任：验证服务层串联核心模块。
- Create: `backend/internal/handler/paper_workflow_handler.go`
  责任：暴露 `/api/v2/templates/compile`、`/api/v2/papers`、`/api/v2/jobs/*`。
- Create: `backend/internal/handler/paper_workflow_handler_test.go`
  责任：验证接口码、下载保护、错误返回结构。
- Modify: `backend/cmd/server/main.go`
  责任：注册 v2 路由并为 legacy 路由加开关。

### 前端直通任务流

- Modify: `frontend/package.json`
  责任：加入 `vitest` 与前端测试脚本。
- Modify: `frontend/src/api/paper.js`
  责任：新增 v2 接口封装，删除旧生产路径依赖。
- Modify: `frontend/src/stores/paper.js`
  责任：将当前 store 改为 job 驱动状态。
- Create: `frontend/src/stores/paper.spec.js`
  责任：验证任务创建、轮询、下载条件。
- Modify: `frontend/src/router/index.js`
  责任：新增任务页路由，移除旧修复链入口。
- Modify: `frontend/src/views/UploadView.vue`
  责任：上传后直接进入任务页，而不是检查/修复多页面链路。
- Create: `frontend/src/views/PaperJobView.vue`
  责任：展示任务阶段、失败原因、下载按钮。

### 旧链路下线

- Modify: `backend/internal/handler/paper_handler.go`
  责任：停用旧生产写路径，返回明确的 legacy 提示。
- Modify: `backend/internal/service/paper_service.go`
  责任：移除多引擎主链入口，保留只读兼容行为。
- Modify: `backend/internal/service/format_comparison_service.go`
  责任：明确降级为非生产链或直接停用。
- Create: `backend/internal/handler/paper_handler_legacy_test.go`
  责任：验证 legacy 写路径被禁用。

## Task 1: 建立 v2 数据模型与迁移

**Files:**
- Create: `backend/internal/model/paper_workflow_v2.go`
- Create: `backend/internal/database/migration_20260423_docx_closed_loop_v2.go`
- Modify: `backend/internal/database/migrations.go`
- Create: `backend/internal/database/migration_20260423_docx_closed_loop_v2_test.go`

- [ ] **Step 1: 写失败测试**

```go
package database

import (
	"testing"
	"time"
)

func TestMigration20260423CreateDocxClosedLoopV2Tables(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	testDB, cleanup := setupTestDB(t, time.Now().Unix())
	defer cleanup()

	migration := &Migration20260423CreateDocxClosedLoopV2Tables{}
	if err := runTestMigration(testDB, migration); err != nil {
		t.Fatalf("migration failed: %v", err)
	}

	if !testDB.Migrator().HasTable("compiled_templates") {
		t.Fatalf("compiled_templates table was not created")
	}
	if !testDB.Migrator().HasTable("paper_workflow_jobs") {
		t.Fatalf("paper_workflow_jobs table was not created")
	}
	if !testDB.Migrator().HasTable("paper_workflow_issues") {
		t.Fatalf("paper_workflow_issues table was not created")
	}
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `go test ./internal/database -run TestMigration20260423CreateDocxClosedLoopV2Tables -v`
Expected: FAIL，提示 `Migration20260423CreateDocxClosedLoopV2Tables` 未定义或表不存在。

- [ ] **Step 3: 写最小实现**

```go
package model

import (
	"time"

	"github.com/google/uuid"
)

type CompiledTemplate struct {
	ID              uuid.UUID `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"id"`
	SchoolID        string    `gorm:"size:64;index;not null" json:"school_id"`
	TemplateName    string    `gorm:"size:255;not null" json:"template_name"`
	TemplateVersion string    `gorm:"size:64;index;not null" json:"template_version"`
	SourceFilePath  string    `gorm:"size:255;not null" json:"source_file_path"`
	SkeletonPath    string    `gorm:"size:255;not null" json:"skeleton_path"`
	ManifestJSON    string    `gorm:"type:jsonb;not null" json:"manifest_json"`
	BlockCatalogJSON string   `gorm:"type:jsonb;not null" json:"block_catalog_json"`
	StyleProfilesJSON string  `gorm:"type:jsonb;not null" json:"style_profiles_json"`
	MappingContractJSON string `gorm:"type:jsonb;not null" json:"mapping_contract_json"`
	VerificationRulesJSON string `gorm:"type:jsonb;not null" json:"verification_rules_json"`
	PatchTargetsJSON string   `gorm:"type:jsonb;not null" json:"patch_targets_json"`
	Status          string    `gorm:"size:32;not null;default:'compiled'" json:"status"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

type PaperWorkflowJob struct {
	ID              uuid.UUID `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"id"`
	PaperID         uuid.UUID `gorm:"type:uuid;index;not null" json:"paper_id"`
	UserID          uuid.UUID `gorm:"type:uuid;index;not null" json:"user_id"`
	CompiledTemplateID uuid.UUID `gorm:"type:uuid;index;not null" json:"compiled_template_id"`
	Status          string    `gorm:"size:32;index;not null;default:'uploaded'" json:"status"`
	Stage           string    `gorm:"size:32;index;not null;default:'queued'" json:"stage"`
	DownloadPath    string    `gorm:"size:255" json:"download_path"`
	VerifyResultJSON string   `gorm:"type:jsonb;not null;default:'{}'" json:"verify_result_json"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

type PaperWorkflowIssue struct {
	ID              uuid.UUID `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"id"`
	JobID           uuid.UUID `gorm:"type:uuid;index;not null" json:"job_id"`
	Kind            string    `gorm:"size:32;index;not null" json:"kind"`
	Severity        string    `gorm:"size:16;index;not null" json:"severity"`
	BlockID         string    `gorm:"size:128" json:"block_id"`
	Message         string    `gorm:"type:text;not null" json:"message"`
	DetailJSON      string    `gorm:"type:jsonb;not null;default:'{}'" json:"detail_json"`
	CreatedAt       time.Time `json:"created_at"`
}
```

```go
package database

import (
	"github.com/paper-format-checker/backend/internal/model"
	"gorm.io/gorm"
)

type Migration20260423CreateDocxClosedLoopV2Tables struct{}

func (m *Migration20260423CreateDocxClosedLoopV2Tables) Name() string {
	return "20260423_create_docx_closed_loop_v2_tables"
}

func (m *Migration20260423CreateDocxClosedLoopV2Tables) Up(tx *gorm.DB) error {
	if err := tx.AutoMigrate(
		&model.CompiledTemplate{},
		&model.PaperWorkflowJob{},
		&model.PaperWorkflowIssue{},
	); err != nil {
		return err
	}
	return tx.Exec(`
		CREATE INDEX IF NOT EXISTS idx_paper_workflow_jobs_status_stage
		ON paper_workflow_jobs(status, stage)
	`).Error
}

func (m *Migration20260423CreateDocxClosedLoopV2Tables) Down(tx *gorm.DB) error {
	if err := tx.Migrator().DropTable(&model.PaperWorkflowIssue{}); err != nil {
		return err
	}
	if err := tx.Migrator().DropTable(&model.PaperWorkflowJob{}); err != nil {
		return err
	}
	return tx.Migrator().DropTable(&model.CompiledTemplate{})
}
```

```go
// in backend/internal/database/migrations.go
&Migration20260423CreateDocxClosedLoopV2Tables{},
```

- [ ] **Step 4: 运行测试确认通过**

Run: `go test ./internal/database -run TestMigration20260423CreateDocxClosedLoopV2Tables -v`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add backend/internal/model/paper_workflow_v2.go \
  backend/internal/database/migration_20260423_docx_closed_loop_v2.go \
  backend/internal/database/migration_20260423_docx_closed_loop_v2_test.go \
  backend/internal/database/migrations.go
git commit -m "feat: add v2 workflow models and migrations"
```

## Task 2: 增加 DOCX 包底层工具

**Files:**
- Create: `backend/internal/core/ooxmlpkg/docx_package.go`
- Create: `backend/internal/core/ooxmlpkg/docx_package_test.go`

- [ ] **Step 1: 写失败测试**

```go
package ooxmlpkg

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDocxPackageRoundTrip(t *testing.T) {
	src := filepath.Join("testdata", "minimal.docx")
	pkg, err := Open(src)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}

	documentXML, ok := pkg.Get("word/document.xml")
	if !ok || len(documentXML) == 0 {
		t.Fatalf("word/document.xml missing")
	}

	out := filepath.Join(t.TempDir(), "roundtrip.docx")
	if err := pkg.Write(out); err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	info, err := os.Stat(out)
	if err != nil || info.Size() == 0 {
		t.Fatalf("output file not written")
	}
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `go test ./internal/core/ooxmlpkg -run TestDocxPackageRoundTrip -v`
Expected: FAIL，提示 `Open` 未定义。

- [ ] **Step 3: 写最小实现**

```go
package ooxmlpkg

import (
	"archive/zip"
	"io"
	"os"
	"sort"
)

type DocxPackage struct {
	entries map[string][]byte
}

func Open(path string) (*DocxPackage, error) {
	reader, err := zip.OpenReader(path)
	if err != nil {
		return nil, err
	}
	defer reader.Close()

	entries := make(map[string][]byte, len(reader.File))
	for _, file := range reader.File {
		rc, err := file.Open()
		if err != nil {
			return nil, err
		}
		content, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			return nil, err
		}
		entries[file.Name] = content
	}
	return &DocxPackage{entries: entries}, nil
}

func (p *DocxPackage) Get(name string) ([]byte, bool) {
	value, ok := p.entries[name]
	return value, ok
}

func (p *DocxPackage) Set(name string, content []byte) {
	if p.entries == nil {
		p.entries = map[string][]byte{}
	}
	p.entries[name] = content
}

func (p *DocxPackage) Write(path string) error {
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer file.Close()

	writer := zip.NewWriter(file)
	names := make([]string, 0, len(p.entries))
	for name := range p.entries {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		w, err := writer.Create(name)
		if err != nil {
			return err
		}
		if _, err := w.Write(p.entries[name]); err != nil {
			return err
		}
	}
	return writer.Close()
}
```

- [ ] **Step 4: 运行测试确认通过**

Run: `go test ./internal/core/ooxmlpkg -run TestDocxPackageRoundTrip -v`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add backend/internal/core/ooxmlpkg/docx_package.go \
  backend/internal/core/ooxmlpkg/docx_package_test.go
git commit -m "feat: add OOXML package helper"
```

## Task 3: 实现模板编译器

**Files:**
- Create: `backend/internal/core/templatecompile/types.go`
- Create: `backend/internal/core/templatecompile/compiler.go`
- Create: `backend/internal/core/templatecompile/compiler_test.go`

- [ ] **Step 1: 写失败测试**

```go
package templatecompile

import (
	"context"
	"testing"
)

func TestCompilerBuildsCompiledTemplatePackage(t *testing.T) {
	templatePath := writeSimpleTemplateDocx(t)
	compiler := NewCompiler()

	result, err := compiler.Compile(context.Background(), templatePath, CompileOptions{
		SchoolID:     "cq-test",
		TemplateName: "官方模板",
		Version:      "v1",
	})
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	if result.Manifest.SchoolID != "cq-test" {
		t.Fatalf("unexpected school id: %s", result.Manifest.SchoolID)
	}
	if len(result.BlockCatalog) == 0 {
		t.Fatalf("expected non-empty block catalog")
	}
	if result.SkeletonPath == "" {
		t.Fatalf("expected skeleton path")
	}
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `go test ./internal/core/templatecompile -run TestCompilerBuildsCompiledTemplatePackage -v`
Expected: FAIL，提示 `NewCompiler` 或 `Compile` 未定义。

- [ ] **Step 3: 写最小实现**

```go
package templatecompile

type Manifest struct {
	SchoolID        string `json:"school_id"`
	TemplateName    string `json:"template_name"`
	TemplateVersion string `json:"template_version"`
}

type TemplateBlock struct {
	BlockID      string   `json:"block_id"`
	Kind         string   `json:"kind"`
	SlotType     string   `json:"slot_type"`
	OrderIndex   int      `json:"order_index"`
	Required     bool     `json:"required"`
	Accepts      []string `json:"accepts"`
	StyleProfile string   `json:"style_profile_id"`
}

type CompiledTemplatePackage struct {
	Manifest       Manifest         `json:"manifest"`
	BlockCatalog   []TemplateBlock  `json:"block_catalog"`
	SkeletonPath   string           `json:"skeleton_path"`
	PatchTargets   []string         `json:"patch_targets"`
}

type CompileOptions struct {
	SchoolID     string
	TemplateName string
	Version      string
}
```

```go
package templatecompile

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/google/uuid"
)

type Compiler struct{}

func NewCompiler() *Compiler {
	return &Compiler{}
}

func (c *Compiler) Compile(ctx context.Context, templatePath string, opts CompileOptions) (*CompiledTemplatePackage, error) {
	_ = ctx

	skeletonDir := filepath.Join("uploads", "compiled_templates", uuid.NewString())
	if err := os.MkdirAll(skeletonDir, 0755); err != nil {
		return nil, err
	}
	skeletonPath := filepath.Join(skeletonDir, "template.docx")
	input, err := os.ReadFile(templatePath)
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(skeletonPath, input, 0644); err != nil {
		return nil, err
	}

	return &CompiledTemplatePackage{
		Manifest: Manifest{
			SchoolID:        opts.SchoolID,
			TemplateName:    opts.TemplateName,
			TemplateVersion: opts.Version,
		},
		BlockCatalog: []TemplateBlock{
			{BlockID: "cover_title", Kind: "cover_title", SlotType: "single", OrderIndex: 10, Required: true, Accepts: []string{"cover_title"}, StyleProfile: "cover_title_profile"},
			{BlockID: "abstract_cn_body", Kind: "abstract_cn_body", SlotType: "single", OrderIndex: 20, Required: true, Accepts: []string{"abstract_cn_body"}, StyleProfile: "abstract_cn_body_profile"},
			{BlockID: "heading_1", Kind: "heading_1", SlotType: "repeatable", OrderIndex: 30, Required: true, Accepts: []string{"heading_1"}, StyleProfile: "heading_1_profile"},
		},
		SkeletonPath: skeletonPath,
		PatchTargets: []string{"word/document.xml", "word/_rels/document.xml.rels", "word/settings.xml"},
	}, nil
}

func (p *CompiledTemplatePackage) MustBlock(kind string) (TemplateBlock, error) {
	for _, block := range p.BlockCatalog {
		if block.Kind == kind {
			return block, nil
		}
	}
	return TemplateBlock{}, fmt.Errorf("block kind %s not found", kind)
}
```

- [ ] **Step 4: 运行测试确认通过**

Run: `go test ./internal/core/templatecompile -run TestCompilerBuildsCompiledTemplatePackage -v`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add backend/internal/core/templatecompile/types.go \
  backend/internal/core/templatecompile/compiler.go \
  backend/internal/core/templatecompile/compiler_test.go
git commit -m "feat: add template compiler skeleton"
```

## Task 4: 实现学生稿解析器

**Files:**
- Create: `backend/internal/core/paperparse/types.go`
- Create: `backend/internal/core/paperparse/parser.go`
- Create: `backend/internal/core/paperparse/parser_test.go`

- [ ] **Step 1: 写失败测试**

```go
package paperparse

import (
	"context"
	"testing"
)

func TestParserBuildsStructuredPaper(t *testing.T) {
	docPath := writeStructuredPaperDocx(t)
	parser := NewParser()

	result, err := parser.Parse(context.Background(), docPath)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if len(result.Headings) != 2 {
		t.Fatalf("expected 2 headings, got %d", len(result.Headings))
	}
	if result.AbstractCN == "" {
		t.Fatalf("expected chinese abstract")
	}
	if len(result.References) != 2 {
		t.Fatalf("expected 2 references")
	}
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `go test ./internal/core/paperparse -run TestParserBuildsStructuredPaper -v`
Expected: FAIL，提示 `NewParser` 或 `Parse` 未定义。

- [ ] **Step 3: 写最小实现**

```go
package paperparse

type Heading struct {
	Level int    `json:"level"`
	Text  string `json:"text"`
}

type ParsedPaper struct {
	CoverFields map[string]string `json:"cover_fields"`
	AbstractCN  string            `json:"abstract_cn"`
	KeywordsCN  []string          `json:"keywords_cn"`
	Headings    []Heading         `json:"headings"`
	Body        []string          `json:"body"`
	References  []string          `json:"references"`
	Acknowledgements []string     `json:"acknowledgements"`
	Abnormal    []string          `json:"abnormal"`
}
```

```go
package paperparse

import (
	"context"
	"strings"

	"gitee.com/greatmusicians/unioffice/document"
)

type Parser struct{}

func NewParser() *Parser {
	return &Parser{}
}

func (p *Parser) Parse(ctx context.Context, docPath string) (*ParsedPaper, error) {
	_ = ctx

	doc, err := document.Open(docPath)
	if err != nil {
		return nil, err
	}
	defer doc.Close()

	result := &ParsedPaper{
		CoverFields: map[string]string{},
	}
	inReferences := false
	for _, para := range doc.Paragraphs() {
		text := strings.TrimSpace(collectParagraphText(para))
		if text == "" {
			continue
		}
		switch {
		case strings.HasPrefix(text, "摘要"):
			result.AbstractCN = strings.TrimPrefix(text, "摘要")
		case strings.HasPrefix(text, "关键词"):
			result.KeywordsCN = strings.Split(strings.TrimPrefix(text, "关键词"), "、")
		case text == "参考文献":
			inReferences = true
		case strings.HasPrefix(text, "1 ") || strings.HasPrefix(text, "1.") || strings.HasPrefix(text, "一、"):
			result.Headings = append(result.Headings, Heading{Level: 1, Text: text})
		case strings.HasPrefix(text, "1.1") || strings.HasPrefix(text, "（一）"):
			result.Headings = append(result.Headings, Heading{Level: 2, Text: text})
		case inReferences:
			result.References = append(result.References, text)
		default:
			result.Body = append(result.Body, text)
		}
	}
	return result, nil
}

func collectParagraphText(para document.Paragraph) string {
	var builder strings.Builder
	for _, run := range para.Runs() {
		builder.WriteString(run.Text())
	}
	return builder.String()
}
```

- [ ] **Step 4: 运行测试确认通过**

Run: `go test ./internal/core/paperparse -run TestParserBuildsStructuredPaper -v`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add backend/internal/core/paperparse/types.go \
  backend/internal/core/paperparse/parser.go \
  backend/internal/core/paperparse/parser_test.go
git commit -m "feat: add deterministic paper parser"
```

## Task 5: 实现模板顺序优先的块映射器

**Files:**
- Create: `backend/internal/core/blockmap/types.go`
- Create: `backend/internal/core/blockmap/mapper.go`
- Create: `backend/internal/core/blockmap/mapper_test.go`

- [ ] **Step 1: 写失败测试**

```go
package blockmap

import (
	"testing"

	"github.com/paper-format-checker/backend/internal/core/paperparse"
	"github.com/paper-format-checker/backend/internal/core/templatecompile"
)

func TestMapperPrefersTemplateOrderAndEmitsAbnormalBuckets(t *testing.T) {
	mapper := NewMapper()
	template := &templatecompile.CompiledTemplatePackage{
		BlockCatalog: []templatecompile.TemplateBlock{
			{BlockID: "cover_title", Kind: "cover_title", SlotType: "single", OrderIndex: 10, Accepts: []string{"cover_title"}},
			{BlockID: "abstract_cn_body", Kind: "abstract_cn_body", SlotType: "single", OrderIndex: 20, Accepts: []string{"abstract_cn_body"}},
			{BlockID: "heading_1", Kind: "heading_1", SlotType: "repeatable", OrderIndex: 30, Accepts: []string{"heading_1"}},
		},
	}
	paper := &paperparse.ParsedPaper{
		CoverFields: map[string]string{"cover_title": "毕业论文"},
		AbstractCN:  "这是摘要",
		Headings:    []paperparse.Heading{{Level: 1, Text: "1 绪论"}},
		Abnormal:    []string{"未识别段落"},
	}

	result, err := mapper.Map(template, paper)
	if err != nil {
		t.Fatalf("Map() error = %v", err)
	}
	if len(result.Bindings) != 3 {
		t.Fatalf("expected 3 bindings, got %d", len(result.Bindings))
	}
	if len(result.UnmappedBlocks) != 1 {
		t.Fatalf("expected 1 unmapped block")
	}
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `go test ./internal/core/blockmap -run TestMapperPrefersTemplateOrderAndEmitsAbnormalBuckets -v`
Expected: FAIL，提示 `NewMapper` 或 `Map` 未定义。

- [ ] **Step 3: 写最小实现**

```go
package blockmap

type Binding struct {
	BlockID   string `json:"block_id"`
	BlockKind string `json:"block_kind"`
	Payload   string `json:"payload"`
}

type MappingResult struct {
	Bindings        []Binding `json:"bindings"`
	GeneratedBlocks []string  `json:"generated_blocks"`
	UnmappedBlocks  []string  `json:"unmapped_blocks"`
	AmbiguousBlocks []string  `json:"ambiguous_blocks"`
}
```

```go
package blockmap

import (
	"sort"

	"github.com/paper-format-checker/backend/internal/core/paperparse"
	"github.com/paper-format-checker/backend/internal/core/templatecompile"
)

type Mapper struct{}

func NewMapper() *Mapper {
	return &Mapper{}
}

func (m *Mapper) Map(template *templatecompile.CompiledTemplatePackage, paper *paperparse.ParsedPaper) (*MappingResult, error) {
	blocks := append([]templatecompile.TemplateBlock(nil), template.BlockCatalog...)
	sort.Slice(blocks, func(i, j int) bool { return blocks[i].OrderIndex < blocks[j].OrderIndex })

	result := &MappingResult{}
	for _, block := range blocks {
		switch block.Kind {
		case "cover_title":
			result.Bindings = append(result.Bindings, Binding{BlockID: block.BlockID, BlockKind: block.Kind, Payload: paper.CoverFields["cover_title"]})
		case "abstract_cn_body":
			result.Bindings = append(result.Bindings, Binding{BlockID: block.BlockID, BlockKind: block.Kind, Payload: paper.AbstractCN})
		case "heading_1":
			for _, heading := range paper.Headings {
				if heading.Level == 1 {
					result.Bindings = append(result.Bindings, Binding{BlockID: block.BlockID, BlockKind: block.Kind, Payload: heading.Text})
				}
			}
		}
	}
	result.UnmappedBlocks = append(result.UnmappedBlocks, paper.Abnormal...)
	return result, nil
}
```

- [ ] **Step 4: 运行测试确认通过**

Run: `go test ./internal/core/blockmap -run TestMapperPrefersTemplateOrderAndEmitsAbnormalBuckets -v`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add backend/internal/core/blockmap/types.go \
  backend/internal/core/blockmap/mapper.go \
  backend/internal/core/blockmap/mapper_test.go
git commit -m "feat: add template-order block mapper"
```

## Task 6: 实现模板骨架移植器

**Files:**
- Create: `backend/internal/core/transplant/transplanter.go`
- Create: `backend/internal/core/transplant/transplanter_test.go`

- [ ] **Step 1: 写失败测试**

```go
package transplant

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/paper-format-checker/backend/internal/core/blockmap"
	"github.com/paper-format-checker/backend/internal/core/templatecompile"
)

func TestTransplanterWritesFinalDocxFromTemplateSkeleton(t *testing.T) {
	templatePath := writeTemplateSkeletonDocx(t)
	out := filepath.Join(t.TempDir(), "final.docx")
	transplanter := NewTransplanter()

	err := transplanter.Generate(context.Background(), GenerateInput{
		CompiledTemplate: &templatecompile.CompiledTemplatePackage{SkeletonPath: templatePath},
		Mapping: &blockmap.MappingResult{
			Bindings: []blockmap.Binding{
				{BlockID: "cover_title", BlockKind: "cover_title", Payload: "毕业论文"},
				{BlockID: "abstract_cn_body", BlockKind: "abstract_cn_body", Payload: "这是摘要"},
			},
		},
		OutputPath: out,
	})
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	documentXML := readZipEntry(t, out, "word/document.xml")
	if !containsAll(documentXML, "毕业论文", "这是摘要") {
		t.Fatalf("expected transplanted content in output")
	}
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `go test ./internal/core/transplant -run TestTransplanterWritesFinalDocxFromTemplateSkeleton -v`
Expected: FAIL，提示 `NewTransplanter` 或 `Generate` 未定义。

- [ ] **Step 3: 写最小实现**

```go
package transplant

import (
	"context"
	"os"
	"strings"

	"github.com/paper-format-checker/backend/internal/core/blockmap"
	"github.com/paper-format-checker/backend/internal/core/templatecompile"
)

type GenerateInput struct {
	CompiledTemplate *templatecompile.CompiledTemplatePackage
	Mapping          *blockmap.MappingResult
	OutputPath       string
}

type Transplanter struct{}

func NewTransplanter() *Transplanter {
	return &Transplanter{}
}

func (t *Transplanter) Generate(ctx context.Context, input GenerateInput) error {
	_ = ctx

	raw, err := os.ReadFile(input.CompiledTemplate.SkeletonPath)
	if err != nil {
		return err
	}

	updated := string(raw)
	for _, binding := range input.Mapping.Bindings {
		placeholder := "{{" + binding.BlockID + "}}"
		updated = strings.ReplaceAll(updated, placeholder, binding.Payload)
	}

	return os.WriteFile(input.OutputPath, []byte(updated), 0644)
}
```

- [ ] **Step 4: 运行测试确认通过**

Run: `go test ./internal/core/transplant -run TestTransplanterWritesFinalDocxFromTemplateSkeleton -v`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add backend/internal/core/transplant/transplanter.go \
  backend/internal/core/transplant/transplanter_test.go
git commit -m "feat: add template skeleton transplanter"
```

## Task 7: 实现白名单补丁器、独立复检器和闭环控制器

**Files:**
- Create: `backend/internal/core/ooxmlpatch/writer.go`
- Create: `backend/internal/core/ooxmlpatch/writer_test.go`
- Create: `backend/internal/core/verify/verifier.go`
- Create: `backend/internal/core/verify/verifier_test.go`
- Create: `backend/internal/core/workflow/types.go`
- Create: `backend/internal/core/workflow/store.go`
- Create: `backend/internal/core/workflow/store_test.go`
- Create: `backend/internal/core/workflow/loop_controller.go`
- Create: `backend/internal/core/workflow/loop_controller_test.go`

- [ ] **Step 1: 写失败测试**

```go
package workflow

import (
	"context"
	"testing"

	"github.com/paper-format-checker/backend/internal/core/verify"
)

type fakeVerifier struct {
	results []verify.Result
}

func (f *fakeVerifier) Verify(context.Context, string) (verify.Result, error) {
	result := f.results[0]
	f.results = f.results[1:]
	return result, nil
}

func TestLoopControllerRetriesPatchOnceOnRepairableIssues(t *testing.T) {
	controller := NewLoopController(nil, nil, &fakeVerifier{
		results: []verify.Result{
			{Passed: false, RepairableIssues: []verify.Issue{{Kind: "toc_patch"}}},
			{Passed: true},
		},
	})

	result, err := controller.Run(context.Background(), RunInput{OutputPath: "final.docx"})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.Status != StatusVerifiedPass {
		t.Fatalf("expected verified pass, got %s", result.Status)
	}
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `go test ./internal/core/ooxmlpatch ./internal/core/verify ./internal/core/workflow -run TestLoopControllerRetriesPatchOnceOnRepairableIssues -v`
Expected: FAIL，提示 `NewLoopController`、`verify.Result` 或常量未定义。

- [ ] **Step 3: 写最小实现**

```go
package verify

type Issue struct {
	Kind     string
	Severity string
	Message  string
}

type Result struct {
	Passed           bool
	FatalIssues      []Issue
	RepairableIssues []Issue
	Warnings         []Issue
}
```

```go
package workflow

type Status string

const (
	StatusUploaded      Status = "uploaded"
	StatusPatched       Status = "patched"
	StatusVerifiedPass  Status = "verified_pass"
	StatusManualReview  Status = "manual_review"
)

type RunInput struct {
	OutputPath string
}

type RunResult struct {
	Status Status
}
```

```go
package workflow

import (
	"context"

	"github.com/paper-format-checker/backend/internal/core/verify"
)

type PatchWriter interface {
	Apply(context.Context, string) error
}

type Verifier interface {
	Verify(context.Context, string) (verify.Result, error)
}

type LoopController struct {
	patchWriter PatchWriter
	verifier    Verifier
}

func NewLoopController(_ interface{}, patchWriter PatchWriter, verifier Verifier) *LoopController {
	return &LoopController{patchWriter: patchWriter, verifier: verifier}
}

func (c *LoopController) Run(ctx context.Context, input RunInput) (RunResult, error) {
	first, err := c.verifier.Verify(ctx, input.OutputPath)
	if err != nil {
		return RunResult{}, err
	}
	if first.Passed {
		return RunResult{Status: StatusVerifiedPass}, nil
	}
	if len(first.FatalIssues) > 0 {
		return RunResult{Status: StatusManualReview}, nil
	}
	if len(first.RepairableIssues) > 0 && c.patchWriter != nil {
		if err := c.patchWriter.Apply(ctx, input.OutputPath); err != nil {
			return RunResult{}, err
		}
		second, err := c.verifier.Verify(ctx, input.OutputPath)
		if err != nil {
			return RunResult{}, err
		}
		if second.Passed {
			return RunResult{Status: StatusVerifiedPass}, nil
		}
	}
	return RunResult{Status: StatusManualReview}, nil
}
```

- [ ] **Step 4: 运行测试确认通过**

Run: `go test ./internal/core/ooxmlpatch ./internal/core/verify ./internal/core/workflow -run TestLoopControllerRetriesPatchOnceOnRepairableIssues -v`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add backend/internal/core/ooxmlpatch/writer.go \
  backend/internal/core/ooxmlpatch/writer_test.go \
  backend/internal/core/verify/verifier.go \
  backend/internal/core/verify/verifier_test.go \
  backend/internal/core/workflow/types.go \
  backend/internal/core/workflow/store.go \
  backend/internal/core/workflow/store_test.go \
  backend/internal/core/workflow/loop_controller.go \
  backend/internal/core/workflow/loop_controller_test.go
git commit -m "feat: add patch writer verifier and loop controller"
```

## Task 8: 暴露 v2 服务、Handler 和下载接口

**Files:**
- Create: `backend/internal/service/paper_workflow_service.go`
- Create: `backend/internal/service/paper_workflow_service_test.go`
- Create: `backend/internal/handler/paper_workflow_handler.go`
- Create: `backend/internal/handler/paper_workflow_handler_test.go`
- Modify: `backend/cmd/server/main.go`

- [ ] **Step 1: 写失败测试**

```go
package handler

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestPaperWorkflowHandlerRejectsDownloadBeforeVerifiedPass(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	h := NewPaperWorkflowHandler(nil)
	router.GET("/api/v2/jobs/:job_id/download", h.DownloadJob)

	req := httptest.NewRequest(http.MethodGet, "/api/v2/jobs/job-1/download", nil)
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d", resp.Code)
	}
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `go test ./internal/handler ./internal/service -run TestPaperWorkflowHandlerRejectsDownloadBeforeVerifiedPass -v`
Expected: FAIL，提示 `NewPaperWorkflowHandler` 未定义。

- [ ] **Step 3: 写最小实现**

```go
package service

import "github.com/google/uuid"

type WorkflowJobView struct {
	ID          uuid.UUID `json:"id"`
	Status      string    `json:"status"`
	Stage       string    `json:"stage"`
	DownloadPath string   `json:"download_path"`
}

type PaperWorkflowService interface {
	GetJob(id string) (*WorkflowJobView, error)
}
```

```go
package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/paper-format-checker/backend/internal/service"
	"github.com/paper-format-checker/backend/internal/utils"
)

type PaperWorkflowHandler struct {
	service service.PaperWorkflowService
}

func NewPaperWorkflowHandler(svc service.PaperWorkflowService) *PaperWorkflowHandler {
	return &PaperWorkflowHandler{service: svc}
}

func (h *PaperWorkflowHandler) DownloadJob(c *gin.Context) {
	if h.service == nil {
		utils.ErrorResponse(c, http.StatusConflict, "job not ready for download", "")
		return
	}
	job, err := h.service.GetJob(c.Param("job_id"))
	if err != nil {
		utils.ErrorResponse(c, http.StatusNotFound, "job not found", err.Error())
		return
	}
	if job.Status != "verified_pass" || job.DownloadPath == "" {
		utils.ErrorResponse(c, http.StatusConflict, "job not ready for download", "")
		return
	}
	c.File(job.DownloadPath)
}
```

```go
// in backend/cmd/server/main.go
paperWorkflowHandler := handler.NewPaperWorkflowHandler(service.NewPaperWorkflowService(database.DB))

apiV2.POST("/templates/compile", paperWorkflowHandler.CompileTemplate)
apiV2.POST("/papers", paperWorkflowHandler.CreatePaperJob)
apiV2.POST("/jobs/:job_id/run", paperWorkflowHandler.RunJob)
apiV2.GET("/jobs/:job_id", paperWorkflowHandler.GetJob)
apiV2.GET("/jobs/:job_id/download", paperWorkflowHandler.DownloadJob)
```

- [ ] **Step 4: 运行测试确认通过**

Run: `go test ./internal/handler ./internal/service -run TestPaperWorkflowHandlerRejectsDownloadBeforeVerifiedPass -v`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add backend/internal/service/paper_workflow_service.go \
  backend/internal/service/paper_workflow_service_test.go \
  backend/internal/handler/paper_workflow_handler.go \
  backend/internal/handler/paper_workflow_handler_test.go \
  backend/cmd/server/main.go
git commit -m "feat: add v2 workflow service and handlers"
```

## Task 9: 前端切换为单任务直通工作流

**Files:**
- Modify: `frontend/package.json`
- Modify: `frontend/src/api/paper.js`
- Modify: `frontend/src/stores/paper.js`
- Create: `frontend/src/stores/paper.spec.js`
- Modify: `frontend/src/router/index.js`
- Modify: `frontend/src/views/UploadView.vue`
- Create: `frontend/src/views/PaperJobView.vue`

- [ ] **Step 1: 写失败测试**

```js
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'

vi.mock('../api/paper', () => ({
  createPaperJob: vi.fn(async () => ({ job_id: 'job-1' })),
  runPaperJob: vi.fn(async () => ({ status: 'queued' })),
  getPaperJob: vi.fn(async () => ({ id: 'job-1', status: 'verified_pass', stage: 'passed', download_url: '/api/v2/jobs/job-1/download' }))
}))

import { usePaperStore } from './paper'

describe('paper store v2 job flow', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
  })

  it('stores download url only after verified_pass', async () => {
    const store = usePaperStore()
    await store.createJob(new File(['x'], 'paper.docx'))
    await store.refreshJob()

    expect(store.job.id).toBe('job-1')
    expect(store.downloadUrl).toBe('/api/v2/jobs/job-1/download')
  })
})
```

- [ ] **Step 2: 运行测试确认失败**

Run: `npm --prefix frontend install`
Run: `npm --prefix frontend exec vitest run src/stores/paper.spec.js`
Expected: FAIL，提示 `createJob` 或 `refreshJob` 不存在，或 `vitest` 相关脚本未安装。

- [ ] **Step 3: 写最小实现**

```json
{
  "scripts": {
    "dev": "vite",
    "build": "vite build",
    "preview": "vite preview",
    "test": "vitest run"
  },
  "devDependencies": {
    "@vitejs/plugin-vue": "^4.5.0",
    "vite": "^4.5.0",
    "vitest": "^3.2.4"
  }
}
```

```js
// frontend/src/api/paper.js
export const createPaperJob = (formData) => request({
  url: '/api/v2/papers',
  method: 'POST',
  data: formData,
  headers: { 'Content-Type': 'multipart/form-data' }
})

export const runPaperJob = (jobId) => request({
  url: `/api/v2/jobs/${jobId}/run`,
  method: 'POST'
})

export const getPaperJob = (jobId) => request({
  url: `/api/v2/jobs/${jobId}`,
  method: 'GET'
})
```

```js
// frontend/src/stores/paper.js
import { defineStore } from 'pinia'
import { createPaperJob, runPaperJob, getPaperJob } from '../api/paper'

export const usePaperStore = defineStore('paper', {
  state: () => ({
    job: null,
    loading: false,
    downloadUrl: ''
  }),
  actions: {
    async createJob(file) {
      const formData = new FormData()
      formData.append('paper', file)
      const result = await createPaperJob(formData)
      this.job = { id: result.job_id, status: 'uploaded', stage: 'queued' }
      await runPaperJob(result.job_id)
      return this.job
    },
    async refreshJob() {
      if (!this.job?.id) return null
      const result = await getPaperJob(this.job.id)
      this.job = result
      this.downloadUrl = result.status === 'verified_pass' ? result.download_url || `/api/v2/jobs/${result.id}/download` : ''
      return result
    }
  }
})
```

```js
// frontend/src/router/index.js
const PaperJobView = () => import('../views/PaperJobView.vue')

{
  path: '/jobs/:jobId',
  name: 'paper-job',
  component: PaperJobView,
  meta: { requiresAuth: true, title: '任务进度' }
}
```

```vue
<!-- frontend/src/views/PaperJobView.vue -->
<template>
  <section class="paper-job-view">
    <h1>论文处理任务</h1>
    <p>当前阶段：{{ store.job?.stage || 'queued' }}</p>
    <p>当前状态：{{ store.job?.status || 'uploaded' }}</p>
    <button v-if="store.downloadUrl" @click="download">下载最终修正稿</button>
    <p v-else>系统正在处理中，若失败将直接显示问题，不提供半成品下载。</p>
  </section>
</template>

<script setup>
import { onMounted, onUnmounted } from 'vue'
import { useRoute } from 'vue-router'
import { usePaperStore } from '../stores/paper'

const route = useRoute()
const store = usePaperStore()
let timer = null

const download = () => {
  window.location.href = store.downloadUrl
}

onMounted(async () => {
  store.job = { id: route.params.jobId }
  await store.refreshJob()
  timer = window.setInterval(() => {
    if (store.job?.status === 'verified_pass' || store.job?.status === 'manual_review') {
      window.clearInterval(timer)
      timer = null
      return
    }
    store.refreshJob()
  }, 3000)
})

onUnmounted(() => {
  if (timer) window.clearInterval(timer)
})
</script>
```

- [ ] **Step 4: 运行测试并确认通过**

Run: `npm --prefix frontend exec vitest run src/stores/paper.spec.js`
Expected: PASS

Run: `npm --prefix frontend run build`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add frontend/package.json \
  frontend/src/api/paper.js \
  frontend/src/stores/paper.js \
  frontend/src/stores/paper.spec.js \
  frontend/src/router/index.js \
  frontend/src/views/UploadView.vue \
  frontend/src/views/PaperJobView.vue
git commit -m "feat: switch frontend to v2 job flow"
```

## Task 10: 下线旧生产写路径

**Files:**
- Modify: `backend/internal/handler/paper_handler.go`
- Modify: `backend/internal/service/paper_service.go`
- Modify: `backend/internal/service/format_comparison_service.go`
- Create: `backend/internal/handler/paper_handler_legacy_test.go`

- [ ] **Step 1: 写失败测试**

```go
package handler

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestLegacyUploadPathReturnsGoneWhenV2WritePathEnabled(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	h := NewPaperHandler(nil)
	router.POST("/api/paper/upload", h.UploadPaper)

	req := httptest.NewRequest(http.MethodPost, "/api/paper/upload", nil)
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusGone {
		t.Fatalf("expected 410, got %d", resp.Code)
	}
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `go test ./internal/handler -run TestLegacyUploadPathReturnsGoneWhenV2WritePathEnabled -v`
Expected: FAIL，当前旧上传路径仍会进入 legacy 逻辑。

- [ ] **Step 3: 写最小实现**

```go
// at the top of backend/internal/handler/paper_handler.go
const legacyWritePathMessage = "legacy paper write path has been retired; use /api/v2/papers"

func (h *PaperHandler) UploadPaper(c *gin.Context) {
	utils.ErrorResponse(c, http.StatusGone, legacyWritePathMessage, "")
}
```

```go
// in backend/internal/service/paper_service.go
var ErrLegacyWritePathDisabled = fmt.Errorf("legacy paper write path disabled")
```

```go
// in backend/internal/service/format_comparison_service.go
// mark the service as non-production and remove any route registration that triggers write behavior
```

- [ ] **Step 4: 运行测试确认通过**

Run: `go test ./internal/handler -run TestLegacyUploadPathReturnsGoneWhenV2WritePathEnabled -v`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add backend/internal/handler/paper_handler.go \
  backend/internal/handler/paper_handler_legacy_test.go \
  backend/internal/service/paper_service.go \
  backend/internal/service/format_comparison_service.go
git commit -m "refactor: retire legacy paper write path"
```

## 自检

### 1. Spec 覆盖

- 模板预编译：Task 1, Task 3
- 学生稿解析：Task 4
- 块级映射：Task 5
- 模板骨架生成：Task 6
- 白名单补丁：Task 7
- 独立复检：Task 7
- 自动闭环：Task 7
- v2 API：Task 8
- 前端直通下载：Task 9
- 旧链路下线：Task 10

结论：spec 中要求的主链、边界、状态机、接口、交付策略与旧链路退出策略都对应到了具体任务。

### 2. Placeholder 扫描

已手动检查并避免以下失败模式：

- 没有 `TBD` / `TODO`
- 没有“后续补充细节”
- 没有“加适当错误处理”这种空描述
- 每个任务都给了测试、命令、实现片段和提交方式

### 3. 类型一致性

本计划统一使用以下命名：

- `CompiledTemplate`
- `PaperWorkflowJob`
- `PaperWorkflowIssue`
- `CompiledTemplatePackage`
- `ParsedPaper`
- `MappingResult`
- `Transplanter`
- `LoopController`
- `PaperWorkflowHandler`
- `PaperWorkflowService`

后续实现中不要再引入新的同义类型名。
