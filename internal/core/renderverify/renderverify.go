package renderverify

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"

	"rsc.io/pdf"
)

type Severity string

const (
	SeverityFatal   Severity = "fatal"
	SeverityError   Severity = "error"
	SeverityWarning Severity = "warning"
)

type Issue struct {
	Kind     string   `json:"kind"`
	Severity Severity `json:"severity"`
	Message  string   `json:"message"`
	Target   string   `json:"target"`
}

type PDFArtifact struct {
	Path string `json:"path"`
}

type Renderer interface {
	RenderPDF(ctx context.Context, docxPath string, outputDir string) (PDFArtifact, error)
}

type Rasterizer interface {
	RasterizePDF(ctx context.Context, pdfPath string, outputDir string) ([]string, error)
}

type TextExtractor interface {
	ExtractPageTexts(pdfPath string) ([]string, error)
}

type SamePageRule struct {
	Name      string `json:"name"`
	LeftText  string `json:"left_text"`
	RightText string `json:"right_text"`
}

type Options struct {
	Enabled         bool
	Strict          bool
	OutputDir       string
	Renderer        Renderer
	Rasterizer      Rasterizer
	PNGOutputDir    string
	TextExtractor   TextExtractor
	RequiredText    []string
	ForbiddenText   []string
	SamePageRules   []SamePageRule
	AllowBlankPage  map[int]bool
	CheckPageFooter bool
}

type Result struct {
	Enabled   bool     `json:"enabled"`
	Passed    bool     `json:"passed"`
	PDFPath   string   `json:"pdf_path,omitempty"`
	PNGPaths  []string `json:"png_paths,omitempty"`
	PageCount int      `json:"page_count"`
	Issues    []Issue  `json:"issues,omitempty"`
	PageTexts []string `json:"-"`
}

func DefaultEnabled() bool {
	return strings.EqualFold(strings.TrimSpace(os.Getenv("RENDER_VERIFY_ENABLED")), "true")
}

func Check(ctx context.Context, docxPath string, options Options) (Result, error) {
	result := Result{Enabled: options.Enabled}
	if !options.Enabled {
		result.Passed = true
		return result, nil
	}
	if ctx == nil {
		return Result{}, fmt.Errorf("context is nil")
	}
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	docxPath = strings.TrimSpace(docxPath)
	if docxPath == "" {
		return Result{}, fmt.Errorf("docx path is empty")
	}

	renderer := options.Renderer
	if renderer == nil {
		renderer = LibreOfficeRenderer{}
	}
	extractor := options.TextExtractor
	if extractor == nil {
		extractor = RscPDFTextExtractor{}
	}
	outputDir := strings.TrimSpace(options.OutputDir)
	if outputDir == "" {
		tempDir, err := os.MkdirTemp("", "paper-render-*")
		if err != nil {
			return Result{}, fmt.Errorf("create render temp dir: %w", err)
		}
		outputDir = tempDir
	}

	artifact, err := renderer.RenderPDF(ctx, docxPath, outputDir)
	if err != nil {
		result.Issues = append(result.Issues, Issue{
			Kind:     "render_pdf",
			Severity: severityForStrict(options.Strict),
			Message:  fmt.Sprintf("render DOCX to PDF failed: %v", err),
			Target:   docxPath,
		})
		result.Passed = !hasBlockingIssues(result.Issues)
		return result, nil
	}
	result.PDFPath = artifact.Path

	if strings.TrimSpace(options.PNGOutputDir) != "" {
		rasterizer := options.Rasterizer
		if rasterizer == nil {
			rasterizer = PopplerRasterizer{}
		}
		pngPaths, err := rasterizer.RasterizePDF(ctx, artifact.Path, options.PNGOutputDir)
		if err != nil {
			result.Issues = append(result.Issues, Issue{
				Kind:     "render_png",
				Severity: severityForStrict(options.Strict),
				Message:  fmt.Sprintf("render PDF pages to PNG failed: %v", err),
				Target:   artifact.Path,
			})
		} else {
			result.PNGPaths = pngPaths
		}
	}

	pageTexts, err := extractor.ExtractPageTexts(artifact.Path)
	if err != nil {
		result.Issues = append(result.Issues, Issue{
			Kind:     "extract_pdf_text",
			Severity: severityForStrict(options.Strict),
			Message:  fmt.Sprintf("extract rendered PDF text failed: %v", err),
			Target:   artifact.Path,
		})
		result.Passed = !hasBlockingIssues(result.Issues)
		return result, nil
	}
	result.PageTexts = pageTexts
	result.PageCount = len(pageTexts)
	validateRenderedText(&result, options)
	result.Passed = !hasBlockingIssues(result.Issues)
	return result, nil
}

type LibreOfficeRenderer struct {
	Binary string
}

func (r LibreOfficeRenderer) RenderPDF(ctx context.Context, docxPath string, outputDir string) (PDFArtifact, error) {
	soffice, err := r.resolveBinary()
	if err != nil {
		return PDFArtifact{}, err
	}
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return PDFArtifact{}, fmt.Errorf("create output dir: %w", err)
	}
	absDocx, err := filepath.Abs(docxPath)
	if err != nil {
		return PDFArtifact{}, fmt.Errorf("resolve docx path: %w", err)
	}
	base := strings.TrimSuffix(filepath.Base(absDocx), filepath.Ext(absDocx))
	expected := filepath.Join(outputDir, base+".pdf")
	_ = os.Remove(expected)
	profileDir, cleanupProfile, err := createLibreOfficeProfileDir(outputDir)
	if err != nil {
		return PDFArtifact{}, err
	}
	defer cleanupProfile()

	cmd := exec.CommandContext(ctx, soffice, "--headless", "--norestore", "--invisible", libreOfficeUserInstallationArg(profileDir), "--convert-to", "pdf", "--outdir", outputDir, absDocx)
	if runtime.GOOS != "windows" {
		cmd.Env = append(os.Environ(), "HOME=/tmp")
	} else {
		cmd.Env = os.Environ()
	}
	output, err := cmd.CombinedOutput()
	if err != nil {
		return PDFArtifact{}, fmt.Errorf("soffice convert: %w, output: %s", err, strings.TrimSpace(string(output)))
	}
	if _, err := os.Stat(expected); err != nil {
		return PDFArtifact{}, fmt.Errorf("soffice output missing at %s: %w, output: %s", expected, err, strings.TrimSpace(string(output)))
	}
	return PDFArtifact{Path: expected}, nil
}

type PopplerRasterizer struct {
	Binary string
	DPI    int
}

func (r PopplerRasterizer) RasterizePDF(ctx context.Context, pdfPath string, outputDir string) ([]string, error) {
	binary, err := r.resolveBinary()
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return nil, fmt.Errorf("create png output dir: %w", err)
	}
	dpi := r.DPI
	if dpi <= 0 {
		dpi = 120
	}
	prefix := filepath.Join(outputDir, "page")
	for _, old := range mustGlob(prefix + "-*.png") {
		_ = os.Remove(old)
	}
	cmd := exec.CommandContext(ctx, binary, "-png", "-r", strconv.Itoa(dpi), pdfPath, prefix)
	if runtime.GOOS == "windows" {
		cmd.Env = append(os.Environ(), "PATH="+filepath.Dir(binary)+";"+os.Getenv("PATH"))
	}
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("pdftoppm: %w, binary=%s pdf_exists=%t output_dir_exists=%t prefix=%s output: %s", err, binary, fileExists(pdfPath), dirExists(outputDir), prefix, strings.TrimSpace(string(output)))
	}
	paths := mustGlob(prefix + "-*.png")
	if len(paths) == 0 {
		return nil, fmt.Errorf("pdftoppm produced no PNG pages")
	}
	return paths, nil
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func (r PopplerRasterizer) resolveBinary() (string, error) {
	if strings.TrimSpace(r.Binary) != "" {
		if _, err := os.Stat(r.Binary); err == nil {
			return r.Binary, nil
		}
		return "", fmt.Errorf("configured pdftoppm binary missing: %s", r.Binary)
	}
	if custom := strings.TrimSpace(os.Getenv("PDFTOPPM_BIN")); custom != "" {
		if _, err := os.Stat(custom); err == nil {
			return custom, nil
		}
		return "", fmt.Errorf("PDFTOPPM_BIN set but file missing: %s", custom)
	}
	if runtime.GOOS == "windows" {
		if home, err := os.UserHomeDir(); err == nil {
			candidate := filepath.Join(home, ".cache", "codex-runtimes", "codex-primary-runtime", "dependencies", "native", "poppler", "Library", "bin", "pdftoppm.exe")
			if _, err := os.Stat(candidate); err == nil {
				return candidate, nil
			}
		}
	}
	for _, candidate := range []string{"pdftoppm.exe", "pdftoppm"} {
		if path, err := exec.LookPath(candidate); err == nil && !strings.HasSuffix(strings.ToLower(path), ".cmd") {
			return path, nil
		}
	}
	return "", fmt.Errorf("pdftoppm not found")
}

func mustGlob(pattern string) []string {
	paths, err := filepath.Glob(pattern)
	if err != nil {
		return nil
	}
	return paths
}

func createLibreOfficeProfileDir(outputDir string) (string, func(), error) {
	profileDir, err := os.MkdirTemp(outputDir, "lo-profile-")
	if err != nil {
		return "", nil, fmt.Errorf("create libreoffice profile dir: %w", err)
	}
	return profileDir, func() { _ = os.RemoveAll(profileDir) }, nil
}

func libreOfficeUserInstallationArg(profileDir string) string {
	path := filepath.ToSlash(profileDir)
	if runtime.GOOS == "windows" && !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return "-env:UserInstallation=" + (&url.URL{Scheme: "file", Path: path}).String()
}

func (r LibreOfficeRenderer) resolveBinary() (string, error) {
	if strings.TrimSpace(r.Binary) != "" {
		if _, err := os.Stat(r.Binary); err == nil {
			return r.Binary, nil
		}
		return "", fmt.Errorf("configured soffice binary missing: %s", r.Binary)
	}
	for _, envName := range []string{"SOFFICE_BIN", "SOFFICE_PATH"} {
		if custom := strings.TrimSpace(os.Getenv(envName)); custom != "" {
			if _, err := os.Stat(custom); err == nil {
				return custom, nil
			}
			return "", fmt.Errorf("%s set but file missing: %s", envName, custom)
		}
	}
	for _, candidate := range []string{"soffice", "soffice.exe"} {
		if path, err := exec.LookPath(candidate); err == nil {
			return path, nil
		}
	}
	for _, candidate := range []string{
		`C:\Program Files\LibreOffice\program\soffice.exe`,
		`C:\Program Files (x86)\LibreOffice\program\soffice.exe`,
	} {
		if _, err := os.Stat(candidate); err == nil {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("soffice not found")
}

type RscPDFTextExtractor struct{}

func (RscPDFTextExtractor) ExtractPageTexts(pdfPath string) ([]string, error) {
	reader, err := pdf.Open(pdfPath)
	if err != nil {
		return nil, err
	}
	pageTexts := make([]string, 0, reader.NumPage())
	for pageNumber := 1; pageNumber <= reader.NumPage(); pageNumber++ {
		content := reader.Page(pageNumber).Content()
		var builder strings.Builder
		for _, text := range content.Text {
			builder.WriteString(text.S)
		}
		pageTexts = append(pageTexts, normalizeText(builder.String()))
	}
	return pageTexts, nil
}

func validateRenderedText(result *Result, options Options) {
	allText := normalizeText(strings.Join(result.PageTexts, "\n"))
	for _, required := range options.RequiredText {
		required = normalizeText(required)
		if required == "" {
			continue
		}
		if !strings.Contains(allText, required) {
			result.Issues = append(result.Issues, Issue{
				Kind:     "missing_rendered_text",
				Severity: SeverityError,
				Message:  "rendered PDF is missing required text",
				Target:   required,
			})
		}
	}
	for _, forbidden := range options.ForbiddenText {
		forbidden = normalizeText(forbidden)
		if forbidden == "" {
			continue
		}
		if strings.Contains(allText, forbidden) {
			result.Issues = append(result.Issues, Issue{
				Kind:     "forbidden_rendered_text",
				Severity: SeverityError,
				Message:  "rendered PDF contains forbidden text",
				Target:   forbidden,
			})
		}
	}
	for index, text := range result.PageTexts {
		pageNumber := index + 1
		if options.AllowBlankPage != nil && options.AllowBlankPage[pageNumber] {
			continue
		}
		if strings.TrimSpace(text) == "" {
			result.Issues = append(result.Issues, Issue{
				Kind:     "blank_rendered_page",
				Severity: SeverityError,
				Message:  "rendered PDF contains an unexpected blank page",
				Target:   fmt.Sprintf("page:%d", pageNumber),
			})
		}
	}
	for _, rule := range options.SamePageRules {
		leftPage := findPage(result.PageTexts, rule.LeftText)
		rightPage := findPage(result.PageTexts, rule.RightText)
		sharedPage := findSharedPage(result.PageTexts, rule.LeftText, rule.RightText)
		if leftPage == 0 || rightPage == 0 {
			result.Issues = append(result.Issues, Issue{
				Kind:     "same_page_landmark_missing",
				Severity: SeverityError,
				Message:  fmt.Sprintf("same-page rule %q cannot find both landmarks", rule.Name),
				Target:   rule.Name,
			})
			continue
		}
		if sharedPage == 0 {
			result.Issues = append(result.Issues, Issue{
				Kind:     "same_page_rule_failed",
				Severity: SeverityError,
				Message:  fmt.Sprintf("same-page rule %q failed: landmarks are on page %d and %d", rule.Name, leftPage, rightPage),
				Target:   rule.Name,
			})
		}
	}
	if options.CheckPageFooter {
		validateChineseTotalFooter(result)
	}
}

type PythonPDFTextExtractor struct {
	Binary string
}

func (e PythonPDFTextExtractor) ExtractPageTexts(pdfPath string) ([]string, error) {
	binary := strings.TrimSpace(e.Binary)
	if binary == "" {
		binary = strings.TrimSpace(os.Getenv("PDF_TEXT_PYTHON"))
	}
	if binary == "" {
		return nil, fmt.Errorf("PDF_TEXT_PYTHON is not configured")
	}
	script := `import json, sys
import pdfplumber
with pdfplumber.open(sys.argv[1]) as pdf:
    sys.stdout.buffer.write(json.dumps([(page.extract_text() or "") for page in pdf.pages], ensure_ascii=False).encode("utf-8"))
`
	output, err := exec.Command(binary, "-c", script, pdfPath).CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("pdfplumber extract: %w, output: %s", err, strings.TrimSpace(string(output)))
	}
	return parsePythonPDFTextOutput(output)
}

func parsePythonPDFTextOutput(output []byte) ([]string, error) {
	var pages []string
	if err := json.Unmarshal(output, &pages); err != nil {
		return nil, err
	}
	return pages, nil
}

var chineseTotalFooterPattern = regexp.MustCompile(`第(\d+)页共(\d+)页`)

func validateChineseTotalFooter(result *Result) {
	if result == nil || result.PageCount == 0 {
		return
	}
	maxCurrent := 0
	total := 0
	targetPage := 0
	for index, text := range result.PageTexts {
		match := chineseTotalFooterPattern.FindStringSubmatch(normalizeText(text))
		if len(match) != 3 {
			continue
		}
		current, err := strconv.Atoi(match[1])
		if err != nil {
			continue
		}
		pageTotal, err := strconv.Atoi(match[2])
		if err != nil {
			continue
		}
		if current > maxCurrent {
			maxCurrent = current
			total = pageTotal
			targetPage = index + 1
		}
	}
	if maxCurrent == 0 || total == maxCurrent {
		return
	}
	result.Issues = append(result.Issues, Issue{
		Kind:     "page_footer_total_mismatch",
		Severity: SeverityError,
		Message:  fmt.Sprintf("rendered footer total page count is %d, but numbered body has %d pages", total, maxCurrent),
		Target:   fmt.Sprintf("page:%d", targetPage),
	})
}

func findPage(pageTexts []string, needle string) int {
	needle = normalizeText(needle)
	if needle == "" {
		return 0
	}
	for index, pageText := range pageTexts {
		if strings.Contains(normalizeText(pageText), needle) {
			return index + 1
		}
	}
	return 0
}

func findSharedPage(pageTexts []string, left string, right string) int {
	left = normalizeText(left)
	right = normalizeText(right)
	if left == "" || right == "" {
		return 0
	}
	for index, pageText := range pageTexts {
		text := normalizeText(pageText)
		if strings.Contains(text, left) && strings.Contains(text, right) {
			return index + 1
		}
	}
	return 0
}

func normalizeText(value string) string {
	value = strings.ReplaceAll(value, "\u00a0", " ")
	value = strings.ReplaceAll(value, "\u3000", " ")
	value = strings.Join(strings.Fields(value), "")
	return value
}

func severityForStrict(strict bool) Severity {
	if strict {
		return SeverityFatal
	}
	return SeverityWarning
}

func hasBlockingIssues(issues []Issue) bool {
	for _, issue := range issues {
		if issue.Severity == SeverityFatal || issue.Severity == SeverityError {
			return true
		}
	}
	return false
}
