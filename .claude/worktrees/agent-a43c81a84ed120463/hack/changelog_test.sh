#!/usr/bin/env bash
# Table test for changelog.sh. Run directly: hack/changelog_test.sh
#
# The release notes are the only automated step between a user on an old
# version and a breaking change they have to act on, so the parser that decides
# whether the marker survives is worth pinning.
set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
CHANGELOG="${SCRIPT_DIR}/changelog.sh"
export UPGRADE_URL="https://example.com/README.md#upgrading"

failures=0

check() {
  local name="$1" mode="$2" input="$3" want="$4"
  local got

  got="$(printf '%s\n' "$input" | "$CHANGELOG" "$mode")"

  if [ "$got" != "$want" ]; then
    printf 'FAIL %s\n  want: %q\n  got:  %q\n' "$name" "$want" "$got"
    failures=$((failures + 1))
  else
    printf 'ok   %s\n' "$name"
  fi
}

# Same as check, but feeds stdin without a trailing newline. git log always ends
# with one; a file or a here-string built by hand may not.
check_unterminated() {
  local name="$1" mode="$2" input="$3" want="$4"
  local got

  got="$(printf '%s' "$input" | "$CHANGELOG" "$mode")"

  if [ "$got" != "$want" ]; then
    printf 'FAIL %s\n  want: %q\n  got:  %q\n' "$name" "$want" "$got"
    failures=$((failures + 1))
  else
    printf 'ok   %s\n' "$name"
  fi
}

# Asserts the script refuses with the status and message it is supposed to.
#
# Matching the exact status and the message matters more than it looks: an
# earlier version asserted only "non-zero" and used env --unset=, which is GNU
# coreutils syntax. BSD env rejects the flag and exits 1 without ever running
# the script, so the case passed while testing nothing — and since this suite
# runs under make test, that is the platform it runs on.
check_refuses() {
  local name="$1" want_status="$2" want_message="$3" input="$4"
  shift 4
  local status output

  output=$(printf '%s\n' "$input" | "$@" 2>&1)
  status=$?

  if [ "$status" -ne "$want_status" ]; then
    printf 'FAIL %s\n  wanted exit %d, got %d\n  output: %q\n' "$name" "$want_status" "$status" "$output"
    failures=$((failures + 1))
    return
  fi

  if [[ "$output" != *"$want_message"* ]]; then
    printf 'FAIL %s\n  wanted a message containing %q\n  got: %q\n' "$name" "$want_message" "$output"
    failures=$((failures + 1))
    return
  fi

  printf 'ok   %s\n' "$name"
}

# Every recognised type reaches the chart changelog under its own kind.
check "feat maps to added"        chart "feat: add a thing"        "added	add a thing"
check "fix maps to fixed"         chart "fix: repair a thing"      "fixed	repair a thing"
check "security maps to security" chart "security: patch a thing"  "security	patch a thing"
check "refactor maps to changed"  chart "refactor: reshape"        "changed	reshape"
check "docs maps to changed"      chart "docs: write it down"      "changed	write it down"
check "perf maps to changed"      chart "perf: speed it up"        "changed	speed it up"
check "build maps to changed"     chart "build: bump the image"    "changed	bump the image"

# A scope becomes part of the description; its parentheses are stripped.
check "scope is unwrapped" chart "fix(web): repair a thing" "fixed	web: repair a thing"

# The breaking marker is what this whole exercise is about.
check "breaking is labelled and points at the procedure" \
  chart "refactor(api)!: remove legacy fields" \
  "changed	BREAKING: api: remove legacy fields (see the Upgrading section in the README)"

# The marker labels the entry; it does not change which kind the type maps to.
check "breaking without a scope still labels" \
  chart "feat!: drop the old API" \
  "added	BREAKING: drop the old API (see the Upgrading section in the README)"

# Subjects outside the convention contribute nothing rather than crashing.
check "chore is not a release-note type" chart "chore(deps): bump something" ""
check "prose is not a subject"           chart "just some words"             ""
check "type without colon is ignored"    chart "feat add a thing"            ""

# Release notes carry the same decisions with markers a reader can see.
check "notes render an ordinary entry" \
  notes "fix(web): repair a thing" \
  "- 🐛 web: repair a thing"

check "notes flag a breaking entry and append the pointer" \
  notes "refactor(api)!: remove legacy fields" \
  "- ⚠️ **BREAKING** api: remove legacy fields

> This release contains a breaking change. Read the [Upgrading](https://example.com/README.md#upgrading) section before upgrading."

check "notes say so when nothing is user-facing" \
  notes "chore(deps): bump something" \
  "No user-facing changes in this release."

# The banner is set inside the read loop and printed after it. A pipe would run
# that loop in a subshell and lose the flag; the here-string in the caller does
# not. This case is what tells the two apart.
multi="fix: repair a thing
refactor(api)!: remove legacy fields
docs: write it down"

check "banner survives a multi-entry range" \
  notes "$multi" \
  "- 🐛 repair a thing
- ⚠️ **BREAKING** api: remove legacy fields
- 🔄 write it down

> This release contains a breaking change. Read the [Upgrading](https://example.com/README.md#upgrading) section before upgrading."

check "non-breaking range gets no banner" \
  notes "fix: repair a thing
docs: write it down" \
  "- 🐛 repair a thing
- 🔄 write it down"

# Input without a trailing newline. read fails at EOF having already filled the
# variable, so the last subject used to vanish — and the last subject is exactly
# where a breaking change tends to sit.
check_unterminated "unterminated single entry is not dropped" \
  notes "refactor(api)!: remove legacy fields" \
  "- ⚠️ **BREAKING** api: remove legacy fields

> This release contains a breaking change. Read the [Upgrading](https://example.com/README.md#upgrading) section before upgrading."

check_unterminated "unterminated last entry keeps its banner" \
  notes "fix: repair a thing
refactor(api)!: remove legacy fields" \
  "- 🐛 repair a thing
- ⚠️ **BREAKING** api: remove legacy fields

> This release contains a breaking change. Read the [Upgrading](https://example.com/README.md#upgrading) section before upgrading."

check_unterminated "unterminated input works for chart mode too" \
  chart "fix: repair a thing" \
  "fixed	repair a thing"

# The banner carries one link and nothing else, so an unset URL has to stop the
# run rather than render [Upgrading](). env -u is understood by both BSD and GNU.
check_refuses "breaking entry without UPGRADE_URL refuses" \
  1 "UPGRADE_URL" \
  "refactor(api)!: remove legacy fields" \
  env -u UPGRADE_URL "$CHANGELOG" notes

# A non-breaking range has no banner to fill in, so it must not start demanding
# the variable either.
check "no banner means UPGRADE_URL is not needed" \
  notes "fix: repair a thing" \
  "- 🐛 repair a thing"

# An unknown mode used to be caught only if some subject matched, so a typo over
# a range of chore-only commits exited 0 with no output.
check_refuses "unknown mode refuses on non-matching input" \
  2 "unknown mode: bogus" "chore(deps): bump something" "$CHANGELOG" bogus

check_refuses "unknown mode refuses on matching input" \
  2 "unknown mode: bogus" "fix: repair a thing" "$CHANGELOG" bogus

if [ "$failures" -ne 0 ]; then
  printf '\n%d case(s) failed\n' "$failures"
  exit 1
fi

printf '\nall cases passed\n'
