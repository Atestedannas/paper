package ooxmlpatch

import (
	"strings"
	"testing"
)

func TestBuildTablePropertiesAndCellProperties(t *testing.T) {
	tblPr := BuildTableProperties(TablePropertiesSpec{
		WidthTwips:            8640,
		Alignment:             "center",
		FixedLayout:           true,
		ThreeLine:             true,
		CellMarginTopTwips:    60,
		CellMarginStartTwips:  80,
		CellMarginBottomTwips: 60,
		CellMarginEndTwips:    80,
	})
	for _, want := range []string{
		`<w:tblPr>`,
		`<w:tblW w:w="8640" w:type="dxa"/>`,
		`<w:jc w:val="center"/>`,
		`<w:tblLayout w:type="fixed"/>`,
		`<w:tblBorders>`,
		`<w:tblCellMar><w:top w:w="60" w:type="dxa"/><w:start w:w="80" w:type="dxa"/><w:bottom w:w="60" w:type="dxa"/><w:end w:w="80" w:type="dxa"/></w:tblCellMar>`,
		`</w:tblPr>`,
	} {
		if !strings.Contains(tblPr, want) {
			t.Fatalf("table properties missing %s:\n%s", want, tblPr)
		}
	}

	tcPr := BuildTableCellProperties(TableCellPropertiesSpec{
		WidthTwips:        2400,
		GridSpan:          2,
		VMerge:            "restart",
		VerticalAlign:     "center",
		IncludeAllBorders: true,
	})
	for _, want := range []string{
		`<w:tcPr>`,
		`<w:tcW w:w="2400" w:type="dxa"/>`,
		`<w:gridSpan w:val="2"/>`,
		`<w:vMerge w:val="restart"/>`,
		`<w:vAlign w:val="center"/>`,
		`<w:tcBorders>`,
		`</w:tcPr>`,
	} {
		if !strings.Contains(tcPr, want) {
			t.Fatalf("cell properties missing %s:\n%s", want, tcPr)
		}
	}
}
