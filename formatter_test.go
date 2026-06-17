package main

import (
	"strings"
	"testing"
)

func TestTranslateMeasurementEnumValuePestDamage(t *testing.T) {
	got := translateMeasurementEnumValue("Поврежденность вредителями", "individual_plants_damaged")
	want := "повреждены отдельные растения"
	if got != want {
		t.Fatalf("unexpected translation: want %q, got %q", want, got)
	}
}

func TestFormatMeasurementPlantHeightAddsCentimeters(t *testing.T) {
	got := measurementCalculatedValue(map[string]any{
		"measurement_type_name": "plant_height",
		"calculated_value":      "35",
	})
	want := "35 см"
	if got != want {
		t.Fatalf("unexpected plant height: want %q, got %q", want, got)
	}
}

func TestTranslateMeasurementEnumValueSinglePlantsAffected(t *testing.T) {
	got := translateMeasurementEnumValue("Поражение болезнями", "single_plants_affected")
	want := "поражены отдельные растения"
	if got != want {
		t.Fatalf("unexpected disease translation: want %q, got %q", want, got)
	}
}

func TestFormatMeasurementRowsCountBooleanAsOneRow(t *testing.T) {
	got := formatMeasurementValue("density_of_planting_linear_rows_count", "yes")
	want := "1 ряд"
	if got != want {
		t.Fatalf("unexpected rows count: want %q, got %q", want, got)
	}
}

func TestFormatMeasurementLengthDoesNotTranslateOneToYes(t *testing.T) {
	got := formatMeasurementValue("length_of_row", "1")
	want := "1 м"
	if got != want {
		t.Fatalf("unexpected row length: want %q, got %q", want, got)
	}
}

func TestFormatMeasurementTreatmentDepthAddsCentimeters(t *testing.T) {
	got := formatMeasurementValue("glubina_obrabotki", "4")
	want := "4 см"
	if got != want {
		t.Fatalf("unexpected treatment depth: want %q, got %q", want, got)
	}
}

func TestFormatMeasurementSPADDoesNotTranslateOneToYes(t *testing.T) {
	got := measurementCalculatedValue(map[string]any{
		"measurement_type_name":      "SPAD",
		"calculated_value":           "1",
		"calculated_value_unit_name": "ед",
	})
	want := "1.0 ед"
	if got != want {
		t.Fatalf("unexpected SPAD value: want %q, got %q", want, got)
	}
}

func TestEnrichReportWithScoutDataPrefersAggregatedCalculatedValue(t *testing.T) {
	report := CropwiseReport{
		"id": 51561,
		"measurements": []any{
			map[string]any{
				"calculated":            true,
				"id":                    7892,
				"name":                  "Расчетное значение",
				"system_name":           "density_of_planting_linear_thousands_per_ha",
				"type":                  "Густота посева (посадки) с использованием погонных метров",
				"unit":                  "тыс. раст/га",
				"value":                 657.9,
				"scout_report_point_id": 7474,
			},
		},
	}
	points := []CropwiseResource{{"id": 7474}}
	measurements := []CropwiseResource{{
		"id":                               7892,
		"scout_report_point_id":            7474,
		"scout_report_measurement_type_id": 16,
		"calculated_value":                 657.8947368421053,
		"measurement_values": map[string]any{
			"length_of_row":  2,
			"plants_in_rows": 25,
			"row_width":      19,
			"rows_count":     1,
		},
	}}
	measurementTypes := map[int64]string{16: "Густота посева (посадки) с использованием погонных метров"}

	EnrichReportWithScoutData(report, points, measurements, nil, nil, measurementTypes)
	text := FormatReport(report)

	want := "Густота посева (посадки) с использованием погонных метров: 657.9 тыс. раст/га"
	if !strings.Contains(text, want) {
		t.Fatalf("formatted report does not contain aggregated calculated value %q:\n%s", want, text)
	}
	if strings.Contains(text, "657.8947368421053") {
		t.Fatalf("formatted report still contains raw calculated value:\n%s", text)
	}
}

func TestEnrichReportWithScoutDataDoesNotReplaceEnumWithAggregatedText(t *testing.T) {
	report := CropwiseReport{
		"id": 51561,
		"measurements": []any{
			map[string]any{
				"calculated":            true,
				"id":                    7889,
				"name":                  "Расчетное значение",
				"system_name":           "plants_affected_by_diseases",
				"type":                  "Поражение болезнями",
				"unit":                  nil,
				"value":                 "нет поражения",
				"scout_report_point_id": 7474,
			},
		},
	}
	points := []CropwiseResource{{"id": 7474}}
	measurements := []CropwiseResource{{
		"id":                               7889,
		"scout_report_point_id":            7474,
		"scout_report_measurement_type_id": 35,
		"calculated_value":                 0,
		"measurement_values": map[string]any{
			"plants_affected_by_diseases": "no_defeat",
		},
	}}
	measurementTypes := map[int64]string{35: "Поражение болезнями"}

	EnrichReportWithScoutData(report, points, measurements, nil, nil, measurementTypes)
	text := FormatReport(report)

	if !strings.Contains(text, "Поражение болезнями: поражения нет") {
		t.Fatalf("formatted report should keep enum translation:\n%s", text)
	}
	if strings.Contains(text, "нет поражения") {
		t.Fatalf("formatted report should not use unitless aggregated calculated enum:\n%s", text)
	}
}

func TestBuildGrowthStageNameMapCombinesCodeAndDescription(t *testing.T) {
	got := BuildGrowthStageNameMap([]CropwiseResource{{
		"id":          1026,
		"name":        "09",
		"description": "Всходы; колеоптиль проходит поверхность почвы; лист достиг кончика колеоптиля",
	}})
	want := "09 — Всходы; колеоптиль проходит поверхность почвы; лист достиг кончика колеоптиля"
	if got[1026] != want {
		t.Fatalf("unexpected growth stage name: want %q, got %q", want, got[1026])
	}
}

func TestFormatGrowthStageLineExpandsBBCH09(t *testing.T) {
	got := formatGrowthStageLine("09")
	want := "09 — Всходы; колеоптиль проходит поверхность почвы; лист достиг кончика колеоптиля"
	if got != want {
		t.Fatalf("unexpected growth stage line: want %q, got %q", want, got)
	}
}

func TestEnrichReportWithScoutDataMapsMeasurementGrowthStageID(t *testing.T) {
	report := CropwiseReport{"id": 51554}
	points := []CropwiseResource{{"id": 1, "growth_stage_id": 1026}}
	measurements := []CropwiseResource{{
		"id":                               10,
		"scout_report_point_id":            1,
		"scout_report_measurement_type_id": 20,
		"measurement_values": map[string]any{
			"growth_stage_id": 1026,
		},
	}}
	growthStages := map[int64]string{
		1026: "09 — Всходы; колеоптиль проходит поверхность почвы; лист достиг кончика колеоптиля",
	}
	measurementTypes := map[int64]string{20: "growth_stage_id"}

	EnrichReportWithScoutData(report, points, measurements, nil, growthStages, measurementTypes)
	text := FormatReport(report)

	want := "09 — Всходы; колеоптиль проходит поверхность почвы; лист достиг кончика колеоптиля"
	if !strings.Contains(text, want) {
		t.Fatalf("formatted report does not contain mapped growth stage %q:\n%s", want, text)
	}
	if strings.Contains(text, "1026") {
		t.Fatalf("formatted report still contains raw growth_stage_id:\n%s", text)
	}
}
