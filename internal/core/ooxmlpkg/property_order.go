package ooxmlpkg

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"sort"
	"strings"
)

var propertyOrder = map[string]map[string]int{
	"style": ranks([]string{
		"name", "aliases", "basedOn", "next", "link", "autoRedefine", "hidden",
		"uiPriority", "semiHidden", "unhideWhenUsed", "qFormat", "locked", "personal",
		"personalCompose", "personalReply", "rsid", "pPr", "rPr", "tblPr", "trPr",
		"tcPr", "tblStylePr",
	}),
	"pPr": ranks([]string{
		"pStyle", "keepNext", "keepLines", "pageBreakBefore", "framePr", "widowControl",
		"numPr", "suppressLineNumbers", "pBdr", "shd", "tabs", "suppressAutoHyphens",
		"kinsoku", "wordWrap", "overflowPunct", "topLinePunct", "autoSpaceDE", "autoSpaceDN",
		"bidi", "adjustRightInd", "snapToGrid", "spacing", "ind", "contextualSpacing",
		"mirrorIndents", "suppressOverlap", "jc", "textDirection", "textAlignment",
		"textboxTightWrap", "outlineLvl", "divId", "cnfStyle", "rPr", "sectPr", "pPrChange",
	}),
	"rPr": ranks([]string{
		"rStyle", "rFonts", "b", "bCs", "i", "iCs", "caps", "smallCaps", "strike",
		"dstrike", "outline", "shadow", "emboss", "imprint", "noProof", "snapToGrid",
		"vanish", "webHidden", "color", "spacing", "w", "kern", "position", "sz", "szCs",
		"highlight", "u", "effect", "bdr", "shd", "fitText", "vertAlign", "rtl", "cs",
		"em", "lang", "eastAsianLayout", "specVanish", "oMath", "rPrChange",
	}),
	"sectPr": ranks([]string{
		"headerReference", "footerReference", "footnotePr", "endnotePr", "type",
		"pgSz", "pgMar", "paperSrc", "pgBorders", "lnNumType", "pgNumType",
		"cols", "formProt", "vAlign", "noEndnote", "titlePg", "textDirection",
		"bidi", "rtlGutter", "docGrid", "printerSettings", "sectPrChange",
	}),
	"tblPr": ranks([]string{
		"tblStyle", "tblpPr", "tblOverlap", "bidiVisual", "tblStyleRowBandSize",
		"tblStyleColBandSize", "tblW", "jc", "tblCellSpacing", "tblInd", "tblBorders",
		"shd", "tblLayout", "tblCellMar", "tblLook", "tblCaption", "tblDescription",
		"tblPrChange",
	}),
	"tblPrEx": ranks([]string{
		"tblW", "jc", "tblCellSpacing", "tblInd", "tblBorders", "shd", "tblLayout",
		"tblCellMar", "tblLook", "tblPrExChange",
	}),
	"tr": ranksWithStableTail([]string{"tblPrEx", "trPr"}, []string{
		"customXml", "sdt", "proofErr", "permStart", "permEnd",
		"bookmarkStart", "bookmarkEnd", "moveFromRangeStart", "moveFromRangeEnd",
		"moveToRangeStart", "moveToRangeEnd", "commentRangeStart", "commentRangeEnd",
		"customXmlInsRangeStart", "customXmlInsRangeEnd", "customXmlDelRangeStart",
		"customXmlDelRangeEnd", "customXmlMoveFromRangeStart", "customXmlMoveFromRangeEnd",
		"customXmlMoveToRangeStart", "customXmlMoveToRangeEnd", "ins", "del", "moveFrom",
		"moveTo", "oMathPara", "oMath", "altChunk", "tc",
	}),
	"tcPr": ranks([]string{
		"cnfStyle", "tcW", "gridSpan", "hMerge", "vMerge", "tcBorders", "shd",
		"noWrap", "tcMar", "textDirection", "tcFitText", "vAlign", "hideMark",
		"headers", "cellIns", "cellDel", "cellMerge", "tcPrChange",
	}),
}

// RepairPropertyOrder safely reorders known direct children of w:pPr and w:rPr.
// Containers with unknown children are left untouched.
func RepairPropertyOrder(docxPath string) (int, error) {
	pkg, err := Open(docxPath)
	if err != nil {
		return 0, err
	}
	changed := 0
	for _, name := range pkg.Names() {
		if !strings.HasPrefix(name, "word/") || !strings.HasSuffix(name, ".xml") {
			continue
		}
		content, ok := pkg.Get(name)
		if !ok {
			continue
		}
		repaired, count, err := reorderPropertyContainers(content)
		if err != nil {
			return 0, fmt.Errorf("%s: %w", name, err)
		}
		if count == 0 {
			continue
		}
		if err := pkg.Set(name, repaired); err != nil {
			return 0, err
		}
		changed += count
	}
	packageRepairs, err := repairDocumentRelationshipsAndCompatibility(pkg)
	if err != nil {
		return 0, err
	}
	changed += packageRepairs
	if changed == 0 {
		return 0, nil
	}
	return changed, pkg.Write(docxPath)
}

type elementSpan struct {
	name       string
	start, end int
}

func reorderPropertyContainers(content []byte) ([]byte, int, error) {
	decoder := xml.NewDecoder(bytes.NewReader(content))
	var stack []elementSpan
	var spans []elementSpan
	for {
		start := int(decoder.InputOffset())
		token, err := decoder.Token()
		if err != nil {
			if err == io.EOF {
				break
			}
			return nil, 0, err
		}
		switch typed := token.(type) {
		case xml.StartElement:
			stack = append(stack, elementSpan{name: typed.Name.Local, start: start})
		case xml.EndElement:
			if len(stack) == 0 {
				continue
			}
			open := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			if open.name == typed.Name.Local && propertyOrder[open.name] != nil {
				open.end = int(decoder.InputOffset())
				spans = append(spans, open)
			}
		}
	}

	output := append([]byte(nil), content...)
	changed := 0
	sort.Slice(spans, func(i, j int) bool { return spans[i].start > spans[j].start })
	for _, span := range spans {
		repaired, ok, err := reorderPropertyBlock(output[span.start:span.end], span.name)
		if err != nil {
			return nil, 0, err
		}
		if ok {
			copy(output[span.start:span.end], repaired)
			changed++
		}
	}
	return output, changed, nil
}

func reorderPropertyBlock(block []byte, container string) ([]byte, bool, error) {
	order := propertyOrder[container]
	decoder := xml.NewDecoder(bytes.NewReader(block))
	depth := 0
	children := make([]elementSpan, 0, 8)
	for {
		start := int(decoder.InputOffset())
		token, err := decoder.Token()
		if err != nil {
			if err == io.EOF {
				break
			}
			return nil, false, err
		}
		switch typed := token.(type) {
		case xml.StartElement:
			if depth == 1 {
				children = append(children, elementSpan{name: typed.Name.Local, start: start})
			}
			depth++
		case xml.EndElement:
			if depth == 2 && len(children) > 0 {
				children[len(children)-1].end = int(decoder.InputOffset())
			}
			depth--
		}
	}
	if len(children) < 2 {
		return block, false, nil
	}
	for _, child := range children {
		if _, known := order[child.name]; !known {
			return block, false, nil
		}
	}
	for index := 0; index < len(children)-1; index++ {
		children[index].end = children[index+1].start
	}
	sorted := append([]elementSpan(nil), children...)
	sort.SliceStable(sorted, func(i, j int) bool { return order[sorted[i].name] < order[sorted[j].name] })
	for index := range children {
		if children[index].start != sorted[index].start {
			break
		}
		if index == len(children)-1 {
			return block, false, nil
		}
	}

	var repaired bytes.Buffer
	repaired.Grow(len(block))
	repaired.Write(block[:children[0].start])
	for _, child := range sorted {
		repaired.Write(block[child.start:child.end])
	}
	repaired.Write(block[children[len(children)-1].end:])
	if repaired.Len() != len(block) {
		return nil, false, fmt.Errorf("property reorder changed XML byte length")
	}
	return repaired.Bytes(), true, nil
}

// RepairPropertyBlock returns one pPr or rPr block with its known direct
// children in schema order.
func RepairPropertyBlock(block []byte, container string) ([]byte, bool, error) {
	return reorderPropertyBlock(block, container)
}

func ranks(names []string) map[string]int {
	result := make(map[string]int, len(names))
	for index, name := range names {
		result[name] = index
	}
	return result
}

func ranksWithStableTail(prefix, tail []string) map[string]int {
	result := ranks(prefix)
	for _, name := range tail {
		result[name] = len(prefix)
	}
	return result
}
