# Current State

*Rewrite this section after each task — it is the first thing a new session reads.*

**Services** (Go monorepo, one binary per `cmd/<service>`, all behind the nginx gateway on :8080 locally):
- **core** — auth (email + Apple/Google), users/profiles, properties CRUD, favorites, S3 presign upload, offers, viewings, trust, contracts.
- **lookup** — read-only listing search (`/api/v1/search/*`), connects as the SELECT-only `lookup_ro` role.

**MVP flow status**: listing → discovery → viewings → offers → **contract signing** are all built and live-verified. Escrow/payments and completion (sold) are NOT built — that's the next major slice.

**Notable subsystems**: hidden bidirectional trust system (append-only events, derived enforcement, appeals — structurally invisible to users); contract state machine (auto-created on offer accept, 14-day deadline, terms→agree→sign→executed, lazy expiry).

**Provider seams**: media storage is behind `services.MediaStorage` (`PresignPut` only) with an S3 implementation in `internal/services/storage_s3.go` — the sole file importing `aws-sdk-go-v2`. Payments will follow the same pattern with Stripe. Both exist so a provider swap is a new implementation, not a rewrite.

**Next up**: Stripe escrow behind our own provider interface (decided, not started) · admin adjudication surface for pending trust events/appeals (P1) · listing-ownership verification (P0, needs a product decision on mechanism). The first AWS deployment rehearsal was completed and smoke-verified on 18-08-2026 — see realdeal-infra; the AWS account is currently suspended, so anything needing it is blocked.

# Conventions

*Hard-won rules. Violating these has broken things before.*

- **Coverage gate is `bash hooks/pre-commit`**, scoped to `internal/handlers` + `internal/middleware` at ≥90%. The raw `make test-cover` target blends in 0%-covered infra packages and reads ~80% — that number is expected and not the gate. Evaluators must run the hook.
- **`/users/me/*` endpoints take identity ONLY from the auth middleware's `userID`** — never a query param. This is what makes them safe to return non-public data.
- **Multi-step state changes go in one `db.Transaction`.** GORM auto-wraps `Updates(map)` in its own transaction when the model has belongs-to associations — wrap those explicitly so sqlmock expectations stay deterministic.
- **The trust system is structurally invisible**: every field `json:"-"`, no GET endpoints, no Preloads, neutral 403s that never name the mechanism. Never surface it in a response or error.
- **`SupportedCountries` in `internal/handlers/countries.go` is the single source of truth** for which markets accept listings — it feeds both the client dropdown and server validation. Add a code there to open a market.
- **Tests assert the WHERE arguments**, not just status codes (`.WithArgs(...)`), so filters can't silently regress.
- Error shape is `{"error": ..., "code": ...}`; 403 forbidden / 404 not found / 409 conflict-or-wrong-state.
- **Postgres init scripts run only on a fresh volume** — `docker compose down -v` after editing `scripts/postgres-init.sql`.
- **`scripts/localstack-init.sh` must be executable** (`chmod +x`) or LocalStack silently skips it and the bucket never exists.
- **LocalStack has no persistent volume** — uploaded images vanish when the Docker daemon restarts. Dev-only annoyance, not a bug.

# Context

*Newest first. Older entries live in `CONTEXT-ARCHIVE.md`.*

## S3 behind a MediaStorage interface 18-08-2026
- `internal/services/storage.go` defines `MediaStorage`, one method: `PresignPut(ctx, key, contentType, ttl)`. It leaks nothing provider-specific — no bucket, no SDK types, no `s3.Options` — because the bucket and credential chain are captured in the implementation's constructor
- `internal/services/storage_s3.go` is the S3 implementation and is now the ONLY file in the repo importing `aws-sdk-go-v2`. Porting media storage to another provider is one new file
- `UploadService` keeps every business rule: upload-type validation, the `{upload_type}/{user_id}/{uuid}.{ext}` key layout, extension defaulting/lowercasing, public-URL construction, and the 15-minute TTL (`presignTTL`). The TTL is a parameter rather than baked into the implementation precisely so that policy stays on the business side of the seam
- Behaviour unchanged, verified line-by-line against the previous version: the LocalStack path-style switch (`o.UsePathStyle` when `awsCfg.BaseEndpoint != nil`), the expiry, the S3_BUCKET/CLOUDFRONT_BASE_URL validation order, and every error string moved across verbatim. `cmd/core/main.go` needed no change — `NewUploadService` keeps its signature and the nil-service→503 path is intact
- Motivation: the same two-way-door reasoning already applied to payments, prompted by evaluating GCP. A GCS implementation satisfies this interface cleanly (`storage.SignedURL` with `Method: "PUT"` returns a plain URL — different mechanism, same shape)
- Known limit of the seam: it returns only a URL, so a provider needing extra client request headers (Azure Blob SAS wants `x-ms-blob-type`) could not express that. Irrelevant for GCS, and the wire contract already returns only `upload_url`, so it would be a wire change regardless
- Testability was the second win: `UploadService.Presign` could not be unit tested at all before, because constructing it required real AWS config. 9 tests now cover key layout, TTL/content-type passthrough, the allowed-type set, rejection-before-signing, extension handling, CDN-base trimming, key uniqueness, error propagation and the constructor guards. `internal/services` 0%→53.1% on Presign; the handlers+middleware gate unaffected at 91.9%
- Evaluator APPROVE

## GET /users/me/listings — seller's own listings across statuses 19-07-2026
- New authed endpoint returns the caller's own active/pending/sold listings (deleted excluded), Images+Seller preloaded, created_at DESC, `{"data": [...]}` — mirrors ListMyOffers/ListMyContracts exactly (no pagination)
- Fixes an iOS bug found in the live contract walkthrough: My Listings was fed by the active-only lookup search, so a seller's listing vanished the moment an offer was accepted (pending) — exactly when its offers/contract path matters; sold listings were invisible to their owner too
- Seller identity comes only from the auth middleware's userID (never a query param), so there's no path to another seller's non-active listings; 4 tests pin the WHERE clause (seller_id + the three statuses) rather than just status codes
- Evaluator APPROVE; coverage gate 91.9%

## Contract/signing state machine 10-07-2026
- Product decisions (user): contract auto-created inside AcceptOffer's transaction (draft, `ExecutionDeadline` = now + `CONTRACT_EXECUTION_DEADLINE_DAYS`, default 14); expiry returns the listing to search with NO trust penalty yet (fault attribution needed first — P1 follow-up + hook comment in the model); terms flow = either party proposes, the other agrees
- `internal/models/contract.go`: one contract per offer (unique index), denormalized buyer/seller ids (immutable, set from the stored offer/caller), states draft → terms_agreed → buyer_signed|seller_signed → executed, plus cancelled/expired
- Terms semantics: PUT terms (move-in/transfer dates + free-text conditions ≤5000) auto-agrees the proposer and voids the other party's agreement AND all signatures — signing can never bind terms a party hasn't agreed to; sign only from terms_agreed onward; both signatures → executed (property STAYS pending; escrow flips to sold later)
- Cancel (either party, pre-executed) and lazy expiry (checked on every endpoint access, no background jobs) both flip contract status and revert property pending → active in one transaction, revert guarded on current pending status
- Endpoints: GET/PUT terms/POST agree-terms/POST sign/POST cancel under `/properties/:id/offers/:offerId/contract` + `GET /users/me/contracts`; parties-only 403s derived from the stored contract row
- Implementation note: GORM auto-wraps `Updates(map)` in its own transaction when the model carries belongs-to associations — single-row updates are explicitly wrapped in `h.db.Transaction()` to keep sqlmock expectations deterministic
- 40 new tests (state transitions incl. re-proposal voiding signatures, both-role sign/cancel, expiry-on-read transaction, AcceptOffer INSERT ordering); evaluator APPROVE; coverage gate 91.8%
- Follow-ups tracked in backlog: conditions template library (P0), contract-expiry trust penalty (P1); iOS contract wizard is the client

## Viewing-slot concurrent-accept hardening 10-07-2026
- Partial unique index `idx_viewing_requests_one_accepted_per_slot` on viewing_requests (slot_id) WHERE status='accepted' — created idempotently in database.Connect after AutoMigrate (partial indexes aren't expressible via GORM tags); server refuses to start if creation fails
- AcceptRequest maps a unique-constraint violation inside the transaction to the same 409 SLOT_BOOKED body as the pre-check (reuses isUniqueViolation from trust.go); other errors still 500; rollback verified
- Accepted-row UPDATE runs before competitor auto-decline so the index can't false-positive; cancelled acceptances don't block re-booking (index covers only accepted rows)
- 2 new sqlmock tests; evaluator APPROVE; coverage gate 91.1%

## Trust appeal process 08-07-2026
- `POST /api/v1/users/me/trust-appeal` (auth): a blocked account files an appeal with just a statement (≤2000 chars); eligibility = ≥1 confirmed trust event; one appeal per event (`trust_appeals.trust_event_id` unique) and one pending appeal per user (409 `APPEAL_PENDING`)
- Minimal disclosure: success returns exactly `{data:{status:"pending"}}`; "never blocked", "all events already appealed", and the unique-violation race all funnel through one helper → byte-identical 409 `NO_APPEAL_AVAILABLE`, so the endpoint can't be used to fingerprint an account's trust state
- `TrustAppeal` model all `json:"-"`, no GET/adjudication endpoints; resolution is manual ops — exact uphold/overturn SQL documented on the model (overturn = appeal overturned + linked event dismissed in one tx; the block lifts automatically because enforcement derives from confirmed events)
- Coverage gate discovered mid-commit: the pre-commit hook enforces ≥90% against the working tree — evaluators must check it, not just `make test`. 19 purposeful tests added → 91.0%

## Hidden bidirectional trust system: events, reports, enforcement 07-07-2026
- Design: append-only `trust_events` table (`internal/models/trust.go`) — judgment is DERIVED from events, never stored as a score column; every field `json:"-"`, no GET endpoints, no Preloads — structurally invisible to users (evaluator leak-sweep verified, tests assert no "trust"/"flag" substrings in any response)
- Event types: `offer_default` (buyer), `deed_default`/`document_fraud` (seller); status `pending_review`/`confirmed`/`dismissed` — the adjudication gate: objective events auto-confirm, accusations wait
- `POST .../offers/:offerId/report-nonpayment` (seller): server verifies accepted + payment deadline strictly past → single transaction: offer → `defaulted`, confirmed event insert, property reverts pending → active; duplicate → 409 via composite unique `(offer_id, event_type)`
- `POST .../offers/:offerId/report-seller` (buyer on the offer, accepted only): validated violation enum + notes → `pending_review` event, 204 no body — does NOT enforce until manually confirmed (no adjudication endpoints by design; manual DB ops until the admin surface P1)
- Enforcement (confirmed events only, role-isolated): buyer w/ offer_default blocked from SubmitOffer; seller w/ deed_default|document_fraud blocked from CreateProperty + AcceptOffer — neutral 403s. UpdateProperty/viewings/withdraw deliberately ungated
- `PaymentDeadline` stamped on the offer inside the existing AcceptOffer transaction (`PAYMENT_DEADLINE_HOURS` config, default 72); offer wire contract gains `payment_deadline` + `defaulted` status
- When Stripe lands, its webhook becomes a second writer of the same events — model/enforcement/contract unchanged
- 17 new tests; evaluator APPROVE on a 10-point trust-specific checklist
- Non-blocking note: dismissed reports can't be re-filed for the same offer/type (unique index ignores status) — revisit with the admin surface

## Viewing scheduling API: slots + seller-approved requests 05-07-2026
- Product decisions: sellers post one-off dated time slots (no recurrence — a slot generator can add it later without wire changes); seller approves each request; one buyer per slot (accept auto-declines competing pending requests, same transaction pattern as AcceptOffer)
- `internal/models/viewing.go`: `ViewingSlot` (property child, start/end UTC) and `ViewingRequest` (slot + denormalized property FK, buyer, optional message, status pending/accepted/declined/cancelled)
- `internal/handlers/viewings.go`: 8 endpoints mirroring the offer handler — slot create/list (public, `booked` flag without leaking booker)/delete; request create/seller list/accept (tx auto-decline)/decline/buyer cancel; `GET /users/me/viewing-requests` mirrors ListMyOffers
- "Booked" is computed (count of accepted requests), not a stored flag — no stale-state on cancellation
- 18 sqlmock tests incl. transaction ordering assertions; evaluator-verified (APPROVE)
