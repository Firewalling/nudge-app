# Nudge GPUI desktop — experimental spike

This is an **isolated, read-only native UI proof of concept**, not a replacement for the current app. It exists to validate GPUI on Fedora/Niri before migrating the Svelte dashboard.

## Architecture

```
Nudge Go daemon (unchanged; systemd user service)
    └── loopback-only HTTP API (127.0.0.1:8788 by default)
           ├── existing embedded Svelte UI (unchanged)
           └── GPUI native desktop (this directory; Rust, separate process)
```

The experimental desktop reads `GET /api/v1/status` and `GET /api/v1/config` in the background, refreshes every five seconds, displays status and reminder rows, and does not send mutating requests. It does not access or modify the config file or SQLite history directly.

## Start on Fedora/Niri

1. Keep your existing Go daemon running. **Do not install or restart the production service for this experiment.**
2. Ensure recent stable Rust is installed (`rustup update stable`; confirm `cargo --version`).
3. Install GPUI's native development dependencies using Fedora-equivalent packages (Wayland, XKB including `libxkbcommon-x11` development files, fontconfig, Vulkan headers/loader, pkg-config, compiler toolchain) and verify your NVIDIA Vulkan driver works in a normal Niri session. This spike uses the Wayland feature of [GPUI 0.2.2](https://docs.rs/crate/gpui/0.2.2/features), not the API described by the current upstream README.
4. From the repository root:

   ```sh
   cd desktop-gpui
   cargo check --locked
   cargo run --locked
   ```

If your daemon uses `NUDGE_PORT`, export the same value when launching this desktop app. The client always targets **127.0.0.1**, not a remote hostname; it disables proxy discovery and redirects, uses short timeouts, and does not send browser Origin headers for GET requests.

## Intended acceptance checks

- Keep `Cargo.lock` checked in; its older dependency versions are needed by GPUI 0.2.2's `xattr` dependency.
- Window opens on native Wayland in Niri; check scaling, decorations, resizing and focus.
- Running and disconnected states are understandable.
- Real reminder text and intervals match the Go API, including disabled reminders and empty lists.
- No changes to user config, optional history, notification handling or the existing Svelte app.
- Test keyboard navigation, screen reader integration and reduced-motion handling **before** making this a functional replacement.
- Profile startup, idle CPU, resident memory, renderer/GPU memory and animation frame pacing; compare against Wails and the current browser interface on the same machine.

## Known limitations (intentional)

- Add, Edit, Pause, Snooze, toggles, themes and Insights are not implemented here.
- An active Niri focus is *observed focus*, not proof of deliberate user activity.
- The current visual treatment is not a verified implementation of Figma v2. The supplied Figma URL returned HTTP 403 in an isolated browser; the four v2 frames have not been inspected or compared.
- `cargo check` passed on Fedora with Rust 1.99.1. Niri reported a Wayland window after linking against the installed `libxkbcommon-x11.so.0` via a temporary local development-library symlink. Full rendering, focus, resize, scaling and Vulkan compatibility remain unverified.
- Accessibility remains unverified. The session's AT-SPI `IsEnabled` property was false, and neither this spike nor a separately built GPUI Kit 0.9.1 example appeared on the accessibility bus. [AccessKit's Linux guidance](https://github.com/AccessKit/accesskit/blob/main/README-APPLICATION-DEVELOPERS.md#linux-1) explains that its trees require AT-SPI to be enabled; the live session setting was left unchanged.

## Next slices

1. Stabilize compile and native Wayland behavior on Fedora/Niri; pin lockfile and record metrics.
2. Verify reconnect and partial-error states visually, then implement typed mutation requests and conflict handling.
3. Port the minimal Reminders page and right-side editor using the established local API. Use ETags (`If-Match`) for config edits and preserve all unsaved-change protections.
4. Introduce Settings, privacy-first Insights, and optional theme picker.
5. Add notifications with measured elapsed **observed focus**, after defining session/idle/suspend semantics in the Go backend.
6. Only consider replacing the web frontend after feature parity and accessibility/performance testing.

**Safety:** Rust/GPUI is the UI layer; Go stays the single authority for scheduling, state, filesystem access and notifications.
