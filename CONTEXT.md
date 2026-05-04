# Responsibilities
- Houses API request logic to commnicate between services


# Backlog
- [x][P0] Implement photo saving logic, we will be using S3 to store the images
- [ ][P0} Implement a price recommendation feature for suggesting prices based on market heuristics and the property in relation to nearby listings. We should also come up with our own algorithm after getting the relative market price since we are going to eliminate agent fees (this will require the pricing backlog item)
- [ ][P0] Implement a way to validate a listing is owned by the person who is listing it
- [ ][P0] Implement a way to schedule a viewing 
- [ ][P0] Decide on if we want to enlist realtors as part of the service?
- [ ][P0] Implement a way for a seller to recieve multiple offers from multiple people and for them to decide on the offer they want
- [ ][P0] Design a way to verify the buyer is able to buy the property, also do we need to conect with lenders?
- [ ][P0] Come up with ways we can keep the users on the platform (act as escrow, verification, etc.)
- [ ][P0] Deicde on pricing
- [ ][P0] If an offer is selected, there needs to be a binding contract else a penalty and the sale needs to happen within a certain amount of time
- [ ][P0] Are we only going to allow buyers who reside in the same country?
- [ ][P0] Do we need another repo to handle different service logic? or use this one?
- [ ][P0] Make sure we are only displaying listings that are available and not sold or removed
- [ ][P1] Implement import logic from real estate listing services
- [ ][P0] Implement buying functionality where a buyer can submit an offer that is the price listed or more
- [ ][P0] If a seller has accepted a buyer's offer the listing should be in a PROCESSING state where it is not displayed during this time
- [ ][P0] Our service has to integrate or devise some kind of payment service to facilitate the transaction
- [ ][P0] Implement document signing 
- [ ][P0] Implement when confirming the deal, move in date, transfer date, and any other dates needed. Both parties will need to agree to the conditions
- [ ][P0] Define the template for conditions and sale 

# Context

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

