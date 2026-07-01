#!/usr/bin/env bash
# Reconcile due .trivyignore.yaml entries against a fresh Trivy scan.
#
# Called by .github/workflows/trivy-ignore-audit.yml. Inputs (env):
#   DUE        space-separated CVE ids whose expiry is near/past
#   SCAN_JSON  path to Trivy JSON output scanned WITHOUT the ignore file
#   GH_TOKEN   token for gh
#
# For each due id: if it's no longer detected (fix landed) it's removed from
# .trivyignore.yaml and a PR is opened; if it's still detected, a re-triage
# issue is opened/refreshed.
set -euo pipefail

: "${DUE:?DUE is required}"
: "${SCAN_JSON:?SCAN_JSON is required}"

present=$(jq -r '.Results[]?.Vulnerabilities[]?.VulnerabilityID' "$SCAN_JSON" | sort -u)

fixed=""
still=""
for id in $DUE; do
  if printf '%s\n' "$present" | grep -qx "$id"; then
    still="$still $id"
  else
    fixed="$fixed $id"
  fi
done
fixed=$(echo "$fixed" | xargs || true)
still=$(echo "$still" | xargs || true)
echo "Fixed (removable): ${fixed:-none}"
echo "Still vulnerable:  ${still:-none}"

# --- Fixed CVEs: open a PR removing the now-inert entries ---
if [ -n "$fixed" ]; then
  for id in $fixed; do
    yq -i "del(.vulnerabilities[] | select(.id == \"$id\"))" .trivyignore.yaml
  done
  if ! git diff --quiet .trivyignore.yaml; then
    branch="chore/trivy-ignore-cleanup"
    git config user.name "github-actions[bot]"
    git config user.email "github-actions[bot]@users.noreply.github.com"
    git checkout -B "$branch"
    git add .trivyignore.yaml
    git commit -s -m "chore: drop trivy ignores for fixed CVEs ($fixed)"
    git push -f origin "$branch"
    if ! gh pr view "$branch" >/dev/null 2>&1; then
      gh pr create --head "$branch" --label infrastructure \
        --title "chore: remove fixed trivy ignore entries" \
        --body "Automated by \`trivy-ignore-audit\`. These deferred CVEs are no longer detected in the image (the fix reached the base image), so their \`.trivyignore.yaml\` entries are inert and removed here: **$fixed**."
    fi
  fi
fi

# --- Still-vulnerable CVEs: open/refresh a re-triage issue ---
if [ -n "$still" ]; then
  title="Trivy ignore review due: still-vulnerable CVE(s)"
  body=$(cat <<EOF
One or more \`.trivyignore.yaml\` deferrals are at/near expiry **and still present** in the image, so they will fail the Trivy image scan once expired.

**CVEs:** $still

### Re-triage each one
1. Re-scan locally to confirm current status:
   \`\`\`
   docker build -t technician:scan .
   docker run --rm -v /var/run/docker.sock:/var/run/docker.sock \\
     aquasec/trivy:latest image --severity CRITICAL,HIGH --scanners vuln \\
     --ignore-unfixed technician:scan
   \`\`\`
2. If a fixed base image is now available: bump the base digest (Renovate usually does this). The entry becomes inert and this audit will open a PR to remove it.
3. If still no fix: extend \`expired_at\` in \`.trivyignore.yaml\` with an updated \`statement\`, or reclassify (e.g. \`not_affected\` VEX) if you can justify non-reachability.

_Opened automatically by the \`trivy-ignore-audit\` workflow._
EOF
  )
  existing=$(gh issue list --state open --search "$title in:title" --json number --jq '.[0].number // ""')
  if [ -n "$existing" ]; then
    gh issue comment "$existing" --body "$body"
  else
    gh issue create --title "$title" --label infrastructure --body "$body"
  fi
fi
