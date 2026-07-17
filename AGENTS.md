# CalMirror

## What this is

CalMirror mirrors any ICS calendar feed (HEY Calendar, Basecamp BC5, anything) into a user's real calendars as **full-fidelity native events** — preserving the conferencing/meeting URL, location, notes, and recurrence — so meeting tools like Granola and Zoom can attach and record. Built-in ICS subscriptions in Apple/Google Calendar refresh too slowly and land as read-only events downstream tools ignore; CalMirror materializes the feed as real events, refreshed on a schedule the user controls. Origin: a Basecamp Community post by JJ Thorp asking how to keep HEY + Basecamp + Google calendars in sync.

The full product is a ~6-week, 4-phase effort: (1) headless engine + iCloud, (2) Google Calendar, (3) menu-bar UI, (4) Homebrew tap + launch.

## This cycle: Phase 2 — Google Calendar + OAuth

Phase 1 (headless ICS→iCloud CalDAV engine) is **shipped** — fetch → diff by UID → create/update/delete, per-feed failure isolation, SQLite state, Keychain secrets, and launchd scheduling all work against iCloud. Phase 2 **generalizes the destination** so Google Calendar slots in alongside iCloud, then proves the Granola attach/record use case iCloud couldn't deliver (Granola can't read iCloud).

The work: extract a `Destination` abstraction from the engine's current hardwired CalDAV interface (iCloud CalDAV becomes one implementation), add a Google implementation (Calendar API v3 + installed-app OAuth), and route each feed to its chosen destination.

## Stack

- **Go 1.26**, macOS-only.
- **SQLite** for local state (UID ↔ destination event mapping).
- **iCloud CalDAV** (Phase 1) and **Google Calendar API v3** (Phase 2) as destinations behind one `Destination` interface.
- Secrets in the **macOS Keychain**, never in config or SQLite: the iCloud app-specific password (Phase 1) and the Google OAuth refresh token (Phase 2).
- **Google OAuth:** installed-app loopback (`golang.org/x/oauth2/google`); Calendar access via `google.golang.org/api/calendar/v3`. Sensitive `calendar` scope — testing mode works for Matt immediately; verification is an out-of-band track, not a code deliverable.
- **launchd** login-item for scheduling — unchanged from Phase 1.
- Ask before adding dependencies. Already in use: `github.com/arran4/golang-ical` (ICS), `github.com/emersion/go-webdav` (CalDAV), a Keychain wrapper, `modernc.org/sqlite` (pure-Go). Phase 2 adds the two Google libraries above (already approved).

## Surfaces (CLI)

- **`calmirror setup`** — add a feed (source ICS URL + destination calendar name). Now choose a **destination kind**: iCloud (app-specific password → Keychain) or Google (browser OAuth consent → refresh token in Keychain).
- **`calmirror sync`** — one sync pass across all feeds; each feed routes to its own destination.
- **`calmirror status`** — per-feed **destination kind**, last-sync time, event counts, last error.
- **`calmirror install` / `uninstall`** — register/remove the launchd login-item (unchanged).

## Core flow

1. User runs `setup`, providing an ICS URL, a destination calendar name, and a destination kind. For Google: a browser OAuth consent stores the refresh token in Keychain. For iCloud: the app-specific password (Phase 1 path).
2. CalMirror creates and owns a **dedicated calendar per feed** (e.g. "HEY (synced)") on the chosen destination, so it never touches hand-made events.
3. On each tick (manual `sync` or scheduled): fetch ICS → parse all fields → diff against local state **by ICS `UID`** → create new / update changed / delete removed events via the feed's destination.
4. Events carry conferencing URL, location, notes, and RRULE so Granola/Zoom can attach.
5. Persistent feed failure (expired token → 404, revoked OAuth) surfaces a visible error (notification + `status`), without corrupting other feeds.

## Data model (local SQLite)

- **Feed:** id, source URL, **destination kind (icloud/google)**, destination calendar id/name, sync window, last-sync time, last-error. One destination per feed (mirror to both = two feeds).
- **EventLink:** feed id, ICS `UID`, destination event id/href, content hash/ETag (change detection), last-seen timestamp. The href/ETag fields generalize across destinations.
- Secrets (iCloud app-specific password, Google OAuth refresh token) live in the **macOS Keychain**, never in SQLite or config files.

## Acceptance criteria (Phase 2)

- [ ] `setup` connects a Google account via browser OAuth; the refresh token lands in Keychain, not config or SQLite.
- [ ] Adding a Google feed and running `sync` creates native events in a dedicated, CalMirror-owned Google calendar, including the meeting/conferencing link.
- [ ] Granola detects a Google-synced meeting and can attach/record to it.
- [ ] Editing an event in the source and re-syncing updates the Google mirror; deleting it removes it.
- [ ] Manually-created events in the user's other Google calendars are never modified or deleted.
- [ ] A feed whose Google token is revoked/expired (or whose source 404s) surfaces a visible error and does not corrupt other feeds.
- [ ] A mix of one iCloud feed and one Google feed both sync correctly in a single pass.
- [ ] `status` shows each feed's destination kind, last-sync time, and last error.

## No-gos

- Menu-bar UI, Homebrew packaging — Phases 3–4, not this cycle.
- Two-way sync; editing events back into HEY/Basecamp (sources are read-only by platform).
- Windows/Linux; Outlook/Microsoft; hosted SaaS.
- Google verification/publishing is an out-of-band track, not a code deliverable — testing mode (Matt + up to 100 users) is sufficient for this cycle.

## Known edges / watch out

- **Conferencing fidelity on Google** — third-party meeting URLs (HEY/Zoom) can't be set as native Google `conferenceData` the way Meet links can. Carry the URL where Granola will find it (location + a dedicated description line; native conferenceData entry point if feasible). **Spike this early — it's the crux of Phase 2's value.**
- **Recurring events + timezones** — the classic calendar swamp. Map ICS RRULE/timezone to Google's recurrence model via passthrough; treat exotic recurrence as a known edge (same posture as Phase 1's CalDAV passthrough).
- **Google OAuth verification** — bureaucratic, weeks-long, 100-user cap until approved. Doesn't block Matt's own use (testing mode); start the submission early but don't gate code on it.
- A real HEY feed for testing: ask Matt for a current feed URL (HEY ICS tokens expire/404 over time). A Google account/project with the Calendar API enabled and an OAuth client is also needed — coordinate with Matt.

## Basecamp

This project's Basecamp config (account/project/todolist IDs) is already set in `.basecamp/config.json` (gitignored), so `basecamp` commands work without flags from this directory. The work for this cycle is the **"Build: Phase 2 — Google Calendar"** todolist — run `basecamp todos list` to see it; check each off as you complete it. The Phase 2 PRD is the doc titled `PRD: Phase 2 — Google Calendar` (run `basecamp docs list` and match by title); the full product PRD is `CalMirror — PRD`. The pitch and context live on the project's card.

## Solo

This project runs inside Solo, which manages its processes. For coordination and follow-up work, call `help(topic="coordination")` and `help(topic="timers")`.
