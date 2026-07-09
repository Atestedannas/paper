package paperast

import (
	"encoding/json"
	"fmt"
	"html"
	"regexp"
	"strings"

	"github.com/paper-format-checker/backend/internal/core/ooxmlpkg"
)

const Version = "paper-ast-v1"

type Snapshot struct {
	Version string `json:"version"`
	Source  string `json:"source"`
	Stats   Stats  `json:"stats"`
	Nodes   []Node `json:"nodes"`
}

type Stats struct {
	Paragraphs int `json:"paragraphs"`
	Tables     int `json:"tables"`
	Headings   int `json:"headings"`
}

type Node struct {
	NodeID       string   `json:"node_id"`
	SourcePart   string   `json:"source_part"`
	Index        int      `json:"index"`
	NodeType     string   `json:"node_type"`
	Text         string   `json:"text,omitempty"`
	SemanticRole string   `json:"semantic_role"`
	LogicalLevel int      `json:"logical_level,omitempty"`
	CurrentStyle string   `json:"current_style_id,omitempty"`
	SectionID    string   `json:"section_id"`
	Confidence   float64  `json:"confidence"`
	Evidence     []string `json:"evidence,omitempty"`
}

type ValidationIssue struct {
	Kind    string `json:"kind"`
	Message string `json:"message"`
}

var (
	bodyChildPattern = regexp.MustCompile(`(?s)<w:p(?:\s[^>]*)?>.*?</w:p>|<w:tbl(?:\s[^>]*)?>.*?</w:tbl>`)
	nodeTypePattern  = regexp.MustCompile(`^<w:(p|tbl)\b`)
	textPattern      = regexp.MustCompile(`(?s)<w:t\b[^>]*>(.*?)</w:t>`)
	deletedPattern   = regexp.MustCompile(`(?s)<w:(?:del|moveFrom)\b[^>]*>.*?</w:(?:del|moveFrom)>`)
	stylePattern     = regexp.MustCompile(`<w:pStyle\b[^>]*\bw:val="([^"]+)"`)
	headingPattern   = regexp.MustCompile(`^(\d+(?:\.\d+){0,3})\s+\S+`)
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
	return ExtractDocumentXML(string(documentXML)), nil
}

func ExtractDocumentXML(documentXML string) Snapshot {
	snapshot := Snapshot{
		Version: Version,
		Source:  "word/document.xml",
		Nodes:   []Node{},
	}
	sectionID := "cover"
	matches := bodyChildPattern.FindAllString(documentXML, -1)
	for index, raw := range matches {
		nodeType := detectNodeType(raw)
		text := extractText(raw)
		role, level, confidence, evidence := classify(nodeType, text)
		if role == "abstract_cn" || role == "abstract_en" {
			sectionID = "abstract"
		} else if role == "toc_title" {
			sectionID = "toc"
		} else if role == "heading" && level == 1 {
			sectionID = "body"
		} else if role == "references_title" {
			sectionID = "references"
		} else if role == "acknowledgements_title" {
			sectionID = "acknowledgements"
		}
		node := Node{
			NodeID:       fmt.Sprintf("n_%06d", index),
			SourcePart:   "word/document.xml",
			Index:        index,
			NodeType:     nodeType,
			Text:         text,
			SemanticRole: role,
			LogicalLevel: level,
			CurrentStyle: extractStyleID(raw),
			SectionID:    sectionID,
			Confidence:   confidence,
			Evidence:     evidence,
		}
		snapshot.Nodes = append(snapshot.Nodes, node)
		switch nodeType {
		case "paragraph":
			snapshot.Stats.Paragraphs++
		case "table":
			snapshot.Stats.Tables++
		}
		if role == "heading" {
			snapshot.Stats.Headings++
		}
	}
	return snapshot
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
	return strings.TrimSpace(builder.String())
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
	switch {
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
	case regexp.MustCompile(`^表\d+(\.\d+|-?\d*)?\s*\S*`).MatchString(trimmed):
		return "table_caption", 0, 0.90, []string{"regex:table_caption"}
	case regexp.MustCompile(`^图\d+(\.\d+|-?\d*)?\s*\S*`).MatchString(trimmed):
		return "figure_caption", 0, 0.90, []string{"regex:figure_caption"}
	}
	if match := headingPattern.FindStringSubmatch(trimmed); len(match) == 2 {
		level := strings.Count(match[1], ".") + 1
		return "heading", level, 0.95, []string{"regex:decimal_heading"}
	}
	return "body_paragraph", 0, 0.75, []string{"fallback:non_empty_paragraph"}
}
