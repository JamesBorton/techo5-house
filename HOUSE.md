# Our TECHO5 (house fork)

TECHO5 (upstream: https://github.com/HuskerMinion/techo5, MIT) with a few changes for our house. This
repo is private: it keeps upstream's history, so upstream releases can be merged or rebased onto it.

**Branch `main` = upstream tag `v0.9.14` + our commits.** Every one of our commits is on top of
upstream's; `git log v0.9.14..main` lists them all.

## What we changed

| Change | Where | Home Assistant entities (Living Room Echo) |
|---|---|---|
| **Presence**: opt-in occupancy from touches, voice turns, room sound (ignoring the Echo's own playback) and a quick camera look every 15–20 s (two 16×12 brightness grids compared; nothing saved or sent). Off by default. | `echod/internal/feature/presence/`, `config/presence.go`, `hardware/camera/luma.go` | `binary_sensor.living_room_living_room_presence`, switches *Presence detection* / *Presence uses the camera*, numbers *Presence hold* (s) / *motion threshold* (%) / *sound threshold* (%), diagnostics *motion score* / *last cue* |
| **Tap opens the dashboard**: a tap on the clock/photo opens the dashboard instead of starting a voice turn; how long it stays up is a setting. | `feature/dashboard/dashboard.go`, `feature/display/dashboard.go`, `display.go` | switch *Tap opens the dashboard*, number *Dashboard closes after* (min) |
| **dashcast `DASHCAST_WARM=0`**: no parked ("warm") tabs; a thawed tab stopped taking touches from the Echos. | `dashcast/serve.go` | set in `/opt/dashcast/docker-compose.yml` on Hermes |

The camera look is skipped while the lens cover is closed or the mic is muted. Flipping the mute
latch still takes the camera away until a reboot (upstream behaviour); presence carries on without it.

## How it is deployed

- **Living Room Echo (Show 8, `crown`):** slot b = the official v0.9.14 rootfs with only
  `/usr/local/bin/techo5` replaced by our build (`tools/house/repack_rootfs.py`), installed with
  `slotctl install`. Slot a is the official v0.9.14, the fallback.
- **Office Echo (Show 5, `cronos`):** the same package, installed the same way on 2026-09-29 (slot a = ours,
  slot b = official v0.9.14). One armv7 daemon serves both models.
- **dashcast:** built from `dashcast/` on Hermes (`/opt/dashcast`), with `DASHCAST_WARM=0`.

Version string: `v0.9.14_presence` (the `_build` suffix is the only form Home Assistant's update check
accepts besides a plain release).

## Rebuilding and installing

In WSL (Go ≥ 1.26):

```
cd echod
pkg=github.com/HuskerMinion/techo5/echod/internal/layout
GOOS=linux GOARCH=arm GOARM=7 CGO_ENABLED=0 go build -trimpath \
  -ldflags "-s -w -X $pkg.Version=v0.9.14_presence -X $pkg.GitCommit=$(git rev-parse --short HEAD) -X $pkg.BuildDate=$(date -u +%FT%TZ)" \
  -o ../bin/echod-arm ./cmd/echod
cd ..
python3 tools/house/repack_rootfs.py <official rootfs-vX.Y.Z.tar.gz> bin/echod-arm rootfs-vX.Y.Z_presence.tar.gz
```

The official rootfs comes from the upstream release (`install-show.py` keeps a verified copy in
`build/release-techo5-vX.Y.Z/`). Then on the Echo (SSH switch on in HA, our key):

```
scp -O rootfs-vX.Y.Z_presence.tar.gz root@<echo>:/data/techo5-linux/
ssh root@<echo> 'PATH=/usr/local/sbin:$PATH; slotctl install /data/techo5-linux/rootfs-vX.Y.Z_presence.tar.gz && reboot'
```

The new slot boots on trial and commits itself after five healthy minutes, or falls back.

**Quick test without installing:** `scp -O bin/echod-arm root@<echo>:/tmp/echod-test`, then
`mount --bind /tmp/echod-test /usr/local/bin/techo5 && killall techo5`. A reboot undoes it.

## When upstream releases a new version

Don't press *Update* in Home Assistant on the Living Room Echo: it installs the official release and
our changes are gone until reinstalled (the other slot keeps ours for a rollback: `slotctl rollback`).
Instead:

```
git remote add upstream https://github.com/HuskerMinion/techo5.git   # once
git fetch upstream --tags
git rebase --onto vX.Y.Z v0.9.14 main    # or merge vX.Y.Z; fix conflicts, re-run the tests
cd echod && go test ./... && cd ..
```

Then rebuild with version `vX.Y.Z_presence`, repack the new release's rootfs, and install as above.
Rebuild dashcast on Hermes too if `dashcast/` changed upstream.
