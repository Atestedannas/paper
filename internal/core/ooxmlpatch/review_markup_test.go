package ooxmlpatch

import (
	"strings"
	"testing"

	"github.com/paper-format-checker/backend/internal/core/ooxmlpkg"
)

func TestFinalizeReviewMarkupAcceptsChangesAndRemovesComments(t *testing.T) {
	docxPath := writePatchTestDocx(t, map[string]string{
		"[Content_Types].xml": `<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Override PartName="/word/comments.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.comments+xml"/></Types>`,
		"word/_rels/document.xml.rels": `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` +
			`<Relationship Id="rIdComments" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/comments" Target="comments.xml"/>` +
			`</Relationships>`,
		"word/comments.xml": `<w:comments><w:comment w:id="0"><w:p><w:r><w:t>note</w:t></w:r></w:p></w:comment></w:comments>`,
		"word/document.xml": `<w:document><w:body><w:p>` +
			`<w:commentRangeStart w:id="0"/>` +
			`<w:r><w:t>Keep </w:t></w:r>` +
			`<w:ins><w:r><w:t>inserted</w:t></w:r></w:ins>` +
			`<w:del><w:r><w:delText>deleted</w:delText></w:r></w:del>` +
			`<w:r><w:commentReference w:id="0"/></w:r>` +
			`<w:commentRangeEnd w:id="0"/>` +
			`</w:p></w:body></w:document>`,
	})
	pkg, err := ooxmlpkg.Open(docxPath)
	if err != nil {
		t.Fatalf("open docx: %v", err)
	}

	if changed := FinalizeReviewMarkup(pkg); changed == 0 {
		t.Fatal("FinalizeReviewMarkup() changed = 0")
	}

	document, _ := pkg.Get("word/document.xml")
	rels, _ := pkg.Get("word/_rels/document.xml.rels")
	types, _ := pkg.Get("[Content_Types].xml")
	if _, ok := pkg.Get("word/comments.xml"); ok {
		t.Fatal("comments.xml should be removed")
	}
	for _, forbidden := range []string{"commentRange", "commentReference", "<w:ins", "<w:del", "deleted", "comments"} {
		if strings.Contains(string(document)+string(rels)+string(types), forbidden) {
			t.Fatalf("review markup still contains %q:\ndoc=%s\nrels=%s\ntypes=%s", forbidden, document, rels, types)
		}
	}
	if !strings.Contains(string(document), "inserted") {
		t.Fatalf("accepted insertion text missing: %s", document)
	}
}
