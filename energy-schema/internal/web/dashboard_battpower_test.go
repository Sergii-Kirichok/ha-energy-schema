package web

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestPatchBattPower(t *testing.T) {
	src := `{"views":[{"sections":[{"cards":[
	  {"type":"conditional","conditions":[{"entity":"input_select.diapazon_grafika","state":"1"}],"card":{"type":"vertical-stack","cards":[
	    {"type":"custom:apexcharts-card","series":[
	      {"color":"#7e57c2","entity":"sensor.deye_sun_30k_battery_power","float_precision":2,"group_by":{"duration":"30s","func":"avg"},
	       "name":"АКБ ±","transform":"return x / 1000;","type":"column","unit":"kW","yaxis_id":"power"},
	      {"entity":"sensor.deye_sun_30k_battery","yaxis_id":"soc"}]}]}},
	  {"type":"custom:apexcharts-card","series":[{"color":"#7e57c2","entity":"sensor.deye_sun_30k_battery_power","name":"АКБ","type":"line","yaxis_id":"pw"}]},
	  {"type":"tile","entity":"sensor.deye_sun_30k_battery_power"}]}]}]}`
	var cfg any
	_ = json.Unmarshal([]byte(src), &cfg)
	if changed, err := patchBattPower(cfg); err != nil || !changed {
		t.Fatalf("changed=%v err=%v", changed, err)
	}
	if changed, _ := patchBattPower(cfg); changed {
		t.Error("second run must be a no-op")
	}
	cards := cfg.(map[string]any)["views"].([]any)[0].(map[string]any)["sections"].([]any)[0].(map[string]any)["cards"].([]any)
	stack := cards[0].(map[string]any)["card"].(map[string]any)["cards"].([]any)[0].(map[string]any)
	want := map[string]any{"color": "#7e57c2", "entity": flowBattCorrect, "float_precision": 2.0,
		"group_by": map[string]any{"duration": "30s", "func": "avg"}, "name": "АКБ ±", "type": "column", "unit": "kW", "yaxis_id": "power"}
	if got := stack["series"].([]any)[0]; !reflect.DeepEqual(got, want) {
		t.Errorf("kW series = %v", got)
	}
	if got := stack["series"].([]any)[1].(map[string]any)["entity"]; got != "sensor.deye_sun_30k_battery" {
		t.Errorf("SOC series touched: %v", got)
	}
	w := cards[1].(map[string]any)["series"].([]any)[0].(map[string]any)
	if w["entity"] != flowBattCorrect || w["transform"] != battPowerToW || w["name"] != "АКБ" {
		t.Errorf("W series = %v", w)
	}
	tile := cards[2].(map[string]any)
	if tile["entity"] != flowBattCorrect || tile["transform"] != nil {
		t.Errorf("tile = %v", tile)
	}
}
