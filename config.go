package main

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

var cropwiseTimeLocation = time.FixedZone("MSK", 3*3600)

type Config struct {
	CropwiseBaseURL         string
	CropwiseAPIVersion      string
	CropwiseReportsRes      string
	CropwiseFieldsRes       string
	CropwiseFieldsVer       string
	CropwiseFieldGroupsRes  string
	CropwiseFieldGroupsVer  string
	CropwiseHistRes         string
	CropwiseHistVer         string
	CropwiseCropsRes        string
	CropwiseCropsVer        string
	CropwiseHistValsRes     string
	CropwiseHistValsVer     string
	CropwisePointsRes       string
	CropwisePointsVer       string
	CropwiseMeasRes         string
	CropwiseMeasVer         string
	CropwiseMeasTypesRes    string
	CropwiseMeasTypesVer    string
	CropwiseUsersRes        string
	CropwiseUsersVer        string
	CropwiseGrowthStagesRes string
	CropwiseGrowthStagesVer string
	CropwiseGrowthStructRes string
	CropwiseGrowthStructVer string
	CropwiseThreatMapRes    string
	CropwiseThreatMapVer    string
	CropwisePointIssuesRes  string
	CropwisePointIssuesVer  string
	CropwisePhotosRes       string
	CropwisePhotosVer       string
	CropwiseAggregatedRes   string
	CropwiseAggregatedVer   string
	CropwiseWebBaseURL      string
	CropwiseToken           string
	CropwiseEmail           string
	CropwisePassword        string
	CropwiseFieldIDs        map[int64]bool
	CropwiseFromTime        time.Time
	CropwisePollInterval    time.Duration
	CropwiseLookback        time.Duration
	BackfillSendDelay       time.Duration

	MaxAPIURL             string
	MaxToken              string
	MaxChatID             int64
	MaxMessageFormat      string
	MaxDisableLinkPreview bool
	MaxUploadRetryCount   int
	MaxUploadRetryDelay   time.Duration

	StateFile          string
	StateDB            string
	DryRun             bool
	SendPhotos         bool
	MaxPhotosPerReport int
	EnrichNDVI         bool
	EnrichProduction   bool
	EnrichMeasurements bool
	EnrichUsers        bool
	EnrichGrowthStages bool
	EnrichPointIssues  bool
	EnrichAggregated   bool
}

func LoadDotEnv(path string) error {
	f, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		idx := strings.Index(line, "=")
		if idx < 0 {
			continue
		}
		key := strings.TrimSpace(line[:idx])
		val := strings.TrimSpace(line[idx+1:])
		val = strings.Trim(val, `"'`)
		if _, exists := os.LookupEnv(key); !exists {
			_ = os.Setenv(key, val)
		}
	}
	return scanner.Err()
}

func LoadConfig() (Config, error) {
	_ = LoadDotEnv(".env")

	fromTime, err := parseFlexibleTime(getenv("CROPWISE_FROM_TIME", "2026-01-01T00:00:00"))
	if err != nil {
		return Config{}, fmt.Errorf("bad CROPWISE_FROM_TIME: %w", err)
	}

	chatID, err := strconv.ParseInt(getenv("MAX_CHAT_ID", "0"), 10, 64)
	if err != nil {
		return Config{}, fmt.Errorf("bad MAX_CHAT_ID: %w", err)
	}

	fieldIDs, err := parseFieldIDs(getenv("CROPWISE_FIELD_IDS", ""))
	if err != nil {
		return Config{}, err
	}

	pollSeconds := getenvInt("CROPWISE_POLL_INTERVAL_SECONDS", 300)
	lookbackSeconds := getenvInt("CROPWISE_LOOKBACK_SECONDS", 60)
	backfillDelaySeconds := getenvInt("BACKFILL_SEND_DELAY_SECONDS", 0)

	cfg := Config{
		CropwiseBaseURL:         strings.TrimRight(getenv("CROPWISE_BASE_URL", "https://operations.cropwise.com/api"), "/"),
		CropwiseAPIVersion:      strings.Trim(getenv("CROPWISE_API_VERSION", "v3b"), "/"),
		CropwiseReportsRes:      strings.Trim(getenv("CROPWISE_REPORTS_RESOURCE", "field_scout_reports"), "/"),
		CropwiseFieldsRes:       strings.Trim(getenv("CROPWISE_FIELDS_RESOURCE", "fields"), "/"),
		CropwiseFieldsVer:       strings.Trim(getenv("CROPWISE_FIELDS_API_VERSION", "v3a"), "/"),
		CropwiseFieldGroupsRes:  strings.Trim(getenv("CROPWISE_FIELD_GROUPS_RESOURCE", "field_groups"), "/"),
		CropwiseFieldGroupsVer:  strings.Trim(getenv("CROPWISE_FIELD_GROUPS_API_VERSION", "v3"), "/"),
		CropwiseHistRes:         strings.Trim(getenv("CROPWISE_HISTORY_ITEMS_RESOURCE", "history_items"), "/"),
		CropwiseHistVer:         strings.Trim(getenv("CROPWISE_HISTORY_ITEMS_API_VERSION", "v3"), "/"),
		CropwiseCropsRes:        strings.Trim(getenv("CROPWISE_CROPS_RESOURCE", "crops"), "/"),
		CropwiseCropsVer:        strings.Trim(getenv("CROPWISE_CROPS_API_VERSION", "v3"), "/"),
		CropwiseHistValsRes:     strings.Trim(getenv("CROPWISE_HISTORICAL_VALUES_RESOURCE", "historical_values"), "/"),
		CropwiseHistValsVer:     strings.Trim(getenv("CROPWISE_HISTORICAL_VALUES_API_VERSION", "v3b"), "/"),
		CropwisePointsRes:       strings.Trim(getenv("CROPWISE_POINTS_RESOURCE", "scout_report_points"), "/"),
		CropwisePointsVer:       strings.Trim(getenv("CROPWISE_POINTS_API_VERSION", "v3"), "/"),
		CropwiseMeasRes:         strings.Trim(getenv("CROPWISE_MEASUREMENTS_RESOURCE", "scout_report_point_measurements"), "/"),
		CropwiseMeasVer:         strings.Trim(getenv("CROPWISE_MEASUREMENTS_API_VERSION", "v3b"), "/"),
		CropwiseMeasTypesRes:    strings.Trim(getenv("CROPWISE_MEASUREMENT_TYPES_RESOURCE", "scout_report_measurement_types"), "/"),
		CropwiseMeasTypesVer:    strings.Trim(getenv("CROPWISE_MEASUREMENT_TYPES_API_VERSION", "v3"), "/"),
		CropwiseUsersRes:        strings.Trim(getenv("CROPWISE_USERS_RESOURCE", "users"), "/"),
		CropwiseUsersVer:        strings.Trim(getenv("CROPWISE_USERS_API_VERSION", "v3a"), "/"),
		CropwiseGrowthStagesRes: strings.Trim(getenv("CROPWISE_GROWTH_STAGES_RESOURCE", "growth_stages"), "/"),
		CropwiseGrowthStagesVer: strings.Trim(getenv("CROPWISE_GROWTH_STAGES_API_VERSION", "v3"), "/"),
		CropwiseGrowthStructRes: strings.Trim(getenv("CROPWISE_POINT_GROWTH_STRUCTURES_RESOURCE", "scout_report_point_growth_stage_structures"), "/"),
		CropwiseGrowthStructVer: strings.Trim(getenv("CROPWISE_POINT_GROWTH_STRUCTURES_API_VERSION", "v3"), "/"),
		CropwiseThreatMapRes:    strings.Trim(getenv("CROPWISE_THREAT_MAPPING_ITEMS_RESOURCE", "field_scout_report_threat_mapping_items"), "/"),
		CropwiseThreatMapVer:    strings.Trim(getenv("CROPWISE_THREAT_MAPPING_ITEMS_API_VERSION", "v3"), "/"),
		CropwisePointIssuesRes:  strings.Trim(getenv("CROPWISE_POINT_ISSUES_RESOURCE", "scout_report_point_issues"), "/"),
		CropwisePointIssuesVer:  strings.Trim(getenv("CROPWISE_POINT_ISSUES_API_VERSION", "v3"), "/"),
		CropwisePhotosRes:       strings.Trim(getenv("CROPWISE_PHOTOS_RESOURCE", "photos"), "/"),
		CropwisePhotosVer:       strings.Trim(getenv("CROPWISE_PHOTOS_API_VERSION", "v3"), "/"),
		CropwiseAggregatedRes:   strings.Trim(getenv("CROPWISE_AGGREGATED_REPORTS_RESOURCE", "field_scout_reports_aggregated"), "/"),
		CropwiseAggregatedVer:   strings.Trim(getenv("CROPWISE_AGGREGATED_REPORTS_API_VERSION", "v3"), "/"),
		CropwiseWebBaseURL:      strings.TrimRight(getenv("CROPWISE_WEB_BASE_URL", "https://operations.cropwise.com"), "/"),
		CropwiseToken:           getenv("CROPWISE_USER_API_TOKEN", ""),
		CropwiseEmail:           getenv("CROPWISE_EMAIL", ""),
		CropwisePassword:        getenv("CROPWISE_PASSWORD", ""),
		CropwiseFieldIDs:        fieldIDs,
		CropwiseFromTime:        fromTime,
		CropwisePollInterval:    time.Duration(pollSeconds) * time.Second,
		CropwiseLookback:        time.Duration(lookbackSeconds) * time.Second,
		BackfillSendDelay:       time.Duration(backfillDelaySeconds) * time.Second,
		MaxAPIURL:               strings.TrimRight(getenv("MAX_API_URL", "https://platform-api.max.ru"), "/"),
		MaxToken:                getenv("MAX_TOKEN", ""),
		MaxChatID:               chatID,
		MaxMessageFormat:        strings.TrimSpace(getenv("MAX_MESSAGE_FORMAT", "markdown")),
		MaxDisableLinkPreview:   getenvBool("MAX_DISABLE_LINK_PREVIEW", true),
		MaxUploadRetryCount:     getenvInt("MAX_UPLOAD_RETRY_COUNT", 3),
		MaxUploadRetryDelay:     time.Duration(getenvInt("MAX_UPLOAD_RETRY_DELAY_SECONDS", 2)) * time.Second,
		StateFile:               getenv("STATE_FILE", "state.json"),
		StateDB:                 getenv("STATE_DB", ""),
		DryRun:                  getenvBool("DRY_RUN", true),
		SendPhotos:              getenvBool("SEND_PHOTOS", false),
		MaxPhotosPerReport:      getenvInt("MAX_PHOTOS_PER_REPORT", 3),
		EnrichNDVI:              getenvBool("ENRICH_NDVI", true),
		EnrichProduction:        getenvBool("ENRICH_PRODUCTION_CYCLE", true),
		EnrichMeasurements:      getenvBool("ENRICH_MEASUREMENTS", true),
		EnrichUsers:             getenvBool("ENRICH_USERS", true),
		EnrichGrowthStages:      getenvBool("ENRICH_GROWTH_STAGES", true),
		EnrichPointIssues:       getenvBool("ENRICH_POINT_ISSUES", true),
		EnrichAggregated:        getenvBool("ENRICH_AGGREGATED_REPORT", true),
	}

	if cfg.CropwiseToken == "" && (cfg.CropwiseEmail == "" || cfg.CropwisePassword == "") {
		return Config{}, errors.New("fill CROPWISE_USER_API_TOKEN or CROPWISE_EMAIL/CROPWISE_PASSWORD")
	}
	if cfg.MaxToken == "" && !cfg.DryRun {
		return Config{}, errors.New("fill MAX_TOKEN or set DRY_RUN=true")
	}
	if cfg.MaxChatID == 0 && !cfg.DryRun {
		return Config{}, errors.New("fill MAX_CHAT_ID or set DRY_RUN=true")
	}
	return cfg, nil
}

func getenv(key, def string) string {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return def
	}
	return v
}

func getenvBool(key string, def bool) bool {
	v := strings.ToLower(strings.TrimSpace(os.Getenv(key)))
	if v == "" {
		return def
	}
	return v == "1" || v == "true" || v == "yes" || v == "y"
}

func getenvInt(key string, def int) int {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return n
}

func parseFieldIDs(raw string) (map[int64]bool, error) {
	result := make(map[int64]bool)
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return result, nil
	}
	parts := strings.Split(raw, ",")
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		id, err := strconv.ParseInt(p, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("bad field id %q: %w", p, err)
		}
		result[id] = true
	}
	return result, nil
}

func parseFlexibleTime(s string) (time.Time, error) {
	s = strings.TrimSpace(s)
	layouts := []string{
		time.RFC3339,
		"2006-01-02T15:04:05",
		"2006-01-02 15:04:05",
		"2006-01-02",
	}
	var lastErr error
	for _, layout := range layouts {
		var (
			t   time.Time
			err error
		)
		switch layout {
		case "2006-01-02T15:04:05", "2006-01-02 15:04:05", "2006-01-02":
			t, err = time.ParseInLocation(layout, s, cropwiseTimeLocation)
		default:
			t, err = time.Parse(layout, s)
		}
		if err == nil {
			return t, nil
		}
		lastErr = err
	}
	return time.Time{}, lastErr
}

func cropwiseTime(t time.Time) string {
	return t.In(cropwiseTimeLocation).Format(time.RFC3339)
}
