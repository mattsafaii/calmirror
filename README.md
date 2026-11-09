# Heyday

Get your **HEY Calendar** meetings into **Google Calendar**, so tools that only
read Google or Outlook calendars — [Granola](https://granola.ai), Zoom — see
them and can attach to them.

Heyday copies your HEY events into a Google calendar it creates and owns, as
real native events with the meeting link, location, notes and time zone
intact. It keeps them in step: new HEY events appear, edits update, deletions
disappear, on a schedule and within moments of a change in HEY.

> Formerly **CalMirror**. macOS only. A menu-bar app and a Homebrew tap are
> later phases (see [Roadmap](#roadmap)).

## How it works

1. You connect a Google account once (a browser consent; the refresh token is
   stored in the **macOS Keychain**, never on disk) and pick a HEY calendar.
2. Heyday reads HEY through the [hey CLI](https://github.com/basecamp/hey-cli),
   which owns your HEY sign-in — Heyday never sees or stores a HEY password or
   token, and there is no feed URL to expire.
3. It creates a dedicated Google calendar, "HEY (Heyday)" by default, and only
   ever writes there. Your other Google calendars are never touched.
4. Each sync reads your HEY events for the window (30 days back, 60 ahead),
   compares them with what it wrote last time, and creates, updates or deletes
   to match. Each day of a repeating event becomes its own Google event, so a
   one-day change or cancellation in HEY comes through exactly.
5. The meeting link goes in the event's location and a "Join:" line in its
   description, where Granola and Zoom look for it.

Heyday reads HEY only through the hey CLI (`hey event week`, `hey calendar
list`, `hey watch`). It doesn't use `hey mcp`: that server's calendar tools
return a repeating event once, as its series, rather than each day, and it
needs its own separate sign-in. ICS feeds are an optional extra (below).

If a read from HEY fails, that sync changes nothing, so a hiccup can never
delete your mirrored events. Persistent problems (signed out of HEY, a revoked
Google token) show up as a notification and in `heyday status`.

## Requirements

- macOS, and Go 1.26+ to build.
- The [hey CLI](https://github.com/basecamp/hey-cli), signed in with
  `hey auth login`.
- A Google Cloud project with the **Google Calendar API** enabled and an OAuth
  client of type **Desktop app**. Heyday asks for its client id and secret
  during setup. (While the app is in testing mode, add your Google account as a
  test user.)

## Install

```sh
go install github.com/mattsafaii/heyday/cmd/heyday@latest
```

This puts the `heyday` binary in `$(go env GOPATH)/bin` (usually `~/go/bin`;
make sure it's on your `PATH`).

## Usage

```sh
# List your HEY calendars.
heyday setup

# Connect Google and mirror one HEY calendar (or --hey-calendar all).
# Prompts for the OAuth client secret (or reads $HEYDAY_GOOGLE_CLIENT_SECRET),
# then opens your browser for Google consent.
heyday setup --google-client-id <id> --hey-calendar 123456

# Mirror now.
heyday sync

# Each feed's source, Google calendar, event count, last sync and last error.
heyday status

# Keep it in sync: a launchd job syncs every 15 minutes, and a watch agent
# syncs within moments of a HEY calendar change.
heyday install --interval 15   # minutes
heyday uninstall
```

`heyday watch` runs the watch agent in the foreground. It retries network and
server failures with 1–30 minute backoff. If HEY sign-in fails it stops and
notifies you instead — retrying a refused sign-in only extends HEY's rate
limit — so run `hey auth login`, then `heyday install` to start it again.

### Optional: an ICS feed

Heyday can also mirror any ICS feed URL into Google, for calendars that
aren't in HEY (Basecamp's schedule, for example):

```sh
heyday setup --source ics --url "https://3.basecamp.com/…/schedule.ics" \
  --feed basecamp --calendar "Basecamp (Heyday)"
```

Repeat rules on ICS events pass through to Google as-is.

## Files

- Config: `~/Library/Application Support/heyday/config.json` (feeds and the
  Google account name only).
- State: `state.db` in the same directory, plus `sync.log` and `watch.log`.
- Secrets: the macOS Keychain, service `heyday`.

Moving from CalMirror: Heyday starts fresh. Run `heyday setup` again;
`heyday install` removes CalMirror's old launchd jobs. The old
`~/Library/Application Support/calmirror` folder and any "HEY (synced)" Google
calendar CalMirror made are left for you to delete.

## Known limits

- HEY only shares the calendars switched on in the HEY app. Switching one off
  empties its mirror.
- Third-party meeting links can't be attached as Google's native conferencing
  (that's reserved for Meet), so they go in location and description instead.

## Roadmap

1. Headless engine ✅
2. Google Calendar + HEY CLI source ✅ (this release)
3. Menu-bar UI
4. Homebrew tap + launch

## Built with

- [basecamp/hey-cli](https://github.com/basecamp/hey-cli) — reading HEY
- Google Calendar API v3 and `golang.org/x/oauth2`
- [arran4/golang-ical](https://github.com/arran4/golang-ical) — ICS parsing
- [modernc.org/sqlite](https://modernc.org/sqlite) — pure-Go SQLite (no cgo)

## License

[MIT](LICENSE) © 2026 Matt Safaii
