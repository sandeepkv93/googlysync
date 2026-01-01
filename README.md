# GooglySync

Pop!_OS 24 desktop client for Google Drive: Go sync daemon + GTK4 UI + FUSE streaming, built with Bazel.

## Structure

- `cmd/`: entry points (`drive-daemon`, `drive-ui`, `drive-fuse`)
- `internal/`: core app packages (auth, config, storage, sync, ipc, etc.)
- `proto/`: gRPC definitions
- `configs/`: config templates
- `ui/`: GTK UI resources/layouts
- `assets/`: icons/branding
- `packaging/`: .deb + systemd user service files
- `scripts/`: tooling helpers
- `docs/`: additional docs
