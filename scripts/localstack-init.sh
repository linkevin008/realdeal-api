#!/bin/sh
# Runs inside LocalStack once it is ready (mounted at /etc/localstack/init/ready.d/).
# Creates local stand-ins for the resources defined in
# realdeal-infra/cloudformation/media.yaml — same bucket semantics, zero AWS cost.
set -e

awslocal s3 mb s3://realdeal-media-local 2>/dev/null || true

# Mirror the CORS config from media.yaml (PUT/GET from any origin)
awslocal s3api put-bucket-cors --bucket realdeal-media-local --cors-configuration '{
  "CORSRules": [
    {
      "AllowedHeaders": ["*"],
      "AllowedMethods": ["PUT", "GET"],
      "AllowedOrigins": ["*"]
    }
  ]
}'

echo "LocalStack init complete: s3://realdeal-media-local"
