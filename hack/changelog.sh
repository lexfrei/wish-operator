#!/usr/bin/env bash
# Turns Conventional Commits subjects into changelog entries.
#
# Reads subjects on stdin, one per line, and writes to stdout:
#
#   chart  tab-separated "kind<TAB>description" lines, one per recognised
#          subject, for the Artifact Hub changelog annotation
#   notes  a markdown list for the GitHub release body, followed by a pointer
#          to the upgrade procedure when the range contains a breaking change
#
# Subjects that are not Conventional Commits, or carry a type outside the list,
# produce no entry. The `!` marker is the only breaking-change signal there is:
# the callers pass `git log --format=%s`, so a BREAKING CHANGE footer in a body
# never reaches this script.
set -euo pipefail

MODE="${1:?usage: changelog.sh chart|notes}"
UPGRADE_URL="${UPGRADE_URL:-}"

# Validated before reading anything. Checked per-commit it would only be reached
# when some subject matched, so a typo'd mode over a range of chore-only commits
# used to exit 0 with no output.
case "$MODE" in
  chart | notes) ;;
  *)
    echo "unknown mode: $MODE" >&2
    exit 2
    ;;
esac

entries=""
has_breaking=""

# The `|| [ -n "$commit" ]` keeps the last line when stdin has no trailing
# newline: read reports failure at EOF even though it filled the variable, and
# without this the final subject is silently dropped. Losing precisely that line
# is how a breaking change ships looking like an ordinary one.
while IFS= read -r commit || [ -n "$commit" ]; do
  [ -z "$commit" ] && continue

  if [[ "$commit" =~ ^(feat|fix|security|refactor|docs|perf|build)(\([^\)]+\))?(!)?:\ (.+)$ ]]; then
    TYPE="${BASH_REMATCH[1]}"
    SCOPE="${BASH_REMATCH[2]}"
    BREAKING="${BASH_REMATCH[3]}"
    DESC="${BASH_REMATCH[4]}"
    SCOPE="${SCOPE#\(}"
    SCOPE="${SCOPE%\)}"

    if [ -n "$SCOPE" ]; then
      FULL_DESC="${SCOPE}: ${DESC}"
    else
      FULL_DESC="${DESC}"
    fi

    if [ -n "$BREAKING" ]; then
      has_breaking=1
    fi

    case "$MODE" in
      chart)
        case "$TYPE" in
          feat) KIND="added" ;;
          fix) KIND="fixed" ;;
          security) KIND="security" ;;
          *) KIND="changed" ;;
        esac

        # A breaking change reads as an ordinary entry otherwise, which is how
        # somebody upgrades past a migration step they needed to run.
        if [ -n "$BREAKING" ]; then
          FULL_DESC="BREAKING: ${FULL_DESC} (see the Upgrading section in the README)"
        fi

        entries="${entries}${KIND}	${FULL_DESC}"$'\n'
        ;;
      notes)
        case "$TYPE" in
          feat) emoji="✨" ;;
          fix) emoji="🐛" ;;
          security) emoji="🔒" ;;
          *) emoji="🔄" ;;
        esac

        PREFIX=""
        if [ -n "$BREAKING" ]; then
          emoji="⚠️"
          PREFIX="**BREAKING** "
        fi

        entries="${entries}- ${emoji} ${PREFIX}${FULL_DESC}"$'\n'
        ;;
    esac
  fi
done

if [ "$MODE" = "notes" ]; then
  if [ -z "$entries" ]; then
    echo "No user-facing changes in this release."
    exit 0
  fi

  # Without this the upgrade procedure is only discoverable by reading the
  # README, which is not where somebody upgrading a chart version looks.
  if [ -n "$has_breaking" ]; then
    # The banner exists to carry this link and nothing else. An empty one renders
    # as [Upgrading]() and reads as a broken page rather than a missing step, so
    # refuse to emit it instead.
    : "${UPGRADE_URL:?a breaking change needs UPGRADE_URL set to the upgrade procedure}"

    entries="${entries}"$'\n'"> This release contains a breaking change. Read the [Upgrading](${UPGRADE_URL}) section before upgrading."$'\n'
  fi
fi

printf '%s' "$entries"
