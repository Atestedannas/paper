package fileprocessor

import "testing"

func TestIsAbstractCategory(t *testing.T) {
	tests := []struct {
		category string
		want     bool
	}{
		{category: "abstract", want: true},
		{category: "en_abstract", want: true},
		{category: "english_abstract", want: true},
		{category: "body", want: false},
		{category: "references", want: false},
	}

	for _, tt := range tests {
		got := isAbstractCategory(tt.category)
		if got != tt.want {
			t.Fatalf("isAbstractCategory(%q) = %v, want %v", tt.category, got, tt.want)
		}
	}
}
