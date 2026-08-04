# Context Archive

Entries older than roughly the current working period, moved out of `CONTEXT.md`
to keep the hot path readable. Nothing here is wrong — it's settled history.
Durable rules extracted from these entries live in the `# Conventions` section
of `CONTEXT.md`.

## Listing contract follow-ups: year_built required, lot_size removed 10-06-2026
- `year_built` is now required on create with a plausibility range (1800..next year), validated on create and update
- `lot_size` removed from the create/update request contract — no longer user input; the model column stays so imported MLS listings can still carry it
- Tests cover missing and implausible year_built

## Address contract: postal_code, supported countries, required specs 10-06-2026
- Renamed `zip_code` → `postal_code` everywhere (model gorm column, request/response JSON) — country-neutral name; done while zero clients are deployed so the wire rename was free. Old `zip_code` column lingers in pre-existing dev volumes (AutoMigrate adds, never drops); fresh volumes are clean
- `internal/handlers/countries.go`: `SupportedCountries = ["US","CA"]` is the single source of truth for where listings can be created; public `GET /api/v1/config/countries` serves it to the iOS dropdown and `CreateProperty` validates against the same list
- Country must be ISO 3166-1 alpha-2 AND supported; postal format validated per country (US ZIP `\d{5}(-\d{4})?`, CA `A1A 1A1`); names are a client display concern
- `bedrooms`, `bathrooms`, `square_feet` now required on create — pointer fields with `binding:"required"`, so an explicit 0 (land) passes but omission 400s
- Coordinates remain in the API (search radius depends on them) but the iOS client geocodes them from the address via CLGeocoder — users never enter lat/lon

## Image replacement on property update 10-06-2026
- `PUT /api/v1/properties/:id` accepts an optional `images: [{url, order}]` array — when present it replaces the property's full image set (delete-then-insert in one transaction); omitted/nil leaves images untouched
- Closes the gap where images could only be set at creation; the presign upload flow now works end-to-end for existing listings: presign → PUT bytes to S3/LocalStack → attach via property update → returned by both core GET and lookup search → rendered by the iOS browse card
- Search filter parity (commit 00b8fdb): lookup search gained seller_id, source, and lat/lon/radius_miles (Haversine) filters

## Local AWS-modeled multi-service platform + lookup service 10-06-2026
- Restructured to a multi-binary monorepo: `cmd/api` renamed to `cmd/core`; new `cmd/lookup`; shared `internal/` packages unchanged
- Added parameterized `Dockerfile` (ARG SERVICE builds `cmd/${SERVICE}`; golang:1.26-alpine builder → distroless/static runtime) — one Dockerfile, one image per service, same image locally and on ECS later
- Rewrote `docker-compose.yml` as a 1:1 local model of the AWS topology: nginx `gateway` on :8080 (≙ ALB; routes in `gateway/nginx.conf`, each location block ≙ a future ALB listener rule), `core` and `lookup` containers (≙ ECS services, `profiles: ["full"]`), `postgres` (≙ RDS; DB `realdeal_core`), `localstack` (≙ S3/SecretsManager)
- `scripts/postgres-init.sql`: creates `lookup_ro` SELECT-only role — lookup's read-only boundary is enforced by Postgres grants; default privileges cover tables core's AutoMigrate creates later
- Upload service: relies on the SDK's standard `AWS_ENDPOINT_URL` env override for LocalStack; added `UsePathStyle` when a custom endpoint is set — prod (no endpoint) keeps virtual-host style. Presigning is offline, so the endpoint only needs to be reachable by the uploading client → `http://localhost:4566`
- Lookup service: `internal/handlers/search.go` — `GET /api/v1/search/properties` with q (ILIKE street/city/description), min_price/max_price, beds/baths, property_type, city/state, sort, page/limit (capped 100), active listings only; `database.ConnectReadOnly` (no migrations/extensions)
- Integration suite `tests/integration/` (build tag `integration`): plain HTTP against `API_BASE_URL` (default localhost:8080 = gateway); flows: health, auth, cross-service search/offer lifecycle, presign + real S3 PUT to LocalStack; unique emails per run so re-runnable against persistent DBs
- Makefile: `dev`, `up`/`down`, `docker-build`, `test-integration`, `smoke` (tests against API_BASE_URL — the post-deploy check)
- README.md: service table, local→AWS mapping, "adding a service" recipe

## Implement offer flow API 10-05-2026
- Created `internal/models/offer.go`: `Offer` model with `OfferStatus` (pending/accepted/rejected/withdrawn), GORM uuid primary key, index on `property_id`, associations to `Property` and `User`
- Created `internal/handlers/offers.go`: `OfferHandler` with 6 endpoints — `SubmitOffer` (buyer only, property must be active), `ListOffers` (seller only), `AcceptOffer` (DB transaction: sets offer accepted + rejects others + sets property to pending), `RejectOffer`, `WithdrawOffer`, `ListMyOffers`
- 9 unit tests covering happy paths, auth checks, conflict states, invalid amount, wrong buyer on withdraw

## Implement presigned S3 upload endpoint 03-05-2026
- Added AWS SDK v2 config/s3 + google/uuid dependencies
- `internal/config/config.go`: added `AWSRegion` (default `us-west-2`), `S3Bucket`, `CloudFrontBaseURL`; warns (non-fatal) if S3 fields are missing
- `internal/services/upload.go`: `UploadService` for presign URL generation; key format `{upload_type}/{user_id}/{uuid}.{ext}`; 15-minute expiry; validates `upload_type` against an allowlist
- `internal/handlers/upload.go`: `POST /api/v1/upload/presign`; requires auth; validates filename, content_type (jpeg/png), upload_type; 503 if the upload service isn't configured
- 6 unit tests covering success and each failure mode

## Fix duplicate test function declarations 28-04-2026
- Removed 3 duplicate test functions in `internal/handlers/auth_test.go`
- Duplicates lacked `t.Parallel()` and were likely copy-paste artifacts from an earlier refactor
