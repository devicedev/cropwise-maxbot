package main

import (
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

func FormatReport(r CropwiseReport) string {
	id := reportID(r)
	fieldID := int64Value(r, "field_id")
	fieldName := fieldName(r)
	fieldGroupName := fieldGroupName(r)
	reportTime := stringValue(r, "created_by_user_at", "report_time", "created_at")
	creator := creatorName(r)
	condition := formatFieldConditionCompact(stringValue(r, "field_condition"))
	risk := formatYieldRiskCompact(r["risk_yield_decreasing"])
	productionCycle := stringValue(r, "production_cycle_name", "production_cycle", "productive_cycle", "crop_rotation_name")
	ndviField := firstNonEmpty(
		formatNDVI(numberStringAny(r, "report_ndvi", "ndvi", "ndvi_value")),
		formatNDVI(numberStringAny(r, "field_ndvi", "field_ndvi_history", "historical_ndvi", "field_last_ndvi", "last_ndvi", "average_ndvi")),
	)
	reportGrowthStage := stringValue(r, "growth_stage_name", "growth_stage", "growth_stage_title")
	additionalInfo := cleanText(stringValue(r, "additional_info", "description"))
	reportURL := stringValue(r, "report_url")

	var b strings.Builder

	if fieldName != "" {
		b.WriteString("🌾 " + fieldName + "\n")
	} else if fieldID != 0 {
		b.WriteString(fmt.Sprintf("🌾 Поле ID: %d\n", fieldID))
	} else {
		b.WriteString("🌾 Отчет осмотра поля\n")
	}

	if fieldGroupName != "" {
		b.WriteString("🌍 " + fieldGroupName + "\n")
	}
	if productionCycle != "" {
		b.WriteString("🪴 " + productionCycle + "\n")
	}

	b.WriteString("❗ Создание нового отчета осмотра поля\n")

	if ndviField != "" {
		b.WriteString("🛰️ NDVI поля: " + ndviField + "\n")
	}
	if pointNDVI := firstPointNDVI(r); pointNDVI != "" && pointNDVI != ndviField {
		b.WriteString("📍 NDVI точки: " + pointNDVI + "\n")
	}
	if reportTime != "" {
		b.WriteString("🗓️ " + formatDateTime(reportTime) + "\n")
	}
	if creator != "" {
		b.WriteString("👨‍🌾 " + creator + "\n")
	}
	if condition != "" {
		b.WriteString("📈 " + condition + "\n")
	}
	formattedReportGrowthStage := formatGrowthStageLine(reportGrowthStage)
	if formattedReportGrowthStage != "" && !reportPointsContainStageValue(r, formattedReportGrowthStage) {
		b.WriteString("📄 " + formattedReportGrowthStage + "\n")
	} else if additionalInfo != "" {
		b.WriteString("📄 " + trimForLog(additionalInfo, 220) + "\n")
	}
	if risk != "" {
		b.WriteString(risk + "\n")
	}

	points := reportPoints(r)
	if len(points) > 0 {
		for i, p := range points {
			writePoint(&b, p, i+1)
		}
	} else {
		measurements := summarizeMeasurementsPretty(r, 8)
		for _, line := range measurements {
			b.WriteString(line + "\n")
		}
	}

	b.WriteString("--------------------------------\n")
	b.WriteString("\n")
	if id != "" {
		if reportURL != "" {
			b.WriteString("🔗 [Отчет осмотра #" + id + "](" + reportURL + ")\n")
		} else {
			b.WriteString("Отчет осмотра #" + id + "\n")
		}
	}

	text := b.String()
	if len([]rune(text)) > 3900 {
		r := []rune(text)
		text = string(r[:3900]) + "\n…"
	}
	return text
}

func creatorName(r CropwiseReport) string {
	if s := stringValue(r, "created_by_user_name", "created_user_name", "created_by_name", "user_name", "creator_name", "author_name"); s != "" {
		return s
	}
	if user, ok := r["user"].(map[string]any); ok {
		return userDisplayName(user)
	}
	if user, ok := r["created_by_user"].(map[string]any); ok {
		return userDisplayName(user)
	}
	for _, key := range []string{"created_by", "created_user", "creator", "author"} {
		if user, ok := r[key].(map[string]any); ok {
			if name := userDisplayName(user); name != "" {
				return name
			}
		}
	}
	return ""
}

func cleanText(s string) string {
	s = strings.TrimSpace(s)
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	return s
}

func firstPointNDVI(r CropwiseReport) string {
	for _, p := range reportPoints(r) {
		if ndvi := pointNDVI(p); ndvi != "" {
			return ndvi
		}
	}
	return ""
}

func pointNDVI(p map[string]any) string {
	return formatNDVI(numberStringAny(p, "ndvi", "point_ndvi", "ndvi_value", "last_ndvi", "average_ndvi"))
}

func reportPoints(r CropwiseReport) []map[string]any {
	for _, key := range []string{"scout_report_points", "points"} {
		v, ok := r[key]
		if !ok || v == nil {
			continue
		}
		items := toAnySlice(v)
		out := make([]map[string]any, 0, len(items))
		seen := map[string]bool{}
		for _, item := range items {
			if m, ok := item.(map[string]any); ok {
				if sig := pointDisplaySignature(m); sig != "" {
					if seen[sig] {
						continue
					}
					seen[sig] = true
				}
				out = append(out, m)
			}
		}
		if len(out) > 0 {
			return out
		}
	}
	return nil
}

func writePoint(b *strings.Builder, p map[string]any, fallbackIndex int) {
	idx := int64Value(p, "point_index")
	if idx == 0 {
		idx = int64(fallbackIndex)
	}
	b.WriteString(fmt.Sprintf("📍 Точка %d:\n", idx))

	if ndvi := pointNDVI(p); ndvi != "" {
		b.WriteString("🛰️ NDVI точки: " + ndvi + "\n")
	}

	stage := formatGrowthStageLine(stringValue(p, "growth_stage_name", "growth_stage", "growth_stage_title", "bbch", "bbch_code"))
	if stage != "" && !pointMeasurementsContainValue(p, stage) {
		b.WriteString("🌱 " + stage + "\n")
	}
	info := cleanText(stringValue(p, "additional_info", "description"))
	if info != "" && info != stage {
		b.WriteString("🌱 " + trimForLog(info, 280) + "\n")
	}

	issues := toAnySlice(p["issues"])
	for _, rawIssue := range issues {
		if issue, ok := rawIssue.(map[string]any); ok {
			if line := formatIssueLine(issue); line != "" {
				b.WriteString("⚠️ " + line + "\n")
			}
		}
	}

	measurements := toAnySlice(p["measurements"])
	seenMeasurements := map[string]bool{}
	for _, raw := range measurements {
		if m, ok := raw.(map[string]any); ok {
			if sig := measurementDisplaySignature(m); sig != "" {
				if seenMeasurements[sig] {
					continue
				}
				seenMeasurements[sig] = true
			}
			writeMeasurement(b, m)
		}
	}
}

func pointDisplaySignature(p map[string]any) string {
	lat := stringValue(p, "latitude", "lat")
	lon := stringValue(p, "longitude", "lng", "lon")
	if lat != "" || lon != "" {
		return "loc:" + lat + "," + lon + "|content:" + pointContentSignature(p)
	}
	if id := int64Value(p, "id"); id != 0 {
		return fmt.Sprintf("id:%d", id)
	}
	return ""
}

func pointContentSignature(p map[string]any) string {
	parts := []string{
		pointNDVI(p),
		formatGrowthStageLine(stringValue(p, "growth_stage_name", "growth_stage", "growth_stage_title", "bbch", "bbch_code")),
		cleanText(stringValue(p, "additional_info", "description")),
	}
	for _, raw := range toAnySlice(p["issues"]) {
		if issue, ok := raw.(map[string]any); ok {
			if line := formatIssueLine(issue); line != "" {
				parts = append(parts, "issue:"+line)
			}
		}
	}
	seenMeasurements := map[string]bool{}
	for _, raw := range toAnySlice(p["measurements"]) {
		if m, ok := raw.(map[string]any); ok {
			if sig := measurementDisplaySignature(m); sig != "" {
				if seenMeasurements[sig] {
					continue
				}
				seenMeasurements[sig] = true
				parts = append(parts, "measurement:"+sig)
			}
		}
	}
	return strings.Join(parts, "|")
}

func measurementDisplaySignature(m map[string]any) string {
	title := measurementTitle(m)
	value := measurementCalculatedValue(m)
	vals := measurementValuesMap(m)
	if value == "" {
		if key, val, ok := singleMeaningfulMeasurementValue(vals); ok {
			value = formatMeasurementValue(key, strings.TrimSpace(anyToString(val)))
		}
	}
	if title == "" && value == "" && len(vals) == 0 {
		return ""
	}
	parts := []string{title, value}
	for _, key := range sortedKeys(vals) {
		val := strings.TrimSpace(anyToString(vals[key]))
		if val == "" || val == "<nil>" || shouldSkipMeasurementDetail(m, key, val, value) {
			continue
		}
		parts = append(parts, translateMeasurementKey(key)+"="+formatMeasurementValue(key, val))
	}
	if desc := cleanText(stringValue(m, "description", "additional_info")); desc != "" {
		parts = append(parts, desc)
	}
	return strings.Join(parts, "\x00")
}

func pointMeasurementsContainValue(p map[string]any, value string) bool {
	value = strings.TrimSpace(value)
	if value == "" {
		return false
	}
	for _, raw := range toAnySlice(p["measurements"]) {
		m, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		display := measurementCalculatedValue(m)
		if display == "" {
			if key, val, ok := singleMeaningfulMeasurementValue(measurementValuesMap(m)); ok {
				display = formatMeasurementValue(key, strings.TrimSpace(anyToString(val)))
			}
		}
		if measurementDisplayMatchesValue(m, display, value) {
			return true
		}
	}
	return false
}

func reportPointsContainMeasurementValue(r CropwiseReport, value string) bool {
	value = strings.TrimSpace(value)
	if value == "" {
		return false
	}
	for _, p := range reportPoints(r) {
		if pointMeasurementsContainValue(p, value) {
			return true
		}
	}
	return false
}

func reportPointsContainStageValue(r CropwiseReport, value string) bool {
	value = strings.TrimSpace(value)
	if value == "" {
		return false
	}
	if reportPointsContainMeasurementValue(r, value) {
		return true
	}
	for _, p := range reportPoints(r) {
		stage := stringValue(p, "growth_stage_name", "growth_stage", "growth_stage_title", "bbch", "bbch_code")
		if growthStageDisplayMatchesValue(stage, value) {
			return true
		}
	}
	return false
}

func measurementDisplayMatchesValue(m map[string]any, display, value string) bool {
	display = strings.TrimSpace(display)
	value = strings.TrimSpace(value)
	if display == "" || value == "" {
		return false
	}
	if strings.EqualFold(display, value) {
		return true
	}
	if !isGrowthStageMeasurement(m) {
		return false
	}
	return growthStageDisplayMatchesValue(display, value)
}

func growthStageDisplayMatchesValue(display, value string) bool {
	display = strings.TrimSpace(display)
	value = strings.TrimSpace(value)
	if display == "" || value == "" {
		return false
	}
	if strings.EqualFold(display, value) {
		return true
	}
	displayStage := formatGrowthStageLine(display)
	valueStage := formatGrowthStageLine(value)
	if strings.EqualFold(displayStage, valueStage) {
		return true
	}
	displayCode := leadingGrowthStageCode(displayStage)
	valueCode := leadingGrowthStageCode(valueStage)
	return displayCode != "" && displayCode == valueCode
}

func isGrowthStageMeasurement(m map[string]any) bool {
	hint := strings.ToLower(strings.Join([]string{
		stringValue(m, "measurement_type_name"),
		stringValue(m, "human_name"),
		stringValue(m, "name"),
		stringValue(m, "system_name"),
	}, " "))
	return strings.Contains(hint, "faza") ||
		strings.Contains(hint, "growth") ||
		strings.Contains(hint, "stage") ||
		strings.Contains(hint, "bbch") ||
		strings.Contains(hint, "фаза") ||
		strings.Contains(hint, "рост") ||
		strings.Contains(hint, "стади")
}

func leadingGrowthStageCode(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	i := 0
	for i < len(s) && s[i] >= '0' && s[i] <= '9' {
		i++
	}
	if i == 0 {
		return ""
	}
	return s[:i]
}

func formatIssueLine(issue map[string]any) string {
	name := issueDisplayName(issue)
	parts := []string{}
	if level := translateThreatLevel(stringValue(issue, "threat_level")); level != "" {
		parts = append(parts, level)
	}
	if amount := issueAmountText(issue); amount != "" {
		parts = append(parts, amount)
	}
	if desc := cleanText(stringValue(issue, "additional_info", "description")); desc != "" {
		parts = append(parts, trimForLog(desc, 160))
	}
	if name == "" {
		return strings.Join(parts, ": ")
	}
	if len(parts) == 0 {
		return name
	}
	return name + ": " + strings.Join(parts, "; ")
}

func issueDisplayName(issue map[string]any) string {
	name := stringValue(issue, "plant_threat_name", "threat_name", "name", "title")
	threatType := translateThreatType(stringValue(issue, "threat_type"))
	latinName := stringValue(issue, "latin_name")
	if threat, ok := issue["plant_threat"].(map[string]any); ok {
		if name == "" {
			name = stringValue(threat, "name", "custom_name", "standard_name", "title", "description")
		}
		if threatType == "" {
			threatType = translateThreatType(stringValue(threat, "threat_type"))
		}
		if latinName == "" {
			latinName = stringValue(threat, "latin_name")
		}
	}
	details := make([]string, 0, 2)
	if threatType != "" {
		details = append(details, threatType)
	}
	if latinName != "" {
		details = append(details, latinName)
	}
	if len(details) == 0 {
		return name
	}
	if name == "" {
		return strings.Join(details, " / ")
	}
	return fmt.Sprintf("%s (%s)", name, strings.Join(details, " / "))
}

func issueAmountText(issue map[string]any) string {
	for _, key := range []string{"amount", "damaged_area", "number_pests_in_trap"} {
		if amount := stringValue(issue, key); amount != "" {
			if label := translateIssueAmountKey(key); label != "" {
				return label + " " + amount
			}
			return amount
		}
	}
	return ""
}

func translateIssueAmountKey(key string) string {
	switch normalizeKey(key) {
	case "amount", "quantity", "count":
		return "количество"
	case "damaged_area":
		return "площадь повреждения"
	case "number_pests_in_trap":
		return "количество в ловушке"
	default:
		return ""
	}
}

func translateThreatType(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "weed", "weeds":
		return "Сорняк"
	case "insect", "insects":
		return "Насекомое"
	case "disease", "diseases":
		return "Болезнь"
	case "nematode", "nematodes":
		return "Нематоды"
	case "nutrition_problem":
		return "Проблема питания"
	case "damaged_area":
		return "Повреждение"
	case "technological_mistake":
		return "Технологическая ошибка"
	case "pest", "pests":
		return "Вредитель"
	case "other":
		return "Прочее"
	default:
		return raw
	}
}

func translateThreatLevel(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "low":
		return "низкий уровень"
	case "mid", "medium":
		return "средний уровень"
	case "high":
		return "высокий уровень"
	default:
		return raw
	}
}

func writeMeasurement(b *strings.Builder, m map[string]any) {
	title := measurementTitle(m)
	value := measurementCalculatedValue(m)
	vals := measurementValuesMap(m)

	// Для измерений вида "Высота растений" или "Фаза роста" Cropwise часто возвращает
	// человекочитаемое название измерения + служебный ключ в measurement_values.
	// В таком случае переносим значение в основную строку и не печатаем технический ключ.
	if value == "" {
		if key, val, ok := singleMeaningfulMeasurementValue(vals); ok {
			value = formatMeasurementValue(key, strings.TrimSpace(anyToString(val)))
		}
	}

	if title == "" && value == "" {
		return
	}
	line := "🔍 "
	if title != "" {
		line += title
	}
	if value != "" {
		if title != "" {
			line += ": "
		}
		line += value
	}
	b.WriteString(line + "\n")

	for _, key := range sortedKeys(vals) {
		val := strings.TrimSpace(anyToString(vals[key]))
		if val == "" || val == "<nil>" {
			continue
		}
		if shouldSkipMeasurementDetail(m, key, val, value) {
			continue
		}
		label := translateMeasurementKey(key)
		// Если перевода нет и это технический snake_case — лучше не засорять канал.
		if label == key && looksTechnicalKey(key) {
			continue
		}
		b.WriteString("▫️ " + titleCaseRu(label) + ": " + formatMeasurementValue(key, val) + "\n")
	}
	if desc := cleanText(stringValue(m, "description", "additional_info")); desc != "" {
		b.WriteString(trimForLog(desc, 220) + "\n")
	}
}

func singleMeaningfulMeasurementValue(vals map[string]any) (string, any, bool) {
	if len(vals) != 1 {
		return "", nil, false
	}
	for k, v := range vals {
		val := strings.TrimSpace(anyToString(v))
		if val == "" || val == "<nil>" {
			return "", nil, false
		}
		return k, v, true
	}
	return "", nil, false
}

func shouldSkipMeasurementDetail(m map[string]any, key, rawVal, mainValue string) bool {
	key = strings.TrimSpace(key)
	rawVal = strings.TrimSpace(rawVal)
	mainValue = strings.TrimSpace(mainValue)
	if key == "" || rawVal == "" {
		return true
	}
	// Не печатаем технический ключ, если он уже представлен основной строкой измерения.
	if len(measurementValuesMap(m)) == 1 {
		return true
	}
	if mainValue != "" {
		formatted := formatMeasurementValue(key, rawVal)
		if strings.EqualFold(strings.TrimSpace(formatted), mainValue) || strings.Contains(mainValue, formatted) {
			return true
		}
	}
	return false
}

func looksTechnicalKey(key string) bool {
	return strings.Contains(key, "_") || regexp.MustCompile(`^[a-z0-9_]+$`).MatchString(key)
}

func titleCaseRu(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return s
	}
	r := []rune(s)
	r[0] = []rune(strings.ToUpper(string(r[0])))[0]
	return string(r)
}

func measurementTitle(m map[string]any) string {
	raw := stringValue(m, "measurement_type_name", "human_name", "name", "measurement_type", "system_name")
	return translateMeasurementTitle(raw)
}

func measurementCalculatedValue(m map[string]any) string {
	calc := numberStringAny(m, "calculated_value", "value")
	if calc == "" {
		return ""
	}
	keyHint := strings.ToLower(strings.Join([]string{
		stringValue(m, "measurement_type_name"),
		stringValue(m, "human_name"),
		stringValue(m, "name"),
		stringValue(m, "system_name"),
	}, " "))
	if isSPADMeasurementKey(keyHint) {
		calc = formatSPADValue(calc)
	} else {
		calc = translateMeasurementEnumValue(keyHint, calc)
	}
	unit := stringValue(m, "calculated_value_unit_name", "unit_name", "unit", "calculated_value_unit")
	if unit == "" {
		unit = inferMeasurementUnit(m)
	}
	if unit != "" && !hasRussianUnitOrWord(calc) {
		return calc + " " + unit
	}
	return calc
}

func inferMeasurementUnit(m map[string]any) string {
	combined := strings.ToLower(strings.Join([]string{
		stringValue(m, "measurement_type_name"),
		stringValue(m, "human_name"),
		stringValue(m, "system_name"),
	}, " "))
	switch {
	case strings.Contains(combined, "millions_per_ha") || strings.Contains(combined, "млн"):
		return "млн. раст/га"
	case strings.Contains(combined, "thousands_per_ha") || strings.Contains(combined, "тыс"):
		return "тыс. раст/га"
	case strings.Contains(combined, "temperature") || strings.Contains(combined, "температур"):
		return "°C"
	case strings.Contains(combined, "depth") || strings.Contains(combined, "глубин") || strings.Contains(combined, "height") || strings.Contains(combined, "высот"):
		return "см"
	}
	return ""
}

func measurementValuesMap(m map[string]any) map[string]any {
	v, ok := m["measurement_values"]
	if !ok || v == nil {
		return map[string]any{}
	}
	if vals, ok := v.(map[string]any); ok {
		return vals
	}
	if raw, ok := v.(string); ok {
		var vals map[string]any
		if err := json.Unmarshal([]byte(raw), &vals); err == nil {
			return vals
		}
	}
	return map[string]any{}
}

func formatMeasurementValue(key, val string) string {
	val = strings.TrimSpace(val)
	if val == "" {
		return val
	}
	norm := normalizeKey(key)
	hasUnit := measurementKeyHasUnit(norm)
	switch strings.ToLower(val) {
	case "yes", "true", "y":
		if hasUnit {
			val = "1"
		}
	case "no", "false", "n":
		if hasUnit {
			val = "0"
		}
	}
	if !hasUnit {
		val = translateMeasurementEnumValue(key, val)
	}
	switch norm {
	case "spad":
		return formatSPADValue(val) + " ед"
	case "length_of_row", "density_of_planting_linear_length_of_row":
		return val + " м"
	case "row_width", "density_of_planting_linear_row_width":
		return val + " см"
	case "plants_in_row", "plants_in_rows", "density_of_planting_linear_plants_in_rows":
		return val + " раст"
	case "rows_count", "density_of_planting_linear_rows_count":
		return val + " ряд"
	case "depth", "seeding_depth", "depth_of_sowing", "plant_height", "height_of_plants", "plants_height":
		return val + " см"
	case "glubina_obrabotki", "treatment_depth", "processing_depth", "tillage_depth":
		return val + " см"
	case "temperature", "soil_temperature", "soil_temp":
		return val + " °C"
	default:
		return val
	}
}

func measurementKeyHasUnit(norm string) bool {
	switch norm {
	case "spad",
		"length_of_row", "density_of_planting_linear_length_of_row",
		"row_width", "density_of_planting_linear_row_width",
		"plants_in_row", "plants_in_rows", "density_of_planting_linear_plants_in_rows",
		"rows_count", "density_of_planting_linear_rows_count",
		"depth", "seeding_depth", "depth_of_sowing",
		"plant_height", "height_of_plants", "plants_height",
		"glubina_obrabotki", "treatment_depth", "processing_depth", "tillage_depth",
		"temperature", "soil_temperature", "soil_temp":
		return true
	default:
		return false
	}
}

func isSPADMeasurementKey(key string) bool {
	return strings.Contains(normalizeKey(key), "spad")
}

func formatSPADValue(val string) string {
	if f, err := strconv.ParseFloat(strings.TrimSpace(val), 64); err == nil {
		return strconv.FormatFloat(f, 'f', 1, 64)
	}
	return val
}

func translateMeasurementEnumValue(key, val string) string {
	v := strings.TrimSpace(val)
	if v == "" {
		return v
	}
	low := strings.ToLower(v)
	keyLow := strings.ToLower(key)

	switch low {
	case "yes", "true", "1", "y":
		if strings.Contains(keyLow, "сорня") || strings.Contains(keyLow, "weed") || strings.Contains(keyLow, "болез") || strings.Contains(keyLow, "disease") || strings.Contains(keyLow, "вредител") || strings.Contains(keyLow, "pest") {
			return "есть"
		}
		return "да"
	case "no", "false", "0", "n":
		return "нет"
	case "no_weeds":
		return "сорняков нет"
	case "single_weeds":
		return "единичные сорняки"
	case "few_weeds":
		return "мало сорняков"
	case "medium_weeds", "moderate_weeds":
		return "средняя засоренность"
	case "many_weeds", "high_weeds", "a_lot_of_weeds", "lot_of_weeds", "lots_of_weeds":
		return "сильная засоренность"
	case "average":
		return "среднее"
	case "no_damage":
		return "повреждений нет"
	case "individual_plants_damaged":
		return "повреждены отдельные растения"
	case "single_plants_affected":
		return "поражены отдельные растения"
	case "no_defeat":
		return "поражения нет"
	case "low":
		return "низкий уровень"
	case "medium", "mid":
		return "средний уровень"
	case "high":
		return "высокий уровень"
	case "minor":
		return "слабое"
	case "moderate":
		return "умеренное"
	case "significant":
		return "значительное"
	case "severe":
		return "сильное"
	case "bad":
		return "плохое"
	case "satisfactory":
		return "удовлетворительное"
	case "good":
		return "хорошее"
	case "excellent":
		return "отличное"
	}

	// Значения стадий часто приходят как "65: Полное цветение; ...".
	if strings.Contains(keyLow, "faza") || strings.Contains(keyLow, "growth") || strings.Contains(keyLow, "stage") || strings.Contains(keyLow, "bbch") || strings.Contains(keyLow, "фаза") || strings.Contains(keyLow, "рост") || strings.Contains(keyLow, "стади") {
		return formatGrowthStageLine(v)
	}
	return v
}

func translateMeasurementTitle(raw string) string {
	s := strings.TrimSpace(raw)
	if s == "" {
		return s
	}
	low := strings.ToLower(s)
	switch normalizeKey(s) {
	case "plant_height", "height_of_plants", "plants_height":
		return "Высота растений"
	case "infestation_estimate", "weediness_estimate", "weed_estimate":
		return "Оценка засоренности"
	case "faza_rosta_lyutserny", "alfalfa_growth_stage", "growth_stage_alfalfa":
		return "Фаза роста люцерны"
	case "has_weeds", "weeds_presence", "weed_presence", "presence_of_weeds", "weeds", "weed":
		return "Наличие сорняков"
	case "has_diseases", "disease_presence", "presence_of_diseases", "diseases", "disease":
		return "Наличие болезней"
	case "has_pests", "pests_presence", "pest_presence", "presence_of_pests", "pests", "pest":
		return "Наличие вредителей"
	case "soil_temperature", "soil_temp", "temperature":
		return "Температура почвы"
	case "seeding_depth", "depth_of_sowing", "depth":
		return "Глубина сева"
	case "glubina_obrabotki", "treatment_depth", "processing_depth", "tillage_depth":
		return "Глубина обработки"
	case "crop_plant_suppression", "crop_plants_suppression", "cultivated_plant_suppression", "cultivated_plants_suppression", "crop_plant_depression", "crop_plants_depression":
		return "Угнетение культурных растений"
	case "weed_plant_suppression", "weed_plants_suppression", "weed_suppression", "weed_plant_depression", "weed_plants_depression":
		return "Угнетение сорных растений"
	}
	if strings.Contains(low, "presence") && strings.Contains(low, "weed") {
		return "Наличие сорняков"
	}
	if strings.Contains(low, "presence") && strings.Contains(low, "disease") {
		return "Наличие болезней"
	}
	if strings.Contains(low, "presence") && strings.Contains(low, "pest") {
		return "Наличие вредителей"
	}
	return s
}

func normalizeKey(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.ReplaceAll(s, " ", "_")
	s = strings.ReplaceAll(s, "-", "_")
	s = strings.ReplaceAll(s, "/", "_")
	return s
}

func hasRussianUnitOrWord(s string) bool {
	low := strings.ToLower(s)
	return strings.Contains(low, "есть") || strings.Contains(low, "нет") || strings.Contains(low, "сорня") || strings.Contains(low, "засор") || strings.Contains(low, "уровень") || strings.Contains(low, "средн") || strings.Contains(low, "поврежд") || strings.Contains(low, "поражен") || strings.Contains(low, "плох") || strings.Contains(low, "хорош") || strings.Contains(low, "удовлетвор") || strings.Contains(low, "отлич")
}

func formatGrowthStageLine(raw string) string {
	s := strings.TrimSpace(raw)
	if s == "" {
		return ""
	}
	if idx := strings.Index(s, ":"); idx >= 0 && idx+1 < len(s) {
		prefix := strings.TrimSpace(s[:idx])
		rest := strings.TrimSpace(s[idx+1:])
		if _, err := strconv.Atoi(prefix); err == nil && rest != "" {
			return prefix + " — " + rest
		}
	}
	if idx := strings.Index(s, "—"); idx >= 0 {
		return s
	}
	if _, err := strconv.Atoi(s); err == nil {
		if ru := bbchStageName(s); ru != "" {
			return s + " — " + ru
		}
	}
	return s
}

func bbchStageName(code string) string {
	switch strings.TrimSpace(code) {
	case "00":
		return "Сухое семя"
	case "01":
		return "Начало набухания семян"
	case "03":
		return "Завершение набухания семян"
	case "05":
		return "Появление кончика зародышевого корня"
	case "06":
		return "Удлинение кончика зародышевого корня, появление корневых волосков и/или побочных побегов"
	case "07":
		return "Калеоптиль вышел из семени"
	case "09":
		return "Всходы: колеоптиль проходит поверхность почвы"
	case "10":
		return "Первый лист выходит из колеоптиля"
	case "11":
		return "Первый лист развернут"
	case "12":
		return "2-й лист развернут"
	case "13":
		return "3-й лист развернут"
	case "14":
		return "4-й лист развернут"
	case "15":
		return "5-й лист развернут"
	case "16":
		return "6-й лист развернут"
	case "17":
		return "7-й лист развернут"
	case "18":
		return "8-й лист развернут"
	case "19":
		return "9 и больше листьев развернуто"
	case "20":
		return "Нет кущения"
	case "21":
		return "Появляется первый побег кущения; начало кущения"
	case "22":
		return "2 побега кущения"
	case "23":
		return "3 побега кущения"
	case "24":
		return "4 побега кущения"
	case "25":
		return "5 побегов кущения"
	case "26":
		return "6 побегов кущения"
	case "27":
		return "7 побегов кущения"
	case "28":
		return "8 побегов кущения"
	case "29":
		return "Завершение кущения; появляется максимальное количество побегов"
	case "30":
		return "Начало удлинения стебля; псевдостебель и побег соцветия как минимум на 1 см выше узла кущения"
	case "31":
		return "Первый узел виден на поверхности земли, расстояние от узла кущения по крайней мере 1 см"
	case "32":
		return "2-й узел виден, расстояние от 1-го узла по крайней мере 2 см"
	case "33":
		return "3-й узел виден, расстояние от 2-го узла по крайней мере 2 см"
	case "34":
		return "4-й узел виден, расстояние от 3-го узла по крайней мере 2 см"
	case "35":
		return "5-й узел виден, расстояние от 4-го узла по крайней мере 2 см"
	case "36":
		return "6-й узел виден, расстояние от 5-го узла по крайней мере 2 см"
	case "37":
		return "Появление последнего, флагового, листа"
	case "39":
		return "Стадия лигулы, листового язычка; флаговый лист полностью развит, лигула флагового листа еле видна"
	case "41":
		return "Листовое влагалище флагового листа удлиняется"
	case "43":
		return "Соцветие, колос или метелка, внутри стебля сдвигается вверх; листовое влагалище флагового листа начинает набухать"
	case "45":
		return "Листовое влагалище флагового листа набухло"
	case "47":
		return "Листовое влагалище листа открывается"
	case "49":
		return "Появление остей. Ости появляются над лигулой флагового листа"
	case "51":
		return "Начало появления соцветия, колошение, выметывание. Верхняя часть метелки или колоса видна"
	case "52":
		return "Появление 20% соцветия"
	case "53":
		return "Появление 30% соцветия"
	case "54":
		return "Появление 40% соцветия"
	case "55":
		return "Появление половины соцветия. Нижняя часть еще в листовом влагалище"
	case "56":
		return "Появление 60% соцветия"
	case "57":
		return "Появление 70% соцветия"
	case "58":
		return "Появление 80% соцветия"
	case "59":
		return "Полное появление соцветия. Колос или метелка полностью видны"
	case "61":
		return "Начало цветения. Первые тычинки появляются"
	case "65":
		return "Середина цветения. 50% зрелых тычинок"
	case "69":
		return "Конец цветения"
	case "71":
		return "Содержание зерен водянистое. Первые зерна достигли половины своего окончательного размера"
	case "73":
		return "Ранняя молочная спелость"
	case "75":
		return "Средняя молочная спелость. Все зерна достигли окончательного размера. Содержание зерен молочное. Зерна еще зеленые"
	case "77":
		return "Полная молочная спелость"
	case "83":
		return "Ранняя восковая спелость"
	case "85":
		return "Мягкая восковая спелость. Содержание зерен еще мягкое, но сухое. Вмятина от ногтя выпрямляется"
	case "87":
		return "Твердая восковая спелость. Вмятина от ногтя не выпрямляется"
	case "89":
		return "Ранняя полная спелость. Зерно твердое, только с трудом раскалывается ногтем большого пальца"
	case "92":
		return "Поздняя полная спелость. Зерно твердое, не ломается ногтем большого пальца"
	case "93":
		return "Осыпание зерна в дневное время"
	case "97":
		return "Растение полностью отмершее. Солома ломается"
	case "99":
		return "Собранный продукт"
	default:
		return ""
	}
}

func summarizeMeasurementsPretty(r CropwiseReport, max int) []string {
	var out []string
	for _, key := range []string{"measurements", "scout_report_point_measurements"} {
		items := toAnySlice(r[key])
		for _, item := range items {
			m, ok := item.(map[string]any)
			if !ok {
				continue
			}
			title := measurementTitle(m)
			value := measurementCalculatedValue(m)
			if title == "" && value == "" {
				continue
			}
			line := "🔍 " + title
			if value != "" {
				line += ": " + value
			}
			out = append(out, line)
			if len(out) >= max {
				return out
			}
		}
	}
	return out
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		v = strings.TrimSpace(v)
		if v != "" {
			return v
		}
	}
	return ""
}

func formatFieldConditionCompact(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "bad":
		return "Плохое (⭐)"
	case "satisfactory":
		return "Удовлетворительное (⭐⭐)"
	case "good":
		return "Хорошее (⭐⭐⭐)"
	case "excellent":
		return "Отличное (⭐⭐⭐⭐)"
	case "":
		return ""
	default:
		return raw
	}
}

func formatYieldRiskCompact(v any) string {
	if v == nil {
		return ""
	}
	if isTruthy(v) {
		return "🚨 Риск снижения урожайности"
	}
	return ""
}

func formatNDVI(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if strings.HasPrefix(raw, "[") || strings.HasPrefix(raw, "{") {
		return ""
	}
	if f, err := strconv.ParseFloat(raw, 64); err == nil {
		return strconv.FormatFloat(f, 'f', 3, 64)
	}
	return raw
}

func formatDateOnly(raw string) string {
	s := strings.TrimSpace(raw)
	if s == "" {
		return ""
	}
	if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
		return t.Format("2006-01-02")
	}
	for _, layout := range []string{
		"2006-01-02 15:04:05 -0700 MST",
		"2006-01-02 15:04:05",
		"2006-01-02T15:04:05",
		"2006-01-02",
	} {
		if t, err := time.Parse(layout, s); err == nil {
			return t.Format("2006-01-02")
		}
	}
	return raw
}

func isTruthy(v any) bool {
	switch t := v.(type) {
	case bool:
		return t
	case string:
		s := strings.ToLower(strings.TrimSpace(t))
		return s == "true" || s == "1" || s == "yes" || s == "y"
	case float64:
		return t != 0
	case int:
		return t != 0
	case int64:
		return t != 0
	default:
		return false
	}
}

func formatDateTime(raw string) string {
	s := strings.TrimSpace(raw)
	if s == "" {
		return ""
	}
	if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
		return t.Format("02.01.2006 15:04")
	}
	for _, layout := range []string{
		"2006-01-02 15:04:05 -0700 MST",
		"2006-01-02 15:04:05",
		"2006-01-02T15:04:05",
		"2006-01-02",
	} {
		if t, err := time.Parse(layout, s); err == nil {
			if layout == "2006-01-02" {
				return t.Format("02.01.2006")
			}
			return t.Format("02.01.2006 15:04")
		}
	}
	return raw
}

func parseAnyTime(raw string) (time.Time, error) {
	s := strings.TrimSpace(raw)
	if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
		return t, nil
	}
	layouts := []string{
		"2006-01-02T15:04:05",
		"2006-01-02 15:04:05",
		"2006-01-02",
	}
	var lastErr error
	for _, layout := range layouts {
		t, err := time.Parse(layout, s)
		if err == nil {
			return t, nil
		}
		lastErr = err
	}
	return time.Time{}, lastErr
}

func reportID(r CropwiseReport) string {
	return anyToString(r["id"])
}

func fieldName(r CropwiseReport) string {
	if s := stringValue(r, "field_name", "name"); s != "" {
		return s
	}
	if field, ok := r["field"].(map[string]any); ok {
		return stringValue(field, "name", "title")
	}
	return ""
}

func fieldGroupName(r CropwiseReport) string {
	if s := stringValue(r, "field_group_name", "group_name", "field_group"); s != "" {
		return s
	}
	if fieldGroup, ok := r["field_group"].(map[string]any); ok {
		return stringValue(fieldGroup, "name", "title", "description", "legal_entity")
	}
	if field, ok := r["field"].(map[string]any); ok {
		if s := stringValue(field, "field_group_name", "group_name"); s != "" {
			return s
		}
		if fieldGroup, ok := field["field_group"].(map[string]any); ok {
			return stringValue(fieldGroup, "name", "title", "description", "legal_entity")
		}
	}
	return ""
}

func stringValue(m map[string]any, keys ...string) string {
	for _, k := range keys {
		if v, ok := m[k]; ok {
			s := strings.TrimSpace(anyToString(v))
			if s != "" && s != "<nil>" && s != "{}" && s != "[]" {
				return s
			}
		}
	}
	return ""
}

func numberStringAny(m map[string]any, keys ...string) string {
	for _, key := range keys {
		v, ok := m[key]
		if !ok || v == nil {
			continue
		}
		s := strings.TrimSpace(anyToString(v))
		if s == "" || s == "0" || s == "0.0" || s == "<nil>" {
			continue
		}
		return s
	}
	return ""
}

func int64Value(m map[string]any, key string) int64 {
	v, ok := m[key]
	if !ok || v == nil {
		return 0
	}
	s := anyToString(v)
	i, _ := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
	return i
}

func anyToString(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case float64:
		if t == float64(int64(t)) {
			return strconv.FormatInt(int64(t), 10)
		}
		return strconv.FormatFloat(t, 'f', -1, 64)
	case int:
		return strconv.Itoa(t)
	case int64:
		return strconv.FormatInt(t, 10)
	case bool:
		return strconv.FormatBool(t)
	case nil:
		return ""
	default:
		b, _ := json.Marshal(t)
		return string(b)
	}
}

func toAnySlice(v any) []any {
	switch t := v.(type) {
	case []any:
		return t
	case []CropwiseResource:
		out := make([]any, 0, len(t))
		for _, x := range t {
			out = append(out, map[string]any(x))
		}
		return out
	default:
		return []any{v}
	}
}

func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func translateMeasurementKey(key string) string {
	norm := normalizeKey(key)
	switch norm {
	case "length_of_row", "density_of_planting_linear_length_of_row":
		return "длина ряда"
	case "plants_in_row", "plants_in_rows", "density_of_planting_linear_plants_in_rows", "plants_count":
		return "количество растений во всех рядах"
	case "rows_count", "density_of_planting_linear_rows_count":
		return "количество рядов для расчета"
	case "row_width", "density_of_planting_linear_row_width":
		return "ширина междурядий"
	case "plant_height", "height_of_plants", "plants_height":
		return "высота растений"
	case "infestation_estimate", "weediness_estimate", "weed_estimate":
		return "оценка засоренности"
	case "has_weeds", "weeds_presence", "weed_presence", "presence_of_weeds", "weeds", "weed":
		return "наличие сорняков"
	case "has_diseases", "disease_presence", "presence_of_diseases", "diseases", "disease":
		return "наличие болезней"
	case "has_pests", "pests_presence", "pest_presence", "presence_of_pests", "pests", "pest":
		return "наличие вредителей"
	case "faza_rosta_lyutserny", "alfalfa_growth_stage", "growth_stage_alfalfa":
		return "фаза роста люцерны"
	case "growth_stage", "growth_stage_id", "bbch", "bbch_code":
		return "стадия роста"
	case "soil_temperature", "soil_temp", "temperature":
		return "температура почвы"
	case "seeding_depth", "depth_of_sowing", "depth":
		return "глубина сева"
	case "glubina_obrabotki", "treatment_depth", "processing_depth", "tillage_depth":
		return "глубина обработки"
	case "crop_plant_suppression", "crop_plants_suppression", "cultivated_plant_suppression", "cultivated_plants_suppression", "crop_plant_depression", "crop_plants_depression":
		return "угнетение культурных растений"
	case "weed_plant_suppression", "weed_plants_suppression", "weed_suppression", "weed_plant_depression", "weed_plants_depression":
		return "угнетение сорных растений"
	case "seed_count", "seeds_count":
		return "семена"
	default:
		return key
	}
}

var imageExtRe = regexp.MustCompile(`(?i)\.(jpg|jpeg|png|gif|webp|bmp|tiff|heic)(\?|$)`)

func ExtractImageURLsWithBase(r CropwiseReport, limit int, baseURL string) []string {
	seen := make(map[string]bool)
	var candidates []string
	walkForURLs(any(r), seen, &candidates, 0, strings.TrimRight(baseURL, "/"))
	return bestImageURLs(candidates, limit)
}

func walkForURLs(v any, seen map[string]bool, out *[]string, limit int, baseURL string) {
	if limit > 0 && len(*out) >= limit {
		return
	}
	switch t := v.(type) {
	case string:
		imgURL := normalizeImageURL(t, baseURL)
		if imgURL != "" && isImageURL(imgURL) && !seen[imgURL] {
			seen[imgURL] = true
			*out = append(*out, imgURL)
		}
	case []any:
		for _, item := range t {
			walkForURLs(item, seen, out, limit, baseURL)
			if limit > 0 && len(*out) >= limit {
				return
			}
		}
	case CropwiseReport:
		walkForURLs(map[string]any(t), seen, out, limit, baseURL)
	case CropwiseResource:
		walkForURLs(map[string]any(t), seen, out, limit, baseURL)
	case map[string]any:
		for _, k := range []string{"photo", "original", "large", "medium", "preview_1000", "preview_400", "preview_200", "url", "src", "image", "image1", "image2", "image3"} {
			if val, ok := t[k]; ok {
				walkForURLs(val, seen, out, limit, baseURL)
			}
			if limit > 0 && len(*out) >= limit {
				return
			}
		}
		for _, val := range t {
			walkForURLs(val, seen, out, limit, baseURL)
			if limit > 0 && len(*out) >= limit {
				return
			}
		}
	}
}

func bestImageURLs(urls []string, limit int) []string {
	type choice struct {
		url  string
		rank int
	}
	order := make([]string, 0, len(urls))
	best := make(map[string]choice, len(urls))
	for _, u := range urls {
		u = cropioOriginalImageURL(u)
		key := imageVariantGroupKey(u)
		if key == "" {
			key = u
		}
		rank := imageQualityRank(u)
		if _, ok := best[key]; !ok {
			order = append(order, key)
			best[key] = choice{url: u, rank: rank}
			continue
		}
		if rank > best[key].rank {
			best[key] = choice{url: u, rank: rank}
		}
	}

	out := make([]string, 0, len(order))
	for _, key := range order {
		out = append(out, best[key].url)
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out
}

func imageVariantGroupKey(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	path := u.Path
	if path == "" {
		return ""
	}
	idx := strings.LastIndex(path, "/")
	if idx <= 0 {
		return ""
	}
	name := strings.ToLower(path[idx+1:])
	if !isKnownImageVariantName(name) {
		return rawURL
	}
	return strings.ToLower(u.Scheme) + "://" + strings.ToLower(u.Host) + path[:idx]
}

func isKnownImageVariantName(name string) bool {
	return isCropioPhotoName(name) ||
		name == "photo.jpeg" ||
		name == "photo.png" ||
		name == "photo.webp" ||
		strings.HasPrefix(name, "preview_") ||
		strings.HasPrefix(name, "thumb_")
}

func imageQualityRank(rawURL string) int {
	u, err := url.Parse(rawURL)
	if err != nil {
		return 0
	}
	path := strings.ToLower(u.Path)
	name := path
	if idx := strings.LastIndex(path, "/"); idx >= 0 {
		name = path[idx+1:]
	}
	switch {
	case name == "photo":
		return 11000
	case strings.HasPrefix(name, "photo."):
		return 10000
	case strings.Contains(name, "preview_1000"):
		return 9000
	case strings.Contains(name, "preview_400"):
		return 8000
	case strings.Contains(name, "preview_200"):
		return 7000
	case strings.Contains(name, "large"):
		return 6000
	case strings.Contains(name, "medium"):
		return 5000
	case strings.Contains(name, "small"):
		return 3000
	case strings.Contains(name, "thumb"):
		return 2000
	default:
		return 1000
	}
}

func isCropioPhotoName(name string) bool {
	return name == "photo" || strings.HasPrefix(name, "photo.")
}

func normalizeImageURL(raw string, baseURL string) string {
	s := strings.TrimSpace(raw)
	if s == "" {
		return ""
	}
	if strings.HasPrefix(s, "//") {
		return "https:" + s
	}
	if strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://") {
		return s
	}
	if strings.HasPrefix(s, "/") && baseURL != "" {
		return strings.TrimRight(baseURL, "/") + s
	}
	return ""
}

func cropioOriginalImageURL(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return rawURL
	}
	if !strings.EqualFold(u.Host, "storage.googleapis.com") {
		return rawURL
	}
	if !strings.Contains(strings.ToLower(u.Path), "/cropio-uploads/") {
		return rawURL
	}
	idx := strings.LastIndex(u.Path, "/")
	if idx < 0 || idx == len(u.Path)-1 {
		return rawURL
	}
	name := u.Path[idx+1:]
	nameLower := strings.ToLower(name)
	suffixIdx := strings.LastIndex(nameLower, "_photo")
	if !strings.HasPrefix(nameLower, "preview_") || suffixIdx < 0 {
		return rawURL
	}
	suffix := name[suffixIdx+len("_photo"):]
	u.Path = u.Path[:idx+1] + "photo" + suffix
	return u.String()
}

func isImageURL(s string) bool {
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, "http://") && !strings.HasPrefix(s, "https://") {
		return false
	}
	if imageExtRe.MatchString(s) {
		return true
	}
	u, err := url.Parse(s)
	if err != nil {
		return false
	}
	path := strings.ToLower(u.Path)
	return strings.Contains(path, "image") || strings.Contains(path, "photo") || strings.Contains(path, "/system/") || strings.Contains(path, "/uploads/") || strings.Contains(path, "rails/active_storage")
}

func trimForLog(s string, max int) string {
	r := []rune(strings.TrimSpace(s))
	if len(r) <= max {
		return string(r)
	}
	return string(r[:max]) + "..."
}

// MergeReportData аккуратно дополняет основной отчет данными из aggregated endpoint.
// Значимые поля из aggregated нужны прежде всего для image1/image2/image3/photos и report NDVI.
func MergeReportData(dst CropwiseReport, src CropwiseResource) {
	if dst == nil || src == nil {
		return
	}
	preferKeys := map[string]bool{
		"image1": true, "image2": true, "image3": true, "photos": true,
		"attachments":  true,
		"measurements": true, "threats": true,
		"ndvi": true, "ndvi_value": true,
		"field_condition": true, "risk_yield_decreasing": true,
		"growth_stage_id": true, "growth_stage": true, "growth_scale": true,
		"additional_info": true, "created_by_user_at": true, "updated_by_user_at": true,
		"created_by_user": true, "created_by_user_name": true, "created_by_user_id": true,
		"created_by": true, "created_by_name": true, "created_by_id": true,
		"created_user": true, "created_user_name": true, "user": true, "user_name": true, "user_id": true,
		"creator": true, "creator_name": true, "creator_id": true,
		"author": true, "author_name": true, "author_id": true,
	}
	for k, v := range src {
		if v == nil {
			continue
		}
		if preferKeys[k] {
			if isEmptyAny(dst[k]) {
				dst[k] = v
			}
			continue
		}
		if _, exists := dst[k]; !exists {
			dst[k] = v
		}
	}
}

func isEmptyAny(v any) bool {
	if v == nil {
		return true
	}
	switch t := v.(type) {
	case string:
		return strings.TrimSpace(t) == ""
	case []any:
		return len(t) == 0
	case map[string]any:
		return len(t) == 0
	}
	return false
}
