package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"
)

func main() {
	mode := flag.String("mode", "once", "mode: backfill | once | poll | test-max | debug-report | send-report")
	reportIDFlag := flag.Int64("report-id", 0, "Cropwise scout report id for debug-report or send-report")
	fieldIDFlag := flag.Int64("field-id", 0, "Cropwise field id for debug-report or send-report")
	flag.Parse()

	cfg, err := LoadConfig()
	if err != nil {
		log.Fatal(err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	cw := NewCropwiseClient(cfg)
	max := NewMaxClient(cfg)

	if *mode == "test-max" {
		if err := max.Test(ctx); err != nil {
			log.Fatal(err)
		}
		return
	}

	var state State
	if cfg.StateDB != "" {
		state, err = LoadStateDB(cfg.StateDB)
	} else {
		state, err = LoadState(cfg.StateFile)
	}
	if err != nil {
		log.Fatal(err)
	}

	if err := cw.EnsureAuth(ctx); err != nil {
		log.Fatal(err)
	}

	svc := Service{cfg: cfg, cropwise: cw, max: max, state: &state}

	switch *mode {
	case "backfill":
		err = svc.Backfill(ctx)
	case "once":
		err = svc.SyncOnce(ctx)
	case "poll":
		err = svc.Poll(ctx)
	case "debug-report":
		err = svc.DebugReport(ctx, *reportIDFlag, *fieldIDFlag)
	case "send-report":
		err = svc.SendReport(ctx, *reportIDFlag, *fieldIDFlag)
	default:
		err = fmt.Errorf("unknown mode %q", *mode)
	}
	if err != nil {
		log.Fatal(err)
	}
}

type Service struct {
	cfg      Config
	cropwise *CropwiseClient
	max      *MaxClient
	state    *State
}

func (s *Service) Backfill(ctx context.Context) error {
	fieldIDs := sortedFieldIDs(s.cfg.CropwiseFieldIDs)
	fieldInfos := s.loadFieldInfo(ctx)
	backfillSentCount := 0

	processItems := func(items []CropwiseReport, fieldID int64) error {
		for _, r := range items {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if s.isAlreadySent(r) {
				fmt.Printf("skip already sent report_id=%s\n", reportID(r))
				continue
			}
			// Обогащаем только отчет, который реально будем отправлять.
			// Так backfill не делает сотни лишних запросов к points/measurements/NDVI по уже отправленным отчетам.
			one := []CropwiseReport{r}
			if err := s.enrichReports(ctx, one, fieldInfos); err != nil {
				if ctx.Err() != nil {
					return ctx.Err()
				}
				if fieldID != 0 {
					log.Printf("warning: enrichment failed field_id=%d report_id=%s: %v", fieldID, reportID(r), err)
				} else {
					log.Printf("warning: enrichment failed report_id=%s: %v", reportID(r), err)
				}
			}
			if err := s.sendBackfillReportIfNeeded(ctx, r, &backfillSentCount); err != nil {
				return err
			}
		}
		return nil
	}

	if len(fieldIDs) == 0 {
		fmt.Printf("Backfill from %s for all available fields\n", cropwiseTime(s.cfg.CropwiseFromTime))
		items, err := s.cropwise.FetchOldReports(ctx, 0, s.cfg.CropwiseFromTime)
		if err != nil {
			return fmt.Errorf("fetch old reports for all fields: %w", err)
		}
		fmt.Printf("fetched %d reports\n", len(items))
		if err := processItems(items, 0); err != nil {
			return err
		}
	} else {
		fmt.Printf("Backfill from %s for fields %v\n", cropwiseTime(s.cfg.CropwiseFromTime), fieldIDs)
		for _, fieldID := range fieldIDs {
			items, err := s.cropwise.FetchOldReports(ctx, fieldID, s.cfg.CropwiseFromTime)
			if err != nil {
				return fmt.Errorf("fetch old reports field_id=%d: %w", fieldID, err)
			}
			fmt.Printf("field_id=%d: fetched %d reports\n", fieldID, len(items))
			if err := processItems(items, fieldID); err != nil {
				return err
			}
		}
	}

	s.state.SetLastSync(time.Now().UTC())
	return s.state.Save(s.cfg.StateFile)
}

func (s *Service) SyncOnce(ctx context.Context) error {
	now := time.Now().UTC()
	from := s.state.LastSyncOr(now.Add(-s.cfg.CropwisePollInterval))
	from = from.Add(-s.cfg.CropwiseLookback)
	fmt.Printf("Sync changes from %s to %s\n", cropwiseTime(from), cropwiseTime(now))

	items, err := s.cropwise.FetchChangedReports(ctx, from, now)
	if err != nil {
		return err
	}
	items = filterByFieldIDs(items, s.cfg.CropwiseFieldIDs)
	fmt.Printf("changed reports after field filter: %d\n", len(items))

	fieldInfos := map[int64]FieldInfo{}
	loadedFieldInfos := false
	for _, r := range items {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if s.isAlreadySent(r) {
			fmt.Printf("skip already sent report_id=%s\n", reportID(r))
			continue
		}
		if !loadedFieldInfos {
			fieldInfos = s.loadFieldInfo(ctx)
			loadedFieldInfos = true
		}
		one := []CropwiseReport{r}
		if err := s.enrichReports(ctx, one, fieldInfos); err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			log.Printf("warning: enrichment failed report_id=%s: %v", reportID(r), err)
		}
		if _, err := s.sendReportIfNeeded(ctx, r); err != nil {
			return err
		}
	}
	s.state.SetLastSync(now)
	return s.state.Save(s.cfg.StateFile)
}

func (s *Service) Poll(ctx context.Context) error {
	if err := s.SyncOnce(ctx); err != nil {
		return err
	}
	ticker := time.NewTicker(s.cfg.CropwisePollInterval)
	defer ticker.Stop()
	fmt.Printf("Polling every %s\n", s.cfg.CropwisePollInterval)
	for {
		select {
		case <-ctx.Done():
			return s.state.Save(s.cfg.StateFile)
		case <-ticker.C:
			if err := s.SyncOnce(ctx); err != nil {
				log.Printf("sync error: %v", err)
			}
		}
	}
}

func (s *Service) loadFieldInfo(ctx context.Context) map[int64]FieldInfo {
	fieldGroupNames := map[int64]string{}
	groups, err := s.cropwise.FetchFieldGroups(ctx)
	if err != nil {
		log.Printf("warning: cannot fetch field groups, field group names will be skipped: %v", err)
	} else {
		fieldGroupNames = BuildFieldGroupNameMap(groups)
		fmt.Printf("loaded %d field groups for group name mapping\n", len(fieldGroupNames))
	}

	fields, err := s.cropwise.FetchFields(ctx)
	if err != nil {
		log.Printf("warning: cannot fetch fields for names, will use field_id only: %v", err)
		return map[int64]FieldInfo{}
	}
	fieldInfos := BuildFieldInfoMap(fields, fieldGroupNames)
	fmt.Printf("loaded %d fields for field info mapping\n", len(fieldInfos))
	return fieldInfos
}

func (s *Service) enrichReports(ctx context.Context, items []CropwiseReport, fieldInfos map[int64]FieldInfo) error {
	EnrichReportsWithFieldInfo(items, fieldInfos)

	cropNames := map[int64]string{}
	if s.cfg.EnrichProduction {
		crops, err := s.cropwise.FetchCrops(ctx)
		if err != nil {
			log.Printf("warning: cannot fetch crops for production cycle names: %v", err)
		} else {
			cropNames = BuildCropNameMap(crops)
			fmt.Printf("loaded %d crops for production cycle mapping\n", len(cropNames))
		}
	}

	measurementTypeNames := map[int64]string{}
	if s.cfg.EnrichMeasurements {
		types, err := s.cropwise.FetchMeasurementTypes(ctx)
		if err != nil {
			log.Printf("warning: cannot fetch measurement types: %v", err)
		} else {
			measurementTypeNames = BuildMeasurementTypeNameMap(types)
			fmt.Printf("loaded %d measurement types\n", len(measurementTypeNames))
		}
	}

	userNames := map[int64]string{}
	if s.cfg.EnrichUsers {
		users, err := s.cropwise.FetchUsers(ctx)
		if err != nil {
			log.Printf("warning: cannot fetch users for creator names: %v", err)
		} else {
			userNames = BuildUserNameMap(users)
			fmt.Printf("loaded %d users for creator mapping\n", len(userNames))
			EnrichReportsWithUserNames(items, userNames)
		}
	}

	growthStageNames := map[int64]string{}
	if s.cfg.EnrichGrowthStages {
		stages, err := s.cropwise.FetchGrowthStages(ctx)
		if err != nil {
			log.Printf("warning: cannot fetch growth stages: %v", err)
		} else {
			growthStageNames = BuildGrowthStageNameMap(stages)
			fmt.Printf("loaded %d growth stages\n", len(growthStageNames))
			EnrichReportsWithGrowthStages(items, growthStageNames)
		}
	}

	historyCache := map[string][]CropwiseResource{}
	for _, r := range items {
		fieldID := int64Value(r, "field_id")
		season := stringValue(r, "season")
		reportIDNum, _ := strconv.ParseInt(reportID(r), 10, 64)
		directFetched := false

		if s.cfg.EnrichAggregated {
			if reportIDNum != 0 {
				agg, err := s.cropwise.FetchAggregatedReport(ctx, reportIDNum)
				if err != nil {
					log.Printf("warning: cannot fetch aggregated report report_id=%s: %v", reportID(r), err)
				} else if len(agg) > 0 {
					MergeReportData(r, agg)
					if stringValue(r, "report_ndvi") == "" {
						if ndvi := stringValue(agg, "ndvi", "ndvi_value"); ndvi != "" {
							r["report_ndvi"] = ndvi
						}
					}
				}
			}
		}

		if reportIDNum != 0 && shouldFetchDirectReport(r, s.cfg) {
			direct, err := s.cropwise.FetchReportByID(ctx, reportIDNum)
			directFetched = true
			if err != nil {
				if ctx.Err() != nil {
					return ctx.Err()
				}
				log.Printf("warning: cannot fetch report by id report_id=%s: %v", reportID(r), err)
			} else if len(direct) > 0 {
				MergeReportData(r, CropwiseResource(direct))
				if stringValue(r, "field_ndvi") == "" {
					if ndvi := stringValue(direct, "field_ndvi", "ndvi", "ndvi_value"); ndvi != "" {
						r["field_ndvi"] = ndvi
					}
				}
			}
		}
		if s.cfg.EnrichGrowthStages {
			stageID := int64Value(r, "growth_stage_id")
			if err := s.ensureGrowthStageName(ctx, growthStageNames, stageID); err != nil {
				if ctx.Err() != nil {
					return ctx.Err()
				}
				log.Printf("warning: cannot fetch growth stage report_id=%s growth_stage_id=%d: %v", reportID(r), stageID, err)
			}
			EnrichReportsWithGrowthStages([]CropwiseReport{r}, growthStageNames)
		}
		if s.cfg.EnrichUsers && creatorName(r) == "" {
			if err := s.enrichReportCreatorByID(ctx, r, userNames); err != nil {
				if ctx.Err() != nil {
					return ctx.Err()
				}
				log.Printf("warning: cannot fetch creator user report_id=%s: %v", reportID(r), err)
			}
		}

		if s.cfg.EnrichProduction && fieldID != 0 {
			key := fmt.Sprintf("%d:%s", fieldID, season)
			historyItems, ok := historyCache[key]
			if !ok {
				items, err := s.cropwise.FetchHistoryItems(ctx, fieldID, season)
				if err != nil {
					log.Printf("warning: cannot fetch history_items field_id=%d season=%s: %v", fieldID, season, err)
					items = nil
				}
				historyCache[key] = items
				historyItems = items
			}
			EnrichReportProductionCycle(r, historyItems, cropNames)
		}

		if s.cfg.EnrichNDVI && fieldID != 0 {
			if reportIDNum != 0 && !directFetched && formatNDVI(numberStringAny(r, "report_ndvi", "ndvi", "ndvi_value", "field_ndvi")) == "" {
				direct, err := s.cropwise.FetchReportByID(ctx, reportIDNum)
				if err != nil {
					if ctx.Err() != nil {
						return ctx.Err()
					}
					log.Printf("warning: cannot fetch report by id report_id=%s: %v", reportID(r), err)
				} else if len(direct) > 0 {
					MergeReportData(r, CropwiseResource(direct))
					if ndvi := stringValue(direct, "field_ndvi", "ndvi", "ndvi_value"); ndvi != "" {
						r["field_ndvi"] = ndvi
					}
				}
			}

			at := reportTimeForEnrichment(r)
			if !at.IsZero() && formatNDVI(numberStringAny(r, "field_ndvi", "report_ndvi", "ndvi", "ndvi_value")) == "" {
				ndvi, err := s.cropwise.FetchNDVI(ctx, fieldID, at)
				if err != nil {
					if ctx.Err() != nil {
						return ctx.Err()
					}
					log.Printf("warning: cannot fetch field NDVI field_id=%d report_id=%s: %v", fieldID, reportID(r), err)
				} else if ndvi != "" {
					// Не перезаписываем r["ndvi"], потому что это NDVI именно из отчета Cropwise.
					// Исторический/полевой NDVI храним отдельно.
					r["field_ndvi"] = ndvi
				}
			}
		}

		if reportIDNum != 0 {
			threatItems, err := s.cropwise.FetchFieldScoutReportThreatMappingItems(ctx, reportIDNum)
			if err != nil {
				log.Printf("warning: cannot fetch threat mapping items report_id=%s: %v", reportID(r), err)
			} else if len(threatItems) > 0 {
				arr := make([]any, 0, len(threatItems))
				for _, item := range threatItems {
					arr = append(arr, map[string]any(item))
				}
				r["field_scout_report_threat_mapping_items"] = arr
			}
		}

		if s.cfg.EnrichMeasurements {
			if reportIDNum != 0 {
				points, err := s.cropwise.FetchScoutReportPoints(ctx, reportIDNum)
				if err != nil {
					log.Printf("warning: cannot fetch scout_report_points report_id=%s: %v", reportID(r), err)
					continue
				}
				allMeasurements := make([]CropwiseResource, 0)
				allIssues := make([]CropwiseResource, 0)
				for _, p := range points {
					pointID := int64Value(p, "id")
					if pointID == 0 {
						continue
					}
					if s.cfg.EnrichGrowthStages {
						stageID := int64Value(p, "growth_stage_id")
						if err := s.ensureGrowthStageName(ctx, growthStageNames, stageID); err != nil {
							if ctx.Err() != nil {
								return ctx.Err()
							}
							log.Printf("warning: cannot fetch point growth stage point_id=%d report_id=%s growth_stage_id=%d: %v", pointID, reportID(r), stageID, err)
						}
					}
					s.attachPhotosForResource(ctx, p, "ScoutReportPoint", pointID, reportID(r), "point")
					// На некоторых отчетах фото прикреплены не к отчету и не к точке, а к структурам стадии роста.
					if growthStructs, err := s.cropwise.FetchGrowthStageStructuresForPoint(ctx, pointID); err == nil && len(growthStructs) > 0 {
						gsArr := make([]any, 0, len(growthStructs))
						for _, gs := range growthStructs {
							gsArr = append(gsArr, map[string]any(gs))
						}
						p["growth_stage_structures"] = gsArr
					} else if err != nil {
						log.Printf("warning: cannot fetch growth stage structures point_id=%d report_id=%s: %v", pointID, reportID(r), err)
					}

					measurements, err := s.cropwise.FetchMeasurementsForPoint(ctx, pointID)
					if err != nil {
						log.Printf("warning: cannot fetch measurements point_id=%d report_id=%s: %v", pointID, reportID(r), err)
					} else {
						for _, m := range measurements {
							s.attachPhotosForResource(ctx, m, "ScoutReportPointMeasurement", int64Value(m, "id"), reportID(r), "measurement")
						}
						allMeasurements = append(allMeasurements, measurements...)
					}
					if s.cfg.EnrichPointIssues {
						issues, err := s.cropwise.FetchPointIssuesForPoint(ctx, pointID)
						if err != nil {
							log.Printf("warning: cannot fetch point issues point_id=%d report_id=%s: %v", pointID, reportID(r), err)
						} else {
							for _, issue := range issues {
								s.attachPhotosForResource(ctx, issue, "ScoutReportPointIssue", int64Value(issue, "id"), reportID(r), "issue")
							}
							allIssues = append(allIssues, issues...)
						}
					}
				}
				EnrichReportWithScoutData(r, points, allMeasurements, allIssues, growthStageNames, measurementTypeNames)
			}
		}
	}
	return nil
}

func (s *Service) attachPhotosForResource(ctx context.Context, dst CropwiseResource, photoableType string, photoableID int64, reportID string, label string) {
	if s.cropwise == nil || s.cropwise.photosRes == "" || photoableID == 0 {
		return
	}
	photos, err := s.cropwise.FetchPhotosForPhotoableInVersion(ctx, "v3", photoableType, photoableID)
	if err != nil {
		log.Printf("warning: cannot fetch %s photos %s_id=%d report_id=%s: %v", label, label, photoableID, reportID, err)
		return
	}
	if len(photos) == 0 {
		return
	}
	arr := make([]any, 0, len(photos))
	for _, ph := range photos {
		arr = append(arr, map[string]any(ph))
	}
	dst["photos"] = arr
}

func (s *Service) ensureGrowthStageName(ctx context.Context, growthStageNames map[int64]string, growthStageID int64) error {
	if growthStageID == 0 {
		return nil
	}
	if growthStageNames[growthStageID] != "" {
		return nil
	}
	stage, err := s.cropwise.FetchGrowthStageByID(ctx, growthStageID)
	if err != nil {
		return err
	}
	if len(stage) == 0 {
		return nil
	}
	if name := growthStageDisplayName(stage); name != "" {
		growthStageNames[growthStageID] = name
	}
	return nil
}

func (s *Service) enrichReportCreatorByID(ctx context.Context, r CropwiseReport, userNames map[int64]string) error {
	userID := reportCreatorUserID(r)
	if userID == 0 || creatorName(r) != "" {
		return nil
	}
	if name := userNames[userID]; name != "" {
		r["created_by_user_name"] = name
		return nil
	}
	user, err := s.cropwise.FetchUserByID(ctx, userID)
	if err != nil {
		return err
	}
	if len(user) == 0 {
		return nil
	}
	name := userDisplayName(user)
	if name == "" {
		return nil
	}
	r["created_by_user_name"] = name
	if userNames != nil {
		userNames[userID] = name
	}
	return nil
}

func (s *Service) DebugReport(ctx context.Context, reportIDNum int64, fieldID int64) error {
	if reportIDNum == 0 {
		return fmt.Errorf("set -report-id")
	}
	r, err := s.cropwise.FetchReportByID(ctx, reportIDNum)
	if err != nil {
		return err
	}
	if r == nil {
		r = CropwiseReport{"id": reportIDNum, "field_id": fieldID}
	}
	if int64Value(r, "field_id") == 0 && fieldID != 0 {
		r["field_id"] = fieldID
	}
	items := []CropwiseReport{r}
	if err := s.enrichReports(ctx, items, s.loadFieldInfo(ctx)); err != nil {
		log.Printf("warning: enrichment failed: %v", err)
	}
	r = items[0]
	actualFieldID := int64Value(r, "field_id")
	if stringValue(r, "report_url") == "" && actualFieldID != 0 {
		r["report_url"] = BuildCropwiseReportURL(s.cfg.CropwiseWebBaseURL, actualFieldID, stringValue(r, "field_web_slug"), strconv.FormatInt(reportIDNum, 10))
	}
	urls := s.reportPhotoURLs(ctx, r)
	fmt.Println("\n--- formatted report ---")
	fmt.Println(FormatReport(r))
	fmt.Println("--- image urls ---")
	for i, u := range urls {
		fmt.Printf("%d. %s\n", i+1, u)
	}
	fmt.Printf("found %d image url(s)\n", len(urls))
	return nil
}

func (s *Service) SendReport(ctx context.Context, reportIDNum int64, fieldID int64) error {
	if reportIDNum == 0 {
		return fmt.Errorf("set -report-id")
	}
	r, err := s.cropwise.FetchReportByID(ctx, reportIDNum)
	if err != nil {
		return fmt.Errorf("fetch report_id=%d: %w", reportIDNum, err)
	}
	if r == nil {
		return fmt.Errorf("report_id=%d not found", reportIDNum)
	}
	if int64Value(r, "field_id") == 0 && fieldID != 0 {
		r["field_id"] = fieldID
	}

	items := []CropwiseReport{r}
	if err := s.enrichReports(ctx, items, s.loadFieldInfo(ctx)); err != nil {
		log.Printf("warning: enrichment failed report_id=%d: %v", reportIDNum, err)
	}
	actualFieldID := int64Value(r, "field_id")
	if stringValue(r, "report_url") == "" && actualFieldID != 0 {
		r["report_url"] = BuildCropwiseReportURL(s.cfg.CropwiseWebBaseURL, actualFieldID, stringValue(r, "field_web_slug"), strconv.FormatInt(reportIDNum, 10))
	}
	_, err = s.sendReportIfNeeded(ctx, r)
	return err
}

func findReportByID(reports []CropwiseReport, reportIDNum int64) (CropwiseReport, error) {
	want := strconv.FormatInt(reportIDNum, 10)
	for _, r := range reports {
		if reportID(r) == want {
			return r, nil
		}
	}
	return nil, fmt.Errorf("report_id=%s not found", want)
}

func reportTimeForEnrichment(r CropwiseReport) time.Time {
	for _, key := range []string{"report_time", "created_by_user_at", "created_at"} {
		s := stringValue(r, key)
		if s == "" {
			continue
		}
		if t, err := parseAnyTime(s); err == nil {
			return t
		}
	}
	return time.Time{}
}

func shouldFetchDirectReport(r CropwiseReport, cfg Config) bool {
	if r == nil || reportID(r) == "" {
		return false
	}
	if cfg.SendPhotos && len(ExtractImageURLsWithBase(r, 1, cfg.CropwiseWebBaseURL)) == 0 {
		return true
	}
	if creatorName(r) == "" {
		return true
	}
	if cfg.EnrichNDVI && formatNDVI(numberStringAny(r, "report_ndvi", "ndvi", "ndvi_value", "field_ndvi")) == "" {
		return true
	}
	return false
}

func (s *Service) isAlreadySent(r CropwiseReport) bool {
	id := reportID(r)
	if id == "" {
		return false
	}
	updatedAt := stringValue(r, "updated_at", "updated_by_user_at")
	return s.state.IsSent(id, updatedAt)
}

func (s *Service) sendReportIfNeeded(ctx context.Context, r CropwiseReport) (bool, error) {
	id := reportID(r)
	if id == "" {
		return false, fmt.Errorf("report without id: %+v", r)
	}
	updatedAt := stringValue(r, "updated_at", "updated_by_user_at")
	if s.state.IsSent(id, updatedAt) {
		fmt.Printf("skip already sent report_id=%s\n", id)
		return false, nil
	}

	if stringValue(r, "report_url") == "" {
		fieldID := int64Value(r, "field_id")
		if fieldID != 0 && id != "" && s.cfg.CropwiseWebBaseURL != "" {
			r["report_url"] = BuildCropwiseReportURL(s.cfg.CropwiseWebBaseURL, fieldID, stringValue(r, "field_web_slug"), id)
		}
	}

	var urls []string
	if s.cfg.SendPhotos {
		urls = s.reportPhotoURLs(ctx, r)
	}
	text := FormatReport(r)
	attachments := make([]MaxAttachment, 0)
	if s.cfg.SendPhotos {
		fmt.Printf("report_id=%s: found %d photo url(s)\n", id, len(urls))
		for _, imgURL := range urls {
			att, err := s.max.UploadImageFromURLWithHeaders(ctx, imgURL, s.cropwise.CropwiseAuthHeadersForURL(imgURL))
			if err != nil {
				log.Printf("image upload failed report_id=%s url=%s: %v", id, imgURL, err)
				continue
			}
			attachments = append(attachments, att)
		}
		fmt.Printf("report_id=%s: uploaded %d photo attachment(s)\n", id, len(attachments))
	}

	if err := s.max.SendMessage(ctx, text, attachments); err != nil {
		return false, fmt.Errorf("send report_id=%s to MAX: %w", id, err)
	}
	s.state.MarkSent(id, updatedAt)
	if err := s.state.Save(s.cfg.StateFile); err != nil {
		return false, err
	}
	fmt.Printf("sent report_id=%s\n", id)
	return true, nil
}

func (s *Service) sendBackfillReportIfNeeded(ctx context.Context, r CropwiseReport, sentCount *int) error {
	id := reportID(r)
	if id == "" {
		return fmt.Errorf("report without id: %+v", r)
	}
	updatedAt := stringValue(r, "updated_at", "updated_by_user_at")
	if s.state.IsSent(id, updatedAt) {
		fmt.Printf("skip already sent report_id=%s\n", id)
		return nil
	}

	if *sentCount > 0 && s.cfg.BackfillSendDelay > 0 {
		fmt.Printf("waiting %s before next backfill send...\n", s.cfg.BackfillSendDelay)
		timer := time.NewTimer(s.cfg.BackfillSendDelay)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return ctx.Err()
		case <-timer.C:
		}
	}

	sent, err := s.sendReportIfNeeded(ctx, r)
	if err != nil {
		return err
	}
	if sent {
		*sentCount++
	}
	return nil
}

func BuildCropwiseReportURL(baseURL string, fieldID int64, fieldWebSlug string, reportID string) string {
	baseURL = strings.TrimRight(baseURL, "/")
	fieldPath := cropwiseReportFieldPath(fieldID, fieldWebSlug)
	if fieldPath == "" || reportID == "" {
		return ""
	}
	return fmt.Sprintf("%s/fields/%s/scout_reports/%s", baseURL, fieldPath, reportID)
}

func cropwiseReportFieldPath(fieldID int64, fieldWebSlug string) string {
	if fieldID != 0 {
		return strconv.FormatInt(fieldID, 10)
	}
	return strings.TrimSpace(fieldWebSlug)
}

func sortedFieldIDs(m map[int64]bool) []int64 {
	ids := make([]int64, 0, len(m))
	for id := range m {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	return ids
}
