package paperparse

type ParsedPaper struct {
	CoverFields      map[string]string `json:"cover_fields"`
	AbstractCN       []string          `json:"abstract_cn"`
	KeywordsCN       []string          `json:"keywords_cn"`
	Headings         []Heading         `json:"headings"`
	Body             []string          `json:"body"`
	References       []string          `json:"references"`
	Acknowledgements []string          `json:"acknowledgements"`
	ContentBlocks    []ContentBlock    `json:"content_blocks"`
	Abnormal         []string          `json:"abnormal"`
}

type Heading struct {
	Level int    `json:"level"`
	Text  string `json:"text"`
}

type ContentBlock struct {
	Kind  string `json:"kind"`
	Level int    `json:"level,omitempty"`
	Text  string `json:"text"`
	XML   string `json:"xml,omitempty"`
}
