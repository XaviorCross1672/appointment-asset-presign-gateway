#!/bin/sh
set -eu

curl --fail-with-body \
  --request POST \
  --header 'Content-Type: application/json' \
  --data '{"appointment_id":"apt_demo_1042","workflow_state":"scheduled","asset_kind":"referral","content_type":"application/pdf","size_bytes":2048}' \
  "${UPLOAD_GATEWAY_URL:-http://localhost:8080}/upload-intents"
