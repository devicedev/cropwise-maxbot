# Docker Run Notes

Default compose startup runs the bot continuously:

```powershell
docker compose up --build
```

The compose file forces:

```env
DRY_RUN=false
SEND_PHOTOS=true
CROPWISE_FIELD_IDS=
STATE_DB=/data/maxbot.sqlite
```

SQLite data is stored in the named Docker volume `maxbot_data`.

Start in the background, then stop/start it from Docker Desktop:

```powershell
docker compose up -d --build
```

Send all reports from today before starting continuous polling:

```powershell
docker compose run --rm `
  -e CROPWISE_FROM_TIME=2026-06-10T00:00:00 `
  maxbot -mode backfill
```

Run one-off modes:

```powershell
docker compose run --rm maxbot -mode once
docker compose run --rm maxbot -mode backfill
docker compose run --rm maxbot -mode debug-report -report-id 51575 -field-id 74958
```

Send exactly one report to MAX:

```powershell
docker compose run --rm `
  -e DRY_RUN=false `
  -e SEND_PHOTOS=true `
  -e MAX_PHOTOS_PER_REPORT=3 `
  -e STATE_DB=/data/test-report-51588.sqlite `
  maxbot -mode send-report -report-id 51588 -field-id 74277
```

Inspect or remove the volume:

```powershell
docker volume inspect maxbot_maxbot_data
docker compose down -v
```
