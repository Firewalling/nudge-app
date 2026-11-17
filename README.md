# Nudge

Nudge is a local reminder app for Linux and the Niri compositor.

## Why Nudge?

It's easy to lose track of time when using an app. Nudge can ask your desktop to show a reminder at regular intervals or while a chosen app is in focus.

## What you can do

- Create, edit, disable and delete reminders in the dashboard or add them from the CLI.
- Set quiet hours and a shared cooldown, or pause and snooze reminders. The daemon keeps scheduling when the browser is closed.
- Configure elapsed foreground-session reminders separately from watched rules; see the [usage guide](docs/usage.md#configuration-and-scheduling) for configuration and idle-observation limitations.
- Optionally explore local history of reminder attempts and observed app focus in Insights. History is off by default; app activity is a separate opt-in.

## Preview

**Desktop reminders** — example reminders and an in-app toast, captured in Wails.

![Nudge desktop reminders and a toast](docs/images/desktop-reminders.png)

<details>
<summary>See the centered editor at a narrow window size</summary>

![Nudge desktop centered reminder editor](docs/images/desktop-editor-narrow.png)

</details>

<details>
<summary>See Insights with example activity</summary>

![Nudge Insights with example activity](docs/images/insights.png)

</details>

The Insights preview shows an earlier interface; all screenshots use example data. Nudge Forest follows your system's light or dark appearance; additional themes are available under Settings.

A notification attempt does not prove a notification was displayed or read. Observed window focus is not proof of active use. See [Insights and privacy](docs/usage.md#insights-and-privacy) for what is stored and its limitations.

## Build and run

You need Go 1.23+, Node.js 24+ and npm to build. Desktop integration needs Niri, `notify-send` and a notification service in your graphical session. The built executable does not need Node.js.

From the repository root:

```sh
cd frontend
npm ci
npm run build
cd ..
go build -o nudge .
./nudge daemon
```

The dashboard is embedded in the executable, so build the frontend first. Open [http://127.0.0.1:8788](http://127.0.0.1:8788) and choose **Add reminder**, or leave the daemon running and add your first reminder from another terminal:

```sh
./nudge every 30m "Take a moment to check in."
```

A new configuration has no reminders. A new periodic reminder is initially due, subject to the shared cooldown, pause and quiet hours. For an app-specific reminder, use `./nudge inspect` to find its Niri app ID, then `./nudge watch APP_ID --every 15m "Be intentional."`.

To run Nudge as a systemd user service, follow the [installation guide](docs/service.md). It includes separate safe steps for a new or existing installation and the required graphical-session environment.

### Native desktop

The Wails window uses the same dashboard and connects to the independently running daemon over IPv4 loopback. Closing the window does not stop the daemon or reminders. Set `NUDGE_PORT` when launching the desktop if the daemon uses a non-default port.

On Fedora, building requires Go 1.26+, Node.js 24+, npm, GCC, pkg-config, `gtk4-devel` and `webkitgtk6.0-devel`. The build uses the checked-in npm and Go dependency locks:

```sh
make desktop
./dist/nudge-desktop
```

The production frontend is embedded in `dist/nudge-desktop`; no development server or Node.js runtime is needed. The installed machine needs the GTK4 and WebKitGTK 6 runtime libraries (`gtk4` and `webkitgtk6.0` on Fedora). Start the daemon separately as described above or in the [service guide](docs/service.md).

To install the desktop binary, application launcher and icon for the current user:

```sh
make install-user
make uninstall-user
```

The binary is installed to `~/.local/bin`, and the launcher and icon to `${XDG_DATA_HOME:-~/.local/share}`. Installation requires `desktop-file-install` (`desktop-file-utils` on Fedora) and writes the absolute binary path into the launcher. `BINDIR` and `DATADIR` may be overridden with absolute, non-root paths for a different user prefix or staging directory. Uninstall removes only those three installed files; it does not remove the daemon, its configuration, or history.

## Configuration

The dashboard and CLI share `$XDG_CONFIG_HOME/nudge/config.json` (normally `~/.config/nudge/config.json`). The default cooldown is five minutes; set quiet hours such as 22:00–07:00 under **Settings**. `NUDGE_PORT` changes the dashboard port, which remains bound to IPv4 loopback. Use the same port for `nudge web`. See the [example configuration](config.example.json) and [usage guide](docs/usage.md) for details.

Pause and resume from another terminal:

```sh
./nudge pause 1h
./nudge resume
```

## How it works and development

The Go daemon observes Niri focus events, schedules reminders and invokes `notify-send`. It serves an embedded Svelte dashboard and a loopback-only [HTTP API](docs/api.md). Optional Insights history is stored in a local SQLite database; it does not store window titles or reminder messages.

For frontend development, run the daemon and then `cd frontend && npm run dev`. The Vite `/api` proxy targets the default daemon port. After frontend changes, rebuild the frontend before rebuilding Go. Available checks are `npm run check` and `npm run lint` in `frontend`, and `go test -race ./...` at the repository root.
