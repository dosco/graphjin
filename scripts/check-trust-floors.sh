#!/usr/bin/env bash
# Fast static guard for the eMudhra intermediate CA compromise (October 2026).
# Eight eMudhra intermediate keys were recoverable from their certificates.
# Clients that do not fetch browser revocation lists stay exposed until their
# runtime or CA bundle changes, so the images and toolchains must not slip
# below the first fixed releases. See https://emudhra.we-have-a-problem.com/.
set -euo pipefail

fail=0
at_least() { # label have want
  if [ "$(printf '%s\n%s\n' "$3" "$2" | sort -V | head -n1)" != "$3" ]; then
    echo "$1 is $2; the eMudhra fix needs $3 or later" >&2
    fail=1
  fi
}
one() { # label value: a missing pin must fail, not pass silently
  if [ -z "$2" ]; then
    echo "$1 is not pinned to a version" >&2
    fail=1
    return 1
  fi
}

dockerfile=Dockerfile

while read -r tag; do
  one "node image" "$tag" && at_least "node image $tag" "${tag%%-*}" 24.22.1
done < <(sed -n 's/^FROM node:\([^ ]*\) .*/\1/p' "$dockerfile")
if grep -Eq '^FROM node:[0-9]+-' "$dockerfile"; then
  echo "a node image uses a floating major tag; pin the patch release" >&2
  fail=1
fi

go_image=$(sed -n 's/^FROM golang:\([0-9.]*\)-.*/\1/p' "$dockerfile" | head -n1)
one "golang image" "$go_image" && at_least "golang image" "$go_image" 1.26.10

python_image=$(sed -n 's/^FROM python:\([0-9.]*\)-.*/\1/p' "$dockerfile" | head -n1)
one "python image" "$python_image" && at_least "python image" "$python_image" 3.13.17

pip=$(sed -n 's/.*pip install --no-cache-dir pip==\([0-9.]*\).*/\1/p' "$dockerfile" | head -n1)
one "pip" "$pip" && at_least "pip" "$pip" 26.2.2

certifi=$(sed -n 's/^certifi==\([0-9.]*\).*/\1/p' apps/embedding/requirements.txt)
one "certifi" "$certifi" && at_least "certifi" "$certifi" 2026.10.19

# Every Debian stage that installs ca-certificates checks the installed version.
installs=$(grep -c 'apt-get install.*ca-certificates' "$dockerfile" || true)
guards=$(grep -c 'ge 20250419~deb12u2' "$dockerfile" || true)
if [ "$installs" -gt "$guards" ]; then
  echo "$installs Debian stages install ca-certificates but only $guards check for 20250419~deb12u2" >&2
  fail=1
fi

for work in go.work graphjin/go.work apps/openneko/go.mod graphjin/cmd/go.mod; do
  version=$(sed -n 's/^go \([0-9.]*\)$/\1/p' "$work")
  one "$work go directive" "$version" && at_least "$work go directive" "$version" 1.26.10
done

while read -r version; do
  at_least "workflow go-version" "$version" 1.26.10
done < <(grep -ho 'go-version: "[0-9.]*"' .github/workflows/*.yml | sed 's/.*"\(.*\)"/\1/')
while read -r version; do
  at_least "workflow node-version" "$version" 24.22.1
done < <(grep -ho 'node-version: [0-9.]*' .github/workflows/*.yml | sed 's/node-version: //')

exit "$fail"
