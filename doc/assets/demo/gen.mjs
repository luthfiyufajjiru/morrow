// Builds a HyperFrames terminal composition from a captured transcript.
// Transcript format: "@@CMD <command>" lines followed by that command's raw output.
// Usage: node gen.mjs <scene.txt> <outDir> <windowTitle>
import fs from "node:fs";
import path from "node:path";

const [src, outDir, title] = process.argv.slice(2);
const raw = fs.readFileSync(src, "utf8").replace(/\r\n/g, "\n").trimEnd();

const steps = [];
for (const line of raw.split("\n")) {
  if (line.startsWith("@@CMD ")) steps.push({ cmd: line.slice(6), out: [] });
  else steps.at(-1).out.push(line);
}

const FONT = 20, LINE = 28, CHAR = FONT * 0.6, PAD = 36, BAR = 44;
const esc = (s) => s.replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;");

const allLines = steps.flatMap((s) => ["$ " + s.cmd, ...s.out]);
const maxLen = Math.max(...allLines.map((l) => l.length));
const even = (n) => Math.ceil(n / 2) * 2;
const W = even(Math.max(960, maxLen * CHAR + PAD * 2 + 40));
const H = even(BAR + PAD * 2 + allLines.length * LINE + 40);

let t = 0.6;
const rows = [];
const anim = [];
steps.forEach((s, i) => {
  const id = `c${i}`;
  rows.push(`<div class="row" id="p${i}"><span class="ps">$</span> <span class="cmd" id="${id}">${esc(s.cmd)}</span><span class="cur" id="k${i}"></span></div>`);
  const typeDur = Math.min(1.8, Math.max(0.5, s.cmd.length / 30));
  anim.push(`tl.set("#p${i}", { opacity: 1 }, ${t.toFixed(2)});`);
  anim.push(`tl.fromTo("#${id}", { width: "0ch" }, { width: "${s.cmd.length}ch", duration: ${typeDur.toFixed(2)}, ease: "steps(${s.cmd.length})" }, ${(t + 0.25).toFixed(2)});`);
  t += 0.25 + typeDur + 0.35;
  anim.push(`tl.set("#k${i}", { opacity: 0 }, ${t.toFixed(2)});`);
  // Log lines stream in; other command output appears at once, as it does in a real terminal.
  const stream = s.cmd.includes(" logs ");
  s.out.forEach((l, j) => {
    rows.push(`<div class="row out" id="o${i}_${j}">${esc(l) || "&nbsp;"}</div>`);
    anim.push(`tl.set("#o${i}_${j}", { opacity: 1 }, ${(t + (stream ? j * 0.12 : 0.05)).toFixed(2)});`);
  });
  t += (stream ? s.out.length * 0.12 : 0.05) + (s.out.length > 6 ? 2.6 : 1.1);
});
const total = (t + 2.0).toFixed(2);

const html = `<!doctype html>
<html lang="en">
  <head>
    <meta charset="UTF-8" />
    <meta name="viewport" content="width=${W}, height=${H}" />
    <link rel="preconnect" href="https://fonts.googleapis.com" />
    <link href="https://fonts.googleapis.com/css2?family=JetBrains+Mono:wght@400;600&display=block" rel="stylesheet" />
    <script src="https://cdn.jsdelivr.net/npm/gsap@3.14.2/dist/gsap.min.js"></script>
    <style>
      * { margin: 0; padding: 0; box-sizing: border-box; }
      html, body { width: ${W}px; height: ${H}px; overflow: hidden; background: #0d1117; }
      #root { position: relative; width: ${W}px; height: ${H}px; }
      .win { position: absolute; left: 20px; top: 20px; width: ${W - 40}px; height: ${H - 40}px; background: #161b22; border: 1px solid #30363d; border-radius: 12px; overflow: hidden; }
      .bar { height: ${BAR}px; display: flex; align-items: center; gap: 8px; padding: 0 16px; background: #21262d; border-bottom: 1px solid #30363d; }
      .dot { width: 12px; height: 12px; border-radius: 50%; }
      .ttl { flex: 1; text-align: center; margin-right: 52px; color: #8b949e; font: 14px "JetBrains Mono", monospace; }
      .body { padding: ${PAD}px; font: ${FONT}px/${LINE}px "JetBrains Mono", monospace; color: #c9d1d9; }
      .row { white-space: pre; opacity: 0; height: ${LINE}px; }
      .out { color: #b1bac4; }
      .ps { color: #3fb950; font-weight: 600; }
      .cmd { display: inline-block; overflow: hidden; vertical-align: top; color: #f0f6fc; width: 0; }
      .cur { display: inline-block; width: 10px; height: ${LINE - 6}px; margin-left: 2px; vertical-align: top; margin-top: 3px; background: #c9d1d9; }
    </style>
  </head>
  <body>
    <div id="root" data-composition-id="main" data-start="0" data-duration="${total}" data-width="${W}" data-height="${H}">
      <div class="win clip" data-start="0" data-duration="${total}">
        <div class="bar"><span class="dot" style="background:#ff5f57"></span><span class="dot" style="background:#febc2e"></span><span class="dot" style="background:#28c840"></span><span class="ttl">${esc(title)}</span></div>
        <div class="body">
          ${rows.join("\n          ")}
        </div>
      </div>
    </div>
    <script>
      const tl = gsap.timeline({ paused: true });
      ${anim.join("\n      ")}
      window.__timelines["main"] = tl;
      tl.seek(0);
    </script>
  </body>
</html>
`;

fs.mkdirSync(outDir, { recursive: true });
fs.writeFileSync(path.join(outDir, "index.html"), html);
console.log(`${outDir}: ${W}x${H}, ${total}s, ${steps.length} commands`);
