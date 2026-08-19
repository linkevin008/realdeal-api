# Responsibilities
- Houses API request logic to communicate between services

# Commands
- Unit tests: `make test` (= `go test ./...`)
- Coverage gate: `make test-cover` — the pre-commit hook enforces ≥90% total coverage and runs the full suite against the WORKING TREE; changes must clear both before any commit attempt (evaluators: check this, not just `make test`)
- Build: `make build` (or `go build ./...` for a compile check)
- Vet: `go vet ./...`
- Full local stack (gateway + core + lookup + postgres + LocalStack): `make up` / `make down`
- Dev loop (infra containers + `go run` core): `make dev`
- Integration suite (starts stack, runs, tears down): `make test-integration`
- Smoke against a deployed environment: `API_BASE_URL=<url> make smoke`

# Backlog
Priority: P0 = important/must have, P1 = need to have but not crucial, P2 = nice to have

- [x][P0] Implement photo saving logic, we will be using S3 to store the images
- [ ][P0] Implement a price recommendation feature for suggesting prices based on market heuristics and the property in relation to nearby listings. We should also come up with our own algorithm after getting the relative market price since we are going to eliminate agent fees (this will require the pricing backlog item)
- [ ][P0] Implement a way to validate a listing is owned by the person who is listing it
- [x][P0] Implement a way to schedule a viewing → API done (one-off slots, seller-approved requests, one buyer per slot); iOS UI tracked in realdeal-ios backlog
- [x][P0] Decide on if we want to enlist realtors as part of the service? → No. Agents/realtors are removed from the product; RealDeal is direct buyer↔seller
- [x][P0] Remove the agent user role and all agent-specific logic/tests — 2-user model (buyers + sellers), no agent concept (coordinate with realdeal-ios) → backend was already clean (UserRole is buyer/seller only, no license fields); the removal was iOS-only, done in realdeal-ios
- [x][P0] Hidden per-account trustworthiness score, bidirectional: flag a buyer whose accepted bid goes unpaid (seller-reported, objectively verified, auto-confirmed → blocked from bidding); buyers can report seller violations (deed non-delivery, inauthentic documents) which land pending_review and only enforce (no listing/accepting) once confirmed. Score/events never shown to users
- [x][P0] Trust appeal process: a blocked account can file one appeal per enforcing event (POST /users/me/trust-appeal, statement only, no event details disclosed); overturned → event dismissed and the block lifts automatically (enforcement is derived); upheld → closed. Builds on the trust_events core
- [ ][P1] Admin adjudication surface for pending_review trust events AND pending appeals (confirm/dismiss/uphold/overturn) — manual DB ops until then
- [ ][P1] Deed handover service: seller document upload + authenticity verification as part of the post-acceptance transaction flow; anchors objective deed-delivery deadlines for trust enforcement (depends on contract wizard/escrow)
- [x][P0] Implement a way for a seller to recieve multiple offers from multiple people and for them to decide on the offer they want
- [ ][P0] Design a way to verify the buyer is able to buy the property, also do we need to conect with lenders?
- [ ][P0] Come up with ways we can keep the users on the platform (act as escrow, verification, etc.)
- [ ][P0] Deicde on pricing
- [x][P0] If an offer is selected, there needs to be a binding contract else a penalty and the sale needs to happen within a certain amount of time → contract auto-created on accept with a 14-day execution deadline (expired contracts return the listing to search); the PENALTY piece is deferred until fault can be attributed — tracked as a follow-up below
- [ ][P0] Are we only going to allow buyers who reside in the same country?
- [ ][P0] Do we need another repo to handle different service logic? or use this one?
- [x][P0] Make sure we are only displaying listings that are available and not sold or removed
- [x][P1] Harden viewing-slot booking against concurrent accepts: add a partial unique index on viewing_requests `(slot_id) WHERE status='accepted'` (and handle the constraint-violation error as a 409 in AcceptRequest) — closes the read-then-write race two simultaneous accepts/last-slot requests can hit
- [ ][P1] Implement import logic from real estate listing services
- [x][P0] Implement buying functionality where a buyer can submit an offer that is the price listed or more
- [x][P0] If a seller has accepted a buyer's offer the listing should be in a PROCESSING state where it is not displayed during this time
- [ ][P0] Our service has to integrate or devise some kind of payment service to facilitate the transaction → Decision: start with Stripe-managed escrow/payments (Stripe handles KYC), but put it behind our own payment-provider interface so migrating to another provider later is a two-way door
- [x][P0] Implement document signing → real signing state machine (terms → both agree → both sign → executed); documents themselves stubbed per MVP
- [x][P0] Implement when confirming the deal, move in date, transfer date, and any other dates needed. Both parties will need to agree to the conditions → propose/agree terms flow on the contract; any change after a signature voids signatures
- [ ][P0] Define the template for conditions and sale (conditions are free text ≤5000 chars on the contract for MVP; this item = structured template library)
- [ ][P1] `make push` prints a ready-to-paste `deploy-compute` command that omits `LOOKUP_DESIRED_COUNT=0`. On a FIRST deploy that command deadlocks: lookup connects as `lookup_ro`, which does not exist yet, so its tasks crash-loop, LookupService never reaches steady state, and the compute stack cannot reach CREATE_COMPLETE. Either add `LOOKUP_DESIRED_COUNT=0` to the printed line or point at the realdeal-infra README runbook instead of printing a copy-paste command (found during the 18-08-2026 deployment rehearsal; realdeal-infra 7dda093)
- [ ][P1] Contract-expiry penalty: create a trust event against the party at fault when a contract expires unsigned — needs fault attribution (whose signature/agreement was missing); hook noted in internal/models/contract.go

- [x][P0] Restructure into a multi-service monorepo with a local AWS-modeled environment (nginx gateway ≙ ALB, container per service ≙ ECS, LocalStack ≙ S3/SecretsManager)
- [x][P0] Implement lookup service — listing search & browse (read-only over core's data)
- [x][P0] HTTP integration test suite that runs locally through the gateway and later against the real ALB via API_BASE_URL
