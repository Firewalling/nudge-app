//! Experimental, read-only GPUI frontend for Nudge.
//!
//! Intentionally separate from the Go daemon and existing Svelte dashboard.
//! All HTTP requests target the existing IPv4 loopback API.

use std::time::Duration;

use gpui::{
    App, Application, Bounds, Context, Window, WindowBounds, WindowOptions, div, prelude::*, px, rgb, size,
};
use serde::Deserialize;

#[derive(Default, Deserialize)]
struct Status {
    active: bool,
    scheduler_ready: bool,
    focused_app: String,
    niri_error: String,
}

#[derive(Default, Deserialize)]
struct Config {
    reminders: Vec<Reminder>,
}

#[derive(Default, Deserialize)]
struct Reminder {
    message: String,
    every: String,
    #[serde(default)]
    app: String,
    #[serde(default)]
    disabled: bool,
}

#[derive(Default)]
struct Snapshot {
    status: Option<Status>,
    reminders: Option<Vec<Reminder>>,
    error: Option<String>,
    loading: bool,
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

fn read_json<T: serde::de::DeserializeOwned>(agent: &ureq::Agent, base: &str, path: &str) -> Result<T, String> {
    let url = format!("{base}{path}");
    let response = agent.get(&url).call().map_err(|err| format!("{path}: {err}"))?;
    if !(200..300).contains(&response.status()) {
        return Err(format!("{path}: HTTP {}", response.status()));
    }
    response.into_json::<T>().map_err(|err| format!("{path}: {err}"))
}

fn api_agent() -> ureq::Agent {
    ureq::AgentBuilder::new()
        .try_proxy_from_env(false)
        .redirects(0)
        .timeout(Duration::from_millis(800))
        .build()
}

impl Snapshot {
    fn load() -> Self {
        let agent = api_agent();
        let base = base_url();
        let status: Result<Status, String> = read_json(&agent, &base, "/api/v1/status");
        let config: Result<Config, String> = read_json(&agent, &base, "/api/v1/config");
        let error = status
            .as_ref()
            .err()
            .or_else(|| config.as_ref().err())
            .cloned();

        Self {
            status: status.ok(),
            reminders: config.ok().map(|value| value.reminders),
            error,
            loading: false,
        }
    }
}

struct NudgeDesktop {
    snapshot: Snapshot,
}

impl NudgeDesktop {
    fn new(cx: &mut Context<Self>) -> Self {
        cx.spawn(async move |this, cx| {
            loop {
                let snapshot = cx.background_executor().spawn(async { Snapshot::load() }).await;
                if this.update(cx, |view, cx| {
                    view.snapshot = snapshot;
                    cx.notify();
                }).is_err() {
                    break;
                }
                cx.background_executor().timer(Duration::from_secs(5)).await;
            }
        }).detach();
        Self {
            snapshot: Snapshot { loading: true, ..Default::default() },
        }
    }
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
            None if self.snapshot.loading => "Connecting to daemon",
            None => "Daemon unavailable",
            Some(state) if !state.active => "Paused or in quiet hours",
            Some(state) if !state.scheduler_ready => "Waiting for scheduler",
            Some(_) => "Reminders running",
        };

        let mut reminders = div().flex().flex_col().gap_3();
        for reminder in self.snapshot.reminders.iter().flatten() {
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
                                    .child(self.snapshot.reminders.as_ref().map_or_else(|| "Reminders unavailable".to_string(), |items| format!("{} configured", items.len()))),
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
                    .when(self.snapshot.reminders.as_ref().is_some_and(Vec::is_empty), |view| {
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
    Application::new().run(move |cx: &mut App| {
        let bounds = Bounds::centered(None, size(px(900.0), px(620.0)), cx);
        cx.open_window(
            WindowOptions {
                window_bounds: Some(WindowBounds::Windowed(bounds)),
                ..Default::default()
            },
            |_, cx| cx.new(NudgeDesktop::new),
        )
        .expect("GPUI failed to open a native window");
        cx.activate(true);
    });
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::{io::Write, net::TcpListener, thread};

    #[test]
    fn refuses_api_redirects() {
        let listener = TcpListener::bind("127.0.0.1:0").unwrap();
        let base = format!("http://{}", listener.local_addr().unwrap());
        let server = thread::spawn(move || {
            let (mut stream, _) = listener.accept().unwrap();
            stream.write_all(b"HTTP/1.1 302 Found\r\nLocation: http://127.0.0.1:1/\r\nContent-Length: 0\r\n\r\n").unwrap();
        });
        let result: Result<Status, String> = read_json(&api_agent(), &base, "/api/v1/status");
        assert_eq!(result.err().as_deref(), Some("/api/v1/status: HTTP 302"));
        server.join().unwrap();
    }

    #[test]
    fn incomplete_status_is_not_treated_as_paused() {
        let listener = TcpListener::bind("127.0.0.1:0").unwrap();
        let base = format!("http://{}", listener.local_addr().unwrap());
        let server = thread::spawn(move || {
            let (mut stream, _) = listener.accept().unwrap();
            stream.write_all(b"HTTP/1.1 200 OK\r\nContent-Type: application/json\r\nContent-Length: 2\r\n\r\n{}").unwrap();
        });
        let result: Result<Status, String> = read_json(&api_agent(), &base, "/api/v1/status");
        assert!(result.err().is_some_and(|error| error.contains("missing field")));
        server.join().unwrap();
    }
}
