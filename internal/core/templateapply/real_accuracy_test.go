package templateapply

import (
	"archive/zip"
	"context"
	"fmt"
	"io"
	"os"
	"regexp"
	"testing"

	"github.com/paper-format-checker/backend/internal/core/repaircontract"
	"github.com/paper-format-checker/backend/internal/core/templateprofile"
)

func TestRealProfileAccuracyDoesNotRegress(t *testing.T) {
	templatePath := os.Getenv("PAPER_REAL_TEMPLATE_DOCX")
	baselinePath := os.Getenv("PAPER_REAL_BASELINE_DOCX")
	candidatePath := os.Getenv("PAPER_REAL_CANDIDATE_DOCX")
	if templatePath == "" || baselinePath == "" || candidatePath == "" {
		t.Skip("set PAPER_REAL_TEMPLATE_DOCX, PAPER_REAL_BASELINE_DOCX and PAPER_REAL_CANDIDATE_DOCX")
	}

	profile, err := templateprofile.Build(context.Background(), templatePath, templateprofile.Options{})
	if err != nil {
		t.Fatalf("build template profile: %v", err)
	}
	baseline, err := CheckDOCXWithTemplateProfile(context.Background(), baselinePath, profile)
	if err != nil {
		t.Fatalf("check baseline: %v", err)
	}
	candidate, err := CheckDOCXWithTemplateProfile(context.Background(), candidatePath, profile)
	if err != nil {
		t.Fatalf("check candidate: %v", err)
	}

	t.Logf("template-rule mismatches: baseline=%d candidate=%d; issue groups=%d/%d", baseline.FixCount, candidate.FixCount, len(baseline.Issues), len(candidate.Issues))
	if !candidate.Passed || candidate.FixCount >= baseline.FixCount {
		t.Fatalf("candidate did not improve template-rule accuracy: baseline=%#v candidate=%#v", baseline.Issues, candidate.Issues)
	}

	sourcePath := os.Getenv("PAPER_REAL_SOURCE_DOCX")
	if sourcePath == "" {
		sourcePath = baselinePath
	}
	sourceStructure, err := repaircontract.CaptureStructure(sourcePath)
	if err != nil {
		t.Fatalf("capture source structure: %v", err)
	}
	candidateStructure, err := repaircontract.CaptureStructure(candidatePath)
	if err != nil {
		t.Fatalf("capture candidate structure: %v", err)
	}
	if issues := repaircontract.ValidateStructurePreserved(sourceStructure, candidateStructure); len(issues) != 0 {
		t.Fatalf("candidate lost protected OOXML structure: %#v", issues)
	}
	if err := assertAllSectionMargins(candidatePath, profile.PageSetup); err != nil {
		t.Fatalf("candidate page setup is not applied to every section: %v", err)
	}
	t.Logf("protected structure source/candidate: sections=%d/%d bookmarks=%d/%d fields=%d/%d drawings=%d/%d comments=%d/%d vertAlign=%d/%d relationships=%d/%d",
		sourceStructure.Sections, candidateStructure.Sections,
		mapCount(sourceStructure.Bookmarks), mapCount(candidateStructure.Bookmarks),
		mapCount(sourceStructure.Fields), mapCount(candidateStructure.Fields),
		sourceStructure.Drawings, candidateStructure.Drawings,
		sourceStructure.CommentReferences, candidateStructure.CommentReferences,
		mapCount(sourceStructure.VerticalAlignments), mapCount(candidateStructure.VerticalAlignments),
		mapCount(sourceStructure.RelationshipTargets), mapCount(candidateStructure.RelationshipTargets))
}

func assertAllSectionMargins(path string, setup templateprofile.PageSetupRule) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	stat, err := file.Stat()
	if err != nil {
		return err
	}
	archive, err := zip.NewReader(file, stat.Size())
	if err != nil {
		return err
	}
	var document []byte
	for _, entry := range archive.File {
		if entry.Name != "word/document.xml" {
			continue
		}
		reader, readErr := entry.Open()
		if readErr != nil {
			return readErr
		}
		document, err = io.ReadAll(reader)
		reader.Close()
		if err != nil {
			return err
		}
		break
	}
	if len(document) == 0 {
		return os.ErrNotExist
	}
	margins := regexp.MustCompile(`<w:pgMar\b[^>]*/>`).FindAllString(string(document), -1)
	if len(margins) == 0 {
		return os.ErrNotExist
	}
	expected := map[string]string{
		"top": setup.MarginTopTwips, "right": setup.MarginRightTwips,
		"bottom": setup.MarginBottomTwips, "left": setup.MarginLeftTwips,
	}
	for i, margin := range margins {
		for name, value := range expected {
			if value == "" {
				continue
			}
			if !regexp.MustCompile(`\bw:` + name + `="` + regexp.QuoteMeta(value) + `"`).MatchString(margin) {
				return fmt.Errorf("section %d %s margin missing expected %s in %s", i, name, value, margin)
			}
		}
	}
	return nil
}

func mapCount(values map[string]int) int {
	total := 0
	for _, count := range values {
		total += count
	}
	return total
}
