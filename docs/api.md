# Local HTTP API

The daemon serves the dashboard and JSON API at `http://127.0.0.1:8788` by default. `NUDGE_PORT` changes the port for that process; the bind remains IPv4 loopback. This is intended for the local dashboard, not remote access.

| Method | Endpoint | Purpose |
| --- | --- | --- |
| GET | `/api/v1/status` | Scheduling, focus, notification availability and errors |
| GET | `/api/v1/apps` | Open Niri applications (IDs and display names only) |
| GET | `/api/v1/diagnostics` | Version, Niri and configuration errors, notification availability |
| GET | `/api/v1/config` | Current configuration and its `ETag` |
| PUT | `/api/v1/config` | Validate and replace configuration |
| POST | `/api/v1/control` | Pause, snooze or resume |
| POST | `/api/v1/toast` | Focused desktop reminder offer, claim and dismissal |
| GET | `/api/v1/insights?days=7` | Recorded daily, hourly and app aggregates; `days` may be 7 or 30 |
| PUT | `/api/v1/insights/settings` | Set history and app-activity collection and retention |
| DELETE | `/api/v1/insights/history` | Delete all recorded history |

The apps endpoint returns `{ "apps": [{ "id": "...", "name": "..." }], "error": "" }`. It lists open windows only, deduplicated by application ID. Names are derived from IDs; they are not an installed-application catalog. If Niri is unavailable, `apps` is empty and `error` explains why.

Insights settings accept `{"enabled":true,"app_activity":false,"retention_days":30}`; all fields are required and retention must be 1–3650 days. App activity is recorded only while history is enabled. The Insights GET response includes settings, `days` with reminder attempts and snoozes, `hours` with attempts, and `apps` with attempts and observed foreground seconds. Attempts include failures and do not establish that a notification was displayed or read; foreground time is observed focus, not verified active use. An `error` string can report a recorder failure even when the read succeeds. See [Insights and privacy](usage.md#insights-and-privacy) for collection and deletion semantics.

The desktop client posts `{"action":"poll"}` while focused and outside the reminder editor; the response is `{"toast":null}` or a reminder with `id`, `title`, `body` and `accepted`. It posts `{"action":"claim","id":N}` before showing an unaccepted offer and `{"action":"dismiss","id":N}` when the user dismisses it. An expired offer cannot be claimed. This endpoint uses the same Host, Origin and JSON restrictions as the other mutations. A focused desktop reminder is offered only when Niri reports the Nudge window focused; unclaimed offers fall back to native notification. If the client's heartbeat expires after a claim, the daemon transfers the abandoned reminder to native delivery. While an in-app reminder awaits a response, further foreground reminders wait rather than stack; background reminders continue through native notification.

Control accepts, for example, `{"action":"pause","duration":"1h"}`, `{"action":"snooze","duration":"15m"}`, or `{"action":"resume"}`. Configuration fields and constraints are described in [usage](usage.md#configuration-and-scheduling). PUT preserves the existing pause and last-notification state, and preserves a rule's last-send time when its kind, app, interval and message match an existing rule. Send the `ETag` from GET in `If-Match` to detect concurrent configuration edits; a mismatch returns 409. The check is optional, so clients should use it to avoid overwriting other edits.

Every request must have a `Host` exactly matching the configured address, including port. An `Origin`, when present, must exactly match `http://` plus that address. Non-GET/HEAD requests require that Origin; JSON mutations also require `Content-Type: application/json`. JSON request bodies are limited to 1 MiB and unknown JSON fields are rejected. Errors are JSON objects with an `error` string. No permissive CORS policy is provided. These checks restrict browser-origin access but are not authentication against other local processes; do not expose the service as a remote API.
