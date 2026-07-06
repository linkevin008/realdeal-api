# Responsibilities
- Houses API request logic to communicate between services

# Commands
- Unit tests: `make test` (= `go test ./...`)
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
- [ ][P0] Implement a way to schedule a viewing
- [x][P0] Decide on if we want to enlist realtors as part of the service? → No. Agents/realtors are removed from the product; RealDeal is direct buyer↔seller
- [x][P0] Remove the agent user role and all agent-specific logic/tests — 2-user model (buyers + sellers), no agent concept (coordinate with realdeal-ios) → backend was already clean (UserRole is buyer/seller only, no license fields); the removal was iOS-only, done in realdeal-ios
- [ ][P0] Hidden per-account trustworthiness score: flag a buyer whose accepted bid goes unpaid, and block flagged accounts from submitting any further bids (score is never shown to users)
- [x][P0] Implement a way for a seller to recieve multiple offers from multiple people and for them to decide on the offer they want
- [ ][P0] Design a way to verify the buyer is able to buy the property, also do we need to conect with lenders?
- [ ][P0] Come up with ways we can keep the users on the platform (act as escrow, verification, etc.)
- [ ][P0] Deicde on pricing
- [ ][P0] If an offer is selected, there needs to be a binding contract else a penalty and the sale needs to happen within a certain amount of time
- [ ][P0] Are we only going to allow buyers who reside in the same country?
- [ ][P0] Do we need another repo to handle different service logic? or use this one?
- [x][P0] Make sure we are only displaying listings that are available and not sold or removed
- [ ][P1] Implement import logic from real estate listing services
- [x][P0] Implement buying functionality where a buyer can submit an offer that is the price listed or more
- [x][P0] If a seller has accepted a buyer's offer the listing should be in a PROCESSING state where it is not displayed during this time
- [ ][P0] Our service has to integrate or devise some kind of payment service to facilitate the transaction → Decision: start with Stripe-managed escrow/payments (Stripe handles KYC), but put it behind our own payment-provider interface so migrating to another provider later is a two-way door
- [ ][P0] Implement document signing
- [ ][P0] Implement when confirming the deal, move in date, transfer date, and any other dates needed. Both parties will need to agree to the conditions
- [ ][P0] Define the template for conditions and sale

- [x][P0] Restructure into a multi-service monorepo with a local AWS-modeled environment (nginx gateway ≙ ALB, container per service ≙ ECS, LocalStack ≙ S3/SecretsManager)
- [x][P0] Implement lookup service — listing search & browse (read-only over core's data)
- [x][P0] HTTP integration test suite that runs locally through the gateway and later against the real ALB via API_BASE_URL
