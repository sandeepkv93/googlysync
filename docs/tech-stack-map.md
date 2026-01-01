# Tech Stack to Repo Map

- Sync engine (core daemon): `cmd/drive-daemon`, `internal/sync`, `internal/daemon`, `internal/fswatch`
- Drive API: `internal/driveapi`
- Metadata store (SQLite + migrations): `internal/storage`
- Filesystem watchers: `internal/fswatch`
- Streaming mode (FUSE): `cmd/drive-fuse`, `internal` (future FUSE logic)
- UI (GTK/libadwaita): `cmd/drive-ui`, `ui/`, `assets/`
- Secure credential storage: `internal/auth`
- OAuth 2.0 login: `internal/auth`
- IPC (gRPC + protobuf): `proto/`, `internal/ipc`
- Packaging/updates: `packaging/` (`packaging/deb/`, `packaging/systemd/`)
- Build system (Bazel/rules_go/gazelle): `WORKSPACE.bazel`, `MODULE.bazel`, `BUILD.bazel`, `tools/`
- Logging/diagnostics: `internal/logging`
