//! Experimental, read-only GPUI frontend for Nudge.
//!
//! Intentionally separate from the Go daemon and existing Svelte dashboard.
//! All HTTP requests target the existing IPv4 loopback API.

use std::time::Duration;

use gpui::{
    App, Bounds, Context, Window, WindowBounds, WindowOptions, div, prelude::*, px, rgb, size,
};
use gpui_platform::application;
use serde::Deserialize;

#[derive(Default, Deserialize)]
struct Status {
    #[serde(default)]
    active: bool,
    #[serde(default)]
    scheduler_ready: bool,
    #[serde(default)]
    focused_app: String,
    #[serde(default)]
    niri_error: String,
}

#[derive(Default, Deserialize)]
struct Config {
    #[serde(default)]
    reminders: Vec<Reminder>,
}

#[derive(Default, Deserialize)]
struct Reminder {
    #[serde(default)]
    message: String,
    #[serde(default)]
    every: String,
    #[serde(default)]
    app: String,
    #[serde(default)]
    disabled: bool,
}

#[derive(Default)]
struct Snapshot {
    status: Option<Status>,
    reminders: Vec<Reminder>,
    error: Option<String>,
}

fn base_url() -> String {
    // Match Nudge's NUDGE_PORT override; only ever connect to IPv4 loopback.
    let port = std::env::var("NUDGE_PORT")
        .ok()
        .and_then(|value| value.parse::<u16>().ok())
        .filter(|port| *port != 0)
        .unwrap_or(8788);
    format!("http://127.0.0.1:{port}")
}

fn read_json<T: serde::de::DeserializeOwned>(path: &str) -> Result<T, String> {
    let url = format!("{}{}", base_url(), path);
    ureq::get(&url)
        .timeout(Duration::from_millis(800))
        .call()
        .map_err(|err| format!("{path}: {err}"))?
        .into_json::<T>()
        .map_err(|err| format!("{path}: {err}"))
}

impl Snapshot {
    fn load() -> Self {
        let status: Result<Status, String> = read_json("/api/v1/status");
        let config: Result<Config, String> = read_json("/api/v1/config");
        let error = status
            .as_ref()
            .err()
            .or_else(|| config.as_ref().err())
            .cloned();

        Self {
            status: status.ok(),
            reminders: config.map(|value| value.reminders).unwrap_or_default(),
            error,
        }
    }
}

struct NudgeDesktop {
    snapshot: Snapshot,
}

impl Render for NudgeDesktop {
    fn render(&mut self, _window: &mut Window, _cx: &mut Context<Self>) -> impl IntoElement {
        let background = rgb(0x111b16);
        let surface = rgb(0x1d2a23);
        let border = rgb(0x35463a);
        let primary = rgb(0xedf2ea);
        let secondary = rgb(0xa0aea2);
        let accent = rgb(0xb5e3c7);

        let status = match &self.snapshot.status {
            None => "Daemon unavailable",
            Some(state) if !state.active => "Paused or in quiet hours",
            Some(state) if !state.scheduler_ready => "Waiting for scheduler",
            Some(_) => "Reminders running",
        };

        let mut reminders = div().flex().flex_col().gap_3();
        for reminder in &self.snapshot.reminders {
            let cadence = if reminder.app.is_empty() {
                format!("Every {}", reminder.every)
            } else {
                format!("Every {} · {}", reminder.every, reminder.app)
            };
            reminders = reminders.child(
                div()
                    .flex()
                    .items_center()
                    .justify_between()
                    .gap_4()
                    .p_4()
                    .bg(surface)
                    .border_1()
                    .border_color(border)
                    .rounded_lg()
                    .child(
                        div()
                            .flex()
                            .flex_col()
                            .gap_2()
                            .child(reminder.message.clone())
                            .child(div().text_sm().text_color(secondary).child(cadence)),
                    )
                    .child(
                        div()
                            .text_sm()
                            .text_color(if reminder.disabled { secondary } else { accent })
                            .child(if reminder.disabled { "Off" } else { "On" }),
                    ),
            );
        }

        let focused = self.snapshot.status.as_ref().and_then(|state| {
            if state.focused_app.is_empty() {
                None
            } else {
                Some(format!("Focused app: {}", state.focused_app))
            }
        });

        div()
            .size_full()
            .bg(background)
            .text_color(primary)
            .child(
                div()
                    .flex()
                    .flex_col()
                    .gap_8()
                    .p_8()
                    .child(
                        div()
                            .flex()
                            .items_center()
                            .justify_between()
                            .child(div().text_lg().child("nudge"))
                            .child(div().text_sm().text_color(secondary).child("GPUI experiment")),
                    )
                    .child(
                        div()
                            .flex()
                            .items_center()
                            .justify_between()
                            .child(div().text_2xl().child("Reminders"))
                            .child(
                                div()
                                    .text_sm()
                                    .text_color(accent)
                                    .child(format!("{} configured", self.snapshot.reminders.len())),
                            ),
                    )
                    .child(
                        div()
                            .flex()
                            .flex_col()
                            .gap_2()
                            .child(status)
                            .when_some(focused, |view, focused| {
                                view.child(div().text_sm().text_color(secondary).child(focused))
                            }),
                    )
                    .when(self.snapshot.reminders.is_empty(), |view| {
                        view.child(
                            div()
                                .p_6()
                                .bg(surface)
                                .rounded_lg()
                                .child("No reminders found. Use the existing dashboard to add one."),
                        )
                    })
                    .child(reminders)
                    .when_some(self.snapshot.error.clone(), |view, error| {
                        view.child(
                            div()
                                .text_sm()
                                .text_color(secondary)
                                .child(format!("Unable to read daemon: {error}")),
                        )
                    })
                    .when(
                        self.snapshot
                            .status
                            .as_ref()
                            .is_some_and(|state| !state.niri_error.is_empty()),
                        |view| {
                            view.child(
                                div()
                                    .text_sm()
                                    .text_color(secondary)
                                    .child("Niri integration needs attention; check Diagnostics."),
                            )
                        },
                    ),
            )
    }
}

fn main() {
    // This first proof of concept reads daemon state once at startup.
    // Proper background refresh and mutation controls are future milestones.
    let snapshot = Snapshot::load();
    application().run(move |cx: &mut App| {
        let bounds = Bounds::centered(None, size(px(900.0), px(620.0)), cx);
        cx.open_window(
            WindowOptions {
                window_bounds: Some(WindowBounds::Windowed(bounds)),
                ..Default::default()
            },
            |_, cx| cx.new(|_| NudgeDesktop { snapshot }),
        )
        .expect("GPUI failed to open a native window");
        cx.activate(true);
    });
}
