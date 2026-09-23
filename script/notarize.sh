#!/usr/bin/env bash
# Exit 75 means Apple is still processing; every other nonzero exit is an error.
set -euo pipefail
set +x
artifact=${1:?Missing artifact}
label=${2:?Missing app/dmg label}
[[ "$label" == app || "$label" == dmg ]]
: "${BOT_NOTARY_REPORTS:?Missing report directory}"
: "${BOT_SIGN_KEYCHAIN:?Missing notary keychain}"
timeout=${BOT_NOTARY_WAIT_TIMEOUT:-60m}
[[ "$timeout" =~ ^[1-9][0-9]*[smh]$ ]]
mkdir -p "$BOT_NOTARY_REPORTS"
receipt="$BOT_NOTARY_REPORTS/$label-submission.json"
result="$BOT_NOTARY_REPORTS/$label.json"
digest_file="$BOT_NOTARY_REPORTS/$label.sha256"
digest=$(shasum -a 256 "$artifact" | cut -d ' ' -f 1)
auth=(--keychain-profile caelis-release --keychain "$BOT_SIGN_KEYCHAIN")

if [[ -f "$receipt" ]]; then
  # An existing ID can only be used with the exact bytes originally uploaded.
  [[ -f "$digest_file" && "$(cat "$digest_file")" == "$digest" ]] || {
    echo "Cannot resume $label: submitted artifact bytes changed." >&2; exit 1;
  }
else
  [[ ! -e "$digest_file" ]] || { echo 'Upload outcome is uncertain; inspect the existing request before retrying.' >&2; exit 1; }
  printf '%s\n' "$digest" > "$digest_file"
  xcrun notarytool submit "$artifact" "${auth[@]}" --output-format json > "$receipt"
fi
cat "$receipt"
submission=$(node -pe 'JSON.parse(require("node:fs").readFileSync(process.argv[1],"utf8")).id' "$receipt")
[[ "$submission" =~ ^[[:xdigit:]]{8}-[[:xdigit:]]{4}-[[:xdigit:]]{4}-[[:xdigit:]]{4}-[[:xdigit:]]{12}$ ]]
read_status() {
  xcrun notarytool info "$submission" "${auth[@]}" --output-format json > "$result"
  status=$(node -pe 'JSON.parse(require("node:fs").readFileSync(process.argv[1],"utf8")).status' "$result")
}
read_status
if [[ "$status" == 'In Progress' ]]; then
  # The authoritative info result after a timeout may already be Accepted.
  xcrun notarytool wait "$submission" "${auth[@]}" --timeout "$timeout" --output-format json \
    > "$BOT_NOTARY_REPORTS/$label-wait.json" || true
  read_status
fi
cat "$result"
case "$status" in
  Accepted)
    xcrun notarytool log "$submission" "${auth[@]}" "$BOT_NOTARY_REPORTS/$label-log.json"
    cat "$BOT_NOTARY_REPORTS/$label-log.json"
    node -e 'const fs=require("node:fs"),assert=require("node:assert/strict");
      const log=JSON.parse(fs.readFileSync(process.argv[1],"utf8"));
      assert.equal(log.status,"Accepted"); assert.equal(log.jobId,process.argv[2]);
      assert.equal(log.sha256,process.argv[3],"Apple receipt does not match the submitted bytes");' \
      "$BOT_NOTARY_REPORTS/$label-log.json" "$submission" "$digest"
    ;;
  'In Progress')
    echo "Apple is still processing $label submission $submission; retain its artifact and resume later."
    exit 75
    ;;
  Invalid|Rejected)
    xcrun notarytool log "$submission" "${auth[@]}" "$BOT_NOTARY_REPORTS/$label-log.json"
    cat "$BOT_NOTARY_REPORTS/$label-log.json"
    echo "Apple rejected $label: $status" >&2
    exit 1
    ;;
  *) echo "Unrecognized Apple status: $status" >&2; exit 1 ;;
esac
