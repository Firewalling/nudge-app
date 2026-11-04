# Local HTTP API

The daemon serves the dashboard and JSON API at `http://127.0.0.1:8788` by default. `NUDGE_PORT` changes the port for that process; the bind remains IPv4 loopback. This is intended for the local dashboard, not remote access.

| Method | Endpoint | Purpose |
| --- | --- | --- |
| GET | `/api/v1/status` | Scheduling, focus, notification availability and errors |
| GET | `/api/v1/diagnostics` | Version, Niri and configuration errors, notification availability |
| GET | `/api/v1/config` | Current configuration and its `ETag` |
| PUT | `/api/v1/config` | Validate and replace configuration |
| POST | `/api/v1/control` | Pause, snooze or resume |

Control accepts, for example, `{"action":"pause","duration":"1h"}`, `{"action":"snooze","duration":"15m"}`, or `{"action":"resume"}`. Configuration fields and constraints are described in [usage](usage.md#configuration-and-scheduling). PUT preserves the existing pause and last-notification state, and preserves a rule's last-send time when its kind, app, interval and message match an existing rule. Send the `ETag` from GET in `If-Match` to detect concurrent configuration edits; a mismatch returns 409. The check is optional, so clients should use it to avoid overwriting other edits.

Every request must have a `Host` exactly matching the configured address, including port. An `Origin`, when present, must exactly match `http://` plus that address. Non-GET/HEAD requests require that Origin; JSON mutations also require `Content-Type: application/json`. Bodies are limited to 1 MiB and unknown JSON fields are rejected. Errors are JSON objects with an `error` string. No permissive CORS policy is provided. These checks restrict browser-origin access but are not authentication against other local processes; do not expose the service as a remote API.
