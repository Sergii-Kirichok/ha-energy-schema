package web

import (
	"fmt"
	"strings"
	"testing"
)

// Шаблон страницы: все подстановки на месте, есть защита для ТВ-браузеров
// (visibility у сторон карточки, таймаут/сторож обновления).
func TestIndexHTML(t *testing.T) {
	p := fmt.Sprintf(indexHTML, "false", "energy_schema.svg", 3)
	for _, want := range []string{"var CANCTL=false;", "fetch('energy_schema.svg?t=", "var EVERY=3000;",
		".f-back{transform:scaleX(0);visibility:hidden", "location.reload()", "AbortController"} {
		if !strings.Contains(p, want) {
			t.Errorf("page lacks %q", want)
		}
	}
	if strings.Contains(p, "%!") {
		t.Error("format verbs mismatch")
	}
}
