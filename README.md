# GooglySync

Pop!_OS 24 desktop client for Google Drive: Go sync daemon + GTK4 UI + FUSE streaming, built with Bazel.

## Structure

```
.
|-- assets
|-- cmd
|-- configs
|-- docs
|-- internal
|-- packaging
|   |-- deb
|   `-- systemd
|-- pkg
|-- proto
|-- scripts
|-- third_party
|-- tools
`-- ui
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
