package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

type CropwiseClient struct {
	baseURL         string
	apiVersion      string
	resource        string
	fieldsRes       string
	fieldsVer       string
	fieldGroupsRes  string
	fieldGroupsVer  string
	historyItemsRes string
	historyItemsVer string
	cropsRes        string
	cropsVer        string
	histValsRes     string
	histValsVer     string
	pointsRes       string
	pointsVer       string
	measurementsRes string
	measurementsVer string
	measTypesRes    string
	measTypesVer    string
	usersRes        string
	usersVer        string
	growthStagesRes string
	growthStagesVer string
	growthStructRes string
	growthStructVer string
	threatMapRes    string
	threatMapVer    string
	pointIssuesRes  string
	pointIssuesVer  string
	photosRes       string
	photosVer       string
	aggregatedRes   string
	aggregatedVer   string
	webBaseURL      string
	token           string
	email           string
	password        string
	httpClient      *http.Client
}

type LoginResponse struct {
	Success      bool   `json:"success"`
	UserAPIToken string `json:"user_api_token"`
	UserID       int64  `json:"user_id"`
	Email        string `json:"email"`
	Username     string `json:"username"`
	Company      string `json:"company"`
}

type CropwiseListResponse struct {
	Data []json.RawMessage `json:"data"`
	Meta struct {
		Response struct {
			Limit           *int  `json:"limit"`
			ObtainedRecords int   `json:"obtained_records"`
			LastRecordID    int64 `json:"last_record_id"`
		} `json:"response"`
	} `json:"meta"`
}

type CropwiseReport map[string]any
type CropwiseField map[string]any
type CropwiseResource map[string]any

type FieldInfo struct {
	Name         string
	WebSlug      string
	GroupName    string
	GroupID      int64
	LegalArea    string
	TillableArea string
}

func NewCropwiseClient(cfg Config) *CropwiseClient {
	return &CropwiseClient{
		baseURL:         cfg.CropwiseBaseURL,
		apiVersion:      cfg.CropwiseAPIVersion,
		resource:        cfg.CropwiseReportsRes,
		fieldsRes:       cfg.CropwiseFieldsRes,
		fieldsVer:       cfg.CropwiseFieldsVer,
		fieldGroupsRes:  cfg.CropwiseFieldGroupsRes,
		fieldGroupsVer:  cfg.CropwiseFieldGroupsVer,
		historyItemsRes: cfg.CropwiseHistRes,
		historyItemsVer: cfg.CropwiseHistVer,
		cropsRes:        cfg.CropwiseCropsRes,
		cropsVer:        cfg.CropwiseCropsVer,
		histValsRes:     cfg.CropwiseHistValsRes,
		histValsVer:     cfg.CropwiseHistValsVer,
		pointsRes:       cfg.CropwisePointsRes,
		pointsVer:       cfg.CropwisePointsVer,
		measurementsRes: cfg.CropwiseMeasRes,
		measurementsVer: cfg.CropwiseMeasVer,
		measTypesRes:    cfg.CropwiseMeasTypesRes,
		measTypesVer:    cfg.CropwiseMeasTypesVer,
		usersRes:        cfg.CropwiseUsersRes,
		usersVer:        cfg.CropwiseUsersVer,
		growthStagesRes: cfg.CropwiseGrowthStagesRes,
		growthStagesVer: cfg.CropwiseGrowthStagesVer,
		growthStructRes: cfg.CropwiseGrowthStructRes,
		growthStructVer: cfg.CropwiseGrowthStructVer,
		threatMapRes:    cfg.CropwiseThreatMapRes,
		threatMapVer:    cfg.CropwiseThreatMapVer,
		pointIssuesRes:  cfg.CropwisePointIssuesRes,
		pointIssuesVer:  cfg.CropwisePointIssuesVer,
		photosRes:       cfg.CropwisePhotosRes,
		photosVer:       cfg.CropwisePhotosVer,
		aggregatedRes:   cfg.CropwiseAggregatedRes,
		aggregatedVer:   cfg.CropwiseAggregatedVer,
		webBaseURL:      cfg.CropwiseWebBaseURL,
		token:           cfg.CropwiseToken,
		email:           cfg.CropwiseEmail,
		password:        cfg.CropwisePassword,
		httpClient:      &http.Client{Timeout: 60 * time.Second},
	}
}

func (c *CropwiseClient) EnsureAuth(ctx context.Context) error {
	if c.token != "" {
		return nil
	}
	body := map[string]any{
		"user_login": map[string]string{
			"email":    c.email,
			"password": c.password,
		},
	}
	var res LoginResponse
	if err := c.doJSON(ctx, http.MethodPost, c.url("sign_in", nil), body, &res); err != nil {
		return err
	}
	if res.UserAPIToken == "" {
		return fmt.Errorf("cropwise login response has empty user_api_token")
	}
	c.token = res.UserAPIToken
	fmt.Printf("Cropwise login OK: user_id=%d email=%s company=%s\n", res.UserID, res.Email, res.Company)
	return nil
}

func (c *CropwiseClient) FetchOldReports(ctx context.Context, fieldID int64, fromTime time.Time) ([]CropwiseReport, error) {
	var all []CropwiseReport
	fromID := int64(0)
	limit := 1000
	for {
		q := url.Values{}
		if fieldID > 0 {
			q.Set("field_id", strconv.FormatInt(fieldID, 10))
		}
		q.Set("report_time_gt_eq", cropwiseTime(fromTime))
		q.Set("limit", strconv.Itoa(limit))
		q.Set("from_id", strconv.FormatInt(fromID, 10))
		q.Set("sort_by", "report_time_asc")

		items, lastID, obtained, err := c.fetchReports(ctx, c.url(c.resource, q))
		if err != nil {
			return nil, err
		}
		all = append(all, items...)
		if obtained < limit || lastID <= fromID {
			break
		}
		fromID = lastID
	}
	sortReports(all)
	return all, nil
}

func (c *CropwiseClient) FetchReportByID(ctx context.Context, reportID int64) (CropwiseReport, error) {
	if reportID == 0 {
		return nil, nil
	}
	var lastErr error
	versions := []string{"v3", c.apiVersion}
	seen := map[string]bool{}
	for _, version := range versions {
		version = strings.Trim(version, "/")
		if version == "" || seen[version] {
			continue
		}
		seen[version] = true

		endpoint := c.urlV(version, c.resource+"/"+strconv.FormatInt(reportID, 10), nil)
		var raw json.RawMessage
		if err := c.doJSON(ctx, http.MethodGet, endpoint, nil, &raw); err != nil {
			lastErr = err
			continue
		}
		report, err := decodeReportResponse(raw)
		if err != nil {
			lastErr = err
			continue
		}
		return report, nil
	}
	return nil, lastErr
}

func (c *CropwiseClient) FetchChangedReports(ctx context.Context, fromTime, toTime time.Time) ([]CropwiseReport, error) {
	q := url.Values{}
	q.Set("from_time", cropwiseTime(fromTime))
	q.Set("to_time", cropwiseTime(toTime))
	q.Set("limit", "1000")
	items, _, _, err := c.fetchReports(ctx, c.url(c.resource+"/changes", q))
	if err != nil {
		return nil, err
	}
	sortReports(items)
	return items, nil
}

func (c *CropwiseClient) FetchFields(ctx context.Context) ([]CropwiseField, error) {
	items, err := c.fetchAllResources(ctx, c.fieldsVer, c.fieldsRes, url.Values{})
	if err != nil {
		return nil, err
	}
	out := make([]CropwiseField, 0, len(items))
	for _, item := range items {
		out = append(out, CropwiseField(item))
	}
	return out, nil
}

func (c *CropwiseClient) FetchFieldGroups(ctx context.Context) ([]CropwiseResource, error) {
	return c.fetchAllResources(ctx, c.fieldGroupsVer, c.fieldGroupsRes, url.Values{})
}

func (c *CropwiseClient) FetchCrops(ctx context.Context) ([]CropwiseResource, error) {
	return c.fetchAllResources(ctx, c.cropsVer, c.cropsRes, url.Values{})
}

func (c *CropwiseClient) FetchHistoryItems(ctx context.Context, fieldID int64, year string) ([]CropwiseResource, error) {
	q := url.Values{}
	q.Set("field_id", strconv.FormatInt(fieldID, 10))
	if strings.TrimSpace(year) != "" {
		q.Set("year", strings.TrimSpace(year))
	}
	return c.fetchAllResources(ctx, c.historyItemsVer, c.historyItemsRes, q)
}

func (c *CropwiseClient) FetchNDVI(ctx context.Context, fieldID int64, at time.Time) (string, error) {
	if fieldID == 0 || at.IsZero() {
		return "", nil
	}
	q := url.Values{}
	q.Set("field_id", strconv.FormatInt(fieldID, 10))
	q.Set("type", "ndvi")
	q.Set("from_time", cropwiseTime(at.AddDate(0, 0, -30)))
	q.Set("to_time", cropwiseTime(at.Add(24*time.Hour)))

	var res struct {
		Data []map[string]any `json:"data"`
		Meta map[string]any   `json:"meta"`
	}
	if err := c.doJSON(ctx, http.MethodGet, c.urlV(c.histValsVer, c.histValsRes, q), nil, &res); err != nil {
		return "", err
	}

	// v3/v3a can return values in data array. v3b can return shorter objects with last_value.
	bestTime := time.Time{}
	bestValue := ""
	for _, item := range res.Data {
		value := stringValue(item, "ndvi", "last_value", "value")
		dateRaw := stringValue(item, "date", "last_value_date")
		if value == "" {
			continue
		}
		if dateRaw == "" {
			bestValue = value
			continue
		}
		if t, err := parseAnyTime(dateRaw); err == nil {
			if bestTime.IsZero() || t.After(bestTime) {
				bestTime = t
				bestValue = value
			}
		} else if bestValue == "" {
			bestValue = value
		}
	}
	return bestValue, nil
}

func (c *CropwiseClient) FetchScoutReportPoints(ctx context.Context, reportID int64) ([]CropwiseResource, error) {
	q := url.Values{}
	q.Set("field_scout_report_id", strconv.FormatInt(reportID, 10))
	return c.fetchAllResources(ctx, c.pointsVer, c.pointsRes, q)
}

func (c *CropwiseClient) FetchMeasurementsForPoint(ctx context.Context, pointID int64) ([]CropwiseResource, error) {
	q := url.Values{}
	q.Set("scout_report_point_id", strconv.FormatInt(pointID, 10))
	return c.fetchAllResources(ctx, c.measurementsVer, c.measurementsRes, q)
}

func (c *CropwiseClient) FetchMeasurementTypes(ctx context.Context) ([]CropwiseResource, error) {
	return c.fetchAllResources(ctx, c.measTypesVer, c.measTypesRes, url.Values{})
}

func (c *CropwiseClient) FetchUsers(ctx context.Context) ([]CropwiseResource, error) {
	return c.fetchAllResources(ctx, c.usersVer, c.usersRes, url.Values{})
}

func (c *CropwiseClient) FetchUserByID(ctx context.Context, userID int64) (CropwiseResource, error) {
	if userID == 0 {
		return nil, nil
	}
	q := url.Values{}
	q.Set("id", strconv.FormatInt(userID, 10))
	items, err := c.fetchAllResources(ctx, c.usersVer, c.usersRes, q)
	if err != nil {
		return nil, err
	}
	for _, item := range items {
		if int64Value(item, "id") == userID {
			return item, nil
		}
	}
	if len(items) > 0 {
		return items[0], nil
	}
	return nil, nil
}

func (c *CropwiseClient) FetchGrowthStages(ctx context.Context) ([]CropwiseResource, error) {
	return c.fetchAllResources(ctx, c.growthStagesVer, c.growthStagesRes, url.Values{})
}

func (c *CropwiseClient) FetchGrowthStageByID(ctx context.Context, id int64) (CropwiseResource, error) {
	if id == 0 {
		return nil, nil
	}
	var raw json.RawMessage
	endpoint := c.urlV(c.growthStagesVer, c.growthStagesRes+"/"+strconv.FormatInt(id, 10), nil)
	if err := c.doJSON(ctx, http.MethodGet, endpoint, nil, &raw); err != nil {
		return nil, err
	}
	return decodeResourceResponse(raw)
}

func (c *CropwiseClient) FetchPointIssuesForPoint(ctx context.Context, pointID int64) ([]CropwiseResource, error) {
	q := url.Values{}
	q.Set("scout_report_point_id", strconv.FormatInt(pointID, 10))
	return c.fetchAllResources(ctx, c.pointIssuesVer, c.pointIssuesRes, q)
}

func (c *CropwiseClient) FetchFieldScoutReportThreatMappingItems(ctx context.Context, reportID int64) ([]CropwiseResource, error) {
	if reportID == 0 {
		return nil, nil
	}
	q := url.Values{}
	q.Set("field_scout_report_id", strconv.FormatInt(reportID, 10))
	return c.fetchAllResources(ctx, c.threatMapVer, c.threatMapRes, q)
}

func (c *CropwiseClient) FetchAggregatedReport(ctx context.Context, reportID int64) (CropwiseResource, error) {
	if reportID == 0 {
		return nil, nil
	}
	for _, key := range []string{"id", "field_scout_report_id"} {
		q := url.Values{}
		q.Set(key, strconv.FormatInt(reportID, 10))
		items, err := c.fetchAllResources(ctx, c.aggregatedVer, c.aggregatedRes, q)
		if err != nil {
			return nil, err
		}
		for _, item := range items {
			if int64Value(item, "id") == reportID || int64Value(item, "field_scout_report_id") == reportID {
				return item, nil
			}
		}
		if len(items) > 0 {
			return items[0], nil
		}
	}
	return nil, nil
}

func (c *CropwiseClient) FetchPhotosForPhotoable(ctx context.Context, photoableType string, photoableID int64) ([]CropwiseResource, error) {
	return c.FetchPhotosForPhotoableInVersion(ctx, c.photosVer, photoableType, photoableID)
}

func (c *CropwiseClient) FetchPhotosForPhotoableInVersion(ctx context.Context, version string, photoableType string, photoableID int64) ([]CropwiseResource, error) {
	if strings.TrimSpace(photoableType) == "" || photoableID == 0 {
		return nil, nil
	}
	q := url.Values{}
	q.Set("photoable_type", photoableType)
	q.Set("photoable_id", strconv.FormatInt(photoableID, 10))
	return c.fetchAllResources(ctx, version, c.photosRes, q)
}

func (c *CropwiseClient) FetchPhotosForPhotoableIDOnlyInVersion(ctx context.Context, version string, photoableID int64) ([]CropwiseResource, error) {
	if photoableID == 0 {
		return nil, nil
	}
	q := url.Values{}
	q.Set("photoable_id", strconv.FormatInt(photoableID, 10))
	return c.fetchAllResources(ctx, version, c.photosRes, q)
}

type PhotoProbeResult struct {
	Version string
	Type    string
	IDOnly  bool
	Count   int
	Error   string
}

func (c *CropwiseClient) FetchPhotosLoose(ctx context.Context, photoableID int64, photoableTypes ...string) ([]CropwiseResource, []PhotoProbeResult) {
	seen := map[int64]bool{}
	seenURL := map[string]bool{}
	out := make([]CropwiseResource, 0)
	probes := make([]PhotoProbeResult, 0)
	versions := uniqueStrings(c.photosVer, "v3", "v3a", "v3b")
	types := uniqueStrings(photoableTypes...)
	for _, version := range versions {
		for _, typ := range types {
			photos, err := c.FetchPhotosForPhotoableInVersion(ctx, version, typ, photoableID)
			probe := PhotoProbeResult{Version: version, Type: typ, Count: len(photos)}
			if err != nil {
				probe.Error = err.Error()
				probes = append(probes, probe)
				continue
			}
			probes = append(probes, probe)
			for _, ph := range photos {
				id := int64Value(ph, "id")
				urlKey := stringValue(ph, "uuid", "external_id", "md5")
				if id != 0 && seen[id] {
					continue
				}
				if id == 0 && urlKey != "" && seenURL[urlKey] {
					continue
				}
				if id != 0 {
					seen[id] = true
				}
				if urlKey != "" {
					seenURL[urlKey] = true
				}
				out = append(out, ph)
			}
		}
		// Иногда реальный photoable_type отличается от документации/DOM. Тогда пробуем только photoable_id.
		photos, err := c.FetchPhotosForPhotoableIDOnlyInVersion(ctx, version, photoableID)
		probe := PhotoProbeResult{Version: version, IDOnly: true, Count: len(photos)}
		if err != nil {
			probe.Error = err.Error()
			probes = append(probes, probe)
			continue
		}
		probes = append(probes, probe)
		for _, ph := range photos {
			id := int64Value(ph, "id")
			urlKey := stringValue(ph, "uuid", "external_id", "md5")
			if id != 0 && seen[id] {
				continue
			}
			if id == 0 && urlKey != "" && seenURL[urlKey] {
				continue
			}
			if id != 0 {
				seen[id] = true
			}
			if urlKey != "" {
				seenURL[urlKey] = true
			}
			out = append(out, ph)
		}
	}
	return out, probes
}

func uniqueStrings(vals ...string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(vals))
	for _, v := range vals {
		v = strings.TrimSpace(v)
		if v == "" || seen[v] {
			continue
		}
		seen[v] = true
		out = append(out, v)
	}
	return out
}

func (c *CropwiseClient) FetchGrowthStageStructuresForPoint(ctx context.Context, pointID int64) ([]CropwiseResource, error) {
	if pointID == 0 {
		return nil, nil
	}
	q := url.Values{}
	q.Set("scout_report_point_id", strconv.FormatInt(pointID, 10))
	return c.fetchAllResources(ctx, c.growthStructVer, c.growthStructRes, q)
}

func (c *CropwiseClient) CropwiseAuthHeadersForURL(rawURL string) map[string]string {
	u, err := url.Parse(rawURL)
	if err != nil || c.token == "" {
		return nil
	}
	base, err := url.Parse(c.webBaseURL)
	if err != nil {
		return nil
	}
	if strings.EqualFold(u.Hostname(), base.Hostname()) || strings.HasSuffix(strings.ToLower(u.Hostname()), ".cropwise.com") {
		return map[string]string{"X-User-Api-Token": c.token}
	}
	return nil
}

func (c *CropwiseClient) fetchAllResources(ctx context.Context, version, resource string, baseQ url.Values) ([]CropwiseResource, error) {
	var all []CropwiseResource
	fromID := int64(0)
	limit := 1000
	for {
		q := url.Values{}
		for k, vals := range baseQ {
			for _, v := range vals {
				q.Add(k, v)
			}
		}
		q.Set("limit", strconv.Itoa(limit))
		q.Set("from_id", strconv.FormatInt(fromID, 10))
		if q.Get("sort_by") == "" {
			q.Set("sort_by", "id_asc")
		}
		items, lastID, obtained, err := c.fetchResources(ctx, c.urlV(version, resource, q))
		if err != nil {
			return nil, err
		}
		all = append(all, items...)
		if obtained < limit || lastID <= fromID {
			break
		}
		fromID = lastID
	}
	return all, nil
}

func BuildFieldGroupNameMap(groups []CropwiseResource) map[int64]string {
	out := make(map[int64]string, len(groups))
	for _, g := range groups {
		id := int64Value(g, "id")
		if id == 0 {
			continue
		}
		name := stringValue(g, "name", "short_name", "custom_name", "description", "legal_entity")
		if name != "" {
			out[id] = name
		}
	}
	return out
}

func BuildFieldInfoMap(fields []CropwiseField, fieldGroupNames map[int64]string) map[int64]FieldInfo {
	out := make(map[int64]FieldInfo, len(fields))
	for _, f := range fields {
		id := int64Value(f, "id")
		if id == 0 {
			continue
		}
		groupID := int64Value(f, "field_group_id")
		fieldName := stringValue(f, "name", "short_name", "custom_name", "description")
		info := FieldInfo{
			Name:         fieldName,
			WebSlug:      cropwiseFieldPathSlug(id, fieldName),
			GroupID:      groupID,
			GroupName:    fieldGroupNames[groupID],
			LegalArea:    numberStringAny(f, "legal_area", "calculated_area"),
			TillableArea: numberStringAny(f, "tillable_area"),
		}
		if info.Name != "" || info.WebSlug != "" || info.GroupName != "" || info.LegalArea != "" || info.TillableArea != "" {
			out[id] = info
		}
	}
	return out
}

func cropwiseFieldPathSlug(fieldID int64, fieldName string) string {
	nameSlug := cropwiseSlugify(fieldName)
	if fieldID == 0 {
		return nameSlug
	}
	if nameSlug == "" {
		return strconv.FormatInt(fieldID, 10)
	}
	return strconv.FormatInt(fieldID, 10) + "-" + nameSlug
}

func cropwiseSlugify(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	var b strings.Builder
	lastDash := false
	for _, r := range s {
		part := translitRune(r)
		if part == "" {
			if !lastDash && b.Len() > 0 {
				b.WriteByte('-')
				lastDash = true
			}
			continue
		}
		for _, pr := range part {
			if (pr >= 'a' && pr <= 'z') || (pr >= '0' && pr <= '9') {
				b.WriteRune(pr)
				lastDash = false
			} else if pr == '-' || pr == '_' || pr == ' ' {
				if !lastDash && b.Len() > 0 {
					b.WriteByte('-')
					lastDash = true
				}
			}
		}
	}
	out := strings.Trim(b.String(), "-")
	for strings.Contains(out, "--") {
		out = strings.ReplaceAll(out, "--", "-")
	}
	return out
}

func translitRune(r rune) string {
	if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '_' || r == ' ' {
		return string(r)
	}
	switch r {
	case 'а':
		return "a"
	case 'б':
		return "b"
	case 'в':
		return "v"
	case 'г':
		return "g"
	case 'д':
		return "d"
	case 'е', 'ё', 'э':
		return "e"
	case 'ж':
		return "zh"
	case 'з':
		return "z"
	case 'и', 'й':
		return "i"
	case 'к':
		return "k"
	case 'л':
		return "l"
	case 'м':
		return "m"
	case 'н':
		return "n"
	case 'о':
		return "o"
	case 'п':
		return "p"
	case 'р':
		return "r"
	case 'с':
		return "s"
	case 'т':
		return "t"
	case 'у':
		return "u"
	case 'ф':
		return "f"
	case 'х':
		return "h"
	case 'ц':
		return "ts"
	case 'ч':
		return "ch"
	case 'ш':
		return "sh"
	case 'щ':
		return "sch"
	case 'ы':
		return "y"
	case 'ю':
		return "yu"
	case 'я':
		return "ya"
	case 'ь', 'ъ':
		return ""
	}
	return ""
}

func BuildFieldNameMap(fields []CropwiseField) map[int64]string {
	infos := BuildFieldInfoMap(fields, nil)
	out := make(map[int64]string, len(infos))
	for id, info := range infos {
		out[id] = info.Name
	}
	return out
}

func BuildCropNameMap(crops []CropwiseResource) map[int64]string {
	out := make(map[int64]string, len(crops))
	for _, crop := range crops {
		id := int64Value(crop, "id")
		if id == 0 {
			continue
		}
		name := stringValue(crop, "name", "short_name", "standard_name", "description")
		if name != "" {
			out[id] = name
		}
	}
	return out
}

func BuildMeasurementTypeNameMap(types []CropwiseResource) map[int64]string {
	out := make(map[int64]string, len(types))
	for _, t := range types {
		id := int64Value(t, "id")
		if id == 0 {
			continue
		}
		name := stringValue(t, "human_name", "name", "system_name")
		if name != "" {
			out[id] = name
		}
	}
	return out
}

func BuildUserNameMap(users []CropwiseResource) map[int64]string {
	out := make(map[int64]string, len(users))
	for _, u := range users {
		id := int64Value(u, "id")
		if id == 0 {
			continue
		}
		name := userDisplayName(u)
		if name != "" {
			out[id] = name
		}
	}
	return out
}

func userDisplayName(u map[string]any) string {
	if s := stringValue(u, "full_name", "name", "display_name", "username"); s != "" {
		return s
	}
	parts := []string{}
	for _, k := range []string{"last_name", "first_name", "middle_name", "patronymic"} {
		if v := stringValue(u, k); v != "" {
			parts = append(parts, v)
		}
	}
	if len(parts) > 0 {
		return strings.Join(parts, " ")
	}
	return stringValue(u, "email")
}

func BuildGrowthStageNameMap(stages []CropwiseResource) map[int64]string {
	out := make(map[int64]string, len(stages))
	for _, g := range stages {
		id := int64Value(g, "id")
		if id == 0 {
			continue
		}
		name := growthStageDisplayName(g)
		if name != "" {
			out[id] = name
		}
	}
	return out
}

func growthStageDisplayName(g map[string]any) string {
	name := stringValue(g, "localized_name", "human_name", "name", "title", "stage_name", "growth_stage_name")
	description := stringValue(g, "description")
	code := stringValue(g, "code", "stage", "bbch_code", "value")
	if code == "" && looksGrowthStageCode(name) {
		code = name
		if description != "" {
			name = description
		}
	}
	if name == "" {
		name = description
	}
	if name == "" {
		return code
	}
	if code != "" && name != "" && !strings.HasPrefix(strings.TrimSpace(name), code) {
		name = code + " | " + name
	}
	return name
}

func looksGrowthStageCode(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" {
		return false
	}
	if len([]rune(s)) > 3 {
		return false
	}
	_, err := strconv.Atoi(s)
	return err == nil
}

func EnrichReportsWithUserNames(items []CropwiseReport, userNames map[int64]string) {
	if len(userNames) == 0 {
		return
	}
	for _, r := range items {
		userID := reportCreatorUserID(r)
		if userID == 0 {
			continue
		}
		if name := userNames[userID]; name != "" {
			r["created_by_user_name"] = name
		}
	}
}

func reportCreatorUserID(r CropwiseReport) int64 {
	for _, key := range []string{"user_id", "created_by_user_id", "created_by_id", "creator_id", "author_id"} {
		if id := int64Value(r, key); id != 0 {
			return id
		}
	}
	return 0
}

func EnrichReportsWithGrowthStages(items []CropwiseReport, growthStageNames map[int64]string) {
	if len(growthStageNames) == 0 {
		return
	}
	for _, r := range items {
		growthStageID := int64Value(r, "growth_stage_id")
		if growthStageID == 0 {
			continue
		}
		if name := growthStageNames[growthStageID]; name != "" {
			r["growth_stage_name"] = name
		}
	}
}

func EnrichReportsWithFieldInfo(items []CropwiseReport, fieldInfos map[int64]FieldInfo) {
	if len(fieldInfos) == 0 {
		return
	}
	for _, r := range items {
		fieldID := int64Value(r, "field_id")
		if fieldID == 0 {
			continue
		}
		info, ok := fieldInfos[fieldID]
		if !ok {
			continue
		}
		if fieldName(r) == "" && info.Name != "" {
			r["field_name"] = info.Name
		}
		if stringValue(r, "field_web_slug") == "" && info.WebSlug != "" {
			r["field_web_slug"] = info.WebSlug
		}
		if stringValue(r, "field_group_name") == "" && info.GroupName != "" {
			r["field_group_name"] = info.GroupName
		}
		if int64Value(r, "field_group_id") == 0 && info.GroupID != 0 {
			r["field_group_id"] = info.GroupID
		}
		if numberStringAny(r, "legal_area") == "" && info.LegalArea != "" {
			r["legal_area"] = info.LegalArea
		}
		if numberStringAny(r, "tillable_area") == "" && info.TillableArea != "" {
			r["tillable_area"] = info.TillableArea
		}
	}
}

func EnrichReportsWithFieldNames(items []CropwiseReport, fieldNames map[int64]string) {
	fieldInfos := make(map[int64]FieldInfo, len(fieldNames))
	for id, name := range fieldNames {
		fieldInfos[id] = FieldInfo{Name: name}
	}
	EnrichReportsWithFieldInfo(items, fieldInfos)
}

func EnrichReportProductionCycle(r CropwiseReport, historyItems []CropwiseResource, cropNames map[int64]string) {
	if stringValue(r, "production_cycle", "production_cycle_name") != "" {
		return
	}
	season := stringValue(r, "season")
	var selected CropwiseResource
	for _, h := range historyItems {
		if season != "" && stringValue(h, "year", "season") != "" && stringValue(h, "year", "season") != season {
			continue
		}
		selected = h
		if isTruthy(h["active"]) || isTruthy(h["is_active"]) {
			break
		}
	}
	if selected == nil {
		return
	}
	cropName := stringValue(selected, "crop_name", "crop_title", "name", "title")
	if cropName == "" {
		if crop, ok := selected["crop"].(map[string]any); ok {
			cropName = stringValue(crop, "name", "short_name", "standard_name", "title")
		}
	}
	if cropName == "" {
		cropID := int64Value(selected, "crop_id")
		cropName = cropNames[cropID]
	}
	cycleDateRaw := stringValue(selected,
		"seeding_date", "sowing_date", "planting_date", "start_date", "started_at", "start_time",
		"crop_started_at", "crop_start_date", "from_time", "date",
	)
	cycleDate := formatDateOnly(cycleDateRaw)
	if cropName != "" {
		r["production_crop_name"] = cropName
	}
	if cycleDate != "" {
		r["production_cycle_date"] = cycleDate
	}
	parts := []string{}
	if cropName != "" {
		parts = append(parts, cropName)
	}
	if cycleDate != "" {
		parts = append(parts, cycleDate)
	} else if season != "" {
		parts = append(parts, season)
	}
	if len(parts) > 0 {
		r["production_cycle_name"] = strings.Join(parts, " | ")
	}
}

func EnrichReportWithScoutData(r CropwiseReport, points []CropwiseResource, measurements []CropwiseResource, issues []CropwiseResource, growthStageNames map[int64]string, measurementTypeNames map[int64]string) {
	if len(points) > 0 {
		r["scout_report_points_count"] = len(points)
	}
	aggregatedCalculated := aggregatedCalculatedMeasurementsByID(r)
	measurementsByPoint := map[int64][]map[string]any{}
	flatMeasurements := make([]any, 0, len(measurements))
	for _, m := range measurements {
		mtID := int64Value(m, "scout_report_measurement_type_id")
		if name := measurementTypeNames[mtID]; name != "" {
			m["measurement_type_name"] = name
		}
		enrichMeasurementCalculatedValue(map[string]any(m), aggregatedCalculated)
		enrichMeasurementGrowthStageValue(map[string]any(m), growthStageNames)
		pointID := int64Value(m, "scout_report_point_id")
		mm := map[string]any(m)
		measurementsByPoint[pointID] = append(measurementsByPoint[pointID], mm)
		flatMeasurements = append(flatMeasurements, mm)
	}
	issuesByPoint := map[int64][]map[string]any{}
	for _, issue := range issues {
		pointID := int64Value(issue, "scout_report_point_id")
		issuesByPoint[pointID] = append(issuesByPoint[pointID], map[string]any(issue))
	}

	pointArr := make([]any, 0, len(points))
	for i, p := range points {
		pm := map[string]any(p)
		pointID := int64Value(pm, "id")
		pm["point_index"] = i + 1
		if name := growthStageNames[int64Value(pm, "growth_stage_id")]; name != "" {
			pm["growth_stage_name"] = name
		}
		if ms := measurementsByPoint[pointID]; len(ms) > 0 {
			arr := make([]any, 0, len(ms))
			for _, m := range ms {
				arr = append(arr, m)
			}
			pm["measurements"] = arr
		}
		if is := issuesByPoint[pointID]; len(is) > 0 {
			arr := make([]any, 0, len(is))
			for _, issue := range is {
				arr = append(arr, issue)
			}
			pm["issues"] = arr
		}
		pointArr = append(pointArr, pm)
	}
	if len(pointArr) > 0 {
		r["scout_report_points"] = pointArr
	}
	if len(flatMeasurements) > 0 {
		r["scout_report_point_measurements"] = flatMeasurements
	}
}

func EnrichReportWithMeasurements(r CropwiseReport, points []CropwiseResource, measurements []CropwiseResource, measurementTypeNames map[int64]string) {
	if len(points) > 0 {
		r["scout_report_points_count"] = len(points)
	}
	if len(measurements) == 0 {
		return
	}
	arr := make([]any, 0, len(measurements))
	for _, m := range measurements {
		mtID := int64Value(m, "scout_report_measurement_type_id")
		if name := measurementTypeNames[mtID]; name != "" {
			m["measurement_type_name"] = name
		}
		enrichMeasurementGrowthStageValue(map[string]any(m), nil)
		arr = append(arr, map[string]any(m))
	}
	r["scout_report_point_measurements"] = arr
}

func enrichMeasurementGrowthStageValue(m map[string]any, growthStageNames map[int64]string) {
	if len(growthStageNames) == 0 || m == nil {
		return
	}
	vals := measurementValuesMap(m)
	for key, raw := range vals {
		if normalizeKey(key) != "growth_stage_id" {
			continue
		}
		if name := mappedGrowthStageName(raw, growthStageNames); name != "" {
			vals[key] = name
			m["measurement_values"] = vals
		}
	}
	if !isGrowthStageMeasurement(m) {
		return
	}
	for _, key := range []string{"calculated_value", "value"} {
		if name := mappedGrowthStageName(m[key], growthStageNames); name != "" {
			m[key] = name
		}
	}
}

func mappedGrowthStageName(raw any, growthStageNames map[int64]string) string {
	if len(growthStageNames) == 0 || raw == nil {
		return ""
	}
	id, err := strconv.ParseInt(strings.TrimSpace(anyToString(raw)), 10, 64)
	if err != nil || id == 0 {
		return ""
	}
	return growthStageNames[id]
}

func aggregatedCalculatedMeasurementsByID(r CropwiseReport) map[int64]map[string]any {
	out := map[int64]map[string]any{}
	for _, raw := range toAnySlice(r["measurements"]) {
		m, ok := raw.(map[string]any)
		if !ok || !isTruthy(m["calculated"]) {
			continue
		}
		id := int64Value(m, "id")
		if id == 0 {
			continue
		}
		if stringValue(m, "value") == "" {
			continue
		}
		out[id] = m
	}
	return out
}

func enrichMeasurementCalculatedValue(m map[string]any, aggregated map[int64]map[string]any) {
	if m == nil || len(aggregated) == 0 {
		return
	}
	agg := aggregated[int64Value(m, "id")]
	if len(agg) == 0 {
		return
	}
	unit := stringValue(agg, "unit")
	if unit == "" {
		return
	}
	if value := stringValue(agg, "value"); value != "" {
		m["calculated_value"] = value
	}
	m["calculated_value_unit_name"] = unit
	if systemName := stringValue(agg, "system_name"); systemName != "" {
		m["calculated_value_system_name"] = systemName
	}
	if measurementType := stringValue(agg, "type"); measurementType != "" && measurementTitle(m) == "" {
		m["measurement_type_name"] = measurementType
	}
}

func (c *CropwiseClient) fetchReports(ctx context.Context, endpoint string) ([]CropwiseReport, int64, int, error) {
	items, lastID, obtained, err := c.fetchResources(ctx, endpoint)
	if err != nil {
		return nil, 0, 0, err
	}
	out := make([]CropwiseReport, 0, len(items))
	for _, item := range items {
		out = append(out, CropwiseReport(item))
	}
	return out, lastID, obtained, nil
}

func decodeReportResponse(raw []byte) (CropwiseReport, error) {
	var wrapped struct {
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(raw, &wrapped); err == nil && len(wrapped.Data) > 0 && string(wrapped.Data) != "null" {
		var report map[string]any
		if err := json.Unmarshal(wrapped.Data, &report); err != nil {
			return nil, fmt.Errorf("unmarshal report data: %w; raw=%s", err, string(wrapped.Data))
		}
		return CropwiseReport(report), nil
	}

	var report map[string]any
	if err := json.Unmarshal(raw, &report); err != nil {
		return nil, fmt.Errorf("unmarshal report: %w; raw=%s", err, string(raw))
	}
	return CropwiseReport(report), nil
}

func decodeResourceResponse(raw []byte) (CropwiseResource, error) {
	var wrapped struct {
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(raw, &wrapped); err == nil && len(wrapped.Data) > 0 && string(wrapped.Data) != "null" {
		var resource map[string]any
		if err := json.Unmarshal(wrapped.Data, &resource); err != nil {
			return nil, fmt.Errorf("unmarshal resource data: %w; raw=%s", err, string(wrapped.Data))
		}
		return CropwiseResource(resource), nil
	}

	var resource map[string]any
	if err := json.Unmarshal(raw, &resource); err != nil {
		return nil, fmt.Errorf("unmarshal resource: %w; raw=%s", err, string(raw))
	}
	return CropwiseResource(resource), nil
}

func (c *CropwiseClient) fetchResources(ctx context.Context, endpoint string) ([]CropwiseResource, int64, int, error) {
	var res CropwiseListResponse
	if err := c.doJSON(ctx, http.MethodGet, endpoint, nil, &res); err != nil {
		return nil, 0, 0, err
	}
	items := make([]CropwiseResource, 0, len(res.Data))
	for _, raw := range res.Data {
		var m map[string]any
		if err := json.Unmarshal(raw, &m); err != nil {
			return nil, 0, 0, fmt.Errorf("unmarshal resource: %w; raw=%s", err, string(raw))
		}
		items = append(items, CropwiseResource(m))
	}
	obtained := res.Meta.Response.ObtainedRecords
	if obtained == 0 {
		obtained = len(items)
	}
	return items, res.Meta.Response.LastRecordID, obtained, nil
}

func (c *CropwiseClient) doJSON(ctx context.Context, method, endpoint string, requestBody any, out any) error {
	var body io.Reader
	if requestBody != nil {
		b, err := json.Marshal(requestBody)
		if err != nil {
			return err
		}
		body = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, body)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	if requestBody != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.token != "" && !strings.HasSuffix(endpoint, "/sign_in") {
		req.Header.Set("X-User-Api-Token", c.token)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("cropwise %s %s: HTTP %d: %s", method, endpoint, resp.StatusCode, trimForLog(string(b), 1000))
	}
	if out != nil {
		if err := json.Unmarshal(b, out); err != nil {
			return fmt.Errorf("cropwise decode response: %w; body=%s", err, trimForLog(string(b), 1000))
		}
	}
	return nil
}

func (c *CropwiseClient) url(path string, q url.Values) string {
	return c.urlV(c.apiVersion, path, q)
}

func (c *CropwiseClient) urlV(version, path string, q url.Values) string {
	u := fmt.Sprintf("%s/%s/%s", c.baseURL, strings.Trim(version, "/"), strings.TrimLeft(path, "/"))
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	return u
}

func sortReports(items []CropwiseReport) {
	sort.SliceStable(items, func(i, j int) bool {
		a := stringValue(items[i], "report_time", "created_by_user_at", "created_at", "updated_at")
		b := stringValue(items[j], "report_time", "created_by_user_at", "created_at", "updated_at")
		return a < b
	})
}

func filterByFieldIDs(items []CropwiseReport, fieldIDs map[int64]bool) []CropwiseReport {
	if len(fieldIDs) == 0 {
		return items
	}
	out := make([]CropwiseReport, 0, len(items))
	for _, r := range items {
		fieldID := int64Value(r, "field_id")
		if fieldIDs[fieldID] {
			out = append(out, r)
		}
	}
	return out
}
