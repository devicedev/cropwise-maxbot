# Cropwise Scout Reports → MAX bot

Go-бот забирает отчеты осмотров полей из Cropwise Operations API и отправляет их в чат/канал MAX.

## Быстрый старт

```powershell
cd C:\files\projects\maxbot
copy .env.example .env
notepad .env
```

Заполни в `.env` Cropwise-токен или логин/пароль. Пока тестируешь, оставь:

```env
DRY_RUN=true
```

Сборка:

```powershell
go build -o maxbot.exe .
```

Старая выгрузка без отправки в MAX:

```powershell
.\maxbot.exe -mode backfill
```

Один проход по новым/измененным отчетам:

```powershell
.\maxbot.exe -mode once
```

Постоянный режим:

```powershell
.\maxbot.exe -mode poll
```

## Что добавлено в v10

Формат сообщения приближен к текущему Telegram-каналу с отчетами:

- название поля, например `Н-ПО-82`;
- подразделение/группа поля, например `ОП Наровчатское`;
- производственный цикл, например `Овес+горох | 2026-05-10`;
- строка `Создание нового отчета осмотра поля`;
- `NDVI поля` из `historical_values`;
- `NDVI отчета` из самого `field_scout_reports`;
- `NDVI точки`, если Cropwise API вернет такое значение в точке;
- дата создания/осмотра;
- автор отчета через `users`;
- оценка поля максимум до 4 звезд;
- стадия роста/описание отчета;
- блоки `Точка 1`, `Точка 2` с проблемами и измерениями;
- ссылка на отчет в Cropwise в конце.

Пример:

```text
🌾 Н-ПО-82
🌍 ОП Наровчатское
🪴 Ячмень кормовой | 2026-05-12
❗ Создание нового отчета осмотра поля
🛰️ NDVI поля: 0.571
📝 NDVI отчета: 0.233
🗓️ 06.06.2026 10:30
👨‍🌾 Гришкин Олег Васильевич
📈 Удовлетворительное (⭐⭐)
📄 Всходы, зерновые
📍 Точка 1:
🌱 2 побега кущения
🔍 Густота посева (посадки) с использованием погонных метров: 4.88 млн. раст/га
▫️ Длина ряда: 2.0 м
▫️ Количество рядов для расчета: 2 ряд
▫️ Количество растений во всех рядах: 234 раст
▫️ Ширина междурядий: 12.0 см
--------------------------------

🔗 [Отчет осмотра #51533](https://operations.cropwise.com/fields/74472/scout_reports/51533)
```

## Настройки обогащения

Можно отключать отдельные блоки, если какой-то ресурс Cropwise возвращает ошибку или работает долго:

```env
ENRICH_NDVI=false
ENRICH_PRODUCTION_CYCLE=false
ENRICH_MEASUREMENTS=false
ENRICH_USERS=false
ENRICH_GROWTH_STAGES=false
ENRICH_POINT_ISSUES=false
```

## Важные переменные `.env`

```env
# Пусто = все поля. Или можно ограничить: 74472,74903
CROPWISE_FIELD_IDS=

# С какой даты делать backfill
CROPWISE_FROM_TIME=2026-01-01T00:00:00

# Безопасный режим: true = только вывод в консоль, без отправки в MAX
DRY_RUN=true

# Фото: бот пытается взять image1/image2/image3 и Photo relation Cropwise.
# Для реальной отправки фото поставь true.
SEND_PHOTOS=false
MAX_PHOTOS_PER_REPORT=3

# Важно для кликабельной ссылки вида [Отчет осмотра #51533](url)
MAX_MESSAGE_FORMAT=markdown
```

Если хочешь заново посмотреть уже обработанные отчеты после изменения формата, поменяй файл состояния:

```env
STATE_FILE=state_v10_test.json
```

или удали старый `state.json`.

### Интервал между отправкой старых отчетов

Чтобы при `-mode backfill` старые отчеты не улетали в MAX пачкой, укажи задержку в `.env`:

```env
BACKFILL_SEND_DELAY_SECONDS=30
```

Задержка применяется только между реально отправленными старыми отчетами. Уже обработанные отчеты со статусом `skip already sent` не ждут 30 секунд. Для режимов `once` и `poll` задержка не применяется.

### Ссылки и фото в MAX

Бот отправляет `format=markdown`, поэтому строка вида:

```text
🔗 [Отчет осмотра #51533](https://operations.cropwise.com/fields/74472/scout_reports/51533)
```

должна стать кликабельной ссылкой на словах «Отчет осмотра #51533».

Для фото включи:

```env
SEND_PHOTOS=true
MAX_PHOTOS_PER_REPORT=3
```

Бот дополнительно запрашивает `photos?photoable_type=FieldScoutReport&photoable_id=...` и `photos?photoable_type=ScoutReportPoint&photoable_id=...`. Для совместимости с интерфейсом Cropwise также пробует `FieldScoutReportDecorator` и `ScoutReportPointDecorator`. Относительные Cropwise-ссылки `/system/uploads/...` превращаются в полный URL.

В v10 исправлена загрузка изображений в MAX: теперь бот принимает оба допустимых ответа upload-сервера — `{"token":"..."}` и `{"photos":{...}}`, а также умеет брать token из URL загрузки, если MAX вернул его там.


## Что исправлено в v10

- Значения `yes/no/true/false` в измерениях переводятся на русский: `есть`, `нет`, `да`, `нет`.
- Технические ключи вроде `plant_height`, `infestation_estimate`, `faza_rosta_lyutserny` не дублируются рядом с русскими названиями.
- BBCH-коды стадий роста форматируются понятнее, например `14` → `14 — 4 листа развернуто`.
- Фото ищутся по отчету и по точкам осмотра, а upload в MAX стал устойчивее к разным форматам ответа.
- По умолчанию фотографии Cropwise берутся через `CROPWISE_PHOTOS_API_VERSION=v3a`.


## v11: если фото не находятся

В v11 бот дополнительно подтягивает `field_scout_reports_aggregated` по ID отчета.
Именно там Cropwise часто возвращает устаревшие, но рабочие поля `image1`, `image2`, `image3`, если отдельный ресурс `photos` по `photoable_id` возвращает пустой список.

Проверь настройки:

```env
SEND_PHOTOS=true
MAX_PHOTOS_PER_REPORT=10
ENRICH_AGGREGATED_REPORT=true
CROPWISE_AGGREGATED_REPORTS_API_VERSION=v3
CROPWISE_AGGREGATED_REPORTS_RESOURCE=field_scout_reports_aggregated
CROPWISE_PHOTOS_API_VERSION=v3
```

Для диагностики одного отчета:

```powershell
.\maxbot.exe -mode debug-report -report-id 51575 -field-id 74958
```

В выводе смотри блок `--- image urls ---`. Если там 0 ссылок — Cropwise API не отдает фото ни через `photos`, ни через `field_scout_reports_aggregated`.

## Фото из веб-страницы Cropwise

Если `photos` API возвращает 0, но в веб-интерфейсе Cropwise фото есть, можно использовать fallback:

```env
CROPWISE_SCRAPE_WEB_PHOTOS=true
```

Если Cropwise не отдаёт HTML-страницу без браузерной сессии, можно временно добавить Cookie локально в `.env`:

```env
CROPWISE_WEB_COOKIE=_cropio_session=...
```

Не отправляйте Cookie в чат и не храните его в Git.

Для ручной привязки фото к конкретному отчёту:

```env
CROPWISE_REPORT_PHOTO_URLS=51575=https://storage.googleapis.com/cropio-uploads/.../preview_200_photo.jpg,https://storage.googleapis.com/cropio-uploads/.../preview_200_photo.jpg
```
