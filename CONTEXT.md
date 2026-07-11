# Context

## Viewing-slot concurrent-accept hardening 10-07-2026
- Partial unique index `idx_viewing_requests_one_accepted_per_slot` on viewing_requests (slot_id) WHERE status='accepted' — created idempotently in database.Connect after AutoMigrate (partial indexes aren't expressible via GORM tags); server refuses to start if creation fails
- AcceptRequest maps a unique-constraint violation inside the transaction to the same 409 SLOT_BOOKED body as the pre-check (reuses isUniqueViolation from trust.go); other errors still 500; rollback verified
- Accepted-row UPDATE runs before competitor auto-decline so the index can't false-positive; cancelled acceptances don't block re-booking (index covers only accepted rows)
- 2 new sqlmock tests; evaluator APPROVE; coverage gate 91.1%

## Trust appeal process 08-07-2026
- `POST /api/v1/users/me/trust-appeal` (auth): a blocked account files an appeal with just a statement (≤2000 chars); eligibility = ≥1 confirmed trust event; one appeal per event (`trust_appeals.trust_event_id` unique) and one pending appeal per user (409 `APPEAL_PENDING`)
- Minimal disclosure: success returns exactly `{data:{status:"pending"}}`; "never blocked", "all events already appealed", and the unique-violation race all funnel through one helper → byte-identical 409 `NO_APPEAL_AVAILABLE`, so the endpoint can't be used to fingerprint an account's trust state
- `TrustAppeal` model all `json:"-"`, no GET/adjudication endpoints; resolution is manual ops — exact uphold/overturn SQL documented on the model (overturn = appeal overturned + linked event dismissed in one tx; the block lifts automatically because enforcement derives from confirmed events)
- Coverage gate discovered mid-commit: the pre-commit hook enforces `make test-cover` ≥90% against the working tree — now documented in CLAUDE.md Commands; evaluators must check it, not just `make test`. 19 purposeful tests added (FileAppeal branches, trust-core error paths, SubmitOffer trust-check DB error) → 91.0%
- Ops note: two session-limit interruptions left a staged-but-uncommitted core + broken partial appeal in the tree; recovered via stash --keep-index / conflict resolution from the stash tree — the trust core and appeal ship as one commit since they share files

## Hidden bidirectional trust system: events, reports, enforcement 07-07-2026
- Design: append-only `trust_events` table (`internal/models/trust.go`) — judgment is DERIVED from events, never stored as a score column; every field `json:"-"`, no GET endpoints, no Preloads — structurally invisible to users (evaluator leak-sweep verified, tests assert no "trust"/"flag" substrings in any response)
- Event types: `offer_default` (buyer), `deed_default`/`document_fraud` (seller); status `pending_review`/`confirmed`/`dismissed` — the adjudication gate: objective events auto-confirm, accusations wait
- `POST .../offers/:offerId/report-nonpayment` (seller): server verifies accepted + payment deadline strictly past → single transaction: offer → `defaulted`, confirmed event insert, property reverts pending → active (only from pending); duplicate → 409 via composite unique `(offer_id, event_type)`
- `POST .../offers/:offerId/report-seller` (buyer on the offer, accepted only): validated violation enum + notes → `pending_review` event, 204 no body — does NOT enforce until manually confirmed (no adjudication endpoints by design; manual DB ops until the admin surface P1)
- Enforcement (confirmed events only, role-isolated): buyer w/ offer_default blocked from SubmitOffer; seller w/ deed_default|document_fraud blocked from CreateProperty + AcceptOffer — neutral 403s that never mention the mechanism. UpdateProperty/viewings/withdraw deliberately ungated
- `PaymentDeadline` stamped on the offer inside the existing AcceptOffer transaction (`PAYMENT_DEADLINE_HOURS` config, default 72); offer wire contract gains `payment_deadline` + `defaulted` status (legitimate transaction-party data)
- When Stripe lands, its webhook becomes a second writer of the same events — model/enforcement/contract unchanged
- 17 new tests; suite 189/189 handler tests green; evaluator APPROVE on a 10-point trust-specific checklist
- Non-blocking note: dismissed reports can't be re-filed for the same offer/type (unique index ignores status) — revisit with the admin surface

## Viewing scheduling API: slots + seller-approved requests 05-07-2026
- Product decisions: sellers post one-off dated time slots (no recurrence — a slot generator can add it later without wire changes); seller approves each request; one buyer per slot (accept auto-declines competing pending requests, same transaction pattern as AcceptOffer)
- `internal/models/viewing.go`: `ViewingSlot` (property child, start/end UTC) and `ViewingRequest` (slot + denormalized property FK, buyer, optional message, status pending/accepted/declined/cancelled)
- `internal/handlers/viewings.go`: 8 endpoints mirroring the offer handler — slot create (seller, active property, future start, half-open overlap check)/list (public, `booked` flag without leaking booker)/delete (declines pending in tx; 409 if a confirmed booking exists); request create (active re-checked, own-property 403, booked 409, one live request per buyer)/seller list/accept (tx auto-decline)/decline/buyer cancel (pending or accepted → cancelled; a cancelled acceptance frees the slot since booked = has accepted request); `GET /users/me/viewing-requests` mirrors ListMyOffers
- "Booked" is computed (count of accepted requests), not a stored flag — no stale-state on cancellation
- 18 sqlmock tests incl. transaction ordering assertions; suite 142/142 handler tests green; evaluator-verified (APPROVE)
- Known residual risk: concurrent accepts on the same slot rely on read-then-write; a partial unique index on `(slot_id) WHERE status='accepted'` would harden it (candidate backlog item)

## Listing contract follow-ups: year_built required, lot_size removed 10-06-2026
- `year_built` is now required on create with a plausibility range (1800..next year), validated on create and update
- `lot_size` removed from the create/update request contract — no longer user input; the model column stays so imported MLS listings can still carry it
- Tests cover missing and implausible year_built

## Address contract: postal_code, supported countries, required specs 10-06-2026
- Renamed `zip_code` → `postal_code` everywhere (model gorm column, request/response JSON) — country-neutral name; done while zero clients are deployed so the wire rename was free. Old `zip_code` column lingers in pre-existing dev volumes (AutoMigrate adds, never drops); fresh volumes are clean
- `internal/handlers/countries.go`: `SupportedCountries = ["US","CA"]` is the single source of truth for where listings can be created; public `GET /api/v1/config/countries` serves it to the iOS dropdown and `CreateProperty` validates against the same list — adding a code there opens a market for both
- Country must be ISO 3166-1 alpha-2 AND supported; postal format validated per country (US ZIP `\d{5}(-\d{4})?`, CA `A1A 1A1`); names are a client display concern (`Locale.localizedString(forRegionCode:)`)
- `bedrooms`, `bathrooms`, `square_feet` now required on create — pointer fields with `binding:"required"`, so an explicit 0 (land) passes but omission 400s
- Coordinates remain in the API (search radius depends on them) but the iOS client now geocodes them from the address via CLGeocoder — users never enter lat/lon

## Image replacement on property update 10-06-2026
- `PUT /api/v1/properties/:id` accepts an optional `images: [{url, order}]` array — when present it replaces the property's full image set (delete-then-insert in one transaction); omitted/nil leaves images untouched
- Closes the gap where images could only be set at creation; the presign upload flow now works end-to-end for existing listings: presign → PUT bytes to S3/LocalStack → attach via property update → returned by both core GET and lookup search → rendered by the iOS browse card (LocalStack GetObject 200 observed from the simulator)
- Search filter parity (earlier same day, commit 00b8fdb): lookup search gained seller_id, source, and lat/lon/radius_miles (Haversine) filters so the iOS app could adopt /api/v1/search without losing capability

## Local AWS-modeled multi-service platform + lookup service 10-06-2026
- Restructured to a multi-binary monorepo: `cmd/api` renamed to `cmd/core`; new `cmd/lookup`; shared `internal/` packages unchanged
- Added parameterized `Dockerfile` (ARG SERVICE builds `cmd/${SERVICE}`; golang:1.26-alpine builder → distroless/static runtime) — one Dockerfile, one image per service, same image locally and on ECS later
- Rewrote `docker-compose.yml` as a 1:1 local model of the AWS topology: nginx `gateway` on :8080 (≙ ALB; routes in `gateway/nginx.conf`, each location block ≙ a future ALB listener rule), `core` and `lookup` containers (≙ ECS services, `profiles: ["full"]`), `postgres` (≙ RDS; DB now `realdeal_core`), `localstack` (≙ S3/SecretsManager)
- `scripts/postgres-init.sql`: creates `lookup_ro` SELECT-only role — lookup's read-only boundary is enforced by Postgres grants; default privileges cover tables core's AutoMigrate creates later. Init scripts only run on a fresh volume (`docker compose down -v` after changes)
- `scripts/localstack-init.sh`: creates `realdeal-media-local` bucket with CORS mirroring media.yaml; **must be executable** (chmod +x) or LocalStack silently skips it
- Upload service: relies on the SDK's standard `AWS_ENDPOINT_URL` env override for LocalStack (config v1.32 honors it natively); added `UsePathStyle` when a custom endpoint is set — prod (no endpoint) keeps virtual-host style. Presigning is offline, so the endpoint only needs to be reachable by the uploading client (host/simulator) → `http://localhost:4566`
- Lookup service: `internal/handlers/search.go` — `GET /api/v1/search/properties` with q (ILIKE street/city/description), min_price/max_price, beds/baths, property_type, city/state, sort (price_asc/price_desc/newest), page/limit (capped 100), active listings only; `database.ConnectReadOnly` (no migrations/extensions); 6 unit tests with sqlmock
- Integration suite `tests/integration/` (build tag `integration`): plain HTTP against `API_BASE_URL` (default localhost:8080 = gateway); flows: health, auth (signup→signin→me), cross-service search/offer lifecycle (listings written via core appear in lookup, accepted offer removes listing from search, competing offers auto-rejected), presign + real S3 PUT to LocalStack; unique emails per run so re-runnable against persistent DBs
- Makefile: `dev` (infra + go run core), `up`/`down` (full stack), `docker-build`, `test-integration` (up → test → down), `smoke` (tests against API_BASE_URL — the future post-deploy check)
- README.md added: service table, local→AWS mapping, "adding a service" recipe (own DB if it writes, read-only role if read side)
- All unit tests and the full integration suite pass; verified end-to-end including S3 upload via presigned URL against LocalStack

## Implement offer flow API 10-05-2026
- Created `internal/models/offer.go`: `Offer` model with `OfferStatus` (pending/accepted/rejected/withdrawn), GORM uuid primary key, index on `property_id`, associations to `Property` and `User`
- Created `internal/handlers/offers.go`: `OfferHandler` with 6 endpoints — `SubmitOffer` (POST, buyer only, property must be active, amount > 0), `ListOffers` (GET, seller only), `AcceptOffer` (PUT, DB transaction: sets offer accepted + rejects others + sets property to pending), `RejectOffer` (PUT), `WithdrawOffer` (DELETE), `ListMyOffers` (GET /users/me/offers)
- Created `internal/handlers/offers_test.go`: 9 unit tests covering happy paths, auth checks (403 for wrong user), conflict states (non-active property, already-accepted offer), invalid amount, wrong buyer on withdraw
- Updated `internal/database/database.go`: added `&models.Offer{}` to AutoMigrate
- Updated `cmd/api/main.go`: registered all 6 offer routes; `GET /users/me/offers` added under users group
- All tests pass (`go test ./...`)

## Implement presigned S3 upload endpoint 03-05-2026
- Added `github.com/aws/aws-sdk-go-v2/config`, `github.com/aws/aws-sdk-go-v2/service/s3`, and `github.com/google/uuid` dependencies to `go.mod`
- Updated `internal/config/config.go`: added `AWSRegion` (default `us-west-2`), `S3Bucket`, `CloudFrontBaseURL` fields; logs warnings if S3 fields are missing (non-fatal — server still starts)
- Created `internal/services/upload.go`: `UploadService` + `UploadServiceInterface` for presign URL generation; key format `{upload_type}/{user_id}/{uuid}.{ext}`; 15-minute presign expiry; validates `upload_type` against allowlist
- Created `internal/handlers/upload.go`: `UploadHandler.Presign` — `POST /api/v1/upload/presign`; requires auth (reads `userID` from gin context); validates `filename` (required), `content_type` (jpeg/png only), `upload_type` (property/profile/id_verification); returns 503 if upload service not configured
- Created `internal/handlers/upload_test.go`: 6 unit tests covering success, missing filename, invalid content type, invalid upload type, nil service (503), and service error
- Wired into `cmd/api/main.go`: upload service created at startup (logs warning if unconfigured), handler registered at `POST /api/v1/upload/presign` with auth middleware
- Updated `.env.example` with `AWS_REGION`, `S3_BUCKET`, `CLOUDFRONT_BASE_URL` entries

## Fix duplicate test function declarations 28-04-2026
- Removed 3 duplicate test functions in `internal/handlers/auth_test.go`: `TestSignup_DBError`, `TestSignout_Success`, `TestSignin_BadJSON`
- Duplicates lacked `t.Parallel()` and were likely copy-paste artifacts from an earlier refactor
- All handlers and middleware tests now pass

