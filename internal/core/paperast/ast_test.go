package paperast

import (
	"strings"
	"testing"
)

func TestExtractDocumentXMLBuildsSemanticAST(t *testing.T) {
	xml := `<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body>` +
		`<w:p><w:r><w:t>封面</w:t></w:r></w:p>` +
		`<w:p><w:pPr><w:pStyle w:val="AbstractCN"/></w:pPr><w:r><w:t>摘要：目的</w:t></w:r></w:p>` +
		`<w:p><w:r><w:t>关键词：护理；糖尿病</w:t></w:r></w:p>` +
		`<w:p><w:r><w:t>目录</w:t></w:r></w:p>` +
		`<w:p><w:pPr><w:pStyle w:val="Heading1"/></w:pPr><w:r><w:t>1 绪论</w:t></w:r></w:p>` +
		`<w:p><w:r><w:t>1.1 研究背景</w:t></w:r></w:p>` +
		`<w:tbl><w:tr><w:tc><w:p><w:r><w:t>数据</w:t></w:r></w:p></w:tc></w:tr></w:tbl>` +
		`<w:p><w:r><w:t>参考文献</w:t></w:r></w:p>` +
		`<w:p><w:r><w:t>致谢</w:t></w:r></w:p>` +
		`</w:body></w:document>`

	snapshot := ExtractDocumentXML(xml)

	if snapshot.Version != Version {
		t.Fatalf("Version = %s, want %s", snapshot.Version, Version)
	}
	if snapshot.Stats.Paragraphs != 8 || snapshot.Stats.Tables != 1 || snapshot.Stats.Headings != 2 {
		t.Fatalf("unexpected stats: %#v", snapshot.Stats)
	}
	assertRole(t, snapshot, "摘要：目的", "abstract_cn", "abstract")
	assertRole(t, snapshot, "1.1 研究背景", "heading", "body")
	assertRole(t, snapshot, "参考文献", "references_title", "references")
	assertRole(t, snapshot, "致谢", "acknowledgements_title", "acknowledgements")
	if snapshot.Nodes[4].LogicalLevel != 1 || snapshot.Nodes[5].LogicalLevel != 2 {
		t.Fatalf("heading levels not detected: %#v %#v", snapshot.Nodes[4], snapshot.Nodes[5])
	}
	if issues := Validate(snapshot); len(issues) != 0 {
		t.Fatalf("Validate() issues = %#v, want none", issues)
	}
}

func TestValidateRejectsEmptyAST(t *testing.T) {
	issues := Validate(Snapshot{Version: Version})

	if len(issues) == 0 {
		t.Fatal("Validate() issues = nil, want empty AST issue")
	}
}

func TestExtractDocumentXMLAcceptsInsertedTextAndDropsDeletedText(t *testing.T) {
	xml := `<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body>` +
		`<w:p><w:r><w:t>Before </w:t></w:r>` +
		`<w:ins><w:r><w:t>accepted source text</w:t></w:r></w:ins>` +
		`<w:del><w:r><w:t>deleted normal text</w:t></w:r><w:r><w:delText>deleted tracked text</w:delText></w:r></w:del>` +
		`<w:moveFrom><w:r><w:t>moved away text</w:t></w:r></w:moveFrom>` +
		`<w:r><w:t> After</w:t></w:r></w:p>` +
		`</w:body></w:document>`

	snapshot := ExtractDocumentXML(xml)

	if len(snapshot.Nodes) != 1 {
		t.Fatalf("nodes = %d, want 1", len(snapshot.Nodes))
	}
	text := snapshot.Nodes[0].Text
	if !strings.Contains(text, "Before accepted source text After") {
		t.Fatalf("extracted text = %q, want accepted inserted text", text)
	}
	for _, forbidden := range []string{"deleted normal text", "deleted tracked text", "moved away text"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("extracted text contains rejected review text %q: %q", forbidden, text)
		}
	}
}

func assertRole(t *testing.T, snapshot Snapshot, text string, role string, section string) {
	t.Helper()
	for _, node := range snapshot.Nodes {
		if node.Text == text {
			if node.SemanticRole != role || node.SectionID != section {
				t.Fatalf("%q role/section = %s/%s, want %s/%s", text, node.SemanticRole, node.SectionID, role, section)
			}
			return
		}
	}
	t.Fatalf("node %q not found in %#v", text, snapshot.Nodes)
}

func TestExtractDocumentXMLRecognizesChineseHeadingsAndSpacing(t *testing.T) {
	xml := `<w:document><w:body><w:p><w:pPr><w:spacing w:before="240" w:after="120" w:line="360"/></w:pPr><w:r><w:t>第一章 绪论</w:t></w:r></w:p><w:p><w:r/></w:p><w:p><w:r><w:t>一、研究背景</w:t></w:r></w:p></w:body></w:document>`
	snapshot := ExtractDocumentXML(xml)
	if snapshot.Stats.Headings != 2 || snapshot.Stats.BlankParagraphs != 1 {
		t.Fatalf("unexpected stats: %#v", snapshot.Stats)
	}
	if snapshot.Nodes[0].LogicalLevel != 1 || snapshot.Nodes[0].BeforeTwips != 240 || snapshot.Nodes[0].AfterTwips != 120 || snapshot.Nodes[0].LineTwips != 360 {
		t.Fatalf("chapter metadata not extracted: %#v", snapshot.Nodes[0])
	}
}
