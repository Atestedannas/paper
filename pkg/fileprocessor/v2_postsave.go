package fileprocessor

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"

	"github.com/paper-format-checker/backend/internal/core/templateprofile"
)

// patchBodySectPrMargins 在 SaveToFile 之后，直接修改 DOCX 内部
// word/document.xml 中 body 直接子级的 sectPr/pgMar 属性值。
//
// 根因: unioffice 序列化时 body 级 sectPr 使用的不是
// d.Document.Body.SectPr 对象，而是某种内部缓存或原始段落副本，
// 导致内存中的 Int64 指针修改无法持久化到 XML 输出。
//
// B-S5 修复: 遍历所有 body 级 sectPr（非段落级 pPr 内的），
// 对每个含 pgMar 的 sectPr 逐一做 EMU 舍入补偿。
func patchBodySectPrMargins(outputPath string, ps *templateprofile.PageSetupRule) error {
	raw, err := os.ReadFile(outputPath)
	if err != nil {
		return fmt.Errorf("读取输出文件: %w", err)
	}

	zipReader, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		return fmt.Errorf("打开 ZIP: %w", err)
	}

	var docXML []byte
	for _, f := range zipReader.File {
		if f.Name == "word/document.xml" {
			rc, err := f.Open()
			if err != nil {
				return fmt.Errorf("打开 document.xml: %w", err)
			}
			docXML, err = io.ReadAll(rc)
			rc.Close()
			if err != nil {
				return fmt.Errorf("读取 document.xml: %w", err)
			}
			break
		}
	}
	if docXML == nil {
		return fmt.Errorf("document.xml 未找到")
	}

	bodyEndMarker := []byte("</w:body>")
	bodyIdx := bytes.LastIndex(docXML, bodyEndMarker)
	if bodyIdx < 0 {
		return fmt.Errorf("未找到 </w:body>")
	}

	fmt.Printf("[B-S5] bodyIdx=%d docXML len=%d\n", bodyIdx, len(docXML))

	// B-S5: 遍历所有 body 级 <w:sectPr>。
	// 段落级 sectPr（w:pPr/w:sectPr）已被 ApplyProfilePageMargins 正确设置，
	// 此处仅修复 body 直接子级的 sectPr。
	// 判定方法：sectPr 前 200 字节内，如果有 </w:p> 出现在 <w:pPr> 之后（或无 pPr），则为 body 级。
	sectPrTag := []byte("<w:sectPr")
	pgMarTag := []byte("<w:pgMar")
	pgMarCloseTag := []byte("</w:pgMar>")
	searchRegion := docXML[:bodyIdx]

	type pgMarHit struct {
		pgMarStart   int
		pgMarFullLen int
		newPgMar     []byte
	}
	var hits []pgMarHit

	searchFrom := 0
	allSectPrCount := 0
	for {
		sectPrIdx := bytes.Index(searchRegion[searchFrom:], sectPrTag)
		if sectPrIdx < 0 {
			break
		}
		allSectPrCount++
		absSectPr := searchFrom + sectPrIdx

		checkStart := absSectPr - 200
		if checkStart < 0 {
			checkStart = 0
		}
		preContext := docXML[checkStart:absSectPr]
		lastPPr := bytes.LastIndex(preContext, []byte("<w:pPr"))
		// Body-level: no <w:pPr> in context, or </w:p> appears after <w:pPr>.
		// Paragraph-level sectPr sits inside <w:pPr>...</w:pPr>.
		isBodyLevel := lastPPr < 0 || bytes.LastIndex(preContext, []byte("</w:p>")) > lastPPr

		fmt.Printf("[B-S5] sectPr#%d offset=%d isBody=%v lastPPr=%d preTail=%q\n",
			allSectPrCount, absSectPr, isBodyLevel, lastPPr,
			string(preContext[max(len(preContext)-60, 0):]))

		// 所有分节都必须使用同一份模板页面设置；段落级 sectPr 也会被
		// unioffice 序列化舍入，不能只修 body 级 sectPr。
		{
			sectPrRegion := searchRegion[absSectPr:]
			pgMarIdx := bytes.Index(sectPrRegion, pgMarTag)
			if pgMarIdx >= 0 {
				absPgMar := absSectPr + pgMarIdx
				pgMarRegion := docXML[absPgMar:]

				pgMarEnd := bytes.Index(pgMarRegion, []byte(">"))
				if pgMarEnd < 0 {
					searchFrom = absSectPr + len(sectPrTag)
					continue
				}

				isSelfClosing := pgMarRegion[pgMarEnd-1] == '/'
				var pgMarFull []byte
				var pgMarInner []byte
				if isSelfClosing {
					pgMarFull = pgMarRegion[:pgMarEnd+1]
					pgMarInner = pgMarRegion[len("<w:pgMar "):pgMarEnd-1]
				} else {
					pgMarCloseEnd := bytes.Index(pgMarRegion, pgMarCloseTag)
					if pgMarCloseEnd < 0 {
						searchFrom = absSectPr + len(sectPrTag)
						continue
					}
					pgMarFull = pgMarRegion[:pgMarCloseEnd+len(pgMarCloseTag)]
					pgMarInner = pgMarRegion[len("<w:pgMar>"):pgMarCloseEnd]
				}

				newAttrs := patchPgMarAttrs(string(pgMarInner), ps)
				var newPgMar []byte
				if isSelfClosing {
					newPgMar = []byte("<w:pgMar " + newAttrs + "/>")
				} else {
					newPgMar = []byte("<w:pgMar " + newAttrs + ">")
				}

				hits = append(hits, pgMarHit{
					pgMarStart:   absPgMar,
					pgMarFullLen: len(pgMarFull),
					newPgMar:     newPgMar,
				})
			}
		}

		searchFrom = absSectPr + len(sectPrTag)
	}

	if len(hits) == 0 {
		return nil
	}

	fmt.Printf("[B-S5] patching %d body-level pgMar(s)\n", len(hits))
	for i, h := range hits {
		oldVal := docXML[h.pgMarStart : h.pgMarStart+h.pgMarFullLen]
		fmt.Printf("[B-S5] hit[%d] offset=%d old=%s new=%s\n",
			i, h.pgMarStart, string(oldVal), string(h.newPgMar))
	}

	// 从后往前替换
	newDocXML := docXML
	for i := len(hits) - 1; i >= 0; i-- {
		h := hits[i]
		result := make([]byte, 0, len(newDocXML)+len(h.newPgMar))
		result = append(result, newDocXML[:h.pgMarStart]...)
		result = append(result, h.newPgMar...)
		result = append(result, newDocXML[h.pgMarStart+h.pgMarFullLen:]...)
		newDocXML = result
	}

	// 写回 ZIP
	var buf bytes.Buffer
	zipWriter := zip.NewWriter(&buf)

	for _, f := range zipReader.File {
		if f.Name == "word/document.xml" {
			w, err := zipWriter.Create(f.Name)
			if err != nil {
				return fmt.Errorf("创建 document.xml entry: %w", err)
			}
			if _, err := w.Write(newDocXML); err != nil {
				return fmt.Errorf("写入 document.xml: %w", err)
			}
		} else {
			rc, err := f.Open()
			if err != nil {
				return fmt.Errorf("打开 %s: %w", f.Name, err)
			}
			w, err := zipWriter.CreateHeader(&zip.FileHeader{
				Name:   f.Name,
				Method: f.Method,
			})
			if err != nil {
				rc.Close()
				return fmt.Errorf("创建 entry %s: %w", f.Name, err)
			}
			if _, err := io.Copy(w, rc); err != nil {
				rc.Close()
				return fmt.Errorf("复制 %s: %w", f.Name, err)
			}
			rc.Close()
		}
	}
	zipWriter.Close()

	if err := os.WriteFile(outputPath, buf.Bytes(), 0644); err != nil {
		return fmt.Errorf("写回 DOCX: %w", err)
	}

	return nil
}

// patchBodySectPrMarginsFromBytes 与 patchBodySectPrMargins 功能相同，
// 但直接操作内存中的 ZIP 字节切片，避免文件系统缓存问题。
func patchBodySectPrMarginsFromBytes(raw []byte, ps *templateprofile.PageSetupRule) ([]byte, error) {
	zipReader, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		return nil, fmt.Errorf("打开 ZIP: %w", err)
	}

	var docXML []byte
	for _, f := range zipReader.File {
		if f.Name == "word/document.xml" {
			rc, err := f.Open()
			if err != nil {
				return nil, fmt.Errorf("打开 document.xml: %w", err)
			}
			docXML, err = io.ReadAll(rc)
			rc.Close()
			if err != nil {
				return nil, fmt.Errorf("读取 document.xml: %w", err)
			}
			break
		}
	}
	if docXML == nil {
		return nil, fmt.Errorf("document.xml 未找到")
	}

	bodyEndMarker := []byte("</w:body>")
	bodyIdx := bytes.LastIndex(docXML, bodyEndMarker)
	if bodyIdx < 0 {
		return nil, fmt.Errorf("未找到 </w:body>")
	}

	fmt.Printf("[B-S5 mem] docXML len=%d bodyIdx=%d\n", len(docXML), bodyIdx)

	sectPrTag := []byte("<w:sectPr")
	pgMarTag := []byte("<w:pgMar")
	pgMarCloseTag := []byte("</w:pgMar>")
	searchRegion := docXML[:bodyIdx]

	type pgMarHit struct {
		pgMarStart   int
		pgMarFullLen int
		newPgMar     []byte
	}
	var hits []pgMarHit

	searchFrom := 0
	allSectPrCount2 := 0
	for {
		sectPrIdx := bytes.Index(searchRegion[searchFrom:], sectPrTag)
		if sectPrIdx < 0 {
			break
		}
		allSectPrCount2++
		absSectPr := searchFrom + sectPrIdx

		checkStart := absSectPr - 200
		if checkStart < 0 {
			checkStart = 0
		}
		preContext := docXML[checkStart:absSectPr]
		lastPPr := bytes.LastIndex(preContext, []byte("<w:pPr"))
		// Body-level: no <w:pPr> in context, or </w:p> appears after <w:pPr>.
		// Paragraph-level sectPr sits inside <w:pPr>...</w:pPr>.
		isBodyLevel := lastPPr < 0 || bytes.LastIndex(preContext, []byte("</w:p>")) > lastPPr

		fmt.Printf("[B-S5 mem] sectPr#%d offset=%d isBody=%v lastPPr=%d preTail=%q\n",
			allSectPrCount2, absSectPr, isBodyLevel, lastPPr,
			string(preContext[max(len(preContext)-60, 0):]))

		// 保存后统一修正所有分节，包括段落级 sectPr。
		{
			sectPrRegion := searchRegion[absSectPr:]
			pgMarIdx := bytes.Index(sectPrRegion, pgMarTag)
			if pgMarIdx >= 0 {
				fmt.Printf("[B-S5 mem]   pgMar found at absSectPr=%d → pgMarIdx=%d, absPgMar=%d\n", absSectPr, pgMarIdx, absSectPr+pgMarIdx)
				absPgMar := absSectPr + pgMarIdx
				pgMarRegion := docXML[absPgMar:]

				pgMarEnd := bytes.Index(pgMarRegion, []byte(">"))
				if pgMarEnd < 0 {
					fmt.Printf("[B-S5 mem]   > NOT found in pgMarRegion\n")
					searchFrom = absSectPr + len(sectPrTag)
					continue
				}

				isSelfClosing := pgMarRegion[pgMarEnd-1] == '/'
				fmt.Printf("[B-S5 mem]   pgMarEnd=%d isSelfClosing=%v snippet=%q\n", pgMarEnd, isSelfClosing, string(pgMarRegion[:min(80, len(pgMarRegion))]))
				var pgMarFull []byte
				var pgMarInner []byte
				if isSelfClosing {
					pgMarFull = pgMarRegion[:pgMarEnd+1]
					pgMarInner = pgMarRegion[len("<w:pgMar "):pgMarEnd-1]
				} else {
					pgMarCloseEnd := bytes.Index(pgMarRegion, pgMarCloseTag)
					if pgMarCloseEnd < 0 {
						searchFrom = absSectPr + len(sectPrTag)
						continue
					}
					pgMarFull = pgMarRegion[:pgMarCloseEnd+len(pgMarCloseTag)]
					pgMarInner = pgMarRegion[len("<w:pgMar>"):pgMarCloseEnd]
				}

				newAttrs := patchPgMarAttrs(string(pgMarInner), ps)
				var newPgMar []byte
				if isSelfClosing {
					newPgMar = []byte("<w:pgMar " + newAttrs + "/>")
				} else {
					newPgMar = []byte("<w:pgMar " + newAttrs + ">")
				}

				hits = append(hits, pgMarHit{
					pgMarStart:   absPgMar,
					pgMarFullLen: len(pgMarFull),
					newPgMar:     newPgMar,
				})
			}
		}

		searchFrom = absSectPr + len(sectPrTag)
	}

	if len(hits) == 0 {
		return raw, nil // nothing to patch
	}

	// 从后往前替换
	newDocXML := docXML
	for i := len(hits) - 1; i >= 0; i-- {
		h := hits[i]
		result := make([]byte, 0, len(newDocXML)+len(h.newPgMar))
		result = append(result, newDocXML[:h.pgMarStart]...)
		result = append(result, h.newPgMar...)
		result = append(result, newDocXML[h.pgMarStart+h.pgMarFullLen:]...)
		newDocXML = result
	}

	// 写回 ZIP
	var buf bytes.Buffer
	zipWriter := zip.NewWriter(&buf)

	for _, f := range zipReader.File {
		if f.Name == "word/document.xml" {
			w, err := zipWriter.Create(f.Name)
			if err != nil {
				return nil, fmt.Errorf("创建 document.xml entry: %w", err)
			}
			if _, err := w.Write(newDocXML); err != nil {
				return nil, fmt.Errorf("写入 document.xml: %w", err)
			}
		} else {
			rc, err := f.Open()
			if err != nil {
				return nil, fmt.Errorf("打开 %s: %w", f.Name, err)
			}
			w, err := zipWriter.CreateHeader(&zip.FileHeader{
				Name:   f.Name,
				Method: f.Method,
			})
			if err != nil {
				rc.Close()
				return nil, fmt.Errorf("创建 entry %s: %w", f.Name, err)
			}
			if _, err := io.Copy(w, rc); err != nil {
				rc.Close()
				return nil, fmt.Errorf("复制 %s: %w", f.Name, err)
			}
			rc.Close()
		}
	}
	zipWriter.Close()

	return buf.Bytes(), nil
}

var pgMarAttrRe = regexp.MustCompile(`(w:[a-z]+)="([^"]*)"`)

func patchPgMarAttrs(attrs string, ps *templateprofile.PageSetupRule) string {
	expected := map[string]string{
		"w:top":    ps.MarginTopTwips,
		"w:right":  ps.MarginRightTwips,
		"w:bottom": ps.MarginBottomTwips,
		"w:left":   ps.MarginLeftTwips,
		"w:header": ps.HeaderMarginTwips,
		"w:footer": ps.FooterMarginTwips,
	}
	return pgMarAttrRe.ReplaceAllStringFunc(attrs, func(match string) string {
		sub := pgMarAttrRe.FindStringSubmatch(match)
		if sub == nil {
			return match
		}
		value := expected[sub[1]]
		if value == "" {
			return match
		}
		return fmt.Sprintf(`%s="%s"`, sub[1], value)
	})
}


var (
	rxSz     = regexp.MustCompile(`<w:sz w:val="[^"]*"/>`)
	rxSzCs   = regexp.MustCompile(`<w:szCs w:val="[^"]*"/>`)
	rxJcVal  = regexp.MustCompile(`(<w:jc[^>]*?\s)w:val="[^"]*"`)
)

// injectSz24 inserts <w:sz w:val="24"/><w:szCs w:val="24"/> into the first <w:rPr> tag found.
func injectSz24(para string) string {
	// Replace first <w:rPr> with <w:rPr><w:sz w:val="24"/><w:szCs w:val="24"/>
	rPrIdx := strings.Index(para, `<w:rPr>`)
	if rPrIdx >= 0 {
		return para[:rPrIdx] + `<w:rPr><w:sz w:val="24"/><w:szCs w:val="24"/>` + para[rPrIdx+len(`<w:rPr>`):]
	}
	// Try <w:rPr followed by space (attributes)
	rPrIdx = strings.Index(para, `<w:rPr `)
	if rPrIdx >= 0 {
		gtIdx := strings.Index(para[rPrIdx:], `>`)
		if gtIdx >= 0 {
			insertAt := rPrIdx + gtIdx + 1
			return para[:insertAt] + `<w:sz w:val="24"/><w:szCs w:val="24"/>` + para[insertAt:]
		}
	}
	return para
}

// patchAcknowledgements 修复致谢段落格式：
// 1. 标题：sz 21→24, jc both→distribute
// 2. 正文：sz 21→24, 移除 firstLine=480
// 在 document.xml 中找到 "致谢" 标题段并修复其后所有内容段。
func patchAcknowledgements(raw []byte) ([]byte, error) {
	zipReader, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		return nil, fmt.Errorf("patchAck: open zip: %w", err)
	}

	var docXML []byte
	var docXMLName string
	for _, f := range zipReader.File {
		if f.Name == "word/document.xml" {
			docXMLName = f.Name
			rc, err := f.Open()
			if err != nil {
				return nil, fmt.Errorf("patchAck: open document.xml: %w", err)
			}
			docXML, err = io.ReadAll(rc)
			rc.Close()
			if err != nil {
				return nil, fmt.Errorf("patchAck: read document.xml: %w", err)
			}
			break
		}
	}
	if docXML == nil {
		return raw, nil
	}

	// Find ALL '致谢' occurrences — the first is a TOC entry, the last is the chapter title.
	// Use the LAST match to skip TOC entries.
	ackTitlePat := regexp.MustCompile(`<w:t[^>]*>致\s*谢</w:t>`)
	allMatches := ackTitlePat.FindAllIndex(docXML, -1)
	if len(allMatches) == 0 {
		return raw, nil
	}
	match := allMatches[len(allMatches)-1] // use the last (chapter title) match

	// Find the start of the paragraph containing the match
	var paraStart int
	for paraStart = match[0]; paraStart > 0; paraStart-- {
		if bytes.HasPrefix(docXML[paraStart:], []byte("<w:p ")) ||
			bytes.HasPrefix(docXML[paraStart:], []byte("<w:p>")) {
			break
		}
	}
	// Find the end of this title paragraph
	var titleEnd int
	for titleEnd = match[1]; titleEnd < len(docXML); titleEnd++ {
		if bytes.HasPrefix(docXML[titleEnd:], []byte("</w:p>")) {
			titleEnd += len("</w:p>")
			break
		}
	}

	// Build prefix (everything before the title paragraph)
	prefix := docXML[:paraStart]
	// Title paragraph content
	titlePara := string(docXML[paraStart:titleEnd])
	// Everything after title paragraph
	suffix := docXML[titleEnd:]

	// Fix title paragraph: force sz=24, jc=distribute, remove existing font overrides
	origTitle := titlePara
	// Remove any existing sz/szCs attributes and inject sz=24
	titlePara = rxSz.ReplaceAllString(titlePara, ``)
	titlePara = rxSzCs.ReplaceAllString(titlePara, ``)
	// Inject sz=24 into the paragraph's rPr (first <w:rPr>)
	titlePara = injectSz24(titlePara)
	// Fix alignment: if jc exists, set distribute; if not, inject jc=distribute
	if strings.Contains(titlePara, `w:jc`) {
		titlePara = rxJcVal.ReplaceAllString(titlePara, `${1}w:val="distribute"`)
	} else {
		titlePara = strings.Replace(titlePara, `<w:pPr>`, `<w:pPr><w:jc w:val="distribute"/>`, 1)
		titlePara = strings.Replace(titlePara, `<w:pPr `, `<w:pPr<w:jc w:val="distribute"/> `, 1)
	}

	// Fix the suffix (content paragraphs after title): remove firstLine=480, fix sz to 24
	patched := false
	origSuffix := string(suffix)
	suffixStr := origSuffix
	suffixStr = strings.ReplaceAll(suffixStr, ` w:firstLine="480"`, ``)
	suffixStr = strings.ReplaceAll(suffixStr, `<w:ind/>`, ``) // empty indent elements
	suffixStr = strings.ReplaceAll(suffixStr, `<w:sz w:val="21"/>`, `<w:sz w:val="24"/>`)
	suffixStr = strings.ReplaceAll(suffixStr, `<w:szCs w:val="21"/>`, `<w:szCs w:val="24"/>`)

	if titlePara == origTitle && suffixStr == origSuffix {
		return raw, nil // no changes
	}
	patched = true

	if !patched {
		return raw, nil
	}

	// Rebuild document.xml
	newDoc := append(append(prefix, []byte(titlePara)...), []byte(suffixStr)...)

	// Rebuild zip
	buf := new(bytes.Buffer)
	w := zip.NewWriter(buf)
	for _, f := range zipReader.File {
		if f.Name == docXMLName {
			h := &zip.FileHeader{Name: f.Name, Method: zip.Deflate}
			h.SetModTime(f.Modified)
			fw, err := w.CreateHeader(h)
			if err != nil {
				return nil, fmt.Errorf("patchAck: create entry: %w", err)
			}
			if _, err := fw.Write(newDoc); err != nil {
				return nil, fmt.Errorf("patchAck: write entry: %w", err)
			}
		} else {
			rc, err := f.Open()
			if err != nil {
				return nil, fmt.Errorf("patchAck: open %s: %w", f.Name, err)
			}
			fw, err := w.CreateHeader(&zip.FileHeader{Name: f.Name, Method: zip.Deflate})
			if err != nil {
				rc.Close()
				return nil, fmt.Errorf("patchAck: create %s: %w", f.Name, err)
			}
			if _, err := io.Copy(fw, rc); err != nil {
				rc.Close()
				return nil, fmt.Errorf("patchAck: copy %s: %w", f.Name, err)
			}
			rc.Close()
		}
	}
	w.Close()
	return buf.Bytes(), nil
}

// patchDocDefaults 修复 unioffice 序列化时空 docDefaults 不生成 RPrDefault 的问题。
// 在 styles.xml 中将 <w:docDefaults/> 替换为包含中英文字体的完整结构。
func patchDocDefaults(raw []byte) ([]byte, error) {
	zipReader, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		return nil, fmt.Errorf("patchDocDefaults: open zip: %w", err)
	}

	var stylesXML []byte
	var stylesXMLName string
	for _, f := range zipReader.File {
		if f.Name == "word/styles.xml" {
			stylesXMLName = f.Name
			rc, err := f.Open()
			if err != nil {
				return nil, fmt.Errorf("patchDocDefaults: open styles.xml: %w", err)
			}
			stylesXML, err = io.ReadAll(rc)
			rc.Close()
			if err != nil {
				return nil, fmt.Errorf("patchDocDefaults: read styles.xml: %w", err)
			}
			break
		}
	}
	if stylesXML == nil {
		return raw, nil // no styles.xml, nothing to patch
	}

	// Replace <w:docDefaults/> with complete structure including fonts
	docDefaultsReplacement := `<w:docDefaults>` +
		`<w:rPrDefault><w:rPr>` +
		`<w:rFonts w:eastAsia="宋体" w:cs="宋体" w:ascii="Times New Roman" w:hAnsi="Times New Roman"/>` +
		`</w:rPr></w:rPrDefault>` +
		`</w:docDefaults>`
	patched := bytes.ReplaceAll(stylesXML, []byte(`<w:docDefaults/>`), []byte(docDefaultsReplacement))

	if bytes.Equal(patched, stylesXML) {
		return raw, nil // no change
	}

	// Rebuild zip with patched styles.xml
	buf := new(bytes.Buffer)
	w := zip.NewWriter(buf)
	for _, f := range zipReader.File {
		if f.Name == stylesXMLName {
			h := &zip.FileHeader{Name: f.Name, Method: zip.Deflate}
			h.SetModTime(f.Modified)
			fw, err := w.CreateHeader(h)
			if err != nil {
				return nil, fmt.Errorf("patchDocDefaults: create entry: %w", err)
			}
			if _, err := fw.Write(patched); err != nil {
				return nil, fmt.Errorf("patchDocDefaults: write entry: %w", err)
			}
		} else {
			rc, err := f.Open()
			if err != nil {
				return nil, fmt.Errorf("patchDocDefaults: open %s: %w", f.Name, err)
			}
			fw, err := w.CreateHeader(&zip.FileHeader{Name: f.Name, Method: zip.Deflate})
			if err != nil {
				rc.Close()
				return nil, fmt.Errorf("patchDocDefaults: create %s: %w", f.Name, err)
			}
			if _, err := io.Copy(fw, rc); err != nil {
				rc.Close()
				return nil, fmt.Errorf("patchDocDefaults: copy %s: %w", f.Name, err)
			}
			rc.Close()
		}
	}
	w.Close()
	return buf.Bytes(), nil
}

// patchNormalStyleFontSize 修复 unioffice 序列化时会覆盖 run 级字体大小的 bug：
// Normal 样式中的 sz=24(12pt) 会在保存时导致显式设 sz=21(10.5pt) 的 run 被
// 回退到 12pt。通过后处理 styles.xml 中的 Normal 样式 sz/szCs 从 24→21 来修复。
func patchNormalStyleFontSize(raw []byte) ([]byte, error) {
	zipReader, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		return nil, fmt.Errorf("patchNormalStyle: open zip: %w", err)
	}

	var stylesXML []byte
	for _, f := range zipReader.File {
		if f.Name == "word/styles.xml" {
			rc, err := f.Open()
			if err != nil {
				return nil, fmt.Errorf("patchNormalStyle: open styles.xml: %w", err)
			}
			stylesXML, err = io.ReadAll(rc)
			rc.Close()
			if err != nil {
				return nil, fmt.Errorf("patchNormalStyle: read styles.xml: %w", err)
			}
			break
		}
	}
	if stylesXML == nil {
		return raw, nil // no styles.xml, nothing to patch
	}

	// 找到 Normal 样式 (<w:style w:styleId="Normal"...) 内的 <w:sz w:val="24"/>
	// 将 val="24" 替换为 val="21"
	normalPat := regexp.MustCompile(`(<w:style[^>]*w:styleId="Normal"[^>]*>.*?</w:style>)`)
	normalMatch := normalPat.FindSubmatch(stylesXML)
	if normalMatch == nil {
		return raw, nil // no Normal style
	}

	normalBlock := normalMatch[1]
	// Only patch if it has sz=24 (12pt) — target is 10.5pt (21)
	if !bytes.Contains(normalBlock, []byte(`sz" w:val="24`)) &&
		!bytes.Contains(normalBlock, []byte(`sz w:val="24`)) {
		return raw, nil // already correct or different value
	}

	replaced := bytes.ReplaceAll(normalBlock, []byte(`w:val="24"`), []byte(`w:val="21"`))
	patched := bytes.ReplaceAll(stylesXML, normalMatch[1], replaced)

	// 写回 ZIP
	var buf bytes.Buffer
	zipWriter := zip.NewWriter(&buf)
	for _, f := range zipReader.File {
		if f.Name == "word/styles.xml" {
			fh := zip.FileHeader{Name: f.Name}
			fh.SetModTime(f.ModTime())
			w, err := zipWriter.CreateHeader(&fh)
			if err != nil {
				return nil, fmt.Errorf("patchNormalStyle: create entry: %w", err)
			}
			if _, err := w.Write(patched); err != nil {
				return nil, fmt.Errorf("patchNormalStyle: write entry: %w", err)
			}
		} else {
			rc, err := f.Open()
			if err != nil {
				return nil, fmt.Errorf("patchNormalStyle: open %s: %w", f.Name, err)
			}
			fh := zip.FileHeader{Name: f.Name}
			fh.SetModTime(f.ModTime())
			w, err := zipWriter.CreateHeader(&fh)
			if err != nil {
				rc.Close()
				return nil, fmt.Errorf("patchNormalStyle: create %s: %w", f.Name, err)
			}
			if _, err := io.Copy(w, rc); err != nil {
				rc.Close()
				return nil, fmt.Errorf("patchNormalStyle: copy %s: %w", f.Name, err)
			}
			rc.Close()
		}
	}
	zipWriter.Close()
	return buf.Bytes(), nil
}

// patchHeaderNormalization removes only school-neutral duplicate suffixes.
func patchHeaderNormalization(raw []byte) ([]byte, error) {
	zipReader, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		return nil, fmt.Errorf("patchHeader: open zip: %w", err)
	}

	var buf bytes.Buffer
	zipWriter := zip.NewWriter(&buf)
	modified := false

	for _, f := range zipReader.File {
		rc, err := f.Open()
		if err != nil {
			return nil, fmt.Errorf("patchHeader: open %s: %w", f.Name, err)
		}

		isHeader := strings.HasPrefix(f.Name, "word/header") && strings.Contains(f.Name, ".xml")
		if !isHeader {
			w, err := zipWriter.CreateHeader(&zip.FileHeader{Name: f.Name, Method: zip.Deflate})
			if err != nil {
				rc.Close()
				return nil, fmt.Errorf("patchHeader: create %s: %w", f.Name, err)
			}
			io.Copy(w, rc)
			rc.Close()
			continue
		}

		xmlBytes, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			return nil, fmt.Errorf("patchHeader: read %s: %w", f.Name, err)
		}

		original := string(xmlBytes)
		patched := strings.ReplaceAll(original, "毕业设计（论文）（设计）", "毕业设计（论文）")

		if patched != original {
			modified = true
		}

		w, err := zipWriter.CreateHeader(&zip.FileHeader{Name: f.Name, Method: zip.Deflate})
		if err != nil {
			return nil, fmt.Errorf("patchHeader: create %s: %w", f.Name, err)
		}
		w.Write([]byte(patched))
	}

	zipWriter.Close()

	if !modified {
		return raw, nil
	}
	return buf.Bytes(), nil
}

// patchReferenceFontSize 修复 unioffice SaveToFile 会将参考文献条目的 run 级 sz=21
// 错误序列化为 sz=24 的 bug。在所有以 [数字] 开头的段落中将 sz=24→21。
func patchReferenceFontSize(raw []byte) ([]byte, error) {
	zipReader, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		return nil, fmt.Errorf("patchRefFont: open zip: %w", err)
	}

	var docXML []byte
	for _, f := range zipReader.File {
		if f.Name == "word/document.xml" {
			rc, err := f.Open()
			if err != nil {
				return nil, fmt.Errorf("patchRefFont: open document.xml: %w", err)
			}
			docXML, err = io.ReadAll(rc)
			rc.Close()
			if err != nil {
				return nil, fmt.Errorf("patchRefFont: read document.xml: %w", err)
			}
			break
		}
	}
	if docXML == nil {
		return raw, nil
	}

	// 匹配以 [数字] 开头的参考文献段落
	// 正则：<w:p ...> ... [number] ... </w:p>
	refParaPat := regexp.MustCompile(`<w:p[ >][^<]*?(?:<[^>]+>)*?\s*<w:r[ >].*?<w:t[^>]*?>\s*\[\d+\]`)
	if !refParaPat.Match(docXML) {
		return raw, nil
	}

	// 在 <w:sz w:val="24"/> 和 <w:szCs w:val="24"/> 中将 24→21
	// 使用更精准的方式：找到所有参考文献段落，在每段内替换
	paragraphs := regexp.MustCompile(`<w:p[ >].*?</w:p>`)
	newDocXML := paragraphs.ReplaceAllFunc(docXML, func(pMatch []byte) []byte {
		// 检查是否为参考文献段落
		if matched, _ := regexp.Match(`\[\d+\]`, pMatch); !matched {
			return pMatch
		}
		// 检查是否确实以 [数字] 开头（去除 lead XML）
		tagStripped := regexp.MustCompile(`<[^>]+>`).ReplaceAll(pMatch, nil)
		if !regexp.MustCompile(`^\s*\[\d+\]`).Match(tagStripped) {
			return pMatch
		}
		// 替换 sz=24 → sz=21
		result := bytes.ReplaceAll(pMatch, []byte(`w:val="24"`), []byte(`w:val="21"`))
		return result
	})

	if bytes.Equal(newDocXML, docXML) {
		return raw, nil
	}

	// 写回 ZIP
	var buf bytes.Buffer
	zipWriter := zip.NewWriter(&buf)
	for _, f := range zipReader.File {
		if f.Name == "word/document.xml" {
			w, err := zipWriter.Create(f.Name)
			if err != nil {
				return nil, fmt.Errorf("patchRefFont: create entry: %w", err)
			}
			if _, err := w.Write(newDocXML); err != nil {
				return nil, fmt.Errorf("patchRefFont: write entry: %w", err)
			}
		} else {
			rc, err := f.Open()
			if err != nil {
				return nil, fmt.Errorf("patchRefFont: open %s: %w", f.Name, err)
			}
			w, err := zipWriter.CreateHeader(&zip.FileHeader{
				Name:   f.Name,
				Method: f.Method,
			})
			if err != nil {
				rc.Close()
				return nil, fmt.Errorf("patchRefFont: create %s: %w", f.Name, err)
			}
			if _, err := io.Copy(w, rc); err != nil {
				rc.Close()
				return nil, fmt.Errorf("patchRefFont: copy %s: %w", f.Name, err)
			}
			rc.Close()
		}
	}
	zipWriter.Close()
	return buf.Bytes(), nil
}

// patchFooterPageNumber strips hardcoded "-" surrounding PAGE fields in footer XML,
// e.g. "-<w:fldSimple w:instr=" PAGE ">1</w:fldSimple>-" → just the PAGE field.
func patchFooterPageNumber(raw []byte) ([]byte, error) {
	zipReader, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		return nil, fmt.Errorf("patchFooter: open zip: %w", err)
	}

	var buf bytes.Buffer
	zipWriter := zip.NewWriter(&buf)
	modified := false

	for _, f := range zipReader.File {
		rc, err := f.Open()
		if err != nil {
			return nil, fmt.Errorf("patchFooter: open %s: %w", f.Name, err)
		}

		isFooter := strings.HasPrefix(f.Name, "word/footer") && strings.Contains(f.Name, ".xml")
		if !isFooter {
			w, _ := zipWriter.CreateHeader(&zip.FileHeader{Name: f.Name, Method: zip.Deflate})
			io.Copy(w, rc)
			rc.Close()
			continue
		}

		xmlBytes, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			return nil, fmt.Errorf("patchFooter: read %s: %w", f.Name, err)
		}

		original := string(xmlBytes)

		// Remove "-" dashes surrounding PAGE field.
		// Leading: <w:t>-</w:t></w:r> right before <w:fldSimple w:instr=" PAGE ">
		patched := strings.ReplaceAll(original,
			`>-</w:t></w:r><w:fldSimple w:instr=" PAGE ">`,
			`></w:t></w:r><w:fldSimple w:instr=" PAGE ">`)
		// Trailing: <w:t>-</w:t></w:r> right before </w:p>
		patched = strings.ReplaceAll(patched,
			`><w:t>-</w:t></w:r></w:p>`,
			`><w:t></w:t></w:r></w:p>`)

		if patched != original {
			modified = true
		}

		w, _ := zipWriter.CreateHeader(&zip.FileHeader{Name: f.Name, Method: zip.Deflate})
		w.Write([]byte(patched))
	}

	zipWriter.Close()

	if !modified {
		return raw, nil
	}
	return buf.Bytes(), nil
}

