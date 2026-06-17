package main

import (
	"context"
	"fmt"
	"html"
	"io"
	"log"
	"net/http"
	"regexp"
	"strconv"
	"strings"
)

var gcsCropioImageRe = regexp.MustCompile(`https?:\\/\\/storage\\.googleapis\\.com\\/cropio-uploads\\/[^\"'<>\\s)]+?\\.(?:jpg|jpeg|png|webp)(?:\\?[^\"'<>\\s)]*)?`)
var gcsCropioImageRe2 = regexp.MustCompile(`https?://storage\.googleapis\.com/cropio-uploads/[^\"'<>\s)]+?\.(?:jpg|jpeg|png|webp)(?:\?[^\"'<>\s)]*)?`)
var reportPageNDVIRe = regexp.MustCompile(`(?is)\bNDVI\b.{0,120}?([01](?:[.,]\d{1,4})?)`)

// reportPhotoURLs combines URLs already present in the enriched report JSON
// with optional manual/web fallback URLs.
func (s *Service) reportPhotoURLs(ctx context.Context, r CropwiseReport) []string {
	external := s.externalPhotoURLs(ctx, r)
	if len(external) > 0 {
		addReportPhotoURLs(r, external)
	}
	return ExtractImageURLsWithBase(r, s.cfg.MaxPhotosPerReport, s.cfg.CropwiseWebBaseURL)
}

// attachExternalPhotoURLs adds photo URLs found by the primary external search.
func (s *Service) attachExternalPhotoURLs(ctx context.Context, r CropwiseReport) {
	addReportPhotoURLs(r, s.externalPhotoURLs(ctx, r))
}

// externalPhotoURLs finds photo URLs outside the report JSON.
// It supports two sources:
//  1. CROPWISE_REPORT_PHOTO_URLS manual mapping.
//  2. Scraping storage.googleapis.com/cropio-uploads links from the report web page.
func (s *Service) externalPhotoURLs(ctx context.Context, r CropwiseReport) []string {
	if r == nil {
		return nil
	}
	idStr := reportID(r)
	if idStr == "" {
		return nil
	}
	reportIDNum, _ := strconv.ParseInt(idStr, 10, 64)
	if reportIDNum == 0 {
		return nil
	}

	urls := make([]string, 0)
	if manual := ManualReportPhotoURLs(idStr); len(manual) > 0 {
		urls = append(urls, manual...)
		log.Printf("report_id=%s: added %d manual photo url(s) from CROPWISE_REPORT_PHOTO_URLS", idStr, len(manual))
	}

	if len(urls) == 0 && s.cropwise != nil && getenvBool("CROPWISE_SCRAPE_WEB_PHOTOS", true) {
		fieldID := int64Value(r, "field_id")
		if fieldID != 0 {
			webURLs, err := s.cropwise.FetchReportPageImageURLs(ctx, fieldID, reportIDNum, stringValue(r, "field_web_slug"), s.cfg.MaxPhotosPerReport)
			if err != nil {
				log.Printf("web photo fallback: report_id=%s field_id=%d unavailable: %v", idStr, fieldID, err)
			} else if len(webURLs) > 0 {
				urls = append(urls, webURLs...)
				log.Printf("report_id=%s: scraped %d photo url(s) from Cropwise report page", idStr, len(webURLs))
			}
		}
	}

	if len(urls) == 0 {
		return nil
	}
	return uniqueStrings(urls...)
}

func addReportPhotoURLs(r CropwiseReport, urls []string) {
	if r == nil || len(urls) == 0 {
		return
	}
	seen := map[string]bool{}
	// Preserve already attached manual/external URLs too.
	if old, ok := r["external_photo_urls"].([]any); ok {
		for _, v := range old {
			if u := strings.TrimSpace(anyToString(v)); u != "" {
				seen[u] = true
			}
		}
	}
	arr := make([]any, 0, len(urls))
	if old, ok := r["external_photo_urls"].([]any); ok {
		arr = append(arr, old...)
	}
	for _, u := range urls {
		u = strings.TrimSpace(u)
		if u == "" || seen[u] {
			continue
		}
		seen[u] = true
		arr = append(arr, u)
	}
	if len(arr) > 0 {
		r["external_photo_urls"] = arr
	}
}

// CROPWISE_REPORT_PHOTO_URLS format:
// 51575=https://.../1.jpg,https://.../2.jpg;51579=https://.../a.jpg
func ManualReportPhotoURLs(reportID string) []string {
	raw := strings.TrimSpace(getenv("CROPWISE_REPORT_PHOTO_URLS", ""))
	if raw == "" || reportID == "" {
		return nil
	}
	out := make([]string, 0)
	blocks := strings.Split(raw, ";")
	for _, block := range blocks {
		block = strings.TrimSpace(block)
		if block == "" {
			continue
		}
		parts := strings.SplitN(block, "=", 2)
		if len(parts) != 2 {
			continue
		}
		if strings.TrimSpace(parts[0]) != reportID {
			continue
		}
		for _, u := range splitURLList(parts[1]) {
			if strings.TrimSpace(u) != "" {
				out = append(out, strings.TrimSpace(u))
			}
		}
	}
	return uniqueStrings(out...)
}

func splitURLList(s string) []string {
	// Prefer comma, but also accept whitespace/newline separated URLs.
	s = strings.ReplaceAll(s, "\r", " ")
	s = strings.ReplaceAll(s, "\n", " ")
	if strings.Contains(s, ",") {
		return strings.Split(s, ",")
	}
	return strings.Fields(s)
}

func (c *CropwiseClient) FetchReportPageImageURLs(ctx context.Context, fieldID int64, reportID int64, fieldWebSlug string, limit int) ([]string, error) {
	if fieldID == 0 || reportID == 0 {
		return nil, nil
	}
	if limit <= 0 {
		limit = 10
	}
	if manualURL := ManualReportWebURL(strconv.FormatInt(reportID, 10)); manualURL != "" {
		return c.FetchImageURLsFromWebPage(ctx, manualURL, limit)
	}
	fieldPath := cropwiseReportFieldPath(fieldID, fieldWebSlug)
	endpoint := fmt.Sprintf("%s/fields/%s/scout_reports/%d", strings.TrimRight(c.webBaseURL, "/"), fieldPath, reportID)
	return c.FetchImageURLsFromWebPage(ctx, endpoint, limit)
}

func (c *CropwiseClient) FetchReportPageNDVI(ctx context.Context, fieldID int64, reportID int64, fieldWebSlug string) (string, error) {
	if fieldID == 0 || reportID == 0 {
		return "", nil
	}
	endpoint := ManualReportWebURL(strconv.FormatInt(reportID, 10))
	if endpoint == "" {
		fieldPath := cropwiseReportFieldPath(fieldID, fieldWebSlug)
		endpoint = fmt.Sprintf("%s/fields/%s/scout_reports/%d", strings.TrimRight(c.webBaseURL, "/"), fieldPath, reportID)
	}
	body, err := c.FetchReportPageBody(ctx, endpoint)
	if err != nil {
		return "", err
	}
	return extractReportPageNDVI(body), nil
}

func ManualReportWebURL(reportID string) string {
	raw := strings.TrimSpace(getenv("CROPWISE_REPORT_WEB_URLS", ""))
	if raw == "" || reportID == "" {
		return ""
	}
	for _, block := range strings.Split(raw, ";") {
		parts := strings.SplitN(strings.TrimSpace(block), "=", 2)
		if len(parts) == 2 && strings.TrimSpace(parts[0]) == reportID {
			return strings.TrimSpace(parts[1])
		}
	}
	return ""
}

func (c *CropwiseClient) FetchImageURLsFromWebPage(ctx context.Context, endpoint string, limit int) ([]string, error) {
	body, err := c.FetchReportPageBody(ctx, endpoint)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	out := make([]string, 0)
	for _, re := range []*regexp.Regexp{gcsCropioImageRe2, gcsCropioImageRe} {
		for _, m := range re.FindAllString(body, -1) {
			u := html.UnescapeString(strings.ReplaceAll(m, `\\/`, `/`))
			u = strings.Trim(u, `"' )]}`)
			if u == "" || seen[u] {
				continue
			}
			seen[u] = true
			out = append(out, u)
			if len(out) >= limit {
				return out, nil
			}
		}
	}
	return out, nil
}

func (c *CropwiseClient) FetchReportPageBody(ctx context.Context, endpoint string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	req.Header.Set("User-Agent", "Mozilla/5.0 cropwise-max-bot/1.0")
	if c.token != "" {
		req.Header.Set("X-User-Api-Token", c.token)
	}
	if cookie := strings.TrimSpace(getenv("CROPWISE_WEB_COOKIE", "")); cookie != "" {
		req.Header.Set("Cookie", cookie)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("GET report page %s: HTTP %d", endpoint, resp.StatusCode)
	}
	body := html.UnescapeString(string(b))
	return strings.ReplaceAll(body, `\\/`, `/`), nil
}

func extractReportPageNDVI(body string) string {
	body = html.UnescapeString(body)
	body = strings.ReplaceAll(body, `\n`, "\n")
	body = strings.ReplaceAll(body, `\u003c`, "<")
	body = strings.ReplaceAll(body, `\u003e`, ">")
	body = regexp.MustCompile(`(?is)<[^>]+>`).ReplaceAllString(body, " ")
	body = regexp.MustCompile(`\s+`).ReplaceAllString(body, " ")
	match := reportPageNDVIRe.FindStringSubmatch(body)
	if len(match) < 2 {
		return ""
	}
	return strings.ReplaceAll(match[1], ",", ".")
}
