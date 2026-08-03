package ooxmlpkg

import (
	"fmt"
	"path"
	"regexp"
	"strings"
)

const officeRelationshipPrefix = "http://schemas.openxmlformats.org/officeDocument/2006/relationships/"

var (
	relationshipElementPattern = regexp.MustCompile(`<Relationship\b[^>]*(?:/>|>\s*</Relationship>)`)
	documentReferencePattern   = regexp.MustCompile(`<w:(headerReference|footerReference)\b[^>]*/>`)
	alternateContentPattern    = regexp.MustCompile(`(?s)<mc:AlternateContent\b[^>]*>.*?</mc:AlternateContent>`)
	namespaceAttributePattern  = regexp.MustCompile(`xmlns:([A-Za-z_][A-Za-z0-9_.-]*)="([^"]+)"`)
	requiresAttributePattern   = regexp.MustCompile(`Requires="([^"]+)"`)
)

type packageRelationship struct {
	ID, Type, Target string
}

func repairDocumentRelationshipsAndCompatibility(pkg *DocxPackage) (int, error) {
	changed := 0
	rels, ok := pkg.Get("word/_rels/document.xml.rels")
	if ok {
		repairedRels, relationships, count := repairSingletonRelationships(rels)
		if count > 0 {
			if err := pkg.Set("word/_rels/document.xml.rels", repairedRels); err != nil {
				return 0, err
			}
			changed += count
		}
		if documentXML, exists := pkg.Get("word/document.xml"); exists {
			repairedDocument, referenceCount := repairHeaderFooterReferences(documentXML, relationships)
			if referenceCount > 0 {
				if err := pkg.Set("word/document.xml", repairedDocument); err != nil {
					return 0, err
				}
				changed += referenceCount
			}
		}
	}
	if settings, ok := pkg.Get("word/settings.xml"); ok {
		repaired, count := repairMarkupCompatibilityRequires(settings)
		if count > 0 {
			if err := pkg.Set("word/settings.xml", repaired); err != nil {
				return 0, err
			}
			changed += count
		}
	}
	return changed, nil
}

func repairSingletonRelationships(content []byte) ([]byte, []packageRelationship, int) {
	seenSingleton := map[string]bool{}
	relationships := make([]packageRelationship, 0)
	changed := 0
	output := relationshipElementPattern.ReplaceAllStringFunc(string(content), func(element string) string {
		relationship := packageRelationship{
			ID:     xmlAttribute(element, "Id"),
			Type:   xmlAttribute(element, "Type"),
			Target: xmlAttribute(element, "Target"),
		}
		if relationship.ID == "" || relationship.Type == "" {
			return element
		}
		if isSingletonOfficeRelationship(relationship.Type) {
			if seenSingleton[relationship.Type] {
				changed++
				return ""
			}
			seenSingleton[relationship.Type] = true
		}
		relationships = append(relationships, relationship)
		return element
	})
	return []byte(output), relationships, changed
}

func isSingletonOfficeRelationship(relationshipType string) bool {
	switch strings.TrimPrefix(relationshipType, officeRelationshipPrefix) {
	case "settings", "styles", "numbering", "fontTable", "theme", "webSettings":
		return true
	default:
		return false
	}
}

func repairHeaderFooterReferences(documentXML []byte, relationships []packageRelationship) ([]byte, int) {
	byID := make(map[string]packageRelationship, len(relationships))
	byTypeTarget := make(map[string]string, len(relationships))
	for _, relationship := range relationships {
		byID[relationship.ID] = relationship
		byTypeTarget[relationship.Type+"|"+path.Base(relationship.Target)] = relationship.ID
	}
	changed := 0
	output := documentReferencePattern.ReplaceAllStringFunc(string(documentXML), func(element string) string {
		match := documentReferencePattern.FindStringSubmatch(element)
		if len(match) != 2 {
			return element
		}
		wantKind := strings.TrimSuffix(match[1], "Reference")
		id := xmlAttribute(element, "r:id")
		current, ok := byID[id]
		if !ok || strings.HasSuffix(current.Type, "/"+wantKind) {
			return element
		}
		currentBase := path.Base(current.Target)
		currentKind := "header"
		if strings.HasPrefix(strings.ToLower(currentBase), "footer") {
			currentKind = "footer"
		}
		if currentKind == wantKind {
			return element
		}
		wantBase := wantKind + strings.TrimPrefix(currentBase, currentKind)
		wantType := officeRelationshipPrefix + wantKind
		replacementID := byTypeTarget[wantType+"|"+wantBase]
		if replacementID == "" {
			return element
		}
		changed++
		return replaceXMLAttribute(element, "r:id", replacementID)
	})
	return []byte(output), changed
}

func repairMarkupCompatibilityRequires(settings []byte) ([]byte, int) {
	changed := 0
	output := alternateContentPattern.ReplaceAllStringFunc(string(settings), func(block string) string {
		namespaces := map[string]string{}
		for _, match := range namespaceAttributePattern.FindAllStringSubmatch(block, -1) {
			namespaces[match[1]] = match[2]
		}
		return requiresAttributePattern.ReplaceAllStringFunc(block, func(attribute string) string {
			match := requiresAttributePattern.FindStringSubmatch(attribute)
			if len(match) != 2 {
				return attribute
			}
			required := strings.Fields(match[1])
			repaired := false
			for index, prefix := range required {
				if _, defined := namespaces[prefix]; defined {
					continue
				}
				for candidate, namespaceURI := range namespaces {
					if strings.Contains(strings.ToLower(namespaceURI), strings.ToLower(prefix)) {
						required[index] = candidate
						repaired = true
						break
					}
				}
			}
			if !repaired {
				return attribute
			}
			changed++
			return `Requires="` + strings.Join(required, " ") + `"`
		})
	})
	return []byte(output), changed
}

func xmlAttribute(element, name string) string {
	pattern := regexp.MustCompile(`(?:^|\s)` + regexp.QuoteMeta(name) + `="([^"]*)"`)
	match := pattern.FindStringSubmatch(element)
	if len(match) != 2 {
		return ""
	}
	return match[1]
}

func replaceXMLAttribute(element, name, value string) string {
	pattern := regexp.MustCompile(`((?:^|\s)` + regexp.QuoteMeta(name) + `=")[^"]*(")`)
	return pattern.ReplaceAllString(element, fmt.Sprintf(`${1}%s${2}`, value))
}
