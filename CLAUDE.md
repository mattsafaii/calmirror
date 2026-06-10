# CalMirror

## What this is

CalMirror mirrors any ICS calendar feed (HEY Calendar, Basecamp BC5, anything) into a user's real calendars as **full-fidelity native events** — preserving the conferencing/meeting URL, location, notes, and recurrence — so meeting tools like Granola and Zoom can attach and record. Built-in ICS subscriptions in Apple/Google Calendar refresh too slowly and land as read-only events downstream tools ignore; CalMirror materializes the feed as real events, refreshed on a schedule the user controls. Origin: a Basecamp Community post by JJ Thorp asking how to keep HEY + Basecamp + Google calendars in sync.

The full product is a ~6-week, 4-phase effort: (1) headless engine + iCloud, (2) Google Calendar, (3) menu-bar UI, (4) Homebrew tap + launch.

## This cycle: Phase 1 — Headless engine + iCloud

A Go CLI (no GUI yet) that mirrors ICS feeds into **iCloud via CalDAV**. This is the non-negotiable core. Phases 2–4 are later cycles.

## Stack

- **Go 1.26**, macOS-only.
- **SQLite** for local state (UID ↔ CalDAV event mapping).
- **iCloud CalDAV** as the destination, authenticated with an **app-specific password stored in the macOS Keychain** (never in config or SQLite).
- **launchd** login-item for scheduling — the tool installs/removes its own job; no manual plist editing.
- Ask before adding dependencies. Likely candidates: an ICS parser (e.g. `github.com/arran4/golang-ical`), a CalDAV client (e.g. `github.com/emersion/go-webdav`/`go-ical`), a Keychain wrapper, and a SQLite driver (`modernc.org/sqlite` is pure-Go, no cgo — preferred). Confirm choices as you go.

## Surfaces (CLI, this phase)

- **`calmirror setup`** — add a feed (source ICS URL + destination calendar name); store the iCloud app-specific password in Keychain.
- **`calmirror sync`** — run one sync pass across all configured feeds now.
- **`calmirror status`** — per-feed last-sync time, event counts, last error.
- **`calmirror install` / `uninstall`** — register/remove the launchd login-item that runs sync on a schedule.

## Core flow

1. User runs `setup`, providing an ICS URL, a destination calendar name, and the iCloud app-specific password (→ Keychain).
2. CalMirror creates and owns a **dedicated CalDAV calendar per feed** (e.g. "HEY (synced)") so it never touches hand-made events.
3. On each tick (manual `sync` or scheduled): fetch ICS → parse all fields → diff against local state **by ICS `UID`** → create new / update changed / delete removed CalDAV events.
4. Events carry conferencing URL, location, notes, and RRULE so Granola/Zoom can attach.
5. Persistent feed failure (e.g. expired token → 404) surfaces a visible error (notification + `status`), without corrupting other feeds.

## Data model (local SQLite)

- **Feed:** id, source URL, destination calendar id/name, sync window, last-sync time, last-error.
- **EventLink:** feed id, ICS `UID`, CalDAV event href, content hash/ETag (change detection), last-seen timestamp.
- Secrets live in the **macOS Keychain**, never in SQLite or config files.

## Acceptance criteria (Phase 1)

- [ ] Adding a HEY ICS feed and running a sync produces native events in the chosen iCloud calendar, including the meeting/conferencing link.
- [ ] Granola (or Zoom) detects a synced meeting and can attach/record to it.
- [ ] Editing an event in the source feed and re-syncing updates the mirror; deleting it in the source removes the mirror.
- [ ] Manually-created events in the user's other calendars are never modified or deleted.
- [ ] A feed that 404s or errors surfaces a visible error and does not corrupt other feeds' syncs.
- [ ] Runs on a schedule after login with no manual launchd/plist editing.

## No-gos

- Google Calendar, menu-bar UI, Homebrew packaging — later phases, not this cycle.
- Two-way sync; editing events back into HEY/Basecamp (sources are read-only by platform).
- Windows/Linux; Outlook/Microsoft; hosted SaaS.

## Known edges / watch out

- **Recurring events + timezones** are the classic calendar swamp — lean on CalDAV **RRULE passthrough** rather than expanding recurrences yourself; treat exotic recurrence as a known v1 edge.
- **iCloud CalDAV discovery** (principal → calendar-home), ETags, and programmatic calendar creation have quirks — spike this early before building the full engine.
- A real HEY feed for testing: ask Matt for a current feed URL (HEY ICS tokens expire/404 over time).

## Basecamp

This project's Basecamp config (account/project/todolist IDs) is already set in `.basecamp/config.json` (gitignored), so `basecamp` commands work without flags from this directory. The work for this cycle is the **"Build: Phase 1"** todolist — run `basecamp todos list` to see it; check each off as you complete it. The Phase 1 PRD is the doc titled `PRD: Phase 1 — Headless Engine + iCloud` (run `basecamp docs list` and match by title); the full product PRD is `CalMirror — PRD`. The pitch and context live on the project.

## Solo

This project runs inside Solo, which manages its processes. For coordination and follow-up work, call `help(topic="coordination")` and `help(topic="timers")`.
