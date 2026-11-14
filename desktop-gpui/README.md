# Nudge GPUI desktop — experimental spike

This is an **isolated, read-only native UI proof of concept**, not a replacement for the current app. It exists to validate GPUI on Fedora/Niri before migrating the Svelte dashboard.

## Architecture

```
Nudge Go daemon (unchanged; systemd user service)
    └── loopback-only HTTP API (127.0.0.1:8788 by default)
           ├── existing embedded Svelte UI (unchanged)
           └── GPUI native desktop (this directory; Rust, separate process)
```

The experimental desktop currently reads `GET /api/v1/status` and `GET /api/v1/config` **once on startup**, displays status and reminder rows, and does not send mutating requests. It does not access or modify the config file or SQLite history directly.

## Start on Fedora/Niri

1. Keep your existing Go daemon running. **Do not install or restart the production service for this experiment.**
2. Ensure recent stable Rust is installed (`rustup update stable`; confirm `cargo --version`).
3. Install GPUI's native development dependencies using Fedora-equivalent packages (Wayland, XKB, X11/XCB, fontconfig, Vulkan headers/loader, pkg-config, compiler toolchain) and verify your NVIDIA Vulkan driver works in a normal Niri session. For authoritative requirements see the [GPUI README](https://github.com/zed-industries/zed/blob/main/crates/gpui/README.md).
4. From the repository root:

   ```sh
   cd desktop-gpui
   cargo check
   cargo run
   ```

If your daemon uses `NUDGE_PORT`, export the same value when launching this desktop app. The client always targets **127.0.0.1**, not a remote hostname. Its requests use short timeouts and do not send browser Origin headers for GET requests.

## Intended acceptance checks

- Compile on the chosen GPUI crate versions and commit the generated `Cargo.lock`.
- Window opens on native Wayland in Niri; check scaling, decorations, resizing and focus.
- Running and disconnected states are understandable.
- Real reminder text and intervals match the Go API, including disabled reminders and empty lists.
- No changes to user config, optional history, notification handling or the existing Svelte app.
- Test keyboard navigation, screen reader integration and reduced-motion handling **before** making this a functional replacement.
- Profile startup, idle CPU, resident memory, renderer/GPU memory and animation frame pacing; compare against Wails and the current browser interface on the same machine.

## Known limitations (intentional)

- Data is sampled once at startup; there is no background refresh yet.
- Add, Edit, Pause, Snooze, toggles, themes and Insights are not implemented here.
- An active Niri focus is *observed focus*, not proof of deliberate user activity.
- The visual treatment is a starting point. It should converge with the Figma Reminders redesign without recreating visual clutter.
- This branch has **not** been compiled or tested on the developer's Fedora/Niri machine. GPUI is pre-1.0 and APIs can change. Treat build or accessibility failures as blockers rather than silently switching frameworks or affecting the live daemon.

## Next slices

1. Stabilize compile and native Wayland behavior on Fedora/Niri; pin lockfile and record metrics.
2. Implement non-blocking status/config refresh, error/reconnect states, and focus-safe UI updates.
3. Port the minimal Reminders page and right-side editor using the established local API. Use ETags (`If-Match`) for config edits and preserve all unsaved-change protections.
4. Introduce Settings, privacy-first Insights, and optional theme picker.
5. Add notifications with measured elapsed **observed focus**, after defining session/idle/suspend semantics in the Go backend.
6. Only consider replacing the web frontend after feature parity and accessibility/performance testing.

**Safety:** Rust/GPUI is the UI layer; Go stays the single authority for scheduling, state, filesystem access and notifications.
