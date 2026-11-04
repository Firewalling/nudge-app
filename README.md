# Nudge

Nudge is a local, app-aware reminder daemon for Niri on Linux. It uses `notify-send` for desktop notifications, stores its configuration as JSON, and serves a browser dashboard at **http://127.0.0.1:8788**. There is no cloud service or Node.js runtime dependency in the built executable.

## Build and run

Requirements: Go 1.23 or newer, Node.js 20.19+ or 22.12+ and npm for building the dashboard; Niri and `notify-send` for desktop integration. On Fedora, install the packages providing `niri` and `notify-send` for your session. Node is not needed to run the compiled program.

```sh
cd frontend
npm ci
npm run check && npm run lint && npm run build
cd ..
go test -race ./...
go build -o nudge .
./nudge daemon
```

Open the URL above. The dashboard is served only on IPv4 loopback; if the port is already occupied, the daemon logs the error and continues scheduling without the dashboard. Configuration defaults to `$XDG_CONFIG_HOME/nudge/config.json` or `~/.config/nudge/config.json`. Existing Nudge JSON configurations are accepted. Writes use an atomic rename and a lock shared by the CLI and daemon. Avoid editing the file while a Nudge command is writing it.
For an isolated local instance, set `NUDGE_PORT` to a different port and `XDG_CONFIG_HOME` to a separate directory. The listener remains restricted to `127.0.0.1`.

The dashboard edits rules, the shared cooldown and quiet hours; changes are saved to the same configuration used by the CLI. Reminders continue when the browser is closed. Niri window events wake the scheduler; a five-second timer also checks due reminders and recovers from missed events. A watched app is reminded on focus and repeats during continuous focus. Periodic reminders run when due. A single cooldown applies across scheduled notifications. Pause and quiet hours suppress scheduling; quiet hours use local time and may cross midnight. `now` sends immediately and bypasses the scheduled cooldown and pause. Snooze is a temporary pause. Due times are estimates and may be delayed by the timer, cooldown, quiet hours or notification failure.

## CLI

```sh
nudge now "Are you being intentional?"
nudge every 30m "Stay focused on what matters."
nudge watch firefox --every 15m "Be intentional."
nudge watch --focused --every 15m
nudge inspect
nudge pause 1h
nudge snooze 15m
nudge resume
nudge status
nudge list
nudge config
nudge diagnostics
nudge web
nudge version
```

Durations use Go syntax such as `30s`, `15m`, and `1h`. App IDs are case-insensitive exact Niri `app_id` matches; `nudge inspect` displays the current ID and title. `watch --focused` captures the app at command time. `config` and `diagnostics` produce JSON. `web` prints the dashboard URL; the daemon must already be running.

## User service and migration

The supplied `nudge.service` uses `~/.local/bin/nudge daemon`. Build and test before replacing an existing installation. Back up the existing binary, configuration, and service file first. With the user service stopped, install the new executable and service, then run `systemctl --user daemon-reload` and `systemctl --user enable --now nudge.service`. To roll back, stop the service, restore the backed-up binary and service definition, reload systemd, and start the service. Preserve the configuration backup separately: the old binary does not understand new quiet-hour or per-rule disabled fields and can discard them when writing configuration.

The graphical session must supply `NIRI_SOCKET`, `XDG_RUNTIME_DIR`, and the session D-Bus environment to the user service. Check `systemctl --user status nudge.service` and `journalctl --user -u nudge.service -n 50` if it cannot connect.

## API and development

The same process serves JSON endpoints: `GET /api/v1/status`, `GET /api/v1/config`, `PUT /api/v1/config`, `POST /api/v1/control`, and `GET /api/v1/diagnostics`. Control accepts `{"action":"pause","duration":"1h"}`, `snooze`, or `resume`. Mutations require `Content-Type: application/json` and `Origin: http://127.0.0.1:8788`; all requests require Host `127.0.0.1:8788`. Errors are JSON objects with an `error` string. There is no permissive CORS policy. The API is intended for the local dashboard, not remote access.

For frontend development, run the daemon, then `cd frontend && npm run dev`. Vite proxies `/api` to the Go server. Run `npm run build` before `go build`: the production assets are embedded into the Go executable. The frontend uses Svelte 5, Vite, TypeScript, Tailwind CSS 4, a customized shadcn-svelte button, and Lucide icons. Dependencies and versions are locked in `frontend/package-lock.json`; fonts are bundled locally.

## Troubleshooting

- **Dashboard unavailable:** check the daemon log and whether another program owns port 8788. Use the exact `127.0.0.1` URL, not `localhost`.
- **No notifications:** verify `notify-send` is installed, the user D-Bus session is available, and the notification daemon is running. A successful notification command only means the request was accepted; it does not prove a popup was visible. SwayNC Do Not Disturb can suppress popups.
- **No focused app:** run `nudge inspect` in the graphical session and check the user service's Niri environment. A missing focused window is normal when none is focused.
- **Configuration error:** run `nudge diagnostics` and inspect the JSON. Quiet-hour start and end must both be set in `HH:MM` format; durations must be positive.
- **Next reminder shown as unavailable:** eligibility depends on current focus and cooldown; it is withheld while scheduling is suppressed, has an error, or the earliest eligibility falls within quiet hours. The displayed time is not a delivery guarantee.
