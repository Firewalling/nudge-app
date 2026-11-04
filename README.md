# nudge

Nudge sends lightweight desktop reminders on Fedora/Niri using `notify-send`. Focused app IDs come from Niri IPC. It uses only the Go standard library.

## Install

Requirements: Go, Niri, `notify-send` (`libnotify`), SwayNC (or another notification daemon), and a logged-in Wayland session.

From this checkout:

```sh
go test ./...
go build -o nudge .
install -Dm755 nudge ~/.local/bin/nudge
install -Dm644 nudge.service ~/.config/systemd/user/nudge.service
mkdir -p "${XDG_CONFIG_HOME:-$HOME/.config}/nudge"
# Copy the example only if you do not already have a config:
if [ ! -e "${XDG_CONFIG_HOME:-$HOME/.config}/nudge/config.json" ]; then
  install -m600 config.example.json "${XDG_CONFIG_HOME:-$HOME/.config}/nudge/config.json"
fi
systemctl --user daemon-reload
systemctl --user enable --now nudge.service
```

Installation leaves an existing config untouched. The daemon rereads it every five seconds, so CLI additions, pause, and resume apply without a restart.

## Commands

```sh
nudge now "Are you being intentional?"        # send one notification now
nudge inspect                                  # show focused Niri app_id and window title
nudge every 30m "Stay focused on what matters."
nudge watch --focused --every 15m              # watch the currently focused app
nudge watch --focused --every 15m "Be intentional."
nudge watch firefox --every 15m                # explicit app_id remains supported
nudge pause 1h
nudge resume
nudge status
nudge list
```

`inspect` prints the current focused app ID and title. If no window is focused, it reports that; if Niri IPC is unavailable, it returns a diagnostic error. `watch --focused` records the focused window's app ID at command time, so focus the desired app before running it. It rejects missing/invalid app IDs without changing the config. Manual `watch APP_ID` continues to work. IDs are matched case-insensitively against Niri's exact `app_id`, not the title.

A watch reminder fires when its app is observed gaining focus, repeats after its configured interval while focus remains there, and resets when focus leaves. Focus polling is every five seconds, so a transition may be noticed up to five seconds later. A shared cooldown (default `5m`) limits all scheduled notifications, including focus changes. If a notification is held by cooldown, it remains eligible while the watched app stays focused. `now` bypasses that scheduled cooldown. Pause suspends scheduled reminders; resuming while a watched app is focused starts a fresh session.

`every` and `watch` append to `${XDG_CONFIG_HOME:-~/.config}/nudge/config.json`. Durations use Go syntax (`15m`, `1h`, `90s`). The example config includes the four default messages and an example Firefox watch. Edit JSON directly to change or remove reminders.

## Notification troubleshooting

```sh
notify-send --urgency=normal "Nudge test" "Normal urgency"
notify-send --urgency=critical "Nudge test" "Critical urgency"
nudge now "Nudge test"
swaync-client --get-dnd
niri msg --json focused-window
```

- Nudge uses normal urgency and respects the desktop notification daemon. A successful `notify-send`/`nudge now` exit means the D-Bus request was accepted; it does not prove a banner was shown.
- If `swaync-client --get-dnd` prints `true`, SwayNC Do Not Disturb is on. SwayNC may suppress notification popups while DND is active. Turn it off through SwayNC only if that matches your preference; Nudge does not change DND or force critical urgency.
- SwayNC's `timeout`, `timeout-low`, and `timeout-critical` control banner dismissal. A timeout of `0` means that urgency does not time out.
- `nudge inspect` shows the current app ID; `watch --focused` avoids guessing it. For a manually configured watch, use the exact `app_id` from `niri msg --json focused-window`.
- Check daemon status/logs with `systemctl --user status nudge.service` and `journalctl --user -u nudge.service -n 50 --no-pager`. The user systemd manager must retain `WAYLAND_DISPLAY`, `NIRI_SOCKET`, `XDG_RUNTIME_DIR`, and the session D-Bus address. Nudge requires the logged-in graphical session; lingering alone cannot provide it.

Nudge intentionally does not bypass DND. Notifications accepted by D-Bus but hidden by SwayNC remain subject to the user's SwayNC preferences. Missed intervals are not replayed; a daemon restart creates a fresh app-focus session.
