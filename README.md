# GooglySync

Pop!_OS 24 desktop client for Google Drive: Go sync daemon + GTK4 UI + FUSE streaming, built with Bazel.

## Structure

```
.
|-- assets            - icons/branding
|-- cmd               - entry points (drive-daemon, drive-ui, drive-fuse)
|-- configs           - config templates
|-- docs              - additional docs
|-- internal          - core app packages (auth, config, storage, sync, ipc, etc.)
|-- packaging         - packaging assets
|   |-- deb           - .deb packaging files
|   |-- systemd       - systemd user units
|-- pkg               - public/shared packages (if any)
|-- proto             - gRPC definitions
|-- scripts           - tooling helpers
|-- third_party       - external assets or vendored code
|-- tools             - dev/build tools
|-- ui                - GTK UI resources/layouts
```

## Tasks

| Task | Command | Description |
| --- | --- | --- |
| list | `task --list` | List available tasks |
| go:build | `task go:build` | Build daemon with Go toolchain |
| go:test | `task go:test` | Run Go tests |
| go:tidy | `task go:tidy` | Tidy Go modules |
| bazel:build | `task bazel:build` | Build all Bazel targets |
| bazel:test | `task bazel:test` | Run Bazel tests |
| gazelle | `task gazelle` | Update Bazel BUILD files |
| clean | `task clean` | Clean Bazel outputs |
