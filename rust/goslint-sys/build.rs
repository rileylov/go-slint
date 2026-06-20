// Extract the Slint workspace version from the pinned checkout so the shim can
// report it at runtime (Slint exposes no version symbol of its own). Emitted as
// the SLINT_VERSION env var, read via env!() in lib.rs.
use std::path::Path;

fn main() {
    let manifest = Path::new("../../slint/Cargo.toml");
    println!("cargo:rerun-if-changed={}", manifest.display());

    let version = std::fs::read_to_string(manifest)
        .ok()
        .and_then(|text| parse_workspace_version(&text))
        .unwrap_or_else(|| "unknown".to_string());

    println!("cargo:rustc-env=SLINT_VERSION={version}");
}

/// Find `version = "x.y.z"` under the `[workspace.package]` table, falling back
/// to the first top-level `version = "..."` line.
fn parse_workspace_version(text: &str) -> Option<String> {
    let mut in_workspace_package = false;
    let mut fallback = None;
    for line in text.lines() {
        let l = line.trim();
        if l.starts_with('[') {
            in_workspace_package = l == "[workspace.package]";
            continue;
        }
        if l.starts_with("version") && l.contains('=') {
            if let Some(v) = l.split('"').nth(1) {
                if in_workspace_package {
                    return Some(v.to_string());
                }
                fallback.get_or_insert_with(|| v.to_string());
            }
        }
    }
    fallback
}
