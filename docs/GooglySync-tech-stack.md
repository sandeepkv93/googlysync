# Best Tech Stack: GooglySync Desktop Client (Pop!_OS 24)

## Summary
A Go-first architecture with a native GNOME UI and a dedicated sync daemon, built with Bazel. This stack optimizes performance, native integration, and long-term maintainability on Pop!_OS 24.

## Core Components

### 1) Sync Engine (Core Daemon)
- Language: Go
- Google Drive API: `google.golang.org/api/drive/v3`
- Background jobs: Go goroutines + worker pools
- Resumable uploads/downloads: Drive API resumable sessions
- Retry/backoff: `github.com/cenkalti/backoff/v4`

### 2) Local Metadata Store
- Database: SQLite
- Go driver: `modernc.org/sqlite`
- Migration tool: `github.com/pressly/goose/v3`

### 3) Filesystem Watchers
- Local FS events: `github.com/fsnotify/fsnotify`
- Linux inotify support via fsnotify

### 4) Streaming Mode (Virtual Filesystem)
- FUSE implementation: `github.com/hanwen/go-fuse/v2`
- Placeholder files + on-demand download

### 5) User Interface (Native GNOME)
- UI toolkit: GTK4 + libadwaita
- Go bindings: `github.com/diamondburned/gotk4`
- Tray integration: StatusNotifierItem via GTK/DBus
- Notifications: `org.freedesktop.Notifications` (DBus)

### 6) Secure Credential Storage
- Keyring: GNOME Keyring (libsecret)
- Go bindings: `github.com/zalando/go-keyring` or direct libsecret binding

### 7) OAuth 2.0 Login
- Flow: System browser OAuth 2.0 with PKCE
- Local redirect handler: Go HTTP server on localhost
- Token storage: Encrypted in Keyring + minimal disk cache

### 8) IPC Between UI and Daemon
- Protocol: gRPC over Unix domain sockets
- Protobuf generation: `google.golang.org/grpc` + `google.golang.org/protobuf`

### 9) Packaging and Updates
- Packaging: `.deb` for Pop!_OS/Ubuntu
- Auto-updater: APT repo + signed packages
- System integration: systemd user service for daemon autostart
### 10) Build System
- Build tool: Bazel
- Rules: `rules_go`, `gazelle`, `rules_pkg`
- Native deps: GTK4/libadwaita via system packages referenced by Bazel targets

### 11) Logging and Diagnostics
- Structured logging: `go.uber.org/zap`
- Log rotation: `gopkg.in/natefinch/lumberjack.v2`
- Diagnostics export: ZIP log bundle

## Runtime Layout
- UI app: `drive-ui`
- Daemon: `drive-daemon`
- FUSE mount helper: `drive-fuse`
- Config/data: `$XDG_CONFIG_HOME/drive-client` and `$XDG_DATA_HOME/drive-client`
