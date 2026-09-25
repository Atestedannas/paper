package repaircontract

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/xml"
	"fmt"
	"io"
	"path"
	"strings"

	"github.com/paper-format-checker/backend/internal/core/ooxmlpatch"
	"github.com/paper-format-checker/backend/internal/core/ooxmlpkg"
)

// StructureSnapshot records content-bearing OOXML objects by semantic identity,
// so harmless relationship-ID or media-path remapping does not look like data loss.
type StructureSnapshot struct {
	Sections              int            `json:"sections"`
	Bookmarks             map[string]int `json:"bookmarks"`
	BookmarkEnds          map[string]int `json:"bookmark_ends"`
	Fields                map[string]int `json:"fields"`
	Formulas              int            `json:"formulas"`
	Drawings              int            `json:"drawings"`
	Pictures              int            `json:"pictures"`
	FootnoteReferences    int            `json:"footnote_references"`
	EndnoteReferences     int            `json:"endnote_references"`
	CommentReferences     int            `json:"comment_references"`
	CommentRangeStarts    int            `json:"comment_range_starts"`
	CommentRangeEnds      int            `json:"comment_range_ends"`
	FootnoteDefinitions   int            `json:"footnote_definitions"`
	EndnoteDefinitions    int            `json:"endnote_definitions"`
	CommentDefinitions    int            `json:"comment_definitions"`
	Hyperlinks            map[string]int `json:"hyperlinks"`
	VerticalAlignments    map[string]int `json:"vertical_alignments"`
	MediaHashes           map[string]int `json:"media_hashes"`
	ReferencedImageHashes map[string]int `json:"referenced_image_hashes"`
	RelationshipTargets   map[string]int `json:"relationship_targets"`
	ProtectedPartHashes   map[string]int `json:"protected_part_hashes"`
}

type packageRelationship struct {
	Type       string
	Target     string
	TargetMode string
}

type complexField struct {
	instruction strings.Builder
}

// CaptureStructure reads only the OOXML package. It does not require Word,
// LibreOffice, rendering, or any external process.
func CaptureStructure(docxPath string) (StructureSnapshot, error) {
	pkg, err := ooxmlpkg.Open(docxPath)
	if err != nil {
		return StructureSnapshot{}, fmt.Errorf("open OOXML structure snapshot: %w", err)
	}
	snapshot := newStructureSnapshot()
	names := pkg.Names()
	mutableParts := map[string]bool{"word/document.xml": true}
	for _, name := range names {
		if strings.HasSuffix(name, ".rels") {
			if err := captureRelationshipTargets(pkg, name, mutableParts, &snapshot); err != nil {
				return StructureSnapshot{}, err
			}
		}
	}
	for _, name := range names {
		if !mutableInfrastructurePart(name, mutableParts) {
			body, ok := pkg.Get(name)
			if !ok {
				return StructureSnapshot{}, fmt.Errorf("read protected OOXML part %s", name)
			}
			snapshot.ProtectedPartHashes[contentHash(body)]++
		}
		if strings.HasPrefix(name, "word/media/") && !strings.HasSuffix(name, "/") {
			if body, ok := pkg.Get(name); ok {
				snapshot.MediaHashes[contentHash(body)]++
			}
		}
		if !isContentPart(name) {
			continue
		}
		body, ok := pkg.Get(name)
		if !ok {
			return StructureSnapshot{}, fmt.Errorf("read OOXML content part %s", name)
		}
		relationships, err := relationshipsForPart(pkg, name)
		if err != nil {
			return StructureSnapshot{}, err
		}
		if err := capturePartStructure(pkg, name, body, relationships, &snapshot); err != nil {
			return StructureSnapshot{}, fmt.Errorf("parse OOXML content part %s: %w", name, err)
		}
	}
	return snapshot, nil
}

// ValidateStructurePreserved rejects any loss of source structure. Extra
// objects are permitted because formatting can legitimately add fields,
// headers, footers, or media while retaining every source object.
func ValidateStructurePreserved(before, after StructureSnapshot) []ValidationIssue {
	var issues []ValidationIssue
	if after.Sections < before.Sections {
		issues = append(issues, structureIssue("structure_sections",
			fmt.Sprintf("section count decreased: source=%d output=%d", before.Sections, after.Sections)))
	}
	issues = appendMissing(issues, "structure_bookmarks", "bookmarks", before.Bookmarks, after.Bookmarks, true)
	issues = appendMissing(issues, "structure_bookmark_ends", "bookmark end markers", before.BookmarkEnds, after.BookmarkEnds, true)
	issues = appendMissing(issues, "structure_fields", "fields", before.Fields, after.Fields, true)
	issues = appendCountLoss(issues, "structure_formulas", "formulas", before.Formulas, after.Formulas)
	issues = appendCountLoss(issues, "structure_drawings", "drawings", before.Drawings, after.Drawings)
	issues = appendCountLoss(issues, "structure_pictures", "pictures", before.Pictures, after.Pictures)
	issues = appendCountLoss(issues, "structure_footnote_references", "footnote references", before.FootnoteReferences, after.FootnoteReferences)
	issues = appendCountLoss(issues, "structure_endnote_references", "endnote references", before.EndnoteReferences, after.EndnoteReferences)
	issues = appendCountLoss(issues, "structure_comment_references", "comment references", before.CommentReferences, after.CommentReferences)
	issues = appendCountLoss(issues, "structure_comment_ranges", "comment range starts", before.CommentRangeStarts, after.CommentRangeStarts)
	issues = appendCountLoss(issues, "structure_comment_ranges", "comment range ends", before.CommentRangeEnds, after.CommentRangeEnds)
	issues = appendCountLoss(issues, "structure_footnote_definitions", "footnote definitions", before.FootnoteDefinitions, after.FootnoteDefinitions)
	issues = appendCountLoss(issues, "structure_endnote_definitions", "endnote definitions", before.EndnoteDefinitions, after.EndnoteDefinitions)
	issues = appendCountLoss(issues, "structure_comment_definitions", "comment definitions", before.CommentDefinitions, after.CommentDefinitions)
	issues = appendMissing(issues, "structure_hyperlinks", "hyperlinks", before.Hyperlinks, after.Hyperlinks, true)
	issues = appendMissing(issues, "structure_vertical_alignments", "superscript/subscript properties", before.VerticalAlignments, after.VerticalAlignments, true)
	issues = appendMissing(issues, "structure_media", "media content", before.MediaHashes, after.MediaHashes, false)
	issues = appendMissing(issues, "structure_image_relationships", "referenced image content", before.ReferencedImageHashes, after.ReferencedImageHashes, true)
	issues = appendMissing(issues, "structure_relationship_targets", "relationship targets", before.RelationshipTargets, after.RelationshipTargets, true)
	issues = appendMissing(issues, "structure_protected_parts", "protected OOXML parts", before.ProtectedPartHashes, after.ProtectedPartHashes, true)
	return issues
}

func newStructureSnapshot() StructureSnapshot {
	return StructureSnapshot{
		Bookmarks:             map[string]int{},
		BookmarkEnds:          map[string]int{},
		Fields:                map[string]int{},
		Hyperlinks:            map[string]int{},
		VerticalAlignments:    map[string]int{},
		MediaHashes:           map[string]int{},
		ReferencedImageHashes: map[string]int{},
		RelationshipTargets:   map[string]int{},
		ProtectedPartHashes:   map[string]int{},
	}
}

func mutableInfrastructurePart(name string, mutableParts map[string]bool) bool {
	if strings.HasSuffix(name, "/") {
		return true
	}
	name = path.Clean(strings.TrimPrefix(name, "/"))
	return name == "[Content_Types].xml" || strings.HasSuffix(name, ".rels") || mutableParts[name]
}

func isContentPart(name string) bool {
	if name == "word/document.xml" || name == "word/footnotes.xml" ||
		name == "word/endnotes.xml" || name == "word/comments.xml" {
		return true
	}
	base := path.Base(name)
	return strings.HasPrefix(base, "header") && strings.HasSuffix(base, ".xml") ||
		strings.HasPrefix(base, "footer") && strings.HasSuffix(base, ".xml")
}

func capturePartStructure(pkg *ooxmlpkg.DocxPackage, partName string, body []byte, relationships map[string]packageRelationship, snapshot *StructureSnapshot) error {
	// Header repairs may change a style ID or localized built-in name into an
	// equivalent selector. Compare that semantic identity, retaining switches.
	var arguments map[string]string
	if strings.HasPrefix(partName, "word/header") {
		if styles, ok := pkg.Get("word/styles.xml"); ok {
			var err error
			arguments, err = ooxmlpatch.RunningHeaderStyleArguments(styles)
			if err != nil {
				return err
			}
		}
	}
	fieldIdentity := func(code string) string {
		return normalizedField(ooxmlpatch.NormalizeStyleRef(code, arguments))
	}
	decoder := xml.NewDecoder(strings.NewReader(string(body)))
	var fields []*complexField
	var instruction strings.Builder
	inInstruction := false
	bookmarkNamesByID := map[string]string{}
	for {
		token, err := decoder.Token()
		if err != nil {
			if err == io.EOF {
				break
			}
			return err
		}
		switch value := token.(type) {
		case xml.StartElement:
			switch value.Name.Local {
			case "sectPr":
				if partName == "word/document.xml" {
					snapshot.Sections++
				}
			case "bookmarkStart":
				name := attrValue(value.Attr, "name")
				snapshot.Bookmarks[name]++
				bookmarkNamesByID[attrValue(value.Attr, "id")] = name
			case "bookmarkEnd":
				name := bookmarkNamesByID[attrValue(value.Attr, "id")]
				if name == "" {
					name = "<unresolved>"
				}
				snapshot.BookmarkEnds[name]++
			case "fldSimple":
				snapshot.Fields[fieldIdentity(attrValue(value.Attr, "instr"))]++
			case "fldChar":
				switch strings.ToLower(attrValue(value.Attr, "fldCharType")) {
				case "begin":
					fields = append(fields, &complexField{})
				case "end":
					if len(fields) > 0 {
						last := fields[len(fields)-1]
						fields = fields[:len(fields)-1]
						snapshot.Fields[fieldIdentity(last.instruction.String())]++
					}
				}
			case "instrText":
				inInstruction = true
				instruction.Reset()
			case "oMath":
				snapshot.Formulas++
			case "drawing":
				snapshot.Drawings++
			case "pict":
				snapshot.Pictures++
			case "footnoteReference":
				snapshot.FootnoteReferences++
			case "endnoteReference":
				snapshot.EndnoteReferences++
			case "commentReference":
				snapshot.CommentReferences++
			case "commentRangeStart":
				snapshot.CommentRangeStarts++
			case "commentRangeEnd":
				snapshot.CommentRangeEnds++
			case "footnote":
				if partName == "word/footnotes.xml" {
					snapshot.FootnoteDefinitions++
				}
			case "endnote":
				if partName == "word/endnotes.xml" {
					snapshot.EndnoteDefinitions++
				}
			case "comment":
				if partName == "word/comments.xml" {
					snapshot.CommentDefinitions++
				}
			case "hyperlink":
				snapshot.Hyperlinks[hyperlinkIdentity(value.Attr, relationships)]++
			case "vertAlign":
				snapshot.VerticalAlignments[attrValue(value.Attr, "val")]++
			case "blip":
				captureImageRelationship(pkg, partName, firstAttr(value.Attr, "embed", "link"), relationships, snapshot)
			case "imagedata":
				captureImageRelationship(pkg, partName, attrValue(value.Attr, "id"), relationships, snapshot)
			}
		case xml.CharData:
			if inInstruction {
				instruction.Write([]byte(value))
			}
		case xml.EndElement:
			if value.Name.Local == "instrText" {
				if len(fields) > 0 {
					fields[len(fields)-1].instruction.WriteString(instruction.String())
				}
				inInstruction = false
			}
		}
	}
	for _, field := range fields {
		snapshot.Fields[fieldIdentity(field.instruction.String())]++
	}
	return nil
}

func relationshipsForPart(pkg *ooxmlpkg.DocxPackage, partName string) (map[string]packageRelationship, error) {
	relsName := path.Join(path.Dir(partName), "_rels", path.Base(partName)+".rels")
	body, ok := pkg.Get(relsName)
	if !ok {
		return map[string]packageRelationship{}, nil
	}
	relationships := map[string]packageRelationship{}
	decoder := xml.NewDecoder(strings.NewReader(string(body)))
	for {
		token, err := decoder.Token()
		if err != nil {
			if err == io.EOF {
				return relationships, nil
			}
			return nil, fmt.Errorf("parse OOXML relationships %s: %w", relsName, err)
		}
		start, ok := token.(xml.StartElement)
		if !ok || start.Name.Local != "Relationship" {
			continue
		}
		relationships[attrValue(start.Attr, "Id")] = packageRelationship{
			Type:       attrValue(start.Attr, "Type"),
			Target:     attrValue(start.Attr, "Target"),
			TargetMode: attrValue(start.Attr, "TargetMode"),
		}
	}
}

func captureRelationshipTargets(pkg *ooxmlpkg.DocxPackage, relationshipsPart string, mutableParts map[string]bool, snapshot *StructureSnapshot) error {
	sourcePart, ok := sourcePartForRelationships(relationshipsPart)
	if !ok {
		return nil
	}
	body, ok := pkg.Get(relationshipsPart)
	if !ok {
		return fmt.Errorf("read OOXML relationships part %s", relationshipsPart)
	}
	decoder := xml.NewDecoder(strings.NewReader(string(body)))
	for {
		token, err := decoder.Token()
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return fmt.Errorf("parse OOXML relationships %s: %w", relationshipsPart, err)
		}
		start, ok := token.(xml.StartElement)
		if !ok || start.Name.Local != "Relationship" {
			continue
		}
		relationship := packageRelationship{
			Type:       attrValue(start.Attr, "Type"),
			Target:     attrValue(start.Attr, "Target"),
			TargetMode: attrValue(start.Attr, "TargetMode"),
		}
		if relationshipInfrastructureType(relationship.Type) && !strings.EqualFold(strings.TrimSpace(relationship.TargetMode), "External") {
			mutableParts[resolveRelationshipTarget(sourcePart, relationship.Target)] = true
			continue
		}
		snapshot.RelationshipTargets[relationshipTargetIdentity(pkg, sourcePart, relationship)]++
	}
}

func sourcePartForRelationships(relationshipsPart string) (string, bool) {
	clean := path.Clean(strings.TrimPrefix(relationshipsPart, "/"))
	base := path.Base(clean)
	parent := path.Dir(clean)
	if !strings.HasSuffix(base, ".rels") || path.Base(parent) != "_rels" {
		return "", false
	}
	sourceBase := strings.TrimSuffix(base, ".rels")
	sourceDir := path.Dir(parent)
	if sourceBase == "" {
		return "", true
	}
	if sourceDir == "." {
		return sourceBase, true
	}
	return path.Join(sourceDir, sourceBase), true
}

func relationshipInfrastructureType(relationshipType string) bool {
	const transitional = "http://schemas.openxmlformats.org/officedocument/2006/relationships/"
	const strict = "http://purl.oclc.org/ooxml/officedocument/relationships/"

	relationshipType = strings.ToLower(strings.TrimSpace(relationshipType))
	if relationshipType == "http://schemas.openxmlformats.org/package/2006/relationships/metadata/core-properties" {
		return true
	}
	var suffix string
	switch {
	case strings.HasPrefix(relationshipType, transitional):
		suffix = strings.TrimPrefix(relationshipType, transitional)
	case strings.HasPrefix(relationshipType, strict):
		suffix = strings.TrimPrefix(relationshipType, strict)
	default:
		return false
	}
	suffix = strings.ToLower(suffix)
	switch suffix {
	case "officedocument",
		"metadata/core-properties",
		"extended-properties",
		"custom-properties",
		"styles",
		"styleswitheffects",
		"numbering",
		"theme",
		"fonttable",
		"settings",
		"websettings",
		"header",
		"footer":
		return true
	default:
		return false
	}
}

func relationshipTargetIdentity(pkg *ooxmlpkg.DocxPackage, sourcePart string, relationship packageRelationship) string {
	relationshipType := strings.TrimSpace(relationship.Type)
	if relationshipType == "" {
		relationshipType = "<unknown>"
	}
	if strings.EqualFold(strings.TrimSpace(relationship.TargetMode), "External") {
		return relationshipType + "\x1fexternal\x1f" + strings.TrimSpace(relationship.Target)
	}
	target := resolveRelationshipTarget(sourcePart, relationship.Target)
	if body, ok := pkg.Get(target); ok {
		return relationshipType + "\x1finternal-sha256\x1f" + contentHash(body)
	}
	return relationshipType + "\x1fmissing\x1f" + target
}

func resolveRelationshipTarget(sourcePart, target string) string {
	target = strings.ReplaceAll(strings.TrimSpace(target), `\`, "/")
	if strings.HasPrefix(target, "/") {
		return path.Clean(strings.TrimPrefix(target, "/"))
	}
	sourceDir := path.Dir(sourcePart)
	if sourcePart == "" || sourceDir == "." {
		sourceDir = ""
	}
	return path.Clean(path.Join(sourceDir, target))
}

func hyperlinkIdentity(attrs []xml.Attr, relationships map[string]packageRelationship) string {
	var values []string
	if anchor := attrValue(attrs, "anchor"); anchor != "" {
		values = append(values, "anchor="+anchor)
	}
	if id := attrValue(attrs, "id"); id != "" {
		if relationship, ok := relationships[id]; ok {
			values = append(values, "target="+relationship.Target)
		} else {
			values = append(values, "target=<unresolved>")
		}
	}
	if len(values) == 0 {
		return "<empty>"
	}
	return strings.Join(values, "\x1f")
}

func captureImageRelationship(pkg *ooxmlpkg.DocxPackage, partName, id string, relationships map[string]packageRelationship, snapshot *StructureSnapshot) {
	if id == "" {
		return
	}
	relationship, ok := relationships[id]
	if !ok {
		snapshot.ReferencedImageHashes["unresolved"]++
		return
	}
	if strings.EqualFold(relationship.TargetMode, "External") {
		snapshot.ReferencedImageHashes["external:"+relationship.Target]++
		return
	}
	target := relationship.Target
	if strings.HasPrefix(target, "/") {
		target = strings.TrimPrefix(target, "/")
	} else {
		target = path.Join(path.Dir(partName), target)
	}
	body, ok := pkg.Get(path.Clean(target))
	if !ok {
		snapshot.ReferencedImageHashes["missing:"+path.Clean(target)]++
		return
	}
	snapshot.ReferencedImageHashes[contentHash(body)]++
}

func attrValue(attrs []xml.Attr, local string) string {
	for _, attr := range attrs {
		if attr.Name.Local == local {
			return attr.Value
		}
	}
	return ""
}

func firstAttr(attrs []xml.Attr, names ...string) string {
	for _, name := range names {
		if value := attrValue(attrs, name); value != "" {
			return value
		}
	}
	return ""
}

func normalizedField(value string) string {
	value = strings.Join(strings.Fields(value), " ")
	if value == "" {
		return "<empty>"
	}
	return value
}

func contentHash(body []byte) string {
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])
}

func structureIssue(kind, message string) ValidationIssue {
	return ValidationIssue{Kind: kind, Message: "structure preservation failed: " + message}
}

func appendCountLoss(issues []ValidationIssue, kind, label string, before, after int) []ValidationIssue {
	if after < before {
		return append(issues, structureIssue(kind, fmt.Sprintf("%s decreased: source=%d output=%d", label, before, after)))
	}
	return issues
}

func appendMissing(issues []ValidationIssue, kind, label string, before, after map[string]int, compareCounts bool) []ValidationIssue {
	for value, count := range before {
		if (!compareCounts && after[value] == 0) || (compareCounts && after[value] < count) {
			return append(issues, structureIssue(kind,
				fmt.Sprintf("%s lost %q: source=%d output=%d", label, value, count, after[value])))
		}
	}
	return issues
}
