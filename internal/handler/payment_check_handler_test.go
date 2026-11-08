package handler

import "testing"

func TestPaperServicePrice(t *testing.T) {
	cfg := map[string]interface{}{
		"is_check_free": true,
		"format_check":  8.0,
		"format_fix":    0.0,
	}
	if got := paperServicePrice(cfg, "check_and_fix"); got != 0 {
		t.Fatalf("free check and free fix should cost 0, got %v", got)
	}

	cfg["format_fix"] = 12.0
	if got := paperServicePrice(cfg, "check_and_fix"); got != 0 {
		t.Fatalf("global free switch should waive check and fix, got %v", got)
	}

	cfg["is_check_free"] = false
	if got := paperServicePrice(cfg, "check_and_fix"); got != 20 {
		t.Fatalf("paid mode should charge check and fix, got %v", got)
	}
}

func TestPaperServicePriceDownload(t *testing.T) {
	for _, tc := range []struct {
		name   string
		config map[string]interface{}
		want   float64
	}{
		{"download price differs from formatting", map[string]interface{}{"paper_download": 9.9, "format_check": 0.0, "format_fix": 0.0}, 9.9},
		{"global free", map[string]interface{}{"paper_download": 9.9, "is_check_free": true}, 0},
		{"zero download price", map[string]interface{}{"paper_download": 0.0, "format_check": 8.0}, 0},
		{"missing download price", map[string]interface{}{"format_check": 8.0}, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := paperServicePrice(tc.config, "paper_download"); got != tc.want {
				t.Errorf("paperServicePrice(%v, paper_download) = %v, want %v", tc.config, got, tc.want)
			}
		})
	}
}
