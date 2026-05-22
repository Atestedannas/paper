package templatefiller

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"log"
	"os"
	"regexp"
	"strings"
)

// CoverFields holds the structured data extracted from the cover page of a student document.
type CoverFields struct {
	Title     string // 论文题目
	Subtitle  string // 副标题 (如 "——以XX为例")
	College   string // 学院
	Major     string // 专业
	Grade     string // 班级
	StudentID string // 学号
	Name      string // 姓名
	Advisor   string // 指导教师
	Date      string // 日期
}

// ToSectionMap converts CoverFields to a map of SectionContent keyed by placeholder section type.
func (cf *CoverFields) ToSectionMap() map[string]*SectionContent {
	m := make(map[string]*SectionContent)

	add := func(sectionType, text string) {
		if text != "" {
			m[sectionType] = &SectionContent{
				SectionType: sectionType,
				Paragraphs:  []ContentParagraph{{Text: text}},
			}
		}
	}

	add("cover_title", cf.Title)
	add("cover_subtitle", cf.Subtitle)
	add("cover_college", cf.College)
	add("cover_major", cf.Major)
	add("cover_grade", cf.Grade)
	add("cover_student_id", cf.StudentID)
	add("cover_student_name", cf.Name)
	add("cover_advisor", cf.Advisor)
	add("cover_date", cf.Date)

	return m
}

// ExtractCoverFields opens a .docx file and extracts cover page field values
// by parsing the XML structure and matching known label patterns in tables.
func ExtractCoverFields(docPath string) (*CoverFields, error) {
	docBytes, err := os.ReadFile(docPath)
	if err != nil {
		return nil, fmt.Errorf("read document: %w", err)
	}
	return ExtractCoverFieldsFromBytes(docBytes)
}

// ExtractCoverFieldsFromBytes extracts cover fields from raw .docx bytes.
func ExtractCoverFieldsFromBytes(docBytes []byte) (*CoverFields, error) {
	reader, err := zip.NewReader(bytes.NewReader(docBytes), int64(len(docBytes)))
	if err != nil {
		return nil, fmt.Errorf("open zip: %w", err)
	}

	var docXML []byte
	for _, f := range reader.File {
		if f.Name == "word/document.xml" {
			rc, err := f.Open()
			if err != nil {
				return nil, fmt.Errorf("open document.xml: %w", err)
			}
			docXML, err = io.ReadAll(rc)
			rc.Close()
			if err != nil {
				return nil, fmt.Errorf("read document.xml: %w", err)
			}
			break
		}
	}

	if docXML == nil {
		return nil, fmt.Errorf("document.xml not found in archive")
	}

	return extractFieldsFromXML(string(docXML))
}

func extractFieldsFromXML(content string) (*CoverFields, error) {
	fields := &CoverFields{}
	coverContent := extractCoverOnlyContent(content)

	// Strategy 1: Extract from tables
	tables := extractAllTables(coverContent)
	for _, tbl := range tables {
		rows := extractTableRows(tbl)
		for _, row := range rows {
			cells := extractTableCells(row)
			if len(cells) >= 2 {
				labelText := cleanText(extractCombinedLabelText(cells))
				valueText := cleanText(extractAllText(cells[len(cells)-1]))

				matchCoverField(fields, labelText, valueText)
			}
		}
	}

	// Strategy 2: If title not found in tables, look for it in paragraphs
	if fields.Title == "" {
		fields.Title = extractTitleFromParagraphs(coverContent)
	}

	// Strategy 3: Extract subtitle from patterns like "——以XX为例"
	if fields.Subtitle == "" {
		fields.Subtitle = extractSubtitleFromContent(coverContent)
	}

	// Strategy 4: Extract date from the cover page area
	if fields.Date == "" {
		fields.Date = extractDateFromContent(coverContent)
	}

	// Strategy 5: If no table-based extraction worked, try line-by-line parsing
	if fields.College == "" {
		extractFieldsFromParagraphs(coverContent, fields)
	}

	log.Printf("[CoverExtractor] extracted: title=%q, college=%q, major=%q, grade=%q, name=%q, id=%q",
		truncate(fields.Title, 30), fields.College, fields.Major, fields.Grade, fields.Name, fields.StudentID)

	return fields, nil
}

func extractCoverOnlyContent(content string) string {
	firstSectPr := strings.Index(content, "<w:sectPr")
	if firstSectPr > 0 {
		return content[:firstSectPr]
	}
	return content
}

func matchCoverField(fields *CoverFields, label, value string) {
	if value == "" {
		return
	}

	label = strings.ReplaceAll(label, " ", "")
	label = strings.ReplaceAll(label, "　", "")

	switch {
	case containsAny(label, "题目", "论文题目", "题　目"):
		if fields.Title == "" {
			fields.Title = value
		}
	case containsAny(label, "学院", "院系", "所在学院"):
		if fields.College == "" {
			fields.College = value
		}
	case containsAny(label, "专业", "所学专业"):
		if fields.Major == "" {
			fields.Major = value
		}
	case containsAny(label, "班级", "所在班级"):
		if fields.Grade == "" {
			fields.Grade = value
		}
	case containsAny(label, "学号"):
		if fields.StudentID == "" {
			fields.StudentID = value
		}
	case containsAny(label, "姓名", "学生姓名"):
		if fields.Name == "" {
			fields.Name = value
		}
	case containsAny(label, "指导教师", "导师", "指导老师"):
		if fields.Advisor == "" {
			fields.Advisor = value
		}
	}
}

func containsAny(s string, patterns ...string) bool {
	for _, p := range patterns {
		if strings.Contains(s, p) {
			return true
		}
	}
	return false
}

func extractAllTables(content string) []string {
	var tables []string
	pos := 0
	for {
		start := strings.Index(content[pos:], "<w:tbl>")
		if start < 0 {
			start = strings.Index(content[pos:], "<w:tbl ")
			if start < 0 {
				break
			}
		}
		absStart := pos + start
		end := strings.Index(content[absStart:], "</w:tbl>")
		if end < 0 {
			break
		}
		tables = append(tables, content[absStart:absStart+end+len("</w:tbl>")])
		pos = absStart + end + len("</w:tbl>")
	}
	return tables
}

func extractTableRows(tbl string) []string {
	var rows []string
	pos := 0
	for {
		start := strings.Index(tbl[pos:], "<w:tr")
		if start < 0 {
			break
		}
		absStart := pos + start
		end := strings.Index(tbl[absStart:], "</w:tr>")
		if end < 0 {
			break
		}
		rows = append(rows, tbl[absStart:absStart+end+len("</w:tr>")])
		pos = absStart + end + len("</w:tr>")
	}
	return rows
}

func extractTableCells(row string) []string {
	var cells []string
	pos := 0
	for {
		start := strings.Index(row[pos:], "<w:tc")
		if start < 0 {
			break
		}
		absStart := pos + start
		end := strings.Index(row[absStart:], "</w:tc>")
		if end < 0 {
			break
		}
		cells = append(cells, row[absStart:absStart+end+len("</w:tc>")])
		pos = absStart + end + len("</w:tc>")
	}
	return cells
}

func extractAllText(xmlStr string) string {
	var texts []string
	search := xmlStr
	for {
		tStart := strings.Index(search, "<w:t")
		if tStart < 0 {
			break
		}
		// Verify this is actually <w:t> or <w:t ...>, not <w:tc>, <w:tbl>, <w:tr>, etc.
		if tStart+4 < len(search) {
			next := search[tStart+4]
			if next != '>' && next != ' ' {
				search = search[tStart+4:]
				continue
			}
		}
		tagClose := strings.Index(search[tStart:], ">")
		if tagClose < 0 {
			break
		}
		contentStart := tStart + tagClose + 1
		tEnd := strings.Index(search[contentStart:], "</w:t>")
		if tEnd < 0 {
			break
		}
		text := search[contentStart : contentStart+tEnd]
		text = strings.ReplaceAll(text, "&amp;", "&")
		text = strings.ReplaceAll(text, "&lt;", "<")
		text = strings.ReplaceAll(text, "&gt;", ">")
		texts = append(texts, text)
		search = search[contentStart+tEnd+len("</w:t>"):]
	}
	return strings.Join(texts, "")
}

func extractTitleFromParagraphs(content string) string {
	// Look for the first large-font centered paragraph that isn't a standard heading
	// This is a heuristic approach for documents without table-based titles
	firstSectPr := strings.Index(content, "<w:sectPr")
	if firstSectPr < 0 {
		firstSectPr = len(content)
	}
	coverContent := content[:firstSectPr]

	// Find paragraphs with large font size (>= 28 half-points = 四号)
	reSz := regexp.MustCompile(`<w:sz w:val="(\d+)"`)
	rePara := regexp.MustCompile(`<w:p[ >][\s\S]*?</w:p>`)

	paragraphs := rePara.FindAllString(coverContent, -1)
	for _, para := range paragraphs {
		text := cleanText(extractAllText(para))
		if text == "" || len(text) < 4 {
			continue
		}
		if text == "本科毕业论文/设计" || text == "本科毕业论文（设计）" {
			continue
		}
		szMatches := reSz.FindStringSubmatch(para)
		if len(szMatches) > 1 {
			// Large font indicates a title
			if szMatches[1] >= "28" {
				return text
			}
		}
	}
	return ""
}

func extractSubtitleFromContent(content string) string {
	reSubtitle := regexp.MustCompile(`——[^<\n]{2,}`)
	match := reSubtitle.FindString(content)
	if match != "" {
		// Clean XML entities
		match = strings.ReplaceAll(match, "&amp;", "&")
		return match
	}
	return ""
}

func extractDateFromContent(content string) string {
	// Look for date patterns like "2026年 3月" or "二零二六年三月"
	reDate := regexp.MustCompile(`20\d{2}\s*年\s*\d{1,2}\s*月`)
	firstSectPr := strings.Index(content, "<w:sectPr")
	if firstSectPr < 0 {
		firstSectPr = len(content)
	}

	// Search in the first section (cover page)
	coverContent := content[:firstSectPr]
	match := reDate.FindString(coverContent)
	if match != "" {
		return cleanText(match)
	}
	return ""
}

func extractFieldsFromParagraphs(content string, fields *CoverFields) {
	// Fallback: look for "学院：XX" or "学 院 XX" patterns in paragraphs
	firstSectPr := strings.Index(content, "<w:sectPr")
	if firstSectPr < 0 {
		return
	}
	coverContent := content[:firstSectPr]

	patterns := []struct {
		re    *regexp.Regexp
		field *string
	}{
		{regexp.MustCompile(`(?:学\s*院|院\s*系)[：:\s]*([^\s<]{2,})`), &fields.College},
		{regexp.MustCompile(`(?:专\s*业)[：:\s]*([^\s<]{2,})`), &fields.Major},
		{regexp.MustCompile(`(?:班\s*级)[：:\s]*([^\s<]{2,})`), &fields.Grade},
		{regexp.MustCompile(`(?:学\s*号)[：:\s]*([^\s<]{2,})`), &fields.StudentID},
		{regexp.MustCompile(`(?:姓\s*名)[：:\s]*([^\s<]{2,})`), &fields.Name},
		{regexp.MustCompile(`(?:指导教师|导\s*师)[：:\s]*([^\s<]{2,})`), &fields.Advisor},
	}

	// Extract all text from cover page
	allText := extractAllText(coverContent)
	for _, p := range patterns {
		if *p.field == "" {
			m := p.re.FindStringSubmatch(allText)
			if len(m) > 1 {
				*p.field = cleanText(m[1])
			}
		}
	}
}

func cleanText(s string) string {
	s = strings.TrimSpace(s)
	s = strings.ReplaceAll(s, "\r", "")
	s = strings.ReplaceAll(s, "\n", " ")
	return s
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
