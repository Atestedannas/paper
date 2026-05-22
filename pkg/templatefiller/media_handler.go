package templatefiller

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"log"
	"path"
	"strings"
)

// MediaHandler migrates images, charts, and other embedded media from the
// student's source .docx into the output .docx, updating all relationship
// references so that the content renders correctly.
type MediaHandler struct {
	// sourceRels maps original rId -> relationship target in source doc
	sourceRels map[string]relEntry
	// sourceMedia maps media filename -> bytes from source doc
	sourceMedia map[string][]byte
	// newRelID is the next available relationship ID in the output
	newRelID int
	// idMapping maps source rId -> new rId in output
	idMapping map[string]string
	// newRels holds relationships that need to be added to the output
	newRels []relEntry
	// newMedia holds media files that need to be added to the output ZIP
	newMedia map[string][]byte
	// newContentTypes holds content type overrides to add
	newContentTypes map[string]string
}

type relEntry struct {
	ID         string
	Type       string
	Target     string
	TargetMode string
}

// NewMediaHandler creates a handler from the student's source document bytes.
func NewMediaHandler(sourceDocBytes []byte) (*MediaHandler, error) {
	h := &MediaHandler{
		sourceRels:      make(map[string]relEntry),
		sourceMedia:     make(map[string][]byte),
		newRelID:        100,
		idMapping:       make(map[string]string),
		newRels:         nil,
		newMedia:        make(map[string][]byte),
		newContentTypes: make(map[string]string),
	}

	reader, err := zip.NewReader(bytes.NewReader(sourceDocBytes), int64(len(sourceDocBytes)))
	if err != nil {
		return nil, fmt.Errorf("open source docx: %w", err)
	}

	for _, file := range reader.File {
		rc, err := file.Open()
		if err != nil {
			continue
		}
		content, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			continue
		}

		if file.Name == "word/_rels/document.xml.rels" {
			h.parseRelationships(content)
		}
		if strings.HasPrefix(file.Name, "word/media/") {
			filename := path.Base(file.Name)
			h.sourceMedia[filename] = content
		}
	}

	return h, nil
}

func (h *MediaHandler) parseRelationships(data []byte) {
	type Relationship struct {
		ID         string `xml:"Id,attr"`
		Type       string `xml:"Type,attr"`
		Target     string `xml:"Target,attr"`
		TargetMode string `xml:"TargetMode,attr"`
	}
	type Relationships struct {
		XMLName xml.Name       `xml:"Relationships"`
		Rels    []Relationship `xml:"Relationship"`
	}

	var rels Relationships
	if err := xml.Unmarshal(data, &rels); err != nil {
		log.Printf("[MediaHandler] failed to parse rels: %v", err)
		return
	}

	for _, r := range rels.Rels {
		h.sourceRels[r.ID] = relEntry{
			ID:         r.ID,
			Type:       r.Type,
			Target:     r.Target,
			TargetMode: r.TargetMode,
		}
	}
}

// RewriteEmbedReferences scans a paragraph XML for r:embed and r:link attributes
// that reference source document media, maps them to new IDs, and stages the
// media files for inclusion in the output ZIP.
func (h *MediaHandler) RewriteEmbedReferences(paraXML string) string {
	// Find all r:embed="rIdXX" and r:link="rIdXX" references
	result := paraXML

	for oldID, rel := range h.sourceRels {
		if !strings.Contains(result, oldID) {
			continue
		}
		if !isMediaRelType(rel.Type) {
			continue
		}

		newID, ok := h.idMapping[oldID]
		if !ok {
			newID = fmt.Sprintf("rId%d", h.newRelID)
			h.newRelID++
			h.idMapping[oldID] = newID

			h.newRels = append(h.newRels, relEntry{
				ID:     newID,
				Type:   rel.Type,
				Target: rel.Target,
			})

			mediaFile := path.Base(rel.Target)
			if data, exists := h.sourceMedia[mediaFile]; exists {
				h.newMedia["word/"+rel.Target] = data
			}
		}

		result = strings.ReplaceAll(result, `"`+oldID+`"`, `"`+newID+`"`)
	}

	return result
}

// GetNewRelationships returns XML for relationships that need to be added
// to the output document's word/_rels/document.xml.rels.
func (h *MediaHandler) GetNewRelationships() []string {
	var rels []string
	for _, r := range h.newRels {
		rel := fmt.Sprintf(`<Relationship Id="%s" Type="%s" Target="%s"/>`, r.ID, r.Type, r.Target)
		rels = append(rels, rel)
	}
	return rels
}

// GetNewMediaFiles returns a map of path -> bytes for media files to add to the ZIP.
func (h *MediaHandler) GetNewMediaFiles() map[string][]byte {
	return h.newMedia
}

// HasMedia returns true if the handler has any media to migrate.
func (h *MediaHandler) HasMedia() bool {
	return len(h.newMedia) > 0
}

func isMediaRelType(relType string) bool {
	return strings.Contains(relType, "image") ||
		strings.Contains(relType, "chart") ||
		strings.Contains(relType, "diagramData") ||
		strings.Contains(relType, "oleObject")
}

// processZipWithMedia extends the basic processZip to also handle media migration.
// This is called from OOXMLFiller when the student document contains images/media.
func processZipWithMedia(templateBytes []byte, sections map[string]*SectionContent, mediaHandler *MediaHandler, filler *OOXMLFiller) ([]byte, error) {
	reader, err := zip.NewReader(bytes.NewReader(templateBytes), int64(len(templateBytes)))
	if err != nil {
		return nil, fmt.Errorf("open template zip: %w", err)
	}

	var buf bytes.Buffer
	writer := zip.NewWriter(&buf)

	for _, file := range reader.File {
		rc, err := file.Open()
		if err != nil {
			return nil, fmt.Errorf("open zip entry %s: %w", file.Name, err)
		}

		content, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			return nil, fmt.Errorf("read zip entry %s: %w", file.Name, err)
		}

		if file.Name == "word/document.xml" {
			content, err = filler.processDocumentXML(content, sections)
			if err != nil {
				return nil, fmt.Errorf("process document.xml: %w", err)
			}
		}

		// Inject new relationships into document.xml.rels
		if file.Name == "word/_rels/document.xml.rels" && mediaHandler.HasMedia() {
			content = injectRelationships(content, mediaHandler.GetNewRelationships())
		}

		header := &zip.FileHeader{Name: file.Name, Method: file.Method}
		w, err := writer.CreateHeader(header)
		if err != nil {
			return nil, fmt.Errorf("create zip entry %s: %w", file.Name, err)
		}
		if _, err := w.Write(content); err != nil {
			return nil, fmt.Errorf("write zip entry %s: %w", file.Name, err)
		}
	}

	// Add new media files
	for path, data := range mediaHandler.GetNewMediaFiles() {
		w, err := writer.Create(path)
		if err != nil {
			return nil, fmt.Errorf("create media entry %s: %w", path, err)
		}
		if _, err := w.Write(data); err != nil {
			return nil, fmt.Errorf("write media entry %s: %w", path, err)
		}
	}

	if err := writer.Close(); err != nil {
		return nil, fmt.Errorf("close zip writer: %w", err)
	}

	return buf.Bytes(), nil
}

// injectRelationships adds new Relationship elements before </Relationships>.
func injectRelationships(relsXML []byte, newRels []string) []byte {
	s := string(relsXML)
	closing := "</Relationships>"
	idx := strings.LastIndex(s, closing)
	if idx == -1 {
		return relsXML
	}
	insert := strings.Join(newRels, "\n  ")
	return []byte(s[:idx] + "\n  " + insert + "\n" + s[idx:])
}
