#!/usr/bin/env bash
#
# Wait until a deployment's /healthz reports an expected version.
#
# Moving a Lambda alias is not instant: invocations already in flight finish on
# the old version, so /healthz can answer with either tag for a moment
# afterwards. A single good response would therefore pass while production is
# still half switched, which is why this insists on a run of consecutive ones
# and starts the count over on any bad answer.
#
# Both directions of a deploy use this: the smoke test waits for the new tag,
# and a rollback waits for the previous one. They are the same question asked
# about different versions, so they are the same code.
#
# usage: poll-healthz.sh <base_url> <expected_version>
#
# env:
#   POLL_INTERVAL  seconds between attempts            (default 2)
#   POLL_TIMEOUT   seconds before giving up            (default 60)
#   POLL_STREAK    consecutive good answers required   (default 3)
#
# Exits 0 once the streak is reached, 1 if the timeout is reached first.

set -euo pipefail

if [ "$#" -ne 2 ]; then
  echo "usage: $0 <base_url> <expected_version>" >&2
  exit 2
fi

base_url="${1%/}"
expected="$2"

interval="${POLL_INTERVAL:-2}"
timeout="${POLL_TIMEOUT:-60}"
needed="${POLL_STREAK:-3}"

echo "Polling ${base_url}/healthz for version ${expected}" \
     "(${needed} consecutive, every ${interval}s, up to ${timeout}s)"

deadline=$((SECONDS + timeout))
streak=0
attempt=0

body=$(mktemp)
trap 'rm -f "$body"' EXIT

while [ "$SECONDS" -lt "$deadline" ]; do
  attempt=$((attempt + 1))

  # curl prints the status code itself, and 000 when it never got a response,
  # so a refused connection is just another failed attempt rather than an error
  # that kills the script.
  code=$(curl -sS -o "$body" -w '%{http_code}' --max-time 5 "${base_url}/healthz" || true)
  got=$(jq -r '.version // empty' "$body" 2>/dev/null || true)

  if [ "$code" = "200" ] && [ "$got" = "$expected" ]; then
    streak=$((streak + 1))
    echo "  attempt ${attempt}: 200, version ${got} (${streak}/${needed})"

    if [ "$streak" -ge "$needed" ]; then
      echo "OK: ${expected} reported on ${needed} consecutive checks."
      exit 0
    fi
  else
    if [ "$streak" -gt 0 ]; then
      echo "  attempt ${attempt}: HTTP ${code}, version '${got:-none}' — streak reset"
    else
      echo "  attempt ${attempt}: HTTP ${code}, version '${got:-none}'"
    fi
    streak=0
  fi

  sleep "$interval"
done

echo "FAILED: ${base_url}/healthz did not report ${expected} on ${needed}" \
     "consecutive attempts within ${timeout}s." >&2
exit 1
