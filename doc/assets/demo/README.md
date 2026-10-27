# README demo GIFs

The GIFs in `doc/assets/` are rendered from **real** command output with [HyperFrames](https://github.com/heygen-com/hyperframes):

| GIF | Scene |
| --- | --- |
| `quickstart.gif` | `scene1.txt`: init, create with a working directory, start, list (as a regular user), logs |
| `secured-env.gif` | `scene2.txt`: secured variable, restart, `detail` as a regular user (masked), logs |
| `pipeline-secrets.gif` | `scene3.txt`: enable the inbox, grant `ci`, `ci` submits a token it cannot read back, apply, outcome |

1. Build Linux binaries (`CGO_ENABLED=0 GOOS=linux`) of morrow and `demo-api/` into one folder, together with `capture.sh`.
2. On a throwaway Linux machine (or WSL), run `sh capture.sh` as root. It installs a real system install (`/etc/morrow`, `/var/lib/morrow`, `/var/log/morrow`), a `morrow-deploy` group and a demo user `ci`, records every command with its output, and removes all of them at the end (it refuses to run if any already exist). Lines shown with `sudo` run as root; the others run as `ci`. Output: `scene1.txt`, `scene2.txt`, `scene3.txt` and `detail.json` (the README's JSON example).
3. `npx hyperframes@0.8.150 init <dir> --example blank`, then `node gen.mjs scene1.txt <dir> "morrow — quick start"` to write the composition.
4. `npx hyperframes render -o scene1.mp4` in `<dir>`, then convert:
   `ffmpeg -i scene1.mp4 -vf "fps=12,split[a][b];[a]palettegen=max_colors=64:stats_mode=diff[p];[b][p]paletteuse=dither=none:diff_mode=rectangle" -loop 0 quickstart.gif`

Set `DO_NOT_TRACK=1 HYPERFRAMES_NO_TELEMETRY=1` to disable HyperFrames telemetry.
