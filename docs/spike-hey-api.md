# Spike: HEY raw API as a CalMirror source

**Date:** 2026-08-26 · **Question:** can CalMirror read HEY calendars over
HEY's JSON API (as used by the `hey` CLI) with full fidelity — notes, location,
meeting URL, recurrence — instead of the expiring ICS feed URL?

**Verdict: yes.** The raw recordings endpoint returns everything the ICS feed
carries, plus attendee data. A `hey` source implementation would be strictly
better than ICS for HEY feeds.

## What was tested

With a bearer token from `hey auth token`:

| Endpoint | Result |
|---|---|
| `GET /calendars/{id}.json` | calendar metadata |
| `GET /calendars/{id}/recordings.json?starts_on=YYYY-MM-DD&ends_on=…` | **full event bodies** — this is the source endpoint |
| `GET /calendar/recordings/{id}/occurrences.json?starts_on=…&ends_on=…` | materialized occurrences of a series, each linked to its parent |
| `GET /calendar/events/{id}.json`, `/calendar/recordings/{id}.json` | 404 — no single-event GET found; not needed (listing has full bodies) |

## Fidelity findings

A probe event created via `hey event add --location --notes --link --repeat every_week`
round-tripped completely through the listing:

- `description` — notes, present when set ✅
- `location` — present when set ✅
- `url` — meeting/reference link ✅ (plus `join_link` on some shapes)
- `attendances` — guest list (ICS feed doesn't carry this) ✅
- `starts_at`/`ends_at`, `all_day`, `updated_at`, stable numeric `id` ✅
- `recurrence_schedule.kind` — preset enum (`every_day`, `every_weekday`,
  `every_week`, `every_other_week`, `every_day_of_month`, `every_year`) plus
  optional `repeat-times`/`repeat-until` style bounds. This is HEY's entire
  repeat model — there is no richer rule underneath — so mapping presets →
  RRULE is lossless relative to what HEY can express.
- Occurrences endpoint expands a series into dated instances (`occurrence_id`
  like `172036556_2026-08-27`) if we ever want expanded rather than rule-based
  mirroring.

Note: the CLI's own `hey event list` output is a *reduced* shape (no notes/
location/url); the raw endpoint is richer than the CLI surface.

## Design implications for a future `hey` source kind

1. **Auth:** shell out to `hey auth token` per sync pass (the CLI owns refresh);
   never persist the token. Requires the user to have installed + authed the
   hey CLI.
2. **Source:** windowed `recordings.json` per calendar ID. Diff by event `id`
   (stable), change-detect via `updated_at` + content hash — same shape as the
   current UID/hash model.
3. **Trigger:** already shipped in this cycle — `calmirror watch` runs syncs
   from `hey watch --events recording_added,recording_updated,
   recording_deleted,calendar_resync`; scheduled sync stays as fallback.
4. **Recurrence:** preset → RRULE table (six entries). Timezone: events carry
   UTC timestamps; zone handling needs one more look before building.
5. **Risk:** undocumented API — but it is what 37signals' own CLI ships
   against, so stability risk is comparable to or lower than scraping the ICS
   feed URL (which expires).

## Related

The same session's live test also surfaced an expired Google refresh token
(`invalid_grant`) surfacing cleanly in logs/status without corrupting other
feeds — Phase 2 acceptance criterion behaving as designed. Token needs
re-auth (`calmirror setup` Google path).
