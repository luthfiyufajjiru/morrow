# README demo GIFs

The GIFs in `doc/assets/` are rendered from **real** command output with [HyperFrames](https://github.com/heygen-com/hyperframes).

1. Build Linux binaries (`CGO_ENABLED=0 GOOS=linux`) of morrow and `demo-api/` into one folder, together with `capture.sh`.
2. Run `sh capture.sh` on Linux (or WSL). It records each command and its output to `scene1.txt` / `scene2.txt`.
3. `npx hyperframes@0.8.150 init <dir> --example blank`, then `node gen.mjs scene1.txt <dir> "morrow — quick start"` to write the composition.
4. `npx hyperframes render -o scene1.mp4` in `<dir>`, then convert:
   `ffmpeg -i scene1.mp4 -vf "fps=12,split[a][b];[a]palettegen=max_colors=64:stats_mode=diff[p];[b][p]paletteuse=dither=none:diff_mode=rectangle" -loop 0 quickstart.gif`

Set `DO_NOT_TRACK=1 HYPERFRAMES_NO_TELEMETRY=1` to disable HyperFrames telemetry.
