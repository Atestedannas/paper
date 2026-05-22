//go:build ignore

package templatefiller

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"
)

// ClassificationParagraph represents a classified paragraph.
type ClassificationParagraph struct {
	Index             int    `json:"index"`
	Type              string `json:"type"`
	Text              string `json:"text"`
	Kind              string `json:"kind,omitempty"`
	SourceXML         string `json:"-"`
	HasComplexContent bool   `json:"-"`
}

// ClassificationResult is the full classification payload.
type ClassificationResult struct {
	Paragraphs []ClassificationParagraph `json:"paragraphs"`
}

// FillResult is returned on success.
type FillResult struct {
	Status     string `json:"status"`
	Output     string `json:"output"`
	Paragraphs int    `json:"paragraphs"`
	Tables     int    `json:"tables"`
}

// TemplateFiller orchestrates template-based document correction.
type TemplateFiller struct {
	PythonBin         string
	ScriptPath        string
	TemplateDir       string
	UsePythonFallback bool
	DeepSeekClient    DeepSeekClient
}

func NewTemplateFiller() *TemplateFiller {
	tf := &TemplateFiller{
		PythonBin:         "python3",
		UsePythonFallback: true,
	}

	if runtime.GOOS == "windows" {
		tf.PythonBin = "python"
	}

	tf.ScriptPath = locateScript("template_filler.py")
	tf.TemplateDir = locateDir("golden_templates", "uploads/golden_templates")

	return tf
}

// Fill uses the Go OOXML engine to fill the golden template with student content.
// It first extracts cover page fields from the student document, then merges
// them with the AI classification results before performing template filling.
func (tf *TemplateFiller) Fill(ctx context.Context, inputPath, templatePath string, classification ClassificationResult, outputDir string) (string, error) {
	start := time.Now()

	if _, err := os.Stat(templatePath); os.IsNotExist(err) {
		return "", fmt.Errorf("golden template not found: %s", templatePath)
	}
	if _, err := os.Stat(inputPath); os.IsNotExist(err) {
		return "", fmt.Errorf("input file not found: %s", inputPath)
	}

	// Step 1: Refine classification with DeepSeek (if available)
	if tf.DeepSeekClient != nil {
		refiner := NewDeepSeekRefiner(tf.DeepSeekClient)
		refined, refineErr := refiner.RefineClassification(classification)
		if refineErr != nil {
			log.Printf("[TemplateFiller] DeepSeek refinement failed (continuing with original): %v", refineErr)
		} else {
			classification = refined
		}
	}
	classification = normalizeClassificationForTemplateFill(classification)

	// Step 2: Extract cover fields from the student document
	coverFields, coverErr := ExtractCoverFields(inputPath)
	if coverErr != nil {
		log.Printf("[TemplateFiller] cover field extraction failed (continuing): %v", coverErr)
		coverFields = &CoverFields{}
	}
	mergeInferredCoverFields(coverFields, classification)

	// Step 3: Convert classification to section content
	sections := classificationToSections(classification)

	// Step 4: Merge cover fields into sections with cover-page-aware title splitting.
	sections = append(sections, buildCoverSections(coverFields)...)

	baseName := strings.TrimSuffix(filepath.Base(inputPath), filepath.Ext(inputPath))
	studentDocBytes, _ := os.ReadFile(inputPath)

	filler := NewOOXMLFiller()
	filler.Debug = false

	result, err := filler.FillTemplateWithMedia(ctx, templatePath, studentDocBytes, sections, outputDir, baseName)
	if err == nil {
		elapsed := time.Since(start)
		log.Printf("[TemplateFiller] Go OOXML engine completed in %v -> %s", elapsed, result)
		return result, nil
	}

	log.Printf("[TemplateFiller] Go OOXML engine failed: %v", err)

	if !tf.UsePythonFallback {
		return "", fmt.Errorf("Go OOXML engine failed and Python fallback disabled: %w", err)
	}

	log.Printf("[TemplateFiller] falling back to Python script")
	return tf.fillWithPython(ctx, inputPath, templatePath, classification, outputDir)
}

// classificationToSections converts the flat classification list into grouped
// SectionContent slices that the OOXML filler expects.
func classificationToSections(cls ClassificationResult) []SectionContent {
	groups := make(map[string]*SectionContent)
	order := make([]string, 0)

	for _, p := range cls.Paragraphs {
		sectionType := mapParaTypeToSection(p.Type)

		sc, ok := groups[sectionType]
		if !ok {
			sc = &SectionContent{SectionType: sectionType}
			groups[sectionType] = sc
			order = append(order, sectionType)
		}

		sc.Paragraphs = append(sc.Paragraphs, classificationParagraphToContentParagraph(p))
	}

	result := make([]SectionContent, 0, len(groups))
	for _, key := range order {
		result = append(result, *groups[key])
	}
	if toc := synthesizeTOCSection(cls, groups["table_of_contents"]); toc != nil {
		result = append(result, *toc)
	}
	return result
}

func normalizeClassificationForTemplateFill(cls ClassificationResult) ClassificationResult {
	result := ClassificationResult{
		Paragraphs: make([]ClassificationParagraph, 0, len(cls.Paragraphs)+4),
	}

	hasAbstractTitle := false
	hasEnAbstractTitle := false
	for _, p := range cls.Paragraphs {
		switch p.Type {
		case "abstract_title":
			hasAbstractTitle = true
		case "en_abstract_title":
			hasEnAbstractTitle = true
		}
	}

	for _, p := range cls.Paragraphs {
		switch p.Type {
		case "abstract":
			if !p.HasComplexContent && !hasAbstractTitle {
				title, rest, ok := splitInlineAbstractLabel(p.Text, true)
				if ok {
					result.Paragraphs = append(result.Paragraphs, ClassificationParagraph{
						Index: p.Index,
						Type:  "abstract_title",
						Text:  title,
					})
					hasAbstractTitle = true
					if strings.TrimSpace(rest) != "" {
						p.Text = rest
						result.Paragraphs = append(result.Paragraphs, p)
					}
					continue
				}
			}
		case "en_abstract":
			if !p.HasComplexContent && !hasEnAbstractTitle {
				title, rest, ok := splitInlineAbstractLabel(p.Text, false)
				if ok {
					result.Paragraphs = append(result.Paragraphs, ClassificationParagraph{
						Index: p.Index,
						Type:  "en_abstract_title",
						Text:  title,
					})
					hasEnAbstractTitle = true
					if strings.TrimSpace(rest) != "" {
						p.Text = rest
						result.Paragraphs = append(result.Paragraphs, p)
					}
					continue
				}
			}
		}
		result.Paragraphs = append(result.Paragraphs, p)
	}
	return result
}

func splitInlineAbstractLabel(text string, chinese bool) (title string, rest string, ok bool) {
	s := strings.TrimSpace(text)
	if s == "" {
		return "", "", false
	}
	if chinese {
		if strings.HasPrefix(s, "摘要：") || strings.HasPrefix(s, "摘要:") {
			return "摘要", strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(s, "摘要："), "摘要:")), true
		}
		if strings.HasPrefix(s, "摘 要：") || strings.HasPrefix(s, "摘 要:") {
			return "摘 要", strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(s, "摘 要："), "摘 要:")), true
		}
	} else {
		lower := strings.ToLower(s)
		if strings.HasPrefix(lower, "abstract:") || strings.HasPrefix(lower, "abstract :") {
			parts := strings.SplitN(s, ":", 2)
			if len(parts) == 2 {
				return "Abstract", strings.TrimSpace(parts[1]), true
			}
		}
		if strings.HasPrefix(lower, "abstract：") {
			parts := strings.SplitN(s, "：", 2)
			if len(parts) == 2 {
				return "Abstract", strings.TrimSpace(parts[1]), true
			}
		}
	}
	return "", "", false
}

func mergeInferredCoverFields(fields *CoverFields, cls ClassificationResult) {
	if fields == nil {
		return
	}
	inferred := inferCoverFieldsFromClassification(cls)
	fields.Title = preferMeaningfulCoverValue(fields.Title, inferred.Title)
	fields.Subtitle = preferMeaningfulCoverValue(fields.Subtitle, inferred.Subtitle)
	fields.College = preferMeaningfulCoverValue(fields.College, inferred.College)
	fields.Major = preferMeaningfulCoverValue(fields.Major, inferred.Major)
	fields.Grade = preferMeaningfulCoverValue(fields.Grade, inferred.Grade)
	fields.StudentID = preferMeaningfulCoverValue(fields.StudentID, inferred.StudentID)
	fields.Name = preferMeaningfulCoverValue(fields.Name, inferred.Name)
	fields.Advisor = preferMeaningfulCoverValue(fields.Advisor, inferred.Advisor)
	fields.Date = preferMeaningfulCoverValue(fields.Date, inferred.Date)
	if !isMeaningfulCoverValue(fields.Date) {
		now := time.Now()
		fields.Date = fmt.Sprintf("%d年%d月", now.Year(), int(now.Month()))
	}
}

func inferCoverFieldsFromClassification(cls ClassificationResult) *CoverFields {
	fields := &CoverFields{}
	for _, p := range leadingCoverParagraphs(cls.Paragraphs) {
		text := strings.TrimSpace(p.Text)
		if text == "" {
			continue
		}

		switch p.Type {
		case "cover_title", "title":
			fields.Title = preferMeaningfulCoverValue(fields.Title, text)
		case "cover_subtitle":
			fields.Subtitle = preferMeaningfulCoverValue(fields.Subtitle, text)
		case "cover_college":
			fields.College = preferMeaningfulCoverValue(fields.College, text)
		case "cover_major":
			fields.Major = preferMeaningfulCoverValue(fields.Major, text)
		case "cover_grade":
			fields.Grade = preferMeaningfulCoverValue(fields.Grade, text)
		case "cover_student_id":
			fields.StudentID = preferMeaningfulCoverValue(fields.StudentID, text)
		case "cover_student_name":
			fields.Name = preferMeaningfulCoverValue(fields.Name, text)
		case "cover_advisor":
			fields.Advisor = preferMeaningfulCoverValue(fields.Advisor, text)
		case "cover_date":
			fields.Date = preferMeaningfulCoverValue(fields.Date, text)
		}

		if p.Type == "cover" || strings.HasPrefix(p.Type, "cover_") || p.Type == "title" {
			parseCoverLineIntoFields(fields, text)
			if looksLikePaperTitle(text) {
				fields.Title = preferMeaningfulCoverValue(fields.Title, text)
			}
			if looksLikeSubtitle(text) {
				fields.Subtitle = preferMeaningfulCoverValue(fields.Subtitle, text)
			}
		}
	}
	return fields
}

func leadingCoverParagraphs(paragraphs []ClassificationParagraph) []ClassificationParagraph {
	cutTypes := map[string]bool{
		"abstract_title":          true,
		"abstract":                true,
		"en_abstract_title":       true,
		"en_abstract":             true,
		"table_of_contents":       true,
		"table_of_contents_title": true,
		"heading_1":               true,
		"references_title":        true,
		"acknowledgements_title":  true,
		"appendix_title":          true,
	}
	var out []ClassificationParagraph
	for _, p := range paragraphs {
		if cutTypes[p.Type] {
			break
		}
		out = append(out, p)
	}
	return out
}

func parseCoverLineIntoFields(fields *CoverFields, text string) {
	raw := strings.TrimSpace(text)
	if raw == "" {
		return
	}
	normalized := normalizeCoverLine(raw)

	pairs := []struct {
		re    *regexp.Regexp
		field *string
	}{
		{regexp.MustCompile(`(?:棰樼洰|璁烘枃棰樼洰|璁捐棰樼洰)[:锛歖?\s*(.+)$`), &fields.Title},
		{regexp.MustCompile(`(?:鍓爣棰榺鍓|棰樼洰琛ュ厖)[:锛歖?\s*(.+)$`), &fields.Subtitle},
		{regexp.MustCompile(`(?:瀛﹂櫌|闄㈢郴|鎵€鍦ㄥ闄?[:锛歖?\s*(.+)$`), &fields.College},
		{regexp.MustCompile(`(?:涓撲笟|鎵€鍦ㄤ笓涓?[:锛歖?\s*(.+)$`), &fields.Major},
		{regexp.MustCompile(`(?:鐝骇|骞寸骇鐝骇)[:锛歖?\s*(.+)$`), &fields.Grade},
		{regexp.MustCompile(`(?:瀛﹀彿)[:锛歖?\s*([A-Za-z0-9\-]+)$`), &fields.StudentID},
		{regexp.MustCompile(`(?:濮撳悕|瀛︾敓濮撳悕)[:锛歖?\s*([^\s]{2,8})$`), &fields.Name},
		{regexp.MustCompile(`(?:鎸囧鏁欏笀|瀵煎笀|鎸囧鑰佸笀)[:锛歖?\s*([^\s]{2,20})$`), &fields.Advisor},
		{regexp.MustCompile(`((?:20\d{2})\s*骞碶s*\d{1,2}\s*鏈?`), &fields.Date},
	}
	for _, pair := range pairs {
		m := pair.re.FindStringSubmatch(normalized)
		if len(m) > 1 {
			*pair.field = preferMeaningfulCoverValue(*pair.field, strings.TrimSpace(m[1]))
		}
	}
}

func preferMeaningfulCoverValue(current, candidate string) string {
	if isMeaningfulCoverValue(current) {
		return strings.TrimSpace(current)
	}
	if isMeaningfulCoverValue(candidate) {
		return strings.TrimSpace(candidate)
	}
	return strings.TrimSpace(current)
}

func isMeaningfulCoverValue(v string) bool {
	v = strings.TrimSpace(v)
	if v == "" {
		return false
	}
	flat := strings.ReplaceAll(v, " ", "")
	if flat == "" {
		return false
	}
	lower := strings.ToLower(flat)
	badFragments := []string{"xxxx", "xxx", "请填写", "删除", "说明", "模板", "{{", "}}"}
	for _, frag := range badFragments {
		if strings.Contains(lower, frag) {
			return false
		}
	}
	placeholderOnly := regexp.MustCompile(`^[xX锛縚路\.鈥擻-]+$`)
	if placeholderOnly.MatchString(flat) {
		return false
	}
	return true
}

func normalizeCoverLine(s string) string {
	s = strings.TrimSpace(s)
	s = strings.ReplaceAll(s, "\u3000", " ")
	s = strings.ReplaceAll(s, "\t", " ")
	for strings.Contains(s, "  ") {
		s = strings.ReplaceAll(s, "  ", " ")
	}
	return s
}

func looksLikePaperTitle(text string) bool {
	text = normalizeCoverLine(text)
	if !isMeaningfulCoverValue(text) {
		return false
	}
	if strings.Contains(text, "鏈姣曚笟璁烘枃") || strings.Contains(text, "鏈姣曚笟璁捐") {
		return false
	}
	if strings.Contains(text, "閲嶅簡浜烘枃绉戞妧瀛﹂櫌") {
		return false
	}
	if strings.Contains(text, "鎽樿") || strings.Contains(text, "鐩綍") || strings.Contains(text, "鍙傝€冩枃鐚?) {
		return false
	}
	if strings.Contains(text, "棰樼洰") || strings.Contains(text, "瀛﹂櫌") || strings.Contains(text, "涓撲笟") ||
		strings.Contains(text, "鐝骇") || strings.Contains(text, "瀛﹀彿") || strings.Contains(text, "濮撳悕") {
		return false
	}
	runes := []rune(text)
	return len(runes) >= 6 && len(runes) <= 40
}

func looksLikeSubtitle(text string) bool {
	text = normalizeCoverLine(text)
	if !isMeaningfulCoverValue(text) {
		return false
	}
	return strings.Contains(text, "鈥斺€?) || strings.Contains(text, "--") || strings.Contains(text, "鈥斺€斾互")
}

func synthesizeTOCSection(cls ClassificationResult, existing *SectionContent) *SectionContent {
	if tocSectionHasMeaningfulContent(existing) {
		return nil
	}

	var tocParas []ContentParagraph
	for _, p := range cls.Paragraphs {
		switch p.Type {
		case "heading_1", "heading_2", "heading_3":
			text := strings.TrimSpace(p.Text)
			if text == "" {
				continue
			}
			tocParas = append(tocParas, ContentParagraph{
				Text:     text,
				ParaType: "table_of_contents",
			})
		}
	}

	if len(tocParas) == 0 {
		return nil
	}

	return &SectionContent{
		SectionType: "table_of_contents",
		Paragraphs:  tocParas,
	}
}

func classificationParagraphToContentParagraph(p ClassificationParagraph) ContentParagraph {
	cp := ContentParagraph{
		Text:              p.Text,
		SourceXML:         p.SourceXML,
		HasComplexContent: p.HasComplexContent,
		ParaType:          p.Type,
	}
	if cp.HasComplexContent && cp.SourceXML != "" {
		return cp
	}

	switch p.Type {
	case "keywords":
		if runs, plain, ok := buildKeywordRuns(p.Text, true); ok {
			cp.Text = plain
			cp.Runs = runs
		}
	case "en_keywords":
		if runs, plain, ok := buildKeywordRuns(p.Text, false); ok {
			cp.Text = plain
			cp.Runs = runs
		}
	}

	return cp
}

func tocSectionHasMeaningfulContent(existing *SectionContent) bool {
	if existing == nil {
		return false
	}
	for _, p := range existing.Paragraphs {
		text := strings.TrimSpace(p.Text)
		if text == "" {
			continue
		}
		flat := strings.ReplaceAll(text, " ", "")
		flat = strings.ReplaceAll(flat, "\u3000", "")
		if flat == "" {
			continue
		}
		if strings.Contains(flat, "鐩綍灏嗗湪鎵撳紑鏂囨。鍚庤嚜鍔ㄦ洿鏂?) {
			continue
		}
		return true
	}
	return false
}

func buildKeywordRuns(text string, chinese bool) ([]ContentRun, string, bool) {
	s := strings.TrimSpace(text)
	if s == "" {
		return nil, "", false
	}

	label := "关键词："
	altLabel := "关键词:"
	if !chinese {
		label = "Keywords:"
		altLabel = "Key words:"
	}

	content := s
	switch {
	case strings.HasPrefix(s, label):
		content = strings.TrimSpace(strings.TrimPrefix(s, label))
	case strings.HasPrefix(s, altLabel):
		content = strings.TrimSpace(strings.TrimPrefix(s, altLabel))
	default:
		if chinese {
			return nil, "", false
		}
	}

	if !chinese {
		content = strings.ReplaceAll(content, "Keywords:", "")
		content = strings.ReplaceAll(content, "Key words:", "")
		content = strings.ReplaceAll(content, "keywords:", "")
		content = strings.ReplaceAll(content, "key words:", "")
	}
	content = strings.ReplaceAll(content, "；", ";")
	content = strings.ReplaceAll(content, "，", ";")
	content = strings.ReplaceAll(content, "、", ";")
	content = strings.ReplaceAll(content, ",", ";")
	parts := strings.Split(content, ";")
	cleaned := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			cleaned = append(cleaned, part)
		}
	}
	if len(cleaned) == 0 {
		return nil, "", false
	}

	content = strings.Join(cleaned, "; ")
	bold := true
	runs := []ContentRun{
		{Text: label, Bold: &bold},
		{Text: " " + content},
	}
	return runs, label + " " + content, true
}

func buildCoverSections(fields *CoverFields) []SectionContent {
	if fields == nil {
		return nil
	}
	coverTitle, coverSubtitle, syntheticSecondLine := splitCoverTitleForTemplate(fields.Title, fields.Subtitle)
	var sections []SectionContent
	add := func(sectionType, text string) {
		if strings.TrimSpace(text) == "" {
			return
		}
		sections = append(sections, SectionContent{
			SectionType: sectionType,
			Paragraphs:  []ContentParagraph{{Text: strings.TrimSpace(text)}},
		})
	}

	add("cover_title", coverTitle)
	add("cover_subtitle", coverSubtitle)
	add("cover_college", fields.College)
	add("cover_major", fields.Major)
	add("cover_grade", fields.Grade)
	add("cover_student_id", fields.StudentID)
	add("cover_student_name", fields.Name)
	add("cover_advisor", fields.Advisor)
	add("cover_date", normalizeCoverDate(fields.Date))

	add("inner_title", strings.TrimSpace(fields.Title))
	if !syntheticSecondLine {
		add("inner_subtitle", strings.TrimSpace(fields.Subtitle))
	}
	return sections
}

func splitCoverTitleForTemplate(title, subtitle string) (string, string, bool) {
	title = strings.TrimSpace(title)
	subtitle = strings.TrimSpace(subtitle)
	if title == "" {
		return "", subtitle, false
	}
	if subtitle != "" {
		return title, subtitle, false
	}

	runes := []rune(title)
	if len(runes) <= 22 {
		return title, "", false
	}

	splitAt := -1
	preferred := []string{"鍥犵礌鍒嗘瀽", "鐜扮姸鍒嗘瀽", "闂鐮旂┒", "褰卞搷鍥犵礌", "璋冩煡鍒嗘瀽", "鐮旂┒", "鍒嗘瀽"}
	for _, marker := range preferred {
		if idx := strings.Index(title, marker); idx > 0 {
			splitAt = idx
			break
		}
	}
	if splitAt <= 0 {
		half := len(runes) / 2
		for i := half; i < len(runes) && i < half+6; i++ {
			if runes[i] == '銆? || runes[i] == '锛? || runes[i] == '锛? || runes[i] == ' ' {
				splitAt = i + 1
				break
			}
		}
	}
	if splitAt <= 0 {
		splitAt = len(runes) / 2
	}

	line1 := strings.TrimSpace(string([]rune(title)[:splitAt]))
	line2 := strings.TrimSpace(string([]rune(title)[splitAt:]))
	if line1 == "" || line2 == "" {
		return title, "", false
	}
	return line1, line2, true
}

func normalizeCoverDate(date string) string {
	date = strings.TrimSpace(date)
	if date != "" {
		date = strings.ReplaceAll(date, " ", "")
		date = strings.ReplaceAll(date, "　", "")
		return date
	}
	now := time.Now()
	return fmt.Sprintf("%d年%d月", now.Year(), int(now.Month()))
}

func mapParaTypeToSection(paraType string) string {
	switch paraType {
	case "cover", "cover_title", "title":
		return "cover_title"
	case "cover_subtitle":
		return "cover_subtitle"
	case "cover_college":
		return "cover_college"
	case "cover_major":
		return "cover_major"
	case "cover_grade":
		return "cover_grade"
	case "cover_student_name":
		return "cover_student_name"
	case "cover_student_id":
		return "cover_student_id"
	case "cover_advisor":
		return "cover_advisor"
	case "cover_date":
		return "cover_date"

	case "abstract_title":
		return "abstract_title"
	case "abstract":
		return "abstract"
	case "keywords":
		return "keywords"

	case "en_abstract_title":
		return "en_abstract_title"
	case "en_abstract":
		return "en_abstract"
	case "en_keywords":
		return "en_keywords"

	case "table_of_contents_title", "table_of_contents":
		return "table_of_contents"

	case "heading_1", "heading_2", "heading_3", "body":
		return "body"

	case "references_title":
		return "references_title"
	case "references":
		return "references"

	case "acknowledgements_title":
		return "acknowledgements_title"
	case "acknowledgements":
		return "acknowledgements"

	case "appendix_title":
		return "appendix_title"
	case "appendix":
		return "appendix"

	default:
		return "body"
	}
}

// GetPreparedTemplatePath returns the path to the prepared golden template.
// It first checks for a prepared template, then falls back to auto-preparing from the real template.
func (tf *TemplateFiller) GetPreparedTemplatePath(templateID string) string {
	if tf.TemplateDir == "" {
		return ""
	}

	// Check for prepared template
	preparedPath := filepath.Join(tf.TemplateDir, templateID+"_prepared.docx")
	if _, err := os.Stat(preparedPath); err == nil {
		return preparedPath
	}

	// Check for regular template
	regularPath := filepath.Join(tf.TemplateDir, templateID+".docx")
	if _, err := os.Stat(regularPath); err == nil {
		return regularPath
	}

	return ""
}

// GetTemplatePath returns the golden template path for a given template ID.
func (tf *TemplateFiller) GetTemplatePath(templateID string) string {
	if tf.TemplateDir == "" {
		return ""
	}
	candidates := []string{
		filepath.Join(tf.TemplateDir, templateID+"_prepared.docx"),
		filepath.Join(tf.TemplateDir, templateID+".docx"),
		filepath.Join(tf.TemplateDir, templateID+"_鏈璁烘枃.docx"),
	}
	for _, p := range candidates {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

func (tf *TemplateFiller) HasTemplate(templateID string) bool {
	return tf.GetTemplatePath(templateID) != ""
}

// EnsureGoldenTemplate ensures the prepared golden template exists.
// If only the real template exists, it auto-prepares it.
func (tf *TemplateFiller) EnsureGoldenTemplate(templateID string) (string, error) {
	if tf.TemplateDir == "" {
		return "", fmt.Errorf("template directory not configured")
	}

	// Check for already-prepared template
	preparedPath := filepath.Join(tf.TemplateDir, templateID+"_prepared.docx")
	if _, err := os.Stat(preparedPath); err == nil {
		return preparedPath, nil
	}

	// Check for real template that needs preparation
	realPath := filepath.Join(tf.TemplateDir, templateID+"_real.docx")
	if _, err := os.Stat(realPath); err == nil {
		log.Printf("[TemplateFiller] preparing golden template from real template: %s", realPath)
		if err := PrepareRealTemplate(realPath, preparedPath); err != nil {
			return "", fmt.Errorf("prepare real template: %w", err)
		}
		return preparedPath, nil
	}

	// Check for any existing template (backward compatibility)
	regularPath := filepath.Join(tf.TemplateDir, templateID+".docx")
	if _, err := os.Stat(regularPath); err == nil {
		return regularPath, nil
	}

	// Generate a basic template as last resort
	var cfg GoldenTemplateConfig
	switch templateID {
	case "cqrwst":
		cfg = DefaultCQRWSTConfig()
	default:
		return "", fmt.Errorf("no golden template config for: %s", templateID)
	}

	outputPath := filepath.Join(tf.TemplateDir, templateID+".docx")
	if err := GenerateGoldenTemplate(outputPath, cfg); err != nil {
		return "", fmt.Errorf("generate golden template: %w", err)
	}
	return outputPath, nil
}

// fillWithPython runs the legacy Python template filler as a subprocess.
func (tf *TemplateFiller) fillWithPython(ctx context.Context, inputPath, templatePath string, classification ClassificationResult, outputDir string) (string, error) {
	if tf.ScriptPath == "" {
		return "", fmt.Errorf("template_filler.py not found")
	}

	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create output dir: %w", err)
	}

	baseName := strings.TrimSuffix(filepath.Base(inputPath), filepath.Ext(inputPath))
	outputPath := filepath.Join(outputDir, fmt.Sprintf("%s_corrected_%d.docx", baseName, time.Now().UnixMilli()))

	clsFile, err := writeTempJSON("cls_*.json", classification)
	if err != nil {
		return "", fmt.Errorf("failed to write classification JSON: %w", err)
	}
	defer os.Remove(clsFile)

	timeout := 60 * time.Second
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, tf.PythonBin, tf.ScriptPath,
		"--input", inputPath,
		"--template", templatePath,
		"--classification", clsFile,
		"--output", outputPath,
	)
	cmd.Env = append(os.Environ(), "PYTHONIOENCODING=utf-8")

	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("python template_filler failed: %v\nOutput:\n%s", err, string(out))
	}

	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	var result FillResult
	if len(lines) > 0 {
		lastLine := lines[len(lines)-1]
		if jsonErr := json.Unmarshal([]byte(lastLine), &result); jsonErr != nil {
			log.Printf("[TemplateFiller] warning: could not parse JSON result: %v", jsonErr)
		}
	}

	if _, err := os.Stat(outputPath); os.IsNotExist(err) {
		return "", fmt.Errorf("python script completed but output file not found: %s", outputPath)
	}

	log.Printf("[TemplateFiller] Python fallback success: %s (%d paragraphs, %d tables)", outputPath, result.Paragraphs, result.Tables)
	return outputPath, nil
}

// helpers

func writeTempJSON(pattern string, v interface{}) (string, error) {
	data, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	f, err := os.CreateTemp("", pattern)
	if err != nil {
		return "", err
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		os.Remove(f.Name())
		return "", err
	}
	f.Close()
	return f.Name(), nil
}

func locateScript(name string) string {
	candidates := []string{
		filepath.Join(execDir(), name),
		filepath.Join(execDir(), "scripts", name),
		filepath.Join(execDir(), "..", "scripts", name),
		filepath.Join(execDir(), "..", "pkg", "templatefiller", name),
		"/opt/paper-backend/scripts/" + name,
	}
	for _, p := range candidates {
		if _, err := os.Stat(p); err == nil {
			abs, _ := filepath.Abs(p)
			log.Printf("[TemplateFiller] found script: %s", abs)
			return abs
		}
	}
	log.Printf("[TemplateFiller] warning: %s not found in any candidate path", name)
	return ""
}

func locateDir(name string, relPath string) string {
	candidates := []string{
		filepath.Join(execDir(), relPath),
		filepath.Join(execDir(), "..", relPath),
		filepath.Join(execDir(), "..", "uploads", name),
		"/opt/paper/uploads/" + name,
		"/opt/paper-backend/uploads/" + name,
	}
	for _, p := range candidates {
		if info, err := os.Stat(p); err == nil && info.IsDir() {
			abs, _ := filepath.Abs(p)
			log.Printf("[TemplateFiller] found template dir: %s", abs)
			return abs
		}
	}
	// Create the directory if possible
	for _, p := range candidates[:2] {
		if err := os.MkdirAll(p, 0755); err == nil {
			abs, _ := filepath.Abs(p)
			log.Printf("[TemplateFiller] created template dir: %s", abs)
			return abs
		}
	}
	log.Printf("[TemplateFiller] warning: template dir '%s' not found", name)
	return ""
}

func execDir() string {
	ex, err := os.Executable()
	if err != nil {
		return "."
	}
	return filepath.Dir(ex)
}

