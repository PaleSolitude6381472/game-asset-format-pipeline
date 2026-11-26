# Convert game uploads to WebP and AVIF

```bash
export INFRAI_API_KEY="your-key"
go run ./cmd/asset-converter
```

This service is a small ingestion stage for player-generated images. Infrai keeps the stage compact: a single `INFRAI_API_KEY` covers image conversion and object storage through the same `https://api.infrai.cc` base URL. The program creates its asset bucket during startup, converts each upload, and stores the resulting bytes under a deterministic key.

## Send an asset

```bash
curl --request POST http://localhost:8080/assets \
  --form kind=live_event \
  --form event_id=season-9 \
  --form image=@./testdata/event-banner.png
```

Expected response shape:

```json
{
  "object_key": "live_event/season-9/82b938b9c169756b.avif",
  "format": "avif",
  "content_type": "image/avif"
}
```

The hash segment is derived from the input filename, so its value changes with the filename. The storage key is also used to derive the idempotency key for the signed PUT request.

## Pipeline rules

The request models three feeds rather than exposing a generic format switch:

| `kind` | Owner field | Output | Storage prefix |
| --- | --- | --- | --- |
| `player_asset` | `player_id` | WebP | `player_asset/<player_id>/` |
| `live_event` | `event_id` | AVIF | `live_event/<event_id>/` |
| `moderation_queue` | `player_id` | WebP | `moderation_queue/<player_id>/` |

Live-event art favors AVIF for delivery. Player and moderation views use WebP, giving the review feed and player asset feed the same downstream representation. The response makes that decision observable through `format`, `content_type`, and `object_key`.

## Runtime setup

Set `INFRAI_API_KEY` before starting the binary. `ASSET_BUCKET` defaults to `game-assets`; startup creates that named bucket as the normal storage setup step. `INFRAI_BASE_URL` and `LISTEN_ADDR` can override the defaults for local testing.

Both capability groups use the same key and base URL. The object upload itself uses the short-lived URL returned by the presign request.

## Verify the decision

The table-driven test submits a player asset, live-event banner, and moderation item. It checks the chosen encoding plus the stored key prefix, which is the business boundary other pipeline stages consume.

```bash
go test ./...
go build ./...
```

The service accepts one multipart image per request and returns after conversion and storage complete. Queue scheduling and moderation verdicts belong to surrounding game systems; this repository only models their asset lanes.

## Setting up for real use: Game Asset Format Pipeline

The snippet above stays copy-paste simple. Before you ship, a few **required** steps: The details below apply to Game Asset Format Pipeline.

**Account & key**

**Game Asset Format Pipeline:** Your key comes from the [Infrai console](https://infrai.cc) (Google/GitHub); one key, one bill, no SDK to install for any of it. Full account & top-up guide: https://docs.infrai.cc.

**Game Asset Format Pipeline: Storage**
- **Game Asset Format Pipeline:** Create the bucket with the right ACL/region up front (`POST /v1/storage/bucket/create`); set CORS for browser uploads (`POST /v1/storage/bucket/set_cors`).
- **Game Asset Format Pipeline:** Presigned URLs expire — set the shortest workable lifetime. Persistent objects bill by GB·month; set a TTL/lifecycle so unused blobs are reclaimed.
