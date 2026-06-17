package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

func TestEnrichReportsAttachesPointIssueAndMeasurementPhotos(t *testing.T) {
	t.Parallel()

	var seen []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.URL.Path+"?"+r.URL.RawQuery)
		q := r.URL.Query()
		switch {
		case r.URL.Path == "/api/v3/scout_report_measurement_types":
			writeCropwiseList(t, w, nil)
		case r.URL.Path == "/api/v3/scout_report_growth_stage_structures":
			writeCropwiseList(t, w, nil)
		case r.URL.Path == "/api/v3/field_scout_report_threat_mapping_items":
			writeCropwiseList(t, w, nil)
		case r.URL.Path == "/api/v3/scout_report_points" && q.Get("field_scout_report_id") == "51602":
			writeCropwiseList(t, w, []map[string]any{
				{"id": 101, "field_scout_report_id": 51602},
			})
		case r.URL.Path == "/api/v3/photos" && q.Get("photoable_type") == "ScoutReportPoint" && q.Get("photoable_id") == "101":
			writeCropwiseList(t, w, []map[string]any{
				photoResource(1, "https://storage.googleapis.com/cropio-uploads/system/uploads/companies/212/photo/photo/point/photo.jpg"),
			})
		case r.URL.Path == "/api/v3/scout_report_point_issues" && q.Get("scout_report_point_id") == "101":
			writeCropwiseList(t, w, []map[string]any{
				{"id": 201, "scout_report_point_id": 101},
			})
		case r.URL.Path == "/api/v3/photos" && q.Get("photoable_type") == "ScoutReportPointIssue" && q.Get("photoable_id") == "201":
			writeCropwiseList(t, w, []map[string]any{
				photoResource(2, "https://storage.googleapis.com/cropio-uploads/system/uploads/companies/212/photo/photo/issue/photo.jpg"),
			})
		case r.URL.Path == "/api/v3b/scout_report_point_measurements" && q.Get("scout_report_point_id") == "101":
			writeCropwiseList(t, w, []map[string]any{
				{"id": 301, "scout_report_point_id": 101},
			})
		case r.URL.Path == "/api/v3/photos" && q.Get("photoable_type") == "ScoutReportPointMeasurement" && q.Get("photoable_id") == "301":
			writeCropwiseList(t, w, []map[string]any{
				photoResource(3, "https://storage.googleapis.com/cropio-uploads/system/uploads/companies/212/photo/photo/measurement/photo.jpg"),
			})
		default:
			t.Fatalf("unexpected request: %s?%s", r.URL.Path, r.URL.RawQuery)
		}
	}))
	defer srv.Close()

	cw := &CropwiseClient{
		baseURL:         srv.URL + "/api",
		pointsRes:       "scout_report_points",
		pointsVer:       "v3",
		measurementsRes: "scout_report_point_measurements",
		measurementsVer: "v3b",
		measTypesRes:    "scout_report_measurement_types",
		measTypesVer:    "v3",
		growthStructRes: "scout_report_growth_stage_structures",
		growthStructVer: "v3",
		pointIssuesRes:  "scout_report_point_issues",
		pointIssuesVer:  "v3",
		photosRes:       "photos",
		photosVer:       "v3",
		threatMapRes:    "field_scout_report_threat_mapping_items",
		threatMapVer:    "v3",
		webBaseURL:      "https://operations.cropwise.com",
		httpClient:      srv.Client(),
	}
	svc := Service{
		cfg: Config{
			EnrichMeasurements: true,
			EnrichPointIssues:  true,
			MaxPhotosPerReport: 10,
			CropwiseWebBaseURL: "https://operations.cropwise.com",
		},
		cropwise: cw,
	}
	r := CropwiseReport{"id": "51602", "field_id": 1, "created_by_user_name": "Tester"}

	if err := svc.enrichReports(context.Background(), []CropwiseReport{r}, nil); err != nil {
		t.Fatalf("enrichReports returned error: %v", err)
	}

	got := ExtractImageURLsWithBase(r, 10, "https://operations.cropwise.com")
	want := []string{
		"https://storage.googleapis.com/cropio-uploads/system/uploads/companies/212/photo/photo/point/photo.jpg",
		"https://storage.googleapis.com/cropio-uploads/system/uploads/companies/212/photo/photo/measurement/photo.jpg",
		"https://storage.googleapis.com/cropio-uploads/system/uploads/companies/212/photo/photo/issue/photo.jpg",
	}
	if !sameStringSet(got, want) {
		t.Fatalf("unexpected image urls:\nwant: %#v\n got: %#v", want, got)
	}

	mustHaveRequest(t, seen, "/api/v3/photos", url.Values{"photoable_type": {"ScoutReportPoint"}, "photoable_id": {"101"}})
	mustHaveRequest(t, seen, "/api/v3/photos", url.Values{"photoable_type": {"ScoutReportPointIssue"}, "photoable_id": {"201"}})
	mustHaveRequest(t, seen, "/api/v3/photos", url.Values{"photoable_type": {"ScoutReportPointMeasurement"}, "photoable_id": {"301"}})
}

func sameStringSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	counts := map[string]int{}
	for _, s := range a {
		counts[s]++
	}
	for _, s := range b {
		counts[s]--
		if counts[s] < 0 {
			return false
		}
	}
	for _, count := range counts {
		if count != 0 {
			return false
		}
	}
	return true
}

func writeCropwiseList(t *testing.T, w http.ResponseWriter, items []map[string]any) {
	t.Helper()
	raw := make([]json.RawMessage, 0, len(items))
	var lastID int64
	for _, item := range items {
		b, err := json.Marshal(item)
		if err != nil {
			t.Fatal(err)
		}
		raw = append(raw, b)
		if id := int64Value(item, "id"); id > lastID {
			lastID = id
		}
	}
	body := map[string]any{
		"data": raw,
		"meta": map[string]any{
			"response": map[string]any{
				"limit":            1000,
				"obtained_records": len(raw),
				"last_record_id":   lastID,
			},
		},
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(body); err != nil {
		t.Fatal(err)
	}
}

func photoResource(id int64, rawURL string) map[string]any {
	return map[string]any{
		"id": id,
		"photo": map[string]any{
			"photo": map[string]any{
				"url": rawURL,
			},
		},
	}
}

func mustHaveRequest(t *testing.T, seen []string, path string, want url.Values) {
	t.Helper()
	for _, req := range seen {
		u, err := url.Parse(req)
		if err != nil {
			t.Fatal(err)
		}
		if u.Path != path {
			continue
		}
		q := u.Query()
		matches := true
		for key, vals := range want {
			if q.Get(key) != vals[0] {
				matches = false
				break
			}
		}
		if matches {
			return
		}
	}
	t.Fatalf("request not seen: %s?%s; seen=%v", path, want.Encode(), seen)
}
