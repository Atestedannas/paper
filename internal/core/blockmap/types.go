package blockmap

type Binding struct {
	BlockID     string `json:"block_id"`
	BlockKind   string `json:"block_kind"`
	Payload     string `json:"payload"`
	PayloadKind string `json:"payload_kind,omitempty"`
	PayloadXML  string `json:"payload_xml,omitempty"`
	Level       int    `json:"level,omitempty"`
	SourceIndex int    `json:"source_index,omitempty"`
}

type MappingResult struct {
	Bindings        []Binding         `json:"bindings"`
	CoverFields     map[string]string `json:"cover_fields,omitempty"`
	GeneratedBlocks []string          `json:"generated_blocks"`
	UnmappedBlocks  []string          `json:"unmapped_blocks"`
	AmbiguousBlocks []string          `json:"ambiguous_blocks"`
}

type Mapper struct{}

func NewMapper() *Mapper {
	return &Mapper{}
}
