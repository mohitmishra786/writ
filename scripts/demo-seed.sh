#!/usr/bin/env bash
# Populate a freshly reset demo with real activity through the public API,
# the same endpoints the console's scenario buttons call: a compromised agent
# that the kernel blocks, one procurement run (Channel3 benchmark, A2A
# quotes, PayPal sandbox charge), and a supplier settlement through PayPal
# Payouts. Nothing is written to the database directly.
#
# Usage: scripts/demo-seed.sh [api-base]   (default: the hosted API)
set -euo pipefail
API="${1:-https://mandate-api-1f5y.onrender.com}"
json() { python3 -c "import json,sys; d=json.load(sys.stdin); print($1)"; }

M=$(curl -fsS "$API/v1/mandates" | json "next(m['id'] for m in d['mandates'] if m['status']=='ACTIVE' and m['has_vault'])")
echo "mandate   $M"

curl -fsS -X POST "$API/v1/procure/simulate-compromise" -H 'Content-Type: application/json' -d "{\"mandate_id\":\"$M\"}" \
  | json "'blocked   ' + d['outcome'] + ' ' + ','.join(d['reasons'] or [])"

RUN=$(curl -fsS -X POST "$API/v1/procure/runs" -H 'Content-Type: application/json' \
  -d "{\"mandate_id\":\"$M\",\"sku\":\"pitcher-20oz\",\"simulate_decline\":false}" | json "d['run_id']")
echo "agent     $RUN"
for _ in $(seq 1 40); do
  STATUS=$(curl -fsS "$API/v1/procure/runs/$RUN" | json "d['status']")
  [ "$STATUS" != RUNNING ] && break
  sleep 6
done
curl -fsS "$API/v1/procure/runs/$RUN" | json "'          ' + d['status'] + ': ' + (d.get('final') or d.get('error') or '')"

sleep 3
curl -fsS -X POST "$API/v1/mandates/$M/settle" | json "'payout    batch ' + str(d.get('payout_batch_id')) + ' ' + str(d.get('status'))"
curl -fsS "$API/v1/audit/verify" | json "'audit     valid=' + str(d['valid']) + ', ' + str(d['entries']) + ' entries'"
