# Integration tests run in Docker Compose

The torrent client integration tests run in a Go container next to the real daemons in one Docker Compose project. All containers mount one named volume at `/data`. The tests create hardlinked season packs, and each daemon must see them at the same import path as the test process. On Docker Desktop for macOS, bind mounts cause transient hardlink `file_stat` errors, and only a named volume avoids them. Local runs and CI use the same compose file, so a pass on one is evidence for the other.

## Considered Options

- **testcontainers-go in `TestMain`**: `go test -tags=integration` alone would be enough, but on macOS the test process runs on the host. The shared import folder would then need a bind mount, which breaks hardlinks. It also adds a Go dependency.
- **GitHub Actions `services:`**: works in CI only and gives no local parity.
