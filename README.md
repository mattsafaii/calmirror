# CalMirror

Mirror your HEY calendars (through the [hey CLI](https://github.com/basecamp/hey-cli))
or any ICS calendar feed (Basecamp, anything) into your real calendar as **full-fidelity native events** — preserving the conferencing/meeting
URL, location, notes, and recurrence — refreshed on a schedule you control.

Built-in ICS subscriptions in Apple and Google Calendar refresh too slowly and
land as read-only events that downstream meeting tools ignore. CalMirror
materializes the feed as real, native events you (and your tools) can actually
use.

> **Status: Phase 1 — headless engine + iCloud.** A Go CLI that mirrors ICS
> feeds into **iCloud via CalDAV**. macOS-only. Google Calendar, a menu-bar UI,
> and a Homebrew tap are later phases (see [Roadmap](#roadmap)).

## How it works

1. You add a feed: an ICS source URL, a destination calendar name, and your
   iCloud app-specific password (stored in the **macOS Keychain**, never on disk).
2. CalMirror creates and owns a **dedicated CalDAV calendar per feed** (e.g.
   "HEY (synced)"), so it never touches your hand-made events.
3. On each sync it fetches the feed, parses every field, diffs against local
   state **by ICS `UID`**, and creates / updates / deletes events in the mirror —
   carrying the conferencing URL, location, notes, alarms, recurrence (RRULE),
   and timezones across intact.
4. A launchd login-item runs the sync on a schedule; persistent feed failures
   (e.g. an expired token returning 404) surface as a notification and in
   `calmirror status`, without disturbing other feeds.

## Requirements

- macOS
- Go 1.26+ (to build)
- An iCloud **app-specific password** — generate one at
  [appleid.apple.com](https://appleid.apple.com) → Sign-In & Security →
  App-Specific Passwords. Your normal Apple ID password won't work over CalDAV
  with 2FA.

## Install

```sh
go install github.com/mattsafaii/calmirror/cmd/calmirror@latest
```

This drops the `calmirror` binary in `$(go env GOPATH)/bin` (usually `~/go/bin`;
make sure it's on your `PATH`).

## Sources

Each feed reads from one source:

- **`hey`** — HEY calendars, read through the hey CLI. Install it from
  [basecamp/hey-cli](https://github.com/basecamp/hey-cli) and run
  `hey auth login`; the CLI owns your HEY sign-in, so CalMirror stores no HEY
  credential and there is no feed URL to expire. Notes, location, the meeting
  join link, and time zones carry across. Repeating events are mirrored as
  **individual occurrences** inside the sync window (60 days ahead by default),
  so one-day edits and cancellations come through exactly. A calendar must be
  switched on in HEY to be read.
- **`ics`** (the default) — any ICS feed URL, such as Basecamp's schedule feed.
  Recurrence passes through as RRULE.

## Usage

```sh
# List your HEY calendars, then mirror one into Google Calendar.
calmirror setup --source hey --kind google --feed hey --calendar "HEY (synced)"
calmirror setup --source hey --hey-calendar 123456 --kind google \
  --google-client-id <id> --feed hey --calendar "HEY (synced)"

# Mirror an ICS feed into iCloud, storing the app-specific password in the
# Keychain. Prompts for it (or reads $CALMIRROR_ICLOUD_PASSWORD).
calmirror setup \
  --icloud-user you@icloud.com \
  --feed basecamp \
  --url "https://3.basecamp.com/…/schedule.ics" \
  --calendar "Basecamp (synced)"

# Run one sync pass across all configured feeds now.
calmirror sync

# Per-feed source, destination, last-sync time, event counts, and last error.
calmirror status

# Register / remove the launchd login-items: scheduled syncs, plus (when the
# hey CLI is installed) a watch agent that syncs as HEY calendars change.
calmirror install --interval 15   # minutes
calmirror uninstall

# Optional: run in the foreground — sync whenever a HEY calendar event changes.
# Requires the hey CLI (github.com/basecamp/hey-cli) and `hey auth login`;
# `calmirror install` wires this up automatically when hey is present. If hey
# fails (signed out, rate-limited), watch backs off from 1 to 30 minutes
# between retries instead of hammering HEY's sign-in.
calmirror watch
```

Configuration lives at `~/Library/Application Support/calmirror/config.json`
(feeds and iCloud username only). Local sync state is a SQLite database in the
same directory. **Secrets live in the macOS Keychain — never in config or the
database.**

## A note on meeting tools (Granola, Zoom)

CalMirror produces correct, full-fidelity native events with the conferencing
URL intact. However, **Granola reads Google Calendar and Outlook, not Apple /
iCloud Calendar** (and Zoom's desktop app is similar). So the "let Granola
auto-attach to a mirrored meeting" workflow needs the **Google Calendar**
destination, which is Phase 2 — not the iCloud target of Phase 1. The iCloud
mirror is still useful for the Apple ecosystem: native, editable events instead
of a slow read-only subscription.

## Roadmap

1. **Headless engine + iCloud** ✅ (this release)
2. Google Calendar destination — unlocks the Granola/Zoom auto-attach workflow
3. Menu-bar UI
4. Homebrew tap + launch

## Built with

- [arran4/golang-ical](https://github.com/arran4/golang-ical) — ICS parsing
- [emersion/go-webdav](https://github.com/emersion/go-webdav) + go-ical — CalDAV
- [modernc.org/sqlite](https://modernc.org/sqlite) — pure-Go SQLite (no cgo)

## License

[MIT](LICENSE) © 2026 Matt Safaii
