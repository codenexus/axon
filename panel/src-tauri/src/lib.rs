use std::fs;

use serde::{Deserialize, Serialize};
use tauri::menu::{Menu, MenuItem};
use tauri::{AppHandle, LogicalSize, Manager, WebviewUrl, WebviewWindowBuilder};

#[derive(Serialize, Deserialize, Default)]
struct DesktopConfig {
    panel_url: Option<String>,
}

// The real Panel window and the tiny local config page share the same
// "main" window id, but the config page (ui/index.html) is deliberately
// small -- once an admin picks a URL, set_panel_url navigates that same
// window in place rather than opening a new one, and navigate() alone
// doesn't resize it. Without an explicit resize afterward the whole app
// stays stuck at the config page's small dimensions, which makes Panel's
// own centered, max-width layout look broken (its background color fills
// the excess space) and can push header controls like Settings outside
// the visible/clickable area entirely.
const PANEL_WINDOW_SIZE: (f64, f64) = (1100.0, 800.0);

// The desktop shell is a thin client, not a local Panel backend: it just
// remembers which already-running Panel instance to point the webview
// at (same thing you'd type into a browser), the same way this same
// SvelteKit app works when accessed remotely or self-hosted. Axon
// doesn't need to know anything about how that Panel is reachable
// (LAN, Tailscale, a public domain) — that's the admin's own choice,
// unchanged from opening Panel in a normal browser tab.
fn config_path(app: &AppHandle) -> Result<std::path::PathBuf, String> {
    let dir = app
        .path()
        .app_config_dir()
        .map_err(|e| format!("resolve app config dir: {e}"))?;
    fs::create_dir_all(&dir).map_err(|e| format!("create app config dir: {e}"))?;
    Ok(dir.join("desktop-config.json"))
}

fn load_config(app: &AppHandle) -> DesktopConfig {
    let Ok(path) = config_path(app) else {
        return DesktopConfig::default();
    };
    let Ok(data) = fs::read_to_string(&path) else {
        return DesktopConfig::default();
    };
    serde_json::from_str(&data).unwrap_or_default()
}

fn save_config(app: &AppHandle, config: &DesktopConfig) -> Result<(), String> {
    let path = config_path(app)?;
    let data = serde_json::to_string_pretty(config).map_err(|e| e.to_string())?;
    fs::write(&path, data).map_err(|e| format!("write config: {e}"))
}

#[tauri::command]
fn get_panel_url(app: AppHandle) -> Option<String> {
    load_config(&app).panel_url
}

#[tauri::command]
fn set_panel_url(app: AppHandle, url: String) -> Result<(), String> {
    let url = url.trim().to_string();
    if url.is_empty() {
        return Err("enter a URL".into());
    }
    if !url.starts_with("http://") && !url.starts_with("https://") {
        return Err("URL must start with http:// or https://".into());
    }
    let parsed = tauri::Url::parse(&url).map_err(|e| format!("invalid URL: {e}"))?;

    save_config(&app, &DesktopConfig { panel_url: Some(url) })?;

    if let Some(window) = app.get_webview_window("main") {
        window
            .navigate(parsed)
            .map_err(|e| format!("navigate: {e}"))?;
        window
            .set_size(LogicalSize::new(PANEL_WINDOW_SIZE.0, PANEL_WINDOW_SIZE.1))
            .map_err(|e| format!("resize window: {e}"))?;
    }
    Ok(())
}

// Rebuilds the main window pointed at the bundled local config page
// (ui/index.html) — used both on first run (no panel_url saved yet) and
// when the admin picks "Change Panel URL…" from the menu. Closing and
// recreating the window, rather than navigating an already-remote
// window back to a local asset, sidesteps needing to know the exact
// platform-specific internal URL scheme Tauri uses for bundled assets.
fn show_config_page(app: &AppHandle) {
    if let Some(window) = app.get_webview_window("main") {
        let _ = window.close();
    }
    let _ = WebviewWindowBuilder::new(app, "main", WebviewUrl::App("index.html".into()))
        .title("Axon Panel")
        .inner_size(420.0, 420.0)
        .resizable(true)
        .build();
}

fn show_panel(app: &AppHandle, url: &str) -> Result<(), String> {
    let parsed = tauri::Url::parse(url).map_err(|e| format!("invalid saved URL: {e}"))?;
    WebviewWindowBuilder::new(app, "main", WebviewUrl::External(parsed))
        .title("Axon Panel")
        .inner_size(PANEL_WINDOW_SIZE.0, PANEL_WINDOW_SIZE.1)
        .resizable(true)
        .build()
        .map_err(|e| format!("build window: {e}"))?;
    Ok(())
}

#[cfg_attr(mobile, tauri::mobile_entry_point)]
pub fn run() {
    tauri::Builder::default()
        .invoke_handler(tauri::generate_handler![get_panel_url, set_panel_url])
        .on_menu_event(|app, event| {
            if event.id() == "change_url" {
                show_config_page(app);
            }
        })
        .setup(|app| {
            if cfg!(debug_assertions) {
                app.handle().plugin(
                    tauri_plugin_log::Builder::default()
                        .level(log::LevelFilter::Info)
                        .build(),
                )?;
            }

            let handle = app.handle().clone();

            match load_config(&handle).panel_url {
                Some(url) => {
                    if let Err(err) = show_panel(&handle, &url) {
                        log::warn!("failed to open saved Panel URL ({err}); falling back to config page");
                        show_config_page(&handle);
                    }
                }
                None => show_config_page(&handle),
            }

            let change_url = MenuItem::with_id(app, "change_url", "Change Panel URL…", true, None::<&str>)?;
            let menu = Menu::with_items(app, &[&change_url])?;
            app.set_menu(menu)?;

            Ok(())
        })
        .run(tauri::generate_context!())
        .expect("error while running tauri application");
}
