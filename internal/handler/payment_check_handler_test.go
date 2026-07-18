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
