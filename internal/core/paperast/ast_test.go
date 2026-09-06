package paperast

import (
	"archive/zip"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExtractSeparatesHeaderFooterAndInstructionTextbox(t *testing.T) {
	path := filepath.Join(t.TempDir(), "parts.docx")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	entries := map[string]string{
		"word/document.xml":  `<w:document><w:body><w:p><w:r><w:t>正文</w:t></w:r></w:p><w:tbl><w:tr><w:tc><w:p><w:r><w:t>规范证据</w:t></w:r></w:p></w:tc></w:tr></w:tbl><w:p><w:r><w:txbxContent><w:p><w:r><w:t>空一行</w:t></w:r></w:p></w:txbxContent></w:r></w:p><w:p><w:r><w:drawing><wp:inline/></w:drawing></w:r></w:p></w:body></w:document>`,
		"word/header1.xml":   `<w:hdr><w:p><w:r><w:t>学校页眉</w:t></w:r></w:p></w:hdr>`,
		"word/footer1.xml":   `<w:ftr><w:p><w:r><w:fldChar w:fldCharType="begin"/></w:r><w:r><w:instrText> PAGE </w:instrText></w:r><w:r><w:fldChar w:fldCharType="end"/></w:r></w:p></w:ftr>`,
		"word/footnotes.xml": `<w:footnotes><w:footnote><w:p><w:r><w:t>脚注文字</w:t></w:r></w:p></w:footnote></w:footnotes>`,
		"word/endnotes.xml":  `<w:endnotes><w:endnote><w:p><w:r><w:t>尾注文字</w:t></w:r></w:p></w:endnote></w:endnotes>`,
	}
	for name, text := range entries {
		w, e := zw.Create(name)
		if e != nil {
			t.Fatal(e)
		}
		if _, e = w.Write([]byte(text)); e != nil {
			t.Fatal(e)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	snapshot, err := Extract(path)
	if err != nil {
		t.Fatal(err)
	}
	roles := map[string]bool{}
	for _, node := range snapshot.Nodes {
		roles[node.SemanticRole] = true
	}
	for _, role := range []string{"header", "footer", "footnote", "endnote", "instruction_textbox", "table_cell", "image"} {
		if !roles[role] {
			t.Fatalf("missing separated role %q: %#v", role, snapshot.Nodes)
		}
	}
	if snapshot.Stats.Headers != 1 || snapshot.Stats.Footers != 1 || snapshot.Stats.Footnotes != 1 || snapshot.Stats.Endnotes != 1 || snapshot.Stats.Textboxes != 1 || snapshot.Stats.Images != 1 {
		t.Fatalf("unexpected auxiliary stats: %#v", snapshot.Stats)
	}
	for _, node := range snapshot.Nodes {
		if node.NodeType == "instruction_textbox" && node.ParentNodeID == "" {
			t.Fatalf("instruction textbox lost its OOXML anchor: %#v", node)
		}
		if node.SemanticRole == "body_paragraph" && strings.Contains(node.Text, "空一行") {
			t.Fatalf("instruction textbox leaked into body role: %#v", node)
		}
	}
	for _, node := range snapshot.Nodes {
		if node.SemanticRole == "footer" && len(node.FieldCodes) == 1 && node.FieldCodes[0] == "PAGE" {
			return
		}
	}
	t.Fatalf("footer PAGE field was not preserved: %#v", snapshot.Nodes)
}

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

func TestFallbackNodeIDSurvivesDirectStyleRepair(t *testing.T) {
	before := `<w:document><w:body><w:p><w:pPr><w:jc w:val="left"/></w:pPr><w:r><w:rPr><w:sz w:val="24"/></w:rPr><w:t>Stable visible content</w:t></w:r></w:p></w:body></w:document>`
	after := `<w:document><w:body><w:p><w:pPr><w:jc w:val="center"/><w:spacing w:line="400" w:lineRule="exact"/></w:pPr><w:r><w:rPr><w:sz w:val="32"/><w:b/></w:rPr><w:t>Stable visible content</w:t></w:r></w:p></w:body></w:document>`
	beforeNodes := ExtractDocumentXML(before).Nodes
	afterNodes := ExtractDocumentXML(after).Nodes
	if len(beforeNodes) != 1 || len(afterNodes) != 1 {
		t.Fatalf("unexpected AST sizes before=%d after=%d", len(beforeNodes), len(afterNodes))
	}
	if beforeNodes[0].NodeID != afterNodes[0].NodeID {
		t.Fatalf("format-only change changed fallback node id: %q != %q", beforeNodes[0].NodeID, afterNodes[0].NodeID)
	}
}

func TestExtractDocumentXMLPreservesFieldsBookmarksAndHyperlinks(t *testing.T) {
	snapshot := ExtractDocumentXML(`<w:document><w:body><w:p><w:bookmarkStart w:name="chapter-one"/><w:hyperlink r:id="rId7"><w:r><w:t>Chapter one</w:t></w:r></w:hyperlink><w:r><w:instrText> TOC \o "1-3" </w:instrText></w:r></w:p></w:body></w:document>`)
	if len(snapshot.Nodes) != 1 {
		t.Fatalf("nodes=%#v", snapshot.Nodes)
	}
	node := snapshot.Nodes[0]
	if len(node.FieldCodes) != 1 || node.FieldCodes[0] != `TOC \o "1-3"` || len(node.BookmarkNames) != 1 || node.BookmarkNames[0] != "chapter-one" || len(node.HyperlinkIDs) != 1 || node.HyperlinkIDs[0] != "rId7" {
		t.Fatalf("OOXML metadata=%#v", node)
	}
}

func TestExtractDocumentXMLPreservesPageBreakBefore(t *testing.T) {
	snapshot := ExtractDocumentXML(`<w:document><w:body><w:p><w:pPr><w:pageBreakBefore/></w:pPr><w:r><w:t>Chapter one</w:t></w:r></w:p></w:body></w:document>`)
	if len(snapshot.Nodes) != 1 || !snapshot.Nodes[0].PageBreakBefore {
		t.Fatalf("page break before lost: %#v", snapshot.Nodes)
	}
}

func TestExtractDocumentXMLPreservesEachPageSection(t *testing.T) {
	snapshot := ExtractDocumentXML(`<w:document><w:body>` +
		`<w:p><w:r><w:t>front matter</w:t></w:r><w:pPr><w:sectPr><w:pgSz w:w="11906" w:h="16838"/><w:pgMar w:top="1417" w:right="1417" w:bottom="1417" w:left="1417" w:header="907" w:footer="1191"/><w:pgNumType w:fmt="upperRoman" w:start="1"/></w:sectPr></w:pPr></w:p>` +
		`<w:p><w:r><w:t>1 Body</w:t></w:r></w:p><w:sectPr><w:pgSz w:w="11906" w:h="16838"/><w:pgMar w:top="1417" w:right="1417" w:bottom="1417" w:left="1417" w:header="907" w:footer="1191"/><w:pgNumType w:fmt="decimal" w:start="1"/></w:sectPr></w:body></w:document>`)
	if len(snapshot.Sections) != 2 || snapshot.Sections[0].PageNumberFormat != "upperRoman" || snapshot.Sections[1].PageNumberFormat != "decimal" {
		t.Fatalf("page sections=%#v", snapshot.Sections)
	}
	if snapshot.Nodes[0].PageSectionID != "section:1" || snapshot.Nodes[1].PageSectionID != "section:2" {
		t.Fatalf("node section mapping=%#v", snapshot.Nodes)
	}
}

func TestExtractDocumentXMLClassifiesReferenceEntriesBySection(t *testing.T) {
	xml := `<w:document><w:body>` +
		"<w:p><w:r><w:t>\u53c2\u8003\u6587\u732e</w:t></w:r></w:p>" +
		`<w:p><w:r><w:t>[1] sample reference</w:t></w:r></w:p>` +
		`</w:body></w:document>`
	snapshot := ExtractDocumentXML(xml)
	assertRole(t, snapshot, "[1] sample reference", "references", "references")
}

func TestExtractDocumentXMLSeparatesAbstractTitleAndBody(t *testing.T) {
	snapshot := ExtractDocumentXML(`<w:document><w:body>` +
		`<w:p><w:r><w:t>摘要</w:t></w:r></w:p>` +
		`<w:p><w:r><w:t>这是摘要正文。</w:t></w:r></w:p>` +
		`<w:p><w:r><w:t>关键词：论文格式；验证</w:t></w:r></w:p>` +
		`</w:body></w:document>`)
	if snapshot.Nodes[0].SemanticRole != "abstract_title" || snapshot.Nodes[0].SectionID != "abstract" {
		t.Fatalf("abstract title = %#v", snapshot.Nodes[0])
	}
	if snapshot.Nodes[1].SemanticRole != "abstract_body" || snapshot.Nodes[1].SectionID != "abstract" {
		t.Fatalf("abstract body = %#v", snapshot.Nodes[1])
	}
}

func TestExtractDocumentXMLRecognizesSectionBodiesWithoutUsingCurrentFormat(t *testing.T) {
	xml := `<w:document><w:body>` +
		`<w:p><w:r><w:t>` + "\u6458\u8981" + `</w:t></w:r></w:p>` +
		`<w:p><w:r><w:t>Chinese abstract body with deliberately plain formatting.</w:t></w:r></w:p>` +
		`<w:p><w:r><w:t>ABSTRACT</w:t></w:r></w:p>` +
		`<w:p><w:r><w:t>English abstract body with deliberately plain formatting.</w:t></w:r></w:p>` +
		`<w:p><w:r><w:t>` + "\u81f4\u8c22" + `</w:t></w:r></w:p>` +
		`<w:p><w:r><w:t>Acknowledgement body.</w:t></w:r></w:p>` +
		`<w:p><w:r><w:t>` + "\u9644\u5f55A" + `</w:t></w:r></w:p>` +
		`<w:p><w:r><w:t>Appendix body.</w:t></w:r></w:p>` +
		`</w:body></w:document>`
	snapshot := ExtractDocumentXML(xml)
	for index, want := range []string{"abstract_title", "abstract_body", "abstract_en", "abstract_en_body", "acknowledgements_title", "acknowledgements", "appendix_title", "appendix"} {
		if got := snapshot.Nodes[index].SemanticRole; got != want {
			t.Fatalf("node %d role=%q want=%q node=%#v", index, got, want, snapshot.Nodes[index])
		}
	}
}

func TestExtractDocumentXMLRecognizesTOCEntryWithoutTOCStyle(t *testing.T) {
	xml := `<w:document><w:body><w:p><w:r><w:t>` + "\u76ee\u5f55" + `</w:t></w:r></w:p>` +
		`<w:p><w:r><w:t>Chapter One....12</w:t></w:r></w:p>` +
		`</w:body></w:document>`
	snapshot := ExtractDocumentXML(xml)
	if got := snapshot.Nodes[1].SemanticRole; got != "toc_entry" {
		t.Fatalf("TOC entry role=%q node=%#v", got, snapshot.Nodes[1])
	}
}

func TestExtractDocumentXMLClassifiesCoverTitleAndAbstractBoundaries(t *testing.T) {
	xml := `<w:document><w:body>` +
		`<w:p><w:r><w:t>本科毕业论文/设计</w:t></w:r></w:p>` +
		`<w:p><w:r><w:t>2026年 3 月</w:t></w:r></w:p>` +
		`<w:p><w:r><w:t>社区2型糖尿病患者疾病知识认知现状及影响因素分析</w:t></w:r></w:p>` +
		`<w:p><w:r><w:t>摘要：这是摘要正文，时间为2026年。</w:t></w:r></w:p>` +
		`</w:body></w:document>`
	snapshot := ExtractDocumentXML(xml)
	want := []string{"cover_title", "cover_date", "cover_title", "abstract_cn"}
	for i, role := range want {
		if snapshot.Nodes[i].SemanticRole != role {
			t.Fatalf("node %d role = %q, want %q; node=%#v", i, snapshot.Nodes[i].SemanticRole, role, snapshot.Nodes[i])
		}
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

func TestExtractDocumentXMLRecognizesCompactAndOutlineHeadings(t *testing.T) {
	xml := `<w:document><w:body>` +
		`<w:p><w:pPr><w:outlineLvl w:val="0"/></w:pPr><w:r><w:t>3Results</w:t></w:r></w:p>` +
		`<w:p><w:pPr><w:outlineLvl w:val="1"/></w:pPr><w:r><w:t>4.2Analysis</w:t></w:r></w:p>` +
		`<w:p><w:r><w:t>5.3Conclusion</w:t></w:r></w:p>` +
		`</w:body></w:document>`

	snapshot := ExtractDocumentXML(xml)

	if snapshot.Stats.Headings != 3 {
		t.Fatalf("Headings = %d, want 3", snapshot.Stats.Headings)
	}
	for index, wantLevel := range []int{1, 2, 2} {
		node := snapshot.Nodes[index]
		if node.SemanticRole != "heading" || node.LogicalLevel != wantLevel {
			t.Fatalf("node %d = %#v, want heading level %d", index, node, wantLevel)
		}
	}
}

func TestExtractDocumentXMLLabelsPostCoverThesisTitleAsTitle(t *testing.T) {
	snapshot := ExtractDocumentXML(`<w:document><w:body>` +
		`<w:p><w:r><w:t>2026年 6 月</w:t></w:r></w:p>` +
		`<w:p><w:r><w:t>社区2型糖尿病患者疾病知识认知现状及影响因素分析</w:t></w:r></w:p>` +
		`<w:p><w:r><w:t>摘要</w:t></w:r></w:p>` +
		`</w:body></w:document>`)

	assertRole(t, snapshot, "社区2型糖尿病患者疾病知识认知现状及影响因素分析", "title", "cover")
}

func TestExtractDocumentXMLRecognizesCompactSingleLevelHeading(t *testing.T) {
	snapshot := ExtractDocumentXML(`<w:document><w:body>` +
		`<w:p><w:r><w:t>1.编写要求</w:t></w:r></w:p>` +
		`<w:p><w:r><w:t>1.毕业设计（论文）必须由学生本人独立完成，不得弄虚作假，不得抄袭他人成果。</w:t></w:r></w:p>` +
		`</w:body></w:document>`)
	if snapshot.Nodes[0].SemanticRole != "heading" || snapshot.Nodes[0].LogicalLevel != 2 {
		t.Fatalf("compact heading = %#v", snapshot.Nodes[0])
	}
	if snapshot.Nodes[1].SemanticRole != "body_paragraph" {
		t.Fatalf("long numbered sentence = %#v", snapshot.Nodes[1])
	}
}

func TestExtractDocumentXMLKeepsTOCStylesOutOfBodyHeadings(t *testing.T) {
	xml := `<w:document><w:body>` +
		`<w:p><w:r><w:t>&#30446;&#24405;</w:t></w:r></w:p>` +
		`<w:p><w:pPr><w:pStyle w:val="TOC1"/></w:pPr><w:r><w:t>3Results4</w:t></w:r></w:p>` +
		`<w:p><w:pPr><w:pStyle w:val="TOC2"/></w:pPr><w:r><w:t>3.1Analysis5</w:t></w:r></w:p>` +
		`<w:p><w:pPr><w:outlineLvl w:val="0"/></w:pPr><w:r><w:t>3Results</w:t></w:r></w:p>` +
		`</w:body></w:document>`

	snapshot := ExtractDocumentXML(xml)

	if snapshot.Nodes[1].SemanticRole != "toc_entry" || snapshot.Nodes[1].SectionID != "toc" {
		t.Fatalf("first TOC node = %#v, want toc_entry in toc", snapshot.Nodes[1])
	}
	if snapshot.Nodes[2].SemanticRole != "toc_entry" || snapshot.Nodes[2].SectionID != "toc" {
		t.Fatalf("second TOC node = %#v, want toc_entry in toc", snapshot.Nodes[2])
	}
	if snapshot.Nodes[3].SemanticRole != "heading" || snapshot.Nodes[3].SectionID != "body" {
		t.Fatalf("body heading = %#v, want heading in body", snapshot.Nodes[3])
	}
	if snapshot.Stats.Headings != 1 {
		t.Fatalf("Headings = %d, want only the real body heading", snapshot.Stats.Headings)
	}
}

func TestExtractDocumentXMLAddsTOCMatchEvidenceToBodyHeading(t *testing.T) {
	xml := `<w:document><w:body>` +
		`<w:p><w:r><w:t>目录</w:t></w:r></w:p>` +
		`<w:p><w:pPr><w:pStyle w:val="TOC1"/></w:pPr><w:r><w:t>1 Introduction....1</w:t></w:r></w:p>` +
		`<w:p><w:pPr><w:outlineLvl w:val="0"/></w:pPr><w:r><w:t>1 Introduction</w:t></w:r></w:p>` +
		`</w:body></w:document>`
	snapshot := ExtractDocumentXML(xml)
	if got := snapshot.Nodes[2].Evidence; len(got) == 0 || got[len(got)-1] != "toc:matched_entry" {
		t.Fatalf("TOC evidence=%#v", snapshot.Nodes[2])
	}
}

func TestExtractDocumentXMLRecognizesContinuedTableCaption(t *testing.T) {
	snapshot := ExtractDocumentXML(`<w:document><w:body><w:p><w:r><w:t>续表4-1 社区患者分析</w:t></w:r></w:p></w:body></w:document>`)
	if len(snapshot.Nodes) != 1 || snapshot.Nodes[0].SemanticRole != "table_caption" {
		t.Fatalf("continued table caption = %#v", snapshot.Nodes)
	}
}

func TestExtractDocumentXMLRecognizesOriginalityDeclarationWithoutStyleEvidence(t *testing.T) {
	snapshot := ExtractDocumentXML(`<w:document><w:body><w:p><w:r><w:t>原创性声明</w:t></w:r></w:p></w:body></w:document>`)
	if len(snapshot.Nodes) != 1 || snapshot.Nodes[0].SemanticRole != "originality_declaration" {
		t.Fatalf("originality declaration = %#v", snapshot.Nodes)
	}
}

func TestExtractDocumentXMLSeparatesFormulaAndAppendix(t *testing.T) {
	snapshot := ExtractDocumentXML(`<w:document><w:body>` +
		`<w:p><w:r><w:t>附录A 调查问卷</w:t></w:r></w:p>` +
		`<w:p><m:oMath><m:r><m:t>x</m:t></m:r><m:r><m:t>=1</m:t></m:r></m:oMath></w:p>` +
		`</w:body></w:document>`)
	if len(snapshot.Nodes) != 2 || snapshot.Nodes[0].SemanticRole != "appendix_title" || snapshot.Nodes[0].SectionID != "appendix" {
		t.Fatalf("appendix node = %#v", snapshot.Nodes)
	}
	if snapshot.Nodes[1].SemanticRole != "formula" || snapshot.Nodes[1].Text != "x=1" || snapshot.Stats.Formulas != 1 {
		t.Fatalf("formula node = %#v stats=%#v", snapshot.Nodes[1], snapshot.Stats)
	}
}
