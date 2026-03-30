# Pending Tasks — Requires External Infrastructure

These features are stubbed or absent in the API and ready to implement once the supporting
infrastructure is provisioned. CloudFormation templates will live in a separate repo.

---

## Image Upload (Presigned S3 URLs)

**Status:** Not implemented — API currently only accepts image URLs embedded in property requests.

**What's needed:**
- `POST /api/v1/upload/presign` endpoint that:
  1. Accepts a `{ filename, content_type }` body
  2. Generates a presigned S3 PUT URL (15 min expiry)
  3. Returns `{ upload_url, public_url }` so the client uploads directly to S3

**CloudFormation resources needed:**
- `AWS::S3::Bucket` with public read + CORS policy
- `AWS::CloudFront::Distribution` (serve public_url via CDN)
- `AWS::IAM::Role` with `s3:PutObject` permission for the API server

---

## Apple Sign In

**Status:** Not implemented.

**What's needed:**
- `POST /api/v1/auth/signin/apple` handler that:
  1. Fetches Apple's JWKS from `https://appleid.apple.com/auth/keys`
  2. Validates the identity token JWT
  3. Upserts a user record (email may be empty on repeat sign-ins)
  4. Returns the standard `authResponse`

**No AWS required.**

---

## Google Sign In

**Status:** Not implemented.

**What's needed:**
- `POST /api/v1/auth/signin/google` handler that:
  1. Fetches Google's JWKS from `https://www.googleapis.com/oauth2/v3/certs`
  2. Validates the ID token JWT
  3. Upserts a user record
  4. Returns the standard `authResponse`

**No AWS required.**

---

## Push Notifications

**Status:** Not started.

**What's needed:**
- `POST /api/v1/devices` to register/update APNs device tokens per user
- `DELETE /api/v1/devices/:token` to deregister on logout
- Notification dispatch logic (new matching listing, price drop, etc.)

**CloudFormation resources needed:**
- `AWS::SNS::PlatformApplication` for APNs
- `AWS::SNS::Topic` per notification category
- `AWS::IAM::Role` with SNS publish permission for the API server
