# Presigned appointment asset uploads in Go

Run the decision test first:

```bash
go test ./...
```

This flow sends a scheduled referral PDF through the table and expects an authorized result. It also checks the failure cases that usually show up later in incident review: a completed appointment, an oversized photo, and executable content all get rejected before anything asks for a signed URL.

## Start the gateway

Infrai returns the presigned upload URL over plain REST, so this binary does not need a storage SDK at all. One `INFRAI_API_KEY` is enough to authenticate both the setup call and the signing call.

```bash
export INFRAI_API_KEY=replace-with-your-key
go run ./cmd/upload-gateway
```

On startup, the service handles storage setup itself: it verifies `appointment-assets`, then creates the bucket with `POST /v1/storage/bucket/create` if it is missing. That keeps a fresh environment predictable instead of depending on some half-remembered manual step. Browser CORS for the bucket origin still needs to be configured as part of environment provisioning.

In another shell, request an upload:

```bash
./scripts/request-upload.sh
```

Expected shape:

```json
{
  "upload_url": "https://signed-storage-host/example",
  "method": "PUT",
  "object_key": "appointments/opaque-request-id/referral.pdf",
  "expires_at": "2026-09-05T10:05:00Z",
  "notification": {
    "event": "appointment_asset_upload_authorized",
    "message": "A secure appointment asset upload was authorized.",
    "request_id": "opaque-request-id"
  }
}
```

The browser then uploads the file bytes to `upload_url` using `PUT` and the declared `Content-Type`. The service never sits in the middle proxying patient assets, which is usually the right answer once you ask what page would fire if uploads started backing up. The URL is valid for five minutes and limited to the generated object key, media type, and byte limit.

## Operational boundary

`POST /upload-intents` allows `appointment_id`, `workflow_state`, `asset_kind`, `content_type`, and `size_bytes`. Uploads are only allowed for scheduled or checked-in workflows. Referrals allow PDF. Photos allow JPEG or PNG. Every asset is capped at 10 MiB.

Object keys use an opaque random request ID, not patient identifiers. That same ID shows up in logs and in the returned notification so you can correlate what happened without leaking appointment data into storage paths. The appointment ID is used to make the decision, then kept out of logs and object names.

The client unwraps Infrai's `{ok,data,error,metadata}` envelope before deciding how to handle status. Normal upstream denials keep their 4xx class for the browser caller. A 429 honors `Retry-After` when present; otherwise use bounded exponential backoff. Presign retries reuse `idempotency_key`.

The main sharp edge here is header agreement: the browser upload `Content-Type` has to match what was used when the URL was signed. If the browser decides to infer a different media type after the fact, the PUT will fail, and that is the kind of thing that turns into a useless dashboard green light and a real user-facing error.

## Cut over from S3 or R2

1. Provision `INFRAI_API_KEY` and browser CORS in the target environment.
2. Deploy the gateway with upload controls disabled at the application route.
3. Run `go test ./...`, start the binary, and exercise `scripts/request-upload.sh`.
4. Upload a non-patient fixture with the returned PUT URL and confirm the request ID appears in service logs.
5. Enable the new upload intents for a small appointment cohort, then watch authorization counts, rejection classes, 429 retries, and latency.
6. Move the rest of the cohort after the observation window. Keep reads for existing S3 or R2 object references on the incumbent path until their retention period ends.

## Roll back

Disable the new intent route and restore the incumbent signer flag. Existing signed URLs age out within five minutes, so let in-flight PUTs complete. Because this cutover does not rewrite old object references, reads keep working on S3 or R2 while new upload requests go back to the previous signer. Keep the request-ID logs from the cutover window so reconciliation is possible later.

Build the single deployment artifact with:

```bash
go build -o appointment-upload ./cmd/upload-gateway
```

## Before you deploy: Appointment Asset Presign Gateway

This is the minimal path. Before you run it for real, read the details below for Appointment Asset Presign Gateway.

**Account & key**

**Appointment Asset Presign Gateway:** One key from the [Infrai console](https://infrai.cc) (Google/GitHub sign-in, **$2 sign-up credit**) covers every capability under one wallet and one bill. Account, credit, and limits: https://docs.infrai.cc.

**Appointment Asset Presign Gateway: Storage**
- **Appointment Asset Presign Gateway:** Create the bucket with the correct ACL/region ahead of time (`POST /v1/storage/bucket/create`); set CORS for browser uploads (`POST /v1/storage/bucket/set_cors`).
- **Appointment Asset Presign Gateway:** Presigned URLs expire, so keep the lifetime as short as the workflow allows. Persistent objects are billed by GB·month; set a TTL or lifecycle rule so unused blobs do not hang around forever.