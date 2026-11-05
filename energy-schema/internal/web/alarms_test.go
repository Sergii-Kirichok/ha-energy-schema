package web

import (
	"strings"
	"testing"
)

func TestDecodeAlarms(t *testing.T) {
	// 553 бит 2 → W03; 555 бит 12 → F13; 557 бит 9 (бит 41) → F42; 558 бит 15 (бит 63) → F64
	got := decodeAlarms(map[int]int{553: 1 << 2, 555: 1 << 12, 557: 1 << 9, 558: 1 << 15})
	want := []string{"F13 — смена режима работы", "F42 — напряжение сети вне диапазона", "F64 — перегрев радиатора", "W03 — ошибка фаз сети (чередование / обрыв фазы)"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("got %q\nwant %q", got, want)
	}
	if got := decodeAlarms(map[int]int{556: 1 << 4}); len(got) != 1 || !strings.HasPrefix(got[0], "F21 — код Deye") {
		t.Errorf("unknown code: %q", got) // неизвестный бит — всё равно с номером
	}
	if got := decodeAlarms(nil); len(got) != 0 {
		t.Errorf("no registers: %q", got)
	}
}

func TestMergeAlarmsKeepsBatteryAndFallsBack(t *testing.T) {
	ha := []string{"неисправность: Problem", "АКБ: тревога BMS"}
	if got := mergeAlarms([]string{"F42 — x"}, ha); len(got) != 2 || got[1] != "АКБ: тревога BMS" {
		t.Errorf("merge: %q", got) // HA-текст неисправности заменён кодом, АКБ остаётся
	}
	if got := mergeAlarms(nil, ha); len(got) != 2 {
		t.Errorf("fallback to HA: %q", got)
	}
}

func TestPatchAlarmsOnce(t *testing.T) {
	cfg := map[string]any{"views": []any{map[string]any{"cards": []any{
		map[string]any{"type": "entities", "entities": []any{dashAnchorEntity}},
	}}}}
	if ch, _ := patchAlarms(cfg); !ch {
		t.Fatal("first patch must change")
	}
	views := cfg["views"].([]any)
	cards := views[0].(map[string]any)["cards"].([]any)
	if len(views) != 2 || len(cards) != 2 || cards[0].(map[string]any)["type"] != "conditional" {
		t.Fatalf("views=%d cards=%d first=%v", len(views), len(cards), cards[0])
	}
	if ch, _ := patchAlarms(cfg); ch {
		t.Error("second patch must be a no-op")
	}
}
