package templatecompile

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCompilerBuildsCompiledTemplatePackage(t *testing.T) {
	templatePath := writeSimpleTemplateDocx(t)
	outputDir := t.TempDir()
	compiler := NewCompiler()

	result, err := compiler.Compile(context.Background(), templatePath, CompileOptions{
		SchoolID:     "cq-test",
		TemplateName: "official-template",
		Version:      "v1",
		OutputDir:    outputDir,
	})
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}

	if result.Manifest.SchoolID != "cq-test" {
		t.Fatalf("unexpected school id: %s", result.Manifest.SchoolID)
	}
	if result.Manifest.TemplateName != "official-template" {
		t.Fatalf("unexpected template name: %s", result.Manifest.TemplateName)
	}
	if result.Manifest.Version != "v1" {
		t.Fatalf("unexpected version: %s", result.Manifest.Version)
	}
	if result.Manifest.DocxHash == "" {
		t.Fatal("expected docx hash")
	}
	if result.Manifest.CompilerVersion == "" {
		t.Fatal("expected compiler version")
	}
	if result.Manifest.CompiledAt.IsZero() {
		t.Fatal("expected compiled at")
	}

	if result.SkeletonPath == "" {
		t.Fatal("expected skeleton path")
	}
	if filepath.Dir(filepath.Dir(result.SkeletonPath)) != outputDir {
		t.Fatalf("skeleton package should be inside output dir, got %s", result.SkeletonPath)
	}
	assertFileBytesEqual(t, templatePath, result.SkeletonPath)
	if result.SkeletonSource != templatePath {
		t.Fatalf("unexpected skeleton source: %s", result.SkeletonSource)
	}

	if len(result.BlockCatalog) == 0 {
		t.Fatal("expected non-empty block catalog")
	}
	requiredBlocks := []string{
		"cover_title",
		"abstract_cn_body",
		"heading_1",
	}
	for _, kind := range requiredBlocks {
		block, err := result.MustBlock(kind)
		if err != nil {
			t.Fatalf("MustBlock(%s) error = %v", kind, err)
		}
		if block.Kind != kind {
			t.Fatalf("MustBlock(%s) returned kind %q", kind, block.Kind)
		}
		assertBlockContract(t, block)
	}
	if _, err := result.MustBlock("missing_block"); err == nil {
		t.Fatal("expected MustBlock to fail for unknown block")
	}
	assertBlocksOrdered(t, result.BlockCatalog)

	requiredPatchTargets := []string{
		"word/document.xml",
		"word/_rels/document.xml.rels",
		"word/settings.xml",
	}
	for _, target := range requiredPatchTargets {
		if !contains(result.PatchTargets, target) {
			t.Fatalf("patch targets missing %s: %#v", target, result.PatchTargets)
		}
	}

	if len(result.StyleProfiles) == 0 {
		t.Fatal("expected style profiles")
	}
	assertStyleProfileContract(t, result.StyleProfiles[0])
	if result.MappingContract.ContractID == "" {
		t.Fatal("expected mapping contract id")
	}
	if len(result.MappingContract.BlockBindings) == 0 {
		t.Fatal("expected mapping contract bindings")
	}
	if result.MappingContract.PatchTarget == "" {
		t.Fatal("expected mapping contract patch target")
	}
	if len(result.VerificationRules) == 0 {
		t.Fatal("expected verification rules")
	}
	assertVerificationRuleContract(t, result.VerificationRules[0])
}

func TestCompilerCreatesUniquePackageDirectories(t *testing.T) {
	templatePath := writeSimpleTemplateDocx(t)
	outputDir := t.TempDir()
	compiler := NewCompiler()

	first, err := compiler.Compile(context.Background(), templatePath, CompileOptions{
		SchoolID:     "cq-test",
		TemplateName: "official-template",
		Version:      "v1",
		OutputDir:    outputDir,
	})
	if err != nil {
		t.Fatalf("first Compile() error = %v", err)
	}

	marker := []byte("first skeleton must remain untouched")
	if err := os.WriteFile(first.SkeletonPath, marker, 0o644); err != nil {
		t.Fatalf("write first skeleton marker: %v", err)
	}

	second, err := compiler.Compile(context.Background(), templatePath, CompileOptions{
		SchoolID:     "cq-test",
		TemplateName: "official-template",
		Version:      "v1",
		OutputDir:    outputDir,
	})
	if err != nil {
		t.Fatalf("second Compile() error = %v", err)
	}

	if first.SkeletonPath == second.SkeletonPath {
		t.Fatalf("expected unique skeleton paths, both were %s", first.SkeletonPath)
	}
	got, err := os.ReadFile(first.SkeletonPath)
	if err != nil {
		t.Fatalf("read first skeleton: %v", err)
	}
	if !bytes.Equal(got, marker) {
		t.Fatal("second compile overwrote first skeleton")
	}
	assertFileBytesEqual(t, templatePath, second.SkeletonPath)
}

func TestCompilerCleansPackageDirWhenFinalContextCheckFails(t *testing.T) {
	templatePath := writeSimpleTemplateDocx(t)
	outputDir := t.TempDir()
	compiler := NewCompiler()

	ctx := &errAfterCallsContext{
		failOnCall: 2,
		err:        context.Canceled,
	}
	result, err := compiler.Compile(ctx, templatePath, CompileOptions{
		SchoolID:     "cq-test",
		TemplateName: "official-template",
		Version:      "v1",
		OutputDir:    outputDir,
	})
	if err == nil {
		t.Fatal("expected Compile to return final context error")
	}
	if result != nil {
		t.Fatalf("expected nil result on error, got %#v", result)
	}
	assertDirectoryEmpty(t, outputDir)
}

func TestCompilerPackageJSONUsesSnakeCaseContractFields(t *testing.T) {
	templatePath := writeSimpleTemplateDocx(t)
	outputDir := t.TempDir()
	compiler := NewCompiler()

	result, err := compiler.Compile(context.Background(), templatePath, CompileOptions{
		SchoolID:     "cq-test",
		TemplateName: "official-template",
		Version:      "v1",
		OutputDir:    outputDir,
	})
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}

	payload, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("marshal compiled package: %v", err)
	}
	body := string(payload)
	requiredKeys := []string{
		`"school_id"`,
		`"block_catalog"`,
		`"block_id"`,
		`"style_profile_id"`,
		`"mapping_contract"`,
		`"verification_rules"`,
	}
	for _, key := range requiredKeys {
		if !strings.Contains(body, key) {
			t.Fatalf("marshaled package missing %s: %s", key, body)
		}
	}
	defaultKeys := []string{
		`"SchoolID"`,
		`"BlockCatalog"`,
		`"BlockID"`,
		`"StyleProfileID"`,
		`"MappingContract"`,
		`"VerificationRules"`,
	}
	for _, key := range defaultKeys {
		if strings.Contains(body, key) {
			t.Fatalf("marshaled package used default Go field key %s: %s", key, body)
		}
	}
}

func writeSimpleTemplateDocx(t *testing.T) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "template.docx")
	file, err := os.Create(path)
	if err != nil {
		t.Fatalf("create docx: %v", err)
	}
	defer file.Close()

	writer := zip.NewWriter(file)
	defer writer.Close()

	entries := map[string]string{
		"[Content_Types].xml":          `<?xml version="1.0" encoding="UTF-8"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="xml" ContentType="application/xml"/></Types>`,
		"_rels/.rels":                  `<?xml version="1.0" encoding="UTF-8"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"></Relationships>`,
		"word/document.xml":            `<w:document><w:body><w:p><w:r><w:t>{{cover_title}}</w:t></w:r></w:p></w:body></w:document>`,
		"word/_rels/document.xml.rels": `<?xml version="1.0" encoding="UTF-8"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"></Relationships>`,
		"word/settings.xml":            `<w:settings></w:settings>`,
	}

	for name, content := range entries {
		entry, err := writer.Create(name)
		if err != nil {
			t.Fatalf("create zip entry %s: %v", name, err)
		}
		if _, err := entry.Write([]byte(content)); err != nil {
			t.Fatalf("write zip entry %s: %v", name, err)
		}
	}

	return path
}

func assertDirectoryEmpty(t *testing.T, path string) {
	t.Helper()

	entries, err := os.ReadDir(path)
	if err != nil {
		t.Fatalf("read output dir: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("expected empty output dir, got %d entries", len(entries))
	}
}

type errAfterCallsContext struct {
	context.Context
	calls      int
	failOnCall int
	err        error
}

func (c *errAfterCallsContext) Deadline() (time.Time, bool) {
	return time.Time{}, false
}

func (c *errAfterCallsContext) Done() <-chan struct{} {
	return nil
}

func (c *errAfterCallsContext) Err() error {
	c.calls++
	if c.calls >= c.failOnCall {
		return c.err
	}
	return nil
}

func (c *errAfterCallsContext) Value(key any) any {
	return nil
}

func assertFileBytesEqual(t *testing.T, wantPath, gotPath string) {
	t.Helper()

	want, err := os.ReadFile(wantPath)
	if err != nil {
		t.Fatalf("read want file: %v", err)
	}
	got, err := os.ReadFile(gotPath)
	if err != nil {
		t.Fatalf("read got file: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatal("copied skeleton differs from source")
	}
}

func assertBlocksOrdered(t *testing.T, blocks []TemplateBlock) {
	t.Helper()

	if len(blocks) == 0 {
		t.Fatal("expected blocks")
	}
	for i, block := range blocks {
		if block.OrderIndex != i {
			t.Fatalf("block %s has order index %d, want %d", block.BlockID, block.OrderIndex, i)
		}
	}
}

func assertBlockContract(t *testing.T, block TemplateBlock) {
	t.Helper()

	if block.BlockID == "" {
		t.Fatalf("block %s missing block id", block.Kind)
	}
	if block.Kind == "" {
		t.Fatalf("block %s missing kind", block.BlockID)
	}
	if block.SlotType == "" {
		t.Fatalf("block %s missing slot type", block.BlockID)
	}
	if block.StyleProfileID == "" {
		t.Fatalf("block %s missing style profile id", block.BlockID)
	}
	if block.Anchor.Path == "" || block.Anchor.Match == "" {
		t.Fatalf("block %s missing anchor contract: %#v", block.BlockID, block.Anchor)
	}
	if block.SourceRegion.Path == "" || block.SourceRegion.Start == "" || block.SourceRegion.End == "" {
		t.Fatalf("block %s missing source region: %#v", block.BlockID, block.SourceRegion)
	}
	if block.Capacity.Min < 0 || block.Capacity.Max <= 0 {
		t.Fatalf("block %s missing capacity: %#v", block.BlockID, block.Capacity)
	}
	if len(block.Accepts) == 0 {
		t.Fatalf("block %s missing accepted content types", block.BlockID)
	}
	if block.PatchPolicy.Target == "" || block.PatchPolicy.Mode == "" {
		t.Fatalf("block %s missing patch policy: %#v", block.BlockID, block.PatchPolicy)
	}
	if block.VerifyPolicy.RuleID == "" || block.VerifyPolicy.Severity == "" {
		t.Fatalf("block %s missing verify policy: %#v", block.BlockID, block.VerifyPolicy)
	}
}

func assertStyleProfileContract(t *testing.T, profile StyleProfile) {
	t.Helper()

	if profile.StyleProfileID == "" || profile.Name == "" || profile.BasedOn == "" {
		t.Fatalf("style profile is too shallow: %#v", profile)
	}
	if len(profile.Properties) == 0 {
		t.Fatalf("style profile missing properties: %#v", profile)
	}
}

func assertVerificationRuleContract(t *testing.T, rule VerificationRule) {
	t.Helper()

	if rule.RuleID == "" || rule.Target == "" || rule.Assertion == "" || rule.Severity == "" {
		t.Fatalf("verification rule is too shallow: %#v", rule)
	}
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
