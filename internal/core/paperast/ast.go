package paperast

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"html"
	"regexp"
	"sort"
	"strings"

	"github.com/paper-format-checker/backend/internal/core/ooxmlpkg"
	"github.com/paper-format-checker/backend/internal/core/templateprofile"
)

const Version = "paper-ast-v1"

type Snapshot struct {
	Version  string        `json:"version"`
	Source   string        `json:"source"`
	Stats    Stats         `json:"stats"`
	Sections []PageSection `json:"page_sections,omitempty"`
	Nodes    []Node        `json:"nodes"`
}

// PageSection preserves page geometry in Word's source unit (twips).
// PDF positions are complementary rendered evidence, not a replacement.
type PageSection struct {
	ID                string `json:"id"`
	PageWidthTwips    string `json:"page_width_twips,omitempty"`
	PageHeightTwips   string `json:"page_height_twips,omitempty"`
	MarginTopTwips    string `json:"margin_top_twips,omitempty"`
	MarginRightTwips  string `json:"margin_right_twips,omitempty"`
	MarginBottomTwips string `json:"margin_bottom_twips,omitempty"`
	MarginLeftTwips   string `json:"margin_left_twips,omitempty"`
	HeaderMarginTwips string `json:"header_margin_twips,omitempty"`
	FooterMarginTwips string `json:"footer_margin_twips,omitempty"`
	Orientation       string `json:"orientation,omitempty"`
	PageNumberFormat  string `json:"page_number_format,omitempty"`
	PageNumberStart   string `json:"page_number_start,omitempty"`
}

type Stats struct {
	Paragraphs      int `json:"paragraphs"`
	BlankParagraphs int `json:"blank_paragraphs"`
	Tables          int `json:"tables"`
	Headings        int `json:"headings"`
	Headers         int `json:"headers"`
	Footers         int `json:"footers"`
	Footnotes       int `json:"footnotes"`
	Endnotes        int `json:"endnotes"`
	Textboxes       int `json:"textboxes"`
	Formulas        int `json:"formulas"`
	Images          int `json:"images"`
}

type Node struct {
	NodeID          string                     `json:"node_id"`
	OfficePath      string                     `json:"office_path,omitempty"`
	ParaID          string                     `json:"para_id,omitempty"`
	SourcePart      string                     `json:"source_part"`
	ParentNodeID    string                     `json:"parent_node_id,omitempty"`
	Index           int                        `json:"index"`
	NodeType        string                     `json:"node_type"`
	Text            string                     `json:"text,omitempty"`
	SemanticRole    string                     `json:"semantic_role"`
	LogicalLevel    int                        `json:"logical_level,omitempty"`
	CurrentStyle    string                     `json:"current_style_id,omitempty"`
	SectionID       string                     `json:"section_id"`
	Confidence      float64                    `json:"confidence"`
	Evidence        []string                   `json:"evidence,omitempty"`
	BeforeTwips     int                        `json:"before_twips,omitempty"`
	AfterTwips      int                        `json:"after_twips,omitempty"`
	LineTwips       int                        `json:"line_twips,omitempty"`
	PageBreakBefore bool                       `json:"page_break_before,omitempty"`
	PageSectionID   string                     `json:"page_section_id,omitempty"`
	EffectiveStyle  *templateprofile.StyleRule `json:"effective_style,omitempty"`
	FieldCodes      []string                   `json:"field_codes,omitempty"`
	BookmarkNames   []string                   `json:"bookmark_names,omitempty"`
	HyperlinkIDs    []string                   `json:"hyperlink_ids,omitempty"`
}

type ValidationIssue struct {
	Kind    string `json:"kind"`
	Message string `json:"message"`
}

var (
	bodyChildPattern              = regexp.MustCompile(`(?s)<w:p(?:\s[^>]*)?>.*?</w:p>|<w:tbl(?:\s[^>]*)?>.*?</w:tbl>`)
	nodeTypePattern               = regexp.MustCompile(`^<w:(p|tbl)\b`)
	textPattern                   = regexp.MustCompile(`(?s)<w:t\b[^>]*>(.*?)</w:t>`)
	mathTextPattern               = regexp.MustCompile(`(?s)<m:t\b[^>]*>(.*?)</m:t>`)
	ommlPattern                   = regexp.MustCompile(`(?s)<m:(?:oMath|oMathPara)\b`)
	deletedPattern                = regexp.MustCompile(`(?s)<w:(?:del|moveFrom)\b[^>]*>.*?</w:(?:del|moveFrom)>`)
	stylePattern                  = regexp.MustCompile(`<w:pStyle\b[^>]*\bw:val="([^"]+)"`)
	paraIDPattern                 = regexp.MustCompile(`(?:w14:|w:)?paraId="([A-Fa-f0-9]+)"`)
	outlinePattern                = regexp.MustCompile(`<w:outlineLvl\b[^>]*\bw:val="(\d+)"`)
	headingPattern                = regexp.MustCompile(`^(\d+(?:\.\d+){0,3})\s+\S+`)
	compactHeadingPattern         = regexp.MustCompile(`^(\d+(?:\.\d+){1,3})(?:\.)?([^\d\s].+)$`)
	compactNumberedHeadingPattern = regexp.MustCompile(`^(\d+(?:\.\d+){0,3})[.、．]?\s*([^\d\s].+)$`)
	chapterPattern                = regexp.MustCompile(`^第[一二三四五六七八九十百千万\d]+章(?:\s*\S+)?$`)
	chineseListPattern            = regexp.MustCompile(`^[一二三四五六七八九十百]+[、．.]\s*\S+`)
	spacingPattern                = regexp.MustCompile(`<w:spacing\b[^>]*/>`)
	pageBreakBeforePattern        = regexp.MustCompile(`<w:pageBreakBefore\b[^>]*/>`)
	attributePattern              = regexp.MustCompile(`\b([A-Za-z0-9_:.]+)="([^"]*)"`)
	correctChapterPattern         = regexp.MustCompile(`^\x{7b2c}[\x{4e00}-\x{9fff}\d]+\x{7ae0}(?:\s*\S+)?$`)
	correctChineseListPattern     = regexp.MustCompile(`^[\x{4e00}\x{4e8c}\x{4e09}\x{56db}\x{4e94}\x{516d}\x{4e03}\x{516b}\x{4e5d}\x{5341}\x{767e}]+[\x{3001}\x{3002}\x{ff0e}.]\s*\S+`)
	correctTableCaptionPattern    = regexp.MustCompile(`^\x{8868}\d+(?:\.\d+|-?\d*)?\s*\S*`)
	continuedTableCaptionPattern  = regexp.MustCompile(`^\x{7eed}\s*\x{8868}\s*\d+(?:\.\d+|-?\d*)?\s*\S*`)
	correctFigureCaptionPattern   = regexp.MustCompile(`^\x{56fe}\d+(?:\.\d+|-?\d*)?\s*\S*`)
	appendixTitlePattern          = regexp.MustCompile(`^\x{9644}\x{5f55}(?:\s*[A-Za-z0-9\x{4e00}-\x{9fff}]+)?(?:\s+\S.*)?$`)
	tocEntryPattern               = regexp.MustCompile(`(?:\.{2,}|…)\s*\d+\s*$`)
	paragraphPartPattern          = regexp.MustCompile(`(?s)<w:p(?:\s[^>]*)?>.*?</w:p>`)
	textboxPattern                = regexp.MustCompile(`(?s)<w:txbxContent\b[^>]*>(.*?)</w:txbxContent>`)
	drawingPattern                = regexp.MustCompile(`(?s)<w:drawing\b[^>]*>.*?</w:drawing>`)
	instrTextPattern              = regexp.MustCompile(`(?s)<w:instrText\b[^>]*>(.*?)</w:instrText>`)
	bookmarkNamePattern           = regexp.MustCompile(`<w:bookmarkStart\b[^>]*\bw:name="([^"]+)"`)
	hyperlinkIDPattern            = regexp.MustCompile(`<w:hyperlink\b[^>]*\br:id="([^"]+)"`)
	tablePattern                  = regexp.MustCompile(`(?s)<w:tbl(?:\s[^>]*)?>(.*?)</w:tbl>`)
	tableRowPattern               = regexp.MustCompile(`(?s)<w:tr(?:\s[^>]*)?>(.*?)</w:tr>`)
	tableCellPattern              = regexp.MustCompile(`(?s)<w:tc(?:\s[^>]*)?>(.*?)</w:tc>`)
	sectionPropertiesPattern      = regexp.MustCompile(`(?s)<w:sectPr\b[^>]*>.*?</w:sectPr>|<w:sectPr\b[^>]*/>`)
	pageSizePattern               = regexp.MustCompile(`<w:pgSz\b[^>]*/>`)
	pageMarginsPattern            = regexp.MustCompile(`<w:pgMar\b[^>]*/>`)
	pageNumberingPattern          = regexp.MustCompile(`<w:pgNumType\b[^>]*/>`)
)

func Extract(docxPath string) (Snapshot, error) {
	pkg, err := ooxmlpkg.Open(docxPath)
	if err != nil {
		return Snapshot{}, fmt.Errorf("open docx for AST: %w", err)
	}
	documentXML, ok := pkg.Get("word/document.xml")
	if !ok {
		return Snapshot{}, fmt.Errorf("word/document.xml missing")
	}
	snapshot := ExtractDocumentXML(string(documentXML))
	styles, err := templateprofile.ResolveDocumentEffectiveStyles(docxPath)
	if err != nil {
		return Snapshot{}, fmt.Errorf("resolve effective paragraph styles: %w", err)
	}
	for index := range snapshot.Nodes {
		if style, ok := styles[snapshot.Nodes[index].Index]; ok {
			copy := style
			snapshot.Nodes[index].EffectiveStyle = &copy
		}
	}
	appendNonBodyParts(&snapshot, pkg)
	return snapshot, nil
}

// appendNonBodyParts preserves source boundaries that are essential for role
// recognition. Header/footer and instructional textbox text must never become
// normal body paragraphs merely because their words look like a format rule.
func appendNonBodyParts(snapshot *Snapshot, pkg *ooxmlpkg.DocxPackage) {
	names := pkg.Names()
	sort.Strings(names)
	for _, name := range names {
		lower := strings.ToLower(name)
		kind, role := "", ""
		switch {
		case strings.HasPrefix(lower, "word/header") && strings.HasSuffix(lower, ".xml"):
			kind, role = "header_paragraph", "header"
		case strings.HasPrefix(lower, "word/footer") && strings.HasSuffix(lower, ".xml"):
			kind, role = "footer_paragraph", "footer"
		case lower == "word/footnotes.xml":
			kind, role = "footnote_paragraph", "footnote"
		case lower == "word/endnotes.xml":
			kind, role = "endnote_paragraph", "endnote"
		default:
			continue
		}
		raw, ok := pkg.Get(name)
		if !ok {
			continue
		}
		for localIndex, paragraphXML := range paragraphPartPattern.FindAllString(string(raw), -1) {
			text := extractText(paragraphXML)
			if strings.TrimSpace(text) == "" && len(extractFieldCodes(paragraphXML)) == 0 {
				continue
			}
			nodeID := fmt.Sprintf("%s:p:%06d", strings.TrimSuffix(filepathBase(name), ".xml"), localIndex)
			node := Node{NodeID: nodeID, OfficePath: fmt.Sprintf("/%s/p[%d]", name, localIndex+1), SourcePart: name, Index: len(snapshot.Nodes), NodeType: kind, Text: text, SemanticRole: role, SectionID: role, Confidence: .99, Evidence: []string{"ooxml:" + role}}
			snapshot.Nodes = append(snapshot.Nodes, withRawMetadata(node, paragraphXML))
			switch role {
			case "header":
				snapshot.Stats.Headers++
			case "footer":
				snapshot.Stats.Footers++
			case "footnote":
				snapshot.Stats.Footnotes++
			case "endnote":
				snapshot.Stats.Endnotes++
			}
		}
	}
	// Text boxes reside in document.xml, but carry instructions in many school
	// templates. Keep them as a separate non-actionable role.
	documentXML, ok := pkg.Get("word/document.xml")
	if !ok {
		return
	}
	anchors := textboxAnchorIndices(string(documentXML))
	for index, match := range textboxPattern.FindAllStringSubmatch(string(documentXML), -1) {
		if len(match) < 2 {
			continue
		}
		text := extractText(match[1])
		if strings.TrimSpace(text) == "" {
			continue
		}
		anchorID := ""
		if index < len(anchors) {
			for _, node := range snapshot.Nodes {
				if node.SourcePart == "word/document.xml" && node.Index == anchors[index] {
					anchorID = node.NodeID
					break
				}
			}
		}
		evidence := []string{"ooxml:w:txbxContent"}
		if anchorID != "" {
			evidence = append(evidence, "relation:annotates:"+anchorID)
		}
		node := Node{NodeID: fmt.Sprintf("textbox:%06d", index), OfficePath: fmt.Sprintf("/word/document.xml/txbxContent[%d]", index+1), SourcePart: "word/document.xml", ParentNodeID: anchorID, Index: len(snapshot.Nodes), NodeType: "instruction_textbox", Text: text, SemanticRole: "instruction_textbox", SectionID: "instruction", Confidence: .90, Evidence: evidence}
		snapshot.Nodes = append(snapshot.Nodes, withRawMetadata(node, match[1]))
		snapshot.Stats.Textboxes++
	}
	for index, drawing := range drawingPattern.FindAllStringIndex(string(documentXML), -1) {
		anchorID := ""
		if anchor := drawingAnchorIndex(string(documentXML), drawing[0]); anchor >= 0 {
			for _, node := range snapshot.Nodes {
				if node.SourcePart == "word/document.xml" && node.Index == anchor {
					anchorID = node.NodeID
					break
				}
			}
		}
		snapshot.Nodes = append(snapshot.Nodes, Node{NodeID: fmt.Sprintf("body:image:%06d", index+1), OfficePath: fmt.Sprintf("/word/document.xml/drawing[%d]", index+1), SourcePart: "word/document.xml", ParentNodeID: anchorID, Index: len(snapshot.Nodes), NodeType: "image", SemanticRole: "image", SectionID: "body", Confidence: .99, Evidence: []string{"ooxml:w:drawing"}})
		snapshot.Stats.Images++
	}
	for tableIndex, table := range tablePattern.FindAllStringSubmatch(string(documentXML), -1) {
		if len(table) < 2 {
			continue
		}
		for rowIndex, row := range tableRowPattern.FindAllStringSubmatch(table[1], -1) {
			if len(row) < 2 {
				continue
			}
			for cellIndex, cell := range tableCellPattern.FindAllStringSubmatch(row[1], -1) {
				if len(cell) < 2 {
					continue
				}
				for paraIndex, paragraphXML := range paragraphPartPattern.FindAllString(cell[1], -1) {
					text := extractText(paragraphXML)
					if strings.TrimSpace(text) == "" {
						continue
					}
					nodeID := fmt.Sprintf("body:table:%d:row:%d:cell:%d:p:%d", tableIndex+1, rowIndex+1, cellIndex+1, paraIndex)
					node := Node{NodeID: nodeID, ParentNodeID: fmt.Sprintf("body:table:%d", tableIndex+1), OfficePath: nodeID, SourcePart: "word/document.xml", Index: len(snapshot.Nodes), NodeType: "table_cell_paragraph", Text: text, SemanticRole: "table_cell", SectionID: "table", Confidence: .99, Evidence: []string{"ooxml:table_cell"}}
					snapshot.Nodes = append(snapshot.Nodes, withRawMetadata(node, paragraphXML))
				}
			}
		}
	}
}

// textboxAnchorIndices keeps the relationship between a text box and the
// body paragraph that contains its drawing anchor. This is OOXML structure,
// not a guessed visual position.
func textboxAnchorIndices(documentXML string) []int {
	anchors := []int{}
	bodyChildren := bodyChildPattern.FindAllStringIndex(documentXML, -1)
	for _, textbox := range textboxPattern.FindAllStringIndex(documentXML, -1) {
		paragraphStart := strings.LastIndex(documentXML[:textbox[0]], "<w:p")
		if paragraphStart < 0 {
			continue
		}
		for index, bodyChild := range bodyChildren {
			if bodyChild[0] == paragraphStart {
				anchors = append(anchors, index)
				break
			}
		}
	}
	return anchors
}

// drawingAnchorIndex returns the body paragraph that owns a drawing. Images
// are separate AST objects, but their parent paragraph preserves the OOXML
// relationship needed to map visual findings back to a document location.
func drawingAnchorIndex(documentXML string, drawingStart int) int {
	paragraphStart := strings.LastIndex(documentXML[:drawingStart], "<w:p")
	if paragraphStart < 0 {
		return -1
	}
	for index, bodyChild := range bodyChildPattern.FindAllStringIndex(documentXML, -1) {
		if bodyChild[0] == paragraphStart {
			return index
		}
	}
	return -1
}

func filepathBase(name string) string {
	parts := strings.Split(strings.ReplaceAll(name, "\\", "/"), "/")
	return parts[len(parts)-1]
}

func ExtractDocumentXML(documentXML string) Snapshot {
	snapshot := Snapshot{
		Version:  Version,
		Source:   "word/document.xml",
		Sections: extractPageSections(documentXML),
		Nodes:    []Node{},
	}
	sectionID := "cover"
	coverDateSeen := false
	coverTitleSeen := false
	abstractEnglish := false
	fallbackIDOccurrences := map[string]int{}
	pageSection := 1
	matches := bodyChildPattern.FindAllStringIndex(documentXML, -1)
	textboxRanges := textboxPattern.FindAllStringIndex(documentXML, -1)
	for index, match := range matches {
		// The non-nesting paragraph matcher sees a <w:p> inside a text box as
		// another body child after the drawing anchor. It is an instruction node
		// created by appendNonBodyParts, never a second body paragraph.
		insideTextbox := false
		textSource := documentXML[match[0]:match[1]]
		for _, textbox := range textboxRanges {
			if match[0] >= textbox[0] && match[0] < textbox[1] {
				insideTextbox = true
				break
			}
			if match[0] < textbox[1] && match[1] > textbox[0] {
				start, end := 0, len(textSource)
				if textbox[0] > match[0] {
					start = textbox[0] - match[0]
				}
				if textbox[1] < match[1] {
					end = textbox[1] - match[0]
				}
				textSource = textSource[:start] + textSource[end:]
			}
		}
		if insideTextbox {
			continue
		}
		raw := documentXML[match[0]:match[1]]
		nodeType := detectNodeType(raw)
		// A drawing's <w:txbxContent> is nested inside its anchor paragraph.
		// Its text is instruction evidence, not thesis body text.  Extract it
		// separately below so it cannot receive a body role or a format plan.
		text := extractText(textboxPattern.ReplaceAllString(textSource, ""))
		styleID := extractStyleID(raw)
		role, level, _, evidence := classify(nodeType, text)
		if nodeType == "paragraph" && ommlPattern.MatchString(raw) {
			role, level, evidence = "formula", 0, []string{"ooxml:omml_formula"}
		}
		if nodeType == "paragraph" && sectionID == "cover" {
			switch {
			case isCoverTitleText(text):
				role, level, evidence = "cover_title", 0, []string{"cover:title"}
				coverTitleSeen = true
			case isCoverDateText(text):
				role, level, evidence = "cover_date", 0, []string{"cover:date"}
				coverDateSeen = true
			case coverDateSeen && coverTitleSeen && likelyCoverThesisTitle(text):
				role, level, evidence = "cover_title", 0, []string{"cover:inner_thesis_title"}
			case coverDateSeen && likelyCoverThesisTitle(text):
				role, level, evidence = "title", 0, []string{"cover:inner_thesis_title"}
			}
		}
		if nodeType == "paragraph" && role == "body_paragraph" {
			trimmed := strings.TrimSpace(text)
			switch {
			case correctChapterPattern.MatchString(trimmed):
				role, level, evidence = "heading", 1, []string{"regex:chinese_chapter_heading"}
			case correctChineseListPattern.MatchString(trimmed):
				role, level, evidence = "heading", 1, []string{"regex:chinese_list_heading"}
			}
		}
		if nodeType == "paragraph" && (role == "body_paragraph" || role == "heading") {
			if outline, ok := extractOutlineLevel(raw); ok {
				role, level, evidence = "heading", outline+1, []string{"ooxml:outline_level"}
			}
		}
		if nodeType == "paragraph" && sectionID == "toc" {
			if tocLevel, ok := tocStyleLevel(styleID); ok {
				role, level, evidence = "toc_entry", tocLevel, []string{"ooxml:toc_style"}
			}
		}
		if nodeType == "paragraph" && role == "body_paragraph" && chapterPattern.MatchString(strings.TrimSpace(text)) {
			role, level, evidence = "heading", 1, []string{"regex:chinese_chapter_heading"}
		} else if nodeType == "paragraph" && role == "body_paragraph" && chineseListPattern.MatchString(strings.TrimSpace(text)) {
			role, level, evidence = "heading", 1, []string{"regex:chinese_list_heading"}
		} else if nodeType == "paragraph" && role == "body_paragraph" {
			if match := compactNumberedHeadingPattern.FindStringSubmatch(strings.TrimSpace(text)); len(match) == 3 && likelyCompactHeadingTitle(match[2]) {
				level = strings.Count(match[1], ".") + 1
				if level == 1 && regexp.MustCompile(`^\d+[.．、]`).MatchString(strings.TrimSpace(text)) {
					level = 2
				}
				role, evidence = "heading", []string{"regex:compact_numbered_heading"}
			}
		}
		// Once the references heading has opened its section, keep its entries
		// distinct from ordinary body paragraphs so the formatter can apply the
		// reference-item rule (font, size and hanging indent) deterministically.
		if nodeType == "paragraph" && sectionID == "references" && role == "body_paragraph" {
			role, evidence = "references", []string{"state:references_section"}
		}
		// Section state identifies the object independently of its current font
		// and size. A malformed abstract or acknowledgement is still recognised
		// as such before compliance decides whether it needs repair.
		if nodeType == "paragraph" && role == "body_paragraph" {
			switch sectionID {
			case "abstract":
				if abstractEnglish {
					role, evidence = "abstract_en_body", []string{"state:abstract_en_section"}
				} else {
					role, evidence = "abstract_body", []string{"state:abstract_section"}
				}
			case "acknowledgements":
				role, evidence = "acknowledgements", []string{"state:acknowledgements_section"}
			case "appendix":
				role, evidence = "appendix", []string{"state:appendix_section"}
			case "toc":
				if tocEntryPattern.MatchString(strings.TrimSpace(text)) {
					role, evidence = "toc_entry", []string{"regex:toc_dot_leader_page"}
				}
			}
		}
		confidence := confidenceFor(role, nodeType, text, styleID, evidence)
		if role == "abstract_en" {
			abstractEnglish = true
			sectionID = "abstract"
		} else if role == "abstract_cn" || role == "abstract_title" {
			abstractEnglish = false
			sectionID = "abstract"
		} else if role == "toc_title" {
			sectionID = "toc"
		} else if role == "heading" && level == 1 {
			sectionID = "body"
		} else if role == "references_title" {
			sectionID = "references"
		} else if role == "acknowledgements_title" {
			sectionID = "acknowledgements"
		} else if role == "appendix_title" {
			sectionID = "appendix"
		}
		paraID := extractParaID(raw)
		nodeID := fmt.Sprintf("p:%06d", index)
		if paraID != "" {
			nodeID = "p:" + strings.ToUpper(paraID)
		} else {
			// Direct formatting is intentionally mutable. Deriving a node identity
			// from the whole paragraph XML made every style repair look like a lost
			// node. Visible text plus its duplicate occurrence remains stable while
			// paragraph/run properties are changed by UniOffice or normalization.
			identity := nodeType + "\x00" + strings.Join(strings.Fields(text), " ")
			fallbackIDOccurrences[identity]++
			digest := sha256.Sum256([]byte(identity))
			nodeID = fmt.Sprintf("p:%x:%03d", digest[:6], fallbackIDOccurrences[identity])
		}
		node := Node{
			NodeID:          nodeID,
			OfficePath:      fmt.Sprintf("/body/p[%d]", index+1),
			ParaID:          paraID,
			SourcePart:      "word/document.xml",
			Index:           index,
			NodeType:        nodeType,
			Text:            text,
			SemanticRole:    role,
			LogicalLevel:    level,
			CurrentStyle:    styleID,
			SectionID:       sectionID,
			Confidence:      confidence,
			Evidence:        evidence,
			BeforeTwips:     spacingValue(raw, "w:before"),
			AfterTwips:      spacingValue(raw, "w:after"),
			LineTwips:       spacingValue(raw, "w:line"),
			PageBreakBefore: pageBreakBeforePattern.MatchString(raw),
			PageSectionID:   fmt.Sprintf("section:%d", pageSection),
		}
		snapshot.Nodes = append(snapshot.Nodes, withRawMetadata(node, raw))
		switch nodeType {
		case "paragraph":
			snapshot.Stats.Paragraphs++
			if role == "blank" {
				snapshot.Stats.BlankParagraphs++
			}
		case "table":
			snapshot.Stats.Tables++
		}
		if role == "heading" {
			snapshot.Stats.Headings++
		} else if role == "formula" {
			snapshot.Stats.Formulas++
		}
		if sectionPropertiesPattern.MatchString(raw) {
			pageSection++
		}
	}
	annotateTOCMatches(&snapshot)
	return snapshot
}

func extractPageSections(documentXML string) []PageSection {
	rawSections := sectionPropertiesPattern.FindAllString(documentXML, -1)
	sections := make([]PageSection, 0, len(rawSections))
	for index, raw := range rawSections {
		section := PageSection{ID: fmt.Sprintf("section:%d", index+1)}
		if pageSize := pageSizePattern.FindString(raw); pageSize != "" {
			section.PageWidthTwips = ooxmlAttribute(pageSize, "w")
			section.PageHeightTwips = ooxmlAttribute(pageSize, "h")
			section.Orientation = ooxmlAttribute(pageSize, "orient")
		}
		if margins := pageMarginsPattern.FindString(raw); margins != "" {
			section.MarginTopTwips = ooxmlAttribute(margins, "top")
			section.MarginRightTwips = ooxmlAttribute(margins, "right")
			section.MarginBottomTwips = ooxmlAttribute(margins, "bottom")
			section.MarginLeftTwips = ooxmlAttribute(margins, "left")
			section.HeaderMarginTwips = ooxmlAttribute(margins, "header")
			section.FooterMarginTwips = ooxmlAttribute(margins, "footer")
		}
		if pageNumbers := pageNumberingPattern.FindString(raw); pageNumbers != "" {
			section.PageNumberFormat = ooxmlAttribute(pageNumbers, "fmt")
			section.PageNumberStart = ooxmlAttribute(pageNumbers, "start")
		}
		sections = append(sections, section)
	}
	return sections
}

func ooxmlAttribute(raw, name string) string {
	match := regexp.MustCompile(`\bw:` + regexp.QuoteMeta(name) + `="([^"]*)"`).FindStringSubmatch(raw)
	if len(match) == 2 {
		return match[1]
	}
	return ""
}

// annotateTOCMatches adds a deterministic cross-reference from a real body
// heading to the same entry in the document's TOC. The TOC itself remains a
// separate role; this is only a role-recognition signal for the body heading.
func annotateTOCMatches(snapshot *Snapshot) {
	if snapshot == nil {
		return
	}
	tocEntries := make(map[string]bool)
	for _, node := range snapshot.Nodes {
		// Some Word TOC fields do not expose dot leaders as visible text. The
		// section boundary is still deterministic, so include non-title TOC
		// paragraphs in the lookup without ever relabelling them as body text.
		if node.SemanticRole == "toc_entry" || (node.SectionID == "toc" && node.SemanticRole != "toc_title") {
			if key := tocComparableText(node.Text); key != "" {
				tocEntries[key] = true
			}
		}
	}
	if len(tocEntries) == 0 {
		return
	}
	for index := range snapshot.Nodes {
		node := &snapshot.Nodes[index]
		if node.SemanticRole != "heading" || node.SectionID != "body" {
			continue
		}
		if tocEntries[tocComparableText(node.Text)] {
			node.Evidence = append(node.Evidence, "toc:matched_entry")
		}
	}
}

func tocComparableText(text string) string {
	text = tocEntryPattern.ReplaceAllString(strings.TrimSpace(text), "")
	return strings.Map(func(r rune) rune {
		if (r >= '0' && r <= '9') || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '\u4e00' && r <= '\u9fff') {
			return r
		}
		return -1
	}, text)
}

func isCoverTitleText(text string) bool {
	trimmed := strings.TrimSpace(text)
	if strings.Contains(trimmed, "\u539f\u521b\u6027") || strings.Contains(trimmed, "\u4f5c\u8005") || len([]rune(trimmed)) > 32 {
		return false
	}
	return strings.Contains(trimmed, "\u6bd5\u4e1a\u8bba\u6587") || strings.Contains(trimmed, "\u6bd5\u4e1a\u8bbe\u8ba1") ||
		strings.Contains(trimmed, "\u5b66\u58eb\u5b66\u4f4d") || strings.Contains(trimmed, "\u7855\u58eb\u5b66\u4f4d") || strings.Contains(trimmed, "\u535a\u58eb\u5b66\u4f4d")
}

func isCoverDateText(text string) bool {
	trimmed := strings.TrimSpace(text)
	return len([]rune(trimmed)) <= 20 && strings.Contains(trimmed, "\u5e74") && strings.Contains(trimmed, "\u6708")
}

func likelyCoverThesisTitle(text string) bool {
	trimmed := strings.TrimSpace(text)
	if len([]rune(trimmed)) <= 20 || strings.HasPrefix(trimmed, "\u6458\u8981") || strings.HasPrefix(strings.ToLower(trimmed), "abstract") {
		return false
	}
	lower := strings.ToLower(trimmed)
	return !strings.Contains(trimmed, "\u5173\u952e\u8bcd") && !strings.HasPrefix(lower, "keywords") && !strings.HasPrefix(lower, "key words")
}

func likelyCompactHeadingTitle(title string) bool {
	title = strings.TrimSpace(title)
	return title != "" && len([]rune(title)) <= 32 && !strings.ContainsAny(title, "。！？；;，,")
}

func Marshal(snapshot Snapshot) string {
	data, err := json.Marshal(snapshot)
	if err != nil {
		return "{}"
	}
	return string(data)
}

func Parse(data string) (Snapshot, error) {
	if strings.TrimSpace(data) == "" {
		return Snapshot{Version: Version, Source: "word/document.xml"}, nil
	}
	var snapshot Snapshot
	if err := json.Unmarshal([]byte(data), &snapshot); err != nil {
		return Snapshot{}, err
	}
	return snapshot, nil
}

func Validate(snapshot Snapshot) []ValidationIssue {
	var issues []ValidationIssue
	if snapshot.Version != Version {
		issues = append(issues, ValidationIssue{Kind: "paper_ast_version", Message: "paper AST version is missing or unsupported"})
	}
	if len(snapshot.Nodes) == 0 {
		issues = append(issues, ValidationIssue{Kind: "paper_ast_empty", Message: "paper AST contains no document nodes"})
		return issues
	}
	for _, node := range snapshot.Nodes {
		if strings.TrimSpace(node.NodeID) == "" || strings.TrimSpace(node.SourcePart) == "" {
			issues = append(issues, ValidationIssue{Kind: "paper_ast_node_identity", Message: "paper AST node identity is incomplete"})
			break
		}
		if strings.TrimSpace(node.SemanticRole) == "" {
			issues = append(issues, ValidationIssue{Kind: "paper_ast_semantic_role", Message: "paper AST node semantic role is missing"})
			break
		}
	}
	return issues
}

func detectNodeType(raw string) string {
	match := nodeTypePattern.FindStringSubmatch(raw)
	if len(match) < 2 {
		return "unknown"
	}
	if match[1] == "tbl" {
		return "table"
	}
	return "paragraph"
}

func extractText(raw string) string {
	raw = deletedPattern.ReplaceAllString(raw, "")
	var builder strings.Builder
	for _, match := range textPattern.FindAllStringSubmatch(raw, -1) {
		if len(match) > 1 {
			builder.WriteString(html.UnescapeString(match[1]))
		}
	}
	for _, match := range mathTextPattern.FindAllStringSubmatch(raw, -1) {
		if len(match) > 1 {
			builder.WriteString(html.UnescapeString(match[1]))
		}
	}
	return strings.TrimSpace(builder.String())
}

// withRawMetadata preserves OOXML-only structure separately from visible
// paragraph text. It lets callers distinguish a TOC/PAGE field or a link from
// ordinary prose without asking an LLM to infer it from flattened text.
func withRawMetadata(node Node, raw string) Node {
	node.PageBreakBefore = node.PageBreakBefore || pageBreakBeforePattern.MatchString(raw)
	node.FieldCodes = extractFieldCodes(raw)
	node.BookmarkNames = extractAttributeValues(bookmarkNamePattern, raw)
	node.HyperlinkIDs = extractAttributeValues(hyperlinkIDPattern, raw)
	return node
}

func extractFieldCodes(raw string) []string {
	values := make([]string, 0)
	for _, match := range instrTextPattern.FindAllStringSubmatch(raw, -1) {
		if len(match) < 2 {
			continue
		}
		if code := strings.Join(strings.Fields(html.UnescapeString(match[1])), " "); code != "" {
			values = append(values, code)
		}
	}
	return values
}

func extractAttributeValues(pattern *regexp.Regexp, raw string) []string {
	values := make([]string, 0)
	for _, match := range pattern.FindAllStringSubmatch(raw, -1) {
		if len(match) == 2 && strings.TrimSpace(match[1]) != "" {
			values = append(values, match[1])
		}
	}
	return values
}

func extractParaID(raw string) string {
	match := paraIDPattern.FindStringSubmatch(raw)
	if len(match) == 2 {
		return match[1]
	}
	return ""
}

func extractStyleID(raw string) string {
	match := stylePattern.FindStringSubmatch(raw)
	if len(match) < 2 {
		return ""
	}
	return match[1]
}

func classify(nodeType string, text string) (string, int, float64, []string) {
	trimmed := strings.TrimSpace(text)
	compact := strings.NewReplacer(" ", "", "\t", "", "\u00a0", "", "　", "").Replace(trimmed)
	lower := strings.ToLower(trimmed)
	if nodeType == "table" {
		return "table", 0, 0.90, []string{"ooxml:w:tbl"}
	}
	if trimmed == "" {
		return "blank", 0, 0.99, []string{"empty_text"}
	}
	correctCompact := strings.NewReplacer(" ", "", "\t", "", "\u00a0", "", "\u3000", "").Replace(trimmed)
	switch {
	case strings.Contains(correctCompact, "\u539f\u521b\u6027\u58f0\u660e") || strings.Contains(correctCompact, "\u539f\u521b\u6027\u7533\u660e") || strings.Contains(correctCompact, "\u5b66\u672f\u8bda\u4fe1\u58f0\u660e"):
		return "originality_declaration", 0, 0.98, []string{"keyword:originality_declaration"}
	case correctCompact == "\u76ee\u5f55" || correctCompact == "\u76ee\u6b21":
		return "toc_title", 0, 0.98, []string{"keyword:toc"}
	case correctCompact == "\u6458\u8981":
		return "abstract_title", 0, 0.98, []string{"keyword:abstract_title"}
	case strings.HasPrefix(correctCompact, "\u6458\u8981"):
		return "abstract_cn", 0, 0.96, []string{"keyword:abstract_cn"}
	case strings.HasPrefix(correctCompact, "\u5173\u952e\u8bcd"):
		return "keywords_cn", 0, 0.96, []string{"keyword:keywords_cn"}
	case correctCompact == "\u53c2\u8003\u6587\u732e":
		return "references_title", 0, 0.98, []string{"keyword:references"}
	case correctCompact == "\u81f4\u8c22":
		return "acknowledgements_title", 0, 0.98, []string{"keyword:acknowledgements"}
	case correctTableCaptionPattern.MatchString(trimmed), continuedTableCaptionPattern.MatchString(trimmed):
		return "table_caption", 0, 0.90, []string{"regex:table_caption"}
	case correctFigureCaptionPattern.MatchString(trimmed):
		return "figure_caption", 0, 0.90, []string{"regex:figure_caption"}
	case appendixTitlePattern.MatchString(trimmed):
		return "appendix_title", 0, 0.96, []string{"regex:appendix_title"}
	}
	switch {
	case strings.Contains(compact, "\u539f\u521b\u6027\u58f0\u660e") || strings.Contains(compact, "\u539f\u521b\u6027\u7533\u660e") || strings.Contains(compact, "\u5b66\u672f\u8bda\u4fe1\u58f0\u660e"):
		return "originality_declaration", 0, 0.98, []string{"keyword:\u539f\u521b\u6027\u58f0\u660e"}
	case compact == "目录":
		return "toc_title", 0, 0.98, []string{"keyword:目录"}
	case strings.HasPrefix(compact, "摘要"):
		return "abstract_cn", 0, 0.96, []string{"keyword:摘要"}
	case strings.HasPrefix(lower, "abstract"):
		return "abstract_en", 0, 0.96, []string{"keyword:abstract"}
	case strings.HasPrefix(compact, "关键词"):
		return "keywords_cn", 0, 0.96, []string{"keyword:关键词"}
	case strings.HasPrefix(lower, "keywords") || strings.HasPrefix(lower, "key words"):
		return "keywords_en", 0, 0.96, []string{"keyword:keywords"}
	case compact == "参考文献":
		return "references_title", 0, 0.98, []string{"keyword:参考文献"}
	case compact == "致谢":
		return "acknowledgements_title", 0, 0.98, []string{"keyword:致谢"}
	case regexp.MustCompile(`^(?:续\s*)?表\s*\d+(\.\d+|-?\d*)?\s*\S*`).MatchString(trimmed):
		return "table_caption", 0, 0.90, []string{"regex:table_caption"}
	case regexp.MustCompile(`^图\d+(\.\d+|-?\d*)?\s*\S*`).MatchString(trimmed):
		return "figure_caption", 0, 0.90, []string{"regex:figure_caption"}
	}
	if match := headingPattern.FindStringSubmatch(trimmed); len(match) == 2 {
		level := strings.Count(match[1], ".") + 1
		return "heading", level, 0.95, []string{"regex:decimal_heading"}
	}
	if match := compactHeadingPattern.FindStringSubmatch(trimmed); len(match) == 3 {
		level := strings.Count(match[1], ".") + 1
		return "heading", level, 0.95, []string{"regex:compact_decimal_heading"}
	}
	return "body_paragraph", 0, 0.75, []string{"fallback:non_empty_paragraph"}
}

func extractOutlineLevel(raw string) (int, bool) {
	match := outlinePattern.FindStringSubmatch(raw)
	if len(match) != 2 {
		return 0, false
	}
	level := 0
	if _, err := fmt.Sscanf(match[1], "%d", &level); err != nil || level < 0 || level > 8 {
		return 0, false
	}
	return level, true
}

func tocStyleLevel(styleID string) (int, bool) {
	normalized := strings.ToLower(strings.NewReplacer(" ", "", "_", "", "-", "").Replace(strings.TrimSpace(styleID)))
	if !strings.HasPrefix(normalized, "toc") {
		return 0, false
	}
	level := 0
	if _, err := fmt.Sscanf(strings.TrimPrefix(normalized, "toc"), "%d", &level); err != nil || level < 1 || level > 9 {
		return 0, false
	}
	return level, true
}

func confidenceFor(role, nodeType, text, styleID string, evidence []string) float64 {
	if nodeType == "table" || role == "blank" {
		return 0.99
	}
	if role == "formula" || role == "appendix_title" {
		return 0.99
	}
	score := 0.55
	if len(evidence) > 0 && strings.HasPrefix(evidence[0], "keyword:") {
		score = 0.88
		if !strings.ContainsAny(strings.TrimSpace(text), ":：;；") {
			score += 0.07
		}
	} else if len(evidence) > 0 && strings.HasPrefix(evidence[0], "regex:") {
		score = 0.82
	}
	if styleID != "" && (strings.Contains(strings.ToLower(styleID), "heading") || strings.Contains(styleID, "标题")) {
		score += 0.12
	}
	if score > 0.99 {
		return 0.99
	}
	return score
}

func spacingValue(raw, name string) int {
	spacing := spacingPattern.FindString(raw)
	for _, match := range attributePattern.FindAllStringSubmatch(spacing, -1) {
		if len(match) == 3 && match[1] == name {
			value := 0
			_, _ = fmt.Sscanf(match[2], "%d", &value)
			return value
		}
	}
	return 0
}
