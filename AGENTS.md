# Heyday

## What this is

Heyday mirrors **HEY Calendar into Google Calendar** as native events — meeting link, location, notes, time zones intact — so meeting tools that only read Google or Outlook calendars (Granola first, Zoom too) see HEY meetings and can attach and record. Granola can't read HEY Calendar; Heyday closes that gap. It was called **CalMirror** until November 2026, when it narrowed from "any ICS feed into iCloud or Google" to this one job. Origin: a Basecamp Community post by JJ Thorp asking how to keep HEY + Basecamp + Google calendars in sync.

HEY is read through the **hey CLI** (`github.com/basecamp/hey-cli`), which owns HEY sign-in — no feed URL to expire, no HEY credential stored here. An **ICS feed** source stays as an opt-in for non-HEY calendars (e.g. Basecamp's schedule).

The full product is a ~6-week, 4-phase effort: (1) headless engine, (2) Google Calendar, (3) menu-bar UI, (4) Homebrew tap + launch.

## This cycle: Phase 2 — Google Calendar + OAuth

Phase 1 shipped a headless engine (fetch → diff by UID → create/update/delete, per-feed failure isolation, SQLite state, Keychain secrets, launchd scheduling) against iCloud CalDAV. Phase 2 added Google Calendar behind a `Destination` interface, then the HEY CLI source and `heyday watch`. With the rename, **iCloud was removed**: Google is the only destination, because Granola can't read iCloud.

## Stack

- **Go 1.26**, macOS-only.
- **SQLite** for local state (UID ↔ Google event mapping), `modernc.org/sqlite` (pure Go).
- **Google Calendar API v3** as the destination behind the engine's `Destination` interface.
- **hey CLI** as the default source: `hey event week --json` per week of the sync window (repeats expanded into occurrences), `hey calendar list`, `hey watch` for near-instant syncs. Never call HEY's API directly; never store HEY tokens.
- Secrets in the **macOS Keychain** (service `heyday`), never in config or SQLite: the Google OAuth refresh token and client secret.
- **Google OAuth:** installed-app loopback (`golang.org/x/oauth2/google`); Calendar access via `google.golang.org/api/calendar/v3`. Sensitive `calendar` scope — testing mode works for Matt immediately; verification is an out-of-band track, not a code deliverable.
- **launchd** login-items: `com.mattsafaii.heyday.sync` (scheduled) and `com.mattsafaii.heyday.watch` (HEY changes). `install` removes CalMirror's old `com.calmirror.*` jobs.
- Ask before adding dependencies. In use: `github.com/arran4/golang-ical` (ICS source), the two Google libraries, `modernc.org/sqlite`.

## Surfaces (CLI)

- **`heyday setup`** — connect Google (browser OAuth consent → refresh token in Keychain; first run needs `--google-client-id`) and add a feed. Defaults: source `hey`, feed `hey`, calendar "HEY (Heyday)". `--hey-calendar <id|all>` picks the HEY calendar (run without it to list them). `--source ics --url <url>` adds an ICS feed instead.
- **`heyday sync`** — one sync pass across all feeds.
- **`heyday status`** — per-feed source, Google calendar, last-sync time, event count, last error.
- **`heyday watch`** — run `hey watch` and sync on HEY calendar changes. Network/server failures back off 1–30 min; a HEY sign-in failure notifies once and exits 0 so launchd leaves it stopped.
- **`heyday install` / `uninstall`** — register/remove the launchd login-items.

## Core flow

1. User runs `setup`: Google consent stores the refresh token in Keychain; the feed names a HEY calendar (or an ICS URL) and a Google calendar name.
2. Heyday creates and owns a **dedicated Google calendar per feed** (marked "Managed by Heyday" in its description), so it never touches hand-made events or other calendars.
3. On each tick (manual `sync`, the schedule, or `watch`): read the source within the sync window → diff against local state **by UID** → create new / update changed / delete removed events.
4. Events carry the meeting link (Google `location` + a "Join:" description line), location, notes and time zone so Granola/Zoom can attach.
5. A failed source read aborts that feed before any change, so a partial read can never delete events. Persistent failures (revoked OAuth, HEY signed out, ICS 404) surface as a notification + `status`, without touching other feeds.

## Data model

- **Config** (`~/Library/Application Support/heyday/config.json`): the Google account + OAuth client id, and feeds — name, **source (hey/ics)**, HEY calendar id (hey, 0 = all switched-on calendars) or source URL (ics), Google calendar name, sync window (HEY default: 30 days back, 60 ahead).
- **SQLite state** (`state.db`, same directory): per-feed last-sync time, last error, Google calendar id; **EventLink** = feed, source UID, Google event id, content hash/ETag, last-seen. HEY UIDs are `hey-<occurrence_id or id>@heyday`.
- Secrets (Google refresh token, client secret) live in the **macOS Keychain**, never in SQLite or config files.

## Acceptance criteria (Phase 2)

- [ ] `setup` connects a Google account via browser OAuth; the refresh token lands in Keychain, not config or SQLite.
- [ ] Adding a HEY feed and running `sync` creates native events in a dedicated, Heyday-owned Google calendar, including the meeting/conferencing link.
- [ ] Granola detects a Google-synced meeting and can attach/record to it.
- [ ] Editing an event in HEY and re-syncing updates the Google mirror; deleting it removes it.
- [ ] Manually-created events in the user's other Google calendars are never modified or deleted.
- [ ] A feed whose Google token is revoked/expired (or whose HEY sign-in lapses, or whose ICS source 404s) surfaces a visible error and does not corrupt other feeds.
- [ ] A HEY feed and an ICS feed both sync correctly in a single pass. (Replaces the old iCloud + Google criterion.)
- [ ] `status` shows each feed's source, last-sync time, and last error.

## No-gos

- iCloud / Apple Calendar, Outlook/Microsoft as destinations.
- Menu-bar UI, Homebrew packaging — Phases 3–4, not this cycle.
- Two-way sync; editing events back into HEY/Basecamp.
- Windows/Linux; hosted SaaS.
- Google verification/publishing is an out-of-band track, not a code deliverable — testing mode (Matt + up to 100 users) is sufficient for this cycle.

## Known edges / watch out

- **HEY sign-in is rate-limited.** Never retry a refused `hey` sign-in in a loop — the old CalMirror watch agent restarted ~172k times and locked Matt's HEY account out. Back off, and stop on auth failures.
- **HEY's Week view only covers calendars switched on in HEY.** Switching one off empties its mirror.
- **Conferencing fidelity on Google** — third-party meeting URLs (HEY/Zoom) can't be set as native Google `conferenceData` the way Meet links can, so the URL goes in location + a description line.
- **Recurring events** — HEY feeds expand occurrences (exact, window-bounded). ICS feeds pass RRULE through; treat exotic recurrence as a known edge.
- **Google OAuth verification** — weeks-long, 100-user cap until approved. Doesn't block Matt's own use (testing mode).
- Testing needs Matt's hey CLI signed in, plus a Google account/project with the Calendar API enabled and an OAuth client — coordinate with Matt.

## Basecamp

This project's Basecamp config (account/project/todolist IDs) is already set in `.basecamp/config.json` (gitignored), so `basecamp` commands work without flags from this directory. The Basecamp project is **Heyday** (renamed from CalMirror; the doc `Heyday — rename and scope change (Nov 2026)` records the change, and the older PRDs keep their CalMirror titles as history). The work for this cycle is the **"Build: Phase 2 — Google Calendar"** todolist — run `basecamp todos list` to see it; check each off as you complete it. The Phase 2 PRD is the doc titled `PRD: Phase 2 — Google Calendar` (run `basecamp docs list` and match by title); the full product PRD is `CalMirror — PRD`. The pitch and context live on the project's card.

## Solo

This project runs inside Solo, which manages its processes. For coordination and follow-up work, call `help(topic="coordination")` and `help(topic="timers")`.
