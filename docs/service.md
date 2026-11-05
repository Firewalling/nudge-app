# systemd user service

The supplied [nudge.service](../nudge.service) runs `%h/.local/bin/nudge daemon`, restarts on failure, and is enabled under `default.target`. Build the executable first as described in the [README](../README.md). Run the commands below from the repository root in the target user's session; no root privileges are needed.

## New installation

These commands are for a fresh installation only. If either destination already exists, stop and use the migration steps instead. They do not copy or replace your configuration.

```sh
mkdir -p "$HOME/.local/bin" "$HOME/.config/systemd/user"
test ! -e "$HOME/.local/bin/nudge" && test ! -L "$HOME/.local/bin/nudge" &&
  test ! -e "$HOME/.config/systemd/user/nudge.service" && test ! -L "$HOME/.config/systemd/user/nudge.service" &&
  cp -n ./nudge "$HOME/.local/bin/nudge" &&
  cp -n ./nudge.service "$HOME/.config/systemd/user/nudge.service"
```

If the command fails or a destination appeared concurrently, inspect both files before continuing. Then load and start the service:

```sh
systemctl --user daemon-reload
systemctl --user enable --now nudge.service
systemctl --user status nudge.service
```

## Existing installation or rollback

Stop the existing user service and any other Nudge processes before replacing its executable or copying its SQLite history. Save copies of the binary, service file, configuration and history to a new private backup directory; if a path does not exist, skip its copy. Set `config` and `history` to the actual paths if the service uses different XDG directories. Inspect the service definition and any drop-ins (`systemctl --user cat nudge.service`) before replacing it; the supplied unit may differ from your existing setup.

```sh
systemctl --user stop nudge.service
backup=$(mktemp -d "$HOME/nudge-backup.XXXXXXXX")
config="${XDG_CONFIG_HOME:-$HOME/.config}/nudge/config.json"
history="${XDG_DATA_HOME:-$HOME/.local/share}/nudge"
printf 'Backup directory: %s\n' "$backup"
[ ! -e "$HOME/.local/bin/nudge" ] || cp -p "$HOME/.local/bin/nudge" "$backup/nudge"
[ ! -e "$HOME/.config/systemd/user/nudge.service" ] || cp -p "$HOME/.config/systemd/user/nudge.service" "$backup/nudge.service"
[ ! -e "$config" ] || cp -p "$config" "$backup/config.json"
[ ! -d "$history" ] || cp -a "$history" "$backup/history"
```

Confirm that each existing file was backed up successfully. Then, if you intend to replace the binary and unit, copy them individually; `cp -i` asks before overwriting. The configuration is not replaced.

```sh
mkdir -p "$HOME/.local/bin" "$HOME/.config/systemd/user"
cp -i ./nudge "$HOME/.local/bin/nudge"
cp -i ./nudge.service "$HOME/.config/systemd/user/nudge.service"
systemctl --user daemon-reload
systemctl --user enable --now nudge.service
```

To roll back, use the printed backup directory (or the `backup` variable in the same shell). The following commands restore saved files and prompt before overwriting; skip a copy if that file was not present in the backup:

```sh
systemctl --user stop nudge.service
cp -i "$backup/nudge" "$HOME/.local/bin/nudge"
cp -i "$backup/nudge.service" "$HOME/.config/systemd/user/nudge.service"
systemctl --user daemon-reload
systemctl --user start nudge.service
```

Keep the configuration backup separately: an older binary may not understand quiet-hour or per-rule disabled fields and can discard them when writing. Restoring an older configuration also discards changes made since its backup.

## Session environment

The service needs the graphical session's `NIRI_SOCKET`, `XDG_RUNTIME_DIR` and session D-Bus environment. Arrange for the user manager to receive them from the graphical session before starting the service; the supplied unit does not set them. For connection failures, inspect `systemctl --user status nudge.service` and `journalctl --user -u nudge.service -n 50`. See also [troubleshooting](usage.md#troubleshooting).
