# CalMirror

Mirror any ICS calendar feed (HEY Calendar, Basecamp, anything) into your real
calendar as **full-fidelity native events** — preserving the conferencing/meeting
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

## Usage

```sh
# Add a feed and store the iCloud app-specific password in the Keychain.
# Prompts for the password (or reads $CALMIRROR_ICLOUD_PASSWORD).
calmirror setup \
  --icloud-user you@icloud.com \
  --feed hey \
  --url "https://app.hey.com/calendars/…/feed" \
  --calendar "HEY (synced)"

# Run one sync pass across all configured feeds now.
calmirror sync

# Per-feed last-sync time, event counts, and last error.
calmirror status

# Register / remove the launchd login-item that syncs on a schedule.
calmirror install --interval 15   # minutes
calmirror uninstall
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
