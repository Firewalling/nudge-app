# Usage and configuration

The examples below use `nudge`; use `./nudge` from the checkout or put the executable on your `PATH`.

## Commands

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
nudge daemon
```

Durations use positive Go duration syntax, such as `30s`, `15m`, and `1h`. App IDs are case-insensitive exact Niri `app_id` matches. `inspect` prints the focused window's ID and title; `watch --focused` captures its app ID at command time. A watch message is optional and defaults to “Stay focused on what matters.” `config` and `diagnostics` output JSON; `status` and `list` output text. `web` prints the URL, but does not start the daemon or open a browser. `--version`, `help` and `--help` are also available.

`pause` and `snooze` both set a temporary pause until the given duration expires; `resume` clears it. `now` sends immediately, bypassing pause, quiet hours and the scheduled cooldown.

## Configuration and scheduling

The JSON configuration lives at `$XDG_CONFIG_HOME/nudge/config.json`, or `~/.config/nudge/config.json` when `XDG_CONFIG_HOME` is unset. When absent, Nudge starts with an empty reminder list and a five-minute cooldown. [config.example.json](../config.example.json) shows periodic and watched rules; it is not installed automatically. The CLI and dashboard write the same file using a shared lock and atomic rename. Avoid manually editing it while Nudge is writing.

In the dashboard, **Add Reminder** creates a reminder directly. Choose **Any app** or an open application under **Only while using**; if an application is not open, its Niri app ID can be entered under **Advanced**. Open applications are suggestions, not a complete list of installed software.

A reminder has `kind` (`every` or `watch`), positive duration `every`, and `message`; `watch` also needs an `app` ID. `disabled: true` skips a rule. `cooldown` is a positive duration shared by scheduled notifications. Optional `quiet_start` and `quiet_end` must both be set to different local times in `HH:MM` format; the interval may cross midnight. API validation permits up to 500 rules and messages of 1–4096 bytes with non-whitespace content. `paused_until`, `last_notification`, and each rule's `last` are state maintained by Nudge. Use the dashboard to edit and remove rules, cooldown and quiet hours.

Niri window events wake the scheduler, and a five-second timer checks due reminders and recovers from missed events. A watched app is eligible on focus and repeats during continuous focus. Periodic reminders are eligible when due; a new periodic rule is initially due. The shared cooldown, pause and quiet hours can delay scheduling. Due times shown in the dashboard are estimates, not delivery guarantees; eligibility also depends on focus and notification success.

The dashboard binds only to IPv4 loopback on port 8788. Set `NUDGE_PORT` to a port from 1 to 65535 to change it; use the same value for `nudge web`. Set `XDG_CONFIG_HOME` to a separate directory for an isolated configuration. If the dashboard port is occupied, the daemon logs the error and continues scheduling without the dashboard.

## Insights and privacy

Insights is optional and stays on this computer. **Record reminder history** is off by default; turn it on in Insights to record new events. **Record app activity** is a separate, initially disabled choice. Retention defaults to 30 days and can be changed in the same view. Turning recording off keeps existing history; **Delete all history** removes recorded events without changing reminders or history settings.

History is stored in `$XDG_DATA_HOME/nudge/history.db`, or `~/.local/share/nudge/history.db` when `XDG_DATA_HOME` is unset. Nudge records notification command attempts (including failures), pause, resume and snooze actions, and a suppression when an otherwise-due reminder is held by pause, quiet hours or cooldown. Suppressions are deduplicated while the same rule remains due for the same reason in a running daemon. A restart or rule change begins a new observation.

If app activity is enabled, app-specific reminder events can include the app ID, and Nudge records short, consistently observed foreground-window intervals. Foreground time is not proof of active use. Focus changes between observations, sleep, disconnects, crashes, clock changes, short intervals and disabled recording can leave gaps; durations are recorded in whole seconds. Unsafe app IDs that resemble paths, URLs or free text are omitted. Window titles, reminder messages and URLs are not stored.

Insights groups event times by local day and hour. A notification attempt does not establish that a notification was displayed or read. Nudge prunes expired records automatically. Clearing history uses SQLite secure deletion, checkpoints its write-ahead log and vacuums the database; this does not guarantee erasure from filesystem snapshots, backups or storage-device copies. New events may be recorded after deletion if history remains enabled.

## Troubleshooting

- **Dashboard unavailable:** check the daemon log and port. Use `127.0.0.1`, not `localhost`, and the configured port.
- **No notifications:** check `notify-send`, the user D-Bus session and the notification daemon. A successful notification command only means the request was accepted, not that a popup was visible. SwayNC Do Not Disturb can suppress popups.
- **No focused app:** run `nudge inspect` in the graphical session and check the [service environment](service.md). No focused window is normal when none is focused.
- **Configuration error:** run `nudge diagnostics` and inspect the JSON. Check positive durations and paired, distinct `HH:MM` quiet hours.
- **Next reminder unavailable:** focus, cooldown, suppression, scheduler errors and quiet hours affect the estimate. It is withheld if the earliest eligibility falls within quiet hours.
