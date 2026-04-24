package paperparse

type ParsedPaper struct {
	CoverFields      map[string]string
	AbstractCN       []string
	KeywordsCN       []string
	Headings         []Heading
	Body             []string
	References       []string
	Acknowledgements []string
	Abnormal         []string
}

type Heading struct {
	Level int
	Text  string
}
