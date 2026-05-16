package blockmap

type Binding struct {
	BlockID   string `json:"block_id"`
	BlockKind string `json:"block_kind"`
	Payload   string `json:"payload"`
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
