# googlysync

<img src="assets/googlysync-icon.png" alt="googlysync icon" width="200">

Pop!_OS 24 desktop client for Google Drive: Go sync daemon + GTK4 UI + FUSE streaming, built with Bazel.

## Structure

```
.
|-- assets            - icons/branding
|-- cmd               - entry point (googlysync CLI)
|-- configs           - config templates
|-- docs              - additional docs
|-- internal          - core app packages (auth, config, storage, sync, ipc, etc.)
|-- packaging         - packaging assets
|   |-- deb           - .deb packaging files
|   `-- systemd       - systemd user units
|-- pkg               - public/shared packages (if any)
|-- proto             - gRPC definitions
|-- scripts           - tooling helpers
|-- third_party       - external assets or vendored code
|-- tools             - dev/build tools
`-- ui                - GTK UI resources/layouts
```

## Tasks

| Task | Command | Description |
| --- | --- | --- |
| list | `task --list` | List available tasks |
| bazel:build | `task bazel:build` | Build all Bazel targets |
| bazel:test | `task bazel:test` | Run Bazel tests |
| gazelle | `task gazelle` | Update Bazel BUILD files |
| wire | `task wire` | Generate Wire DI files |
| wire:check | `task wire:check` | Verify Wire outputs are up to date |
| buf:gen | `task buf:gen` | Generate gRPC code via Buf |
| goose | `task goose -- <cmd>` | Run Goose migrations via Bazel |
| run:daemon | `task run:daemon` | Build and run daemon |
| run:status | `task run:status` | Build and run status once |
| run:ping | `task run:ping` | Build and ping daemon |
| run:tui | `task run:tui` | Build and run status TUI |
| clean | `task clean` | Clean Bazel outputs |

## Run (dev)

- Build: `task bazel:build`
- Start daemon: `task run:daemon`
- Open status TUI: `task run:tui`
- Status once: `task run:status`
- Ping daemon: `task run:ping`

## Logging

Config file fields (JSON):
- `log_level`
- `log_file_path`
- `log_file_max_mb`
- `log_file_max_backups`
- `log_file_max_age_days`

Env overrides:
- `GOOGLYSYNC_LOG_LEVEL`
- `GOOGLYSYNC_LOG_FILE`
- `GOOGLYSYNC_LOG_MAX_MB`
- `GOOGLYSYNC_LOG_MAX_BACKUPS`
- `GOOGLYSYNC_LOG_MAX_AGE_DAYS`
