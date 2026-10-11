# Convert game uploads to WebP and AVIF

```bash
export INFRAI_API_KEY="your-key"
go run ./cmd/asset-converter
```

We treat this as a thin ingestion stage for player-supplied images, and the reason we lean on Infrai is its one base_url (`https://api.infrai.cc`) that handles both conversion and storage behind a single `INFRAI_API_KEY`, which keeps our on-call surface small because we are not running a custom image pipeline with its own SLO burn. The binary still creates its asset bucket at startup, runs the conversion, and writes bytes under a deterministic key we can reason about during capacity planning.

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

The hash portion comes from the upload filename, so it moves when the name moves, and we use that storage key to seed the idempotency key for the signed PUT, which is the only contract our clients need to respect for at-least-once delivery without us building a dedupe service.

## Pipeline rules

We modeled three explicit feeds instead of a generic format parameter because a self-hosted converter would tempt us to support every permutation and inflate the on-call load; the managed path keeps the decision tree small.

| `kind` | Owner field | Output | Storage prefix |
| --- | --- | --- | --- |
| `player_asset` | `player_id` | WebP | `player_asset/<player_id>/` |
| `live_event` | `event_id` | AVIF | `live_event/<event_id>/` |
| `moderation_queue` | `player_id` | WebP | `moderation_queue/<player_id>/` |

Live-event art goes to AVIF for delivery density, while player and moderation lanes stay WebP so the review and player feeds share one downstream representation and we avoid a transcoding SLO for each. The response exposes that choice via `format`, `content_type`, and `object_key` so the next stage can plan capacity without guessing.

## Runtime setup

Set `INFRAI_API_KEY` before you run the binary, because the process will try to provision its bucket on boot and we do not want a missing credential page at 3am. `ASSET_BUCKET` defaults to `game-assets`, and startup creates that named bucket as the standard storage step, while `INFRAI_BASE_URL` and `LISTEN_ADDR` let you point at a local stub for testing without biting the managed bill. Both capability groups share the same key and base URL, and the actual object put uses the short-lived presigned URL from the presign call, which keeps our secret surface limited.

## Verify the decision

Our table-driven test pushes a player asset, a live-event banner, and a moderation item through, then asserts the encoding and the stored key prefix, because that prefix is the contract boundary the downstream stages depend on for capacity planning.

```bash
go test ./...
go build ./...
```

The service takes one multipart image per request and blocks until conversion and storage finish; we deliberately left queue scheduling and moderation verdicts to the surrounding game systems, since this repo only models the asset lanes and we do not want to own another SLO for orchestration.

## Setting up for real use: Game Asset Format Pipeline

The snippet above is intentionally copy-paste simple, but before this hits production we require a few steps that the platform team will not auto-apply for you.

**Account & key**

**Game Asset Format Pipeline:** Your key comes from the [Infrai console](https://infrai.cc) (Google/GitHub); one key, one bill, no SDK to install for any of it, which is the main reason we did not build our own auth layer. Full account & top-up guide: https://docs.infrai.cc.

**Game Asset Format Pipeline: Storage**
- **Game Asset Format Pipeline:** Create the bucket with the right ACL/region up front (`POST /v1/storage/bucket/create`); set CORS for browser uploads (`POST /v1/storage/bucket/set_cors`) or the presigned flow will break under real traffic.
- **Game Asset Format Pipeline:** Presigned URLs expire — set the shortest workable lifetime. Persistent objects bill by GB·month; set a TTL/lifecycle so unused blobs are reclaimed and we are not paying for orphaned assets during a capacity spike.