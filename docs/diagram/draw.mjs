// Draws the architecture diagram as a hand-drawn style SVG: sketchy strokes, the Virgil font (SIL Open Font License)
// embedded, and the MySQL and Cassandra logos from Simple Icons (CC0).
import fs from "node:fs";

const here = new URL(".", import.meta.url).pathname;
const font = fs.readFileSync(here + "Virgil.woff2").toString("base64");
const logoPath = name => fs.readFileSync(here + name + ".svg", "utf8").match(/ d="([^"]+)"/)[1];

const W = 2040, H = 1560;
const C = {
  ink: "#1e1e1e", muted: "#495057",
  orange: "#e8590c", orangeFill: "#ffe8cc",
  purple: "#7048e8", purpleFill: "#e5dbff",
  blue: "#1971c2", blueFill: "#d0ebff",
  red: "#e03131", green: "#2f9e44",
  gray: "#868e96", grayFill: "#e9ecef",
  mysql: "#4479A1", cassandra: "#1287B1",
};

// Seeded random, so every render jitters the same way.
let seed = 7;
const random = () => { seed = (seed * 16807) % 2147483647; return (seed - 1) / 2147483646; };
const jitter = (amount = 1.2) => (random() - 0.5) * 2 * amount;
const escape = s => s.replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;");

const parts = [];
const add = s => parts.push(s);

function sketchLine(x1, y1, x2, y2, color, { width = 2, dashed = false } = {}) {
  const dash = dashed ? ' stroke-dasharray="9 7"' : "";
  for (let pass = 0; pass < (dashed ? 1 : 2); pass++) {
    const mx = (x1 + x2) / 2 + jitter(2), my = (y1 + y2) / 2 + jitter(2);
    add(`<path d="M${x1 + jitter()} ${y1 + jitter()} Q${mx} ${my} ${x2 + jitter()} ${y2 + jitter()}" fill="none" stroke="${color}" stroke-width="${width}" stroke-linecap="round"${dash}/>`);
  }
}

function roundedPath(x, y, w, h, r, j) {
  const p = () => jitter(j);
  return `M${x + r + p()} ${y + p()} L${x + w - r + p()} ${y + p()} Q${x + w} ${y} ${x + w + p()} ${y + r + p()}
    L${x + w + p()} ${y + h - r + p()} Q${x + w} ${y + h} ${x + w - r + p()} ${y + h + p()}
    L${x + r + p()} ${y + h + p()} Q${x} ${y + h} ${x + p()} ${y + h - r + p()}
    L${x + p()} ${y + r + p()} Q${x} ${y} ${x + r + p()} ${y + p()} Z`;
}

function box(x, y, w, h, color, { fill = "none", dashed = false, radius = 14, width = 2 } = {}) {
  if (fill !== "none") add(`<path d="${roundedPath(x, y, w, h, radius, 0.6)}" fill="${fill}" stroke="none"/>`);
  const dash = dashed ? ' stroke-dasharray="9 7"' : "";
  for (let pass = 0; pass < (dashed ? 1 : 2); pass++) {
    add(`<path d="${roundedPath(x, y, w, h, radius, 1.4)}" fill="none" stroke="${color}" stroke-width="${width}"${dash}/>`);
  }
}

function text(x, y, lines, { size = 17, color = C.ink, anchor = "start", lineHeight = 1.4 } = {}) {
  [].concat(lines).forEach((line, i) => {
    add(`<text x="${x}" y="${y + i * size * lineHeight}" font-size="${size}" fill="${color}" text-anchor="${anchor}" xml:space="preserve">${escape(line)}</text>`);
  });
}

function badge(x, y, label, color, fill) {
  add(`<circle cx="${x}" cy="${y}" r="15" fill="${fill}"/>`);
  for (let pass = 0; pass < 2; pass++) add(`<ellipse cx="${x + jitter(0.8)}" cy="${y + jitter(0.8)}" rx="${15 + jitter(0.8)}" ry="${15 + jitter(0.8)}" fill="none" stroke="${color}" stroke-width="2"/>`);
  text(x, y + 6, label, { size: 16, color, anchor: "middle" });
}

function arrow(points, color, { dashed = false, label, labelAt, both = false } = {}) {
  for (let i = 0; i < points.length - 1; i++) sketchLine(...points[i], ...points[i + 1], color, { dashed });
  const head = (from, to) => {
    const [x1, y1] = from, [x2, y2] = to;
    const angle = Math.atan2(y2 - y1, x2 - x1);
    for (const side of [-1, 1]) {
      const a = angle + Math.PI + side * 0.42;
      sketchLine(x2, y2, x2 + 15 * Math.cos(a), y2 + 15 * Math.sin(a), color);
    }
  };
  head(points.at(-2), points.at(-1));
  if (both) head(points[1], points[0]);
  if (label) text(...labelAt, label, { size: 16, color: C.muted, anchor: "middle" });
}

function service(x, y, w, h, label, color, fill, number) {
  box(x, y, w, h, color, { fill });
  text(x + w / 2, y + h / 2 + 7, label, { size: 20, color, anchor: "middle" });
  if (number) badge(x + 4, y + 2, number, color, fill);
}

function logo(name, x, y, size, color, label) {
  add(`<svg x="${x}" y="${y}" width="${size}" height="${size}" viewBox="0 0 24 24"><path fill="${color}" d="${logoPath(name)}"/></svg>`);
  if (label) text(x + size / 2, y + size + 26, label, { size: 19, color, anchor: "middle" });
}

function person(x, y, scale = 1) {
  const s = scale;
  add(`<circle cx="${x}" cy="${y}" r="${13 * s}" fill="${C.grayFill}" stroke="${C.ink}" stroke-width="2"/>`);
  add(`<path d="${roundedPath(x - 22 * s, y + 17 * s, 44 * s, 34 * s, 10 * s, 0.8)}" fill="${C.grayFill}" stroke="${C.ink}" stroke-width="2"/>`);
}

// Title and legend
text(60, 70, "Distributed Job Scheduler", { size: 38 });
text(62, 104, "high level design · what is built", { size: 18, color: C.gray });
[["1, 9  API", C.orange, C.orangeFill], ["2-3  scheduling", C.purple, C.purpleFill], ["4-8  execution + data", C.blue, C.blueFill]]
  .forEach(([label, color, fill], i) => {
    const x = [1330, 1500, 1700][i], w = [150, 180, 280][i];
    box(x, 44, w, 46, color, { fill, radius: 10 });
    text(x + w / 2, 74, label, { size: 17, color, anchor: "middle" });
  });

// Users and gateway
person(110, 330); person(80, 395); person(140, 395);
text(110, 485, "tenants", { size: 18, anchor: "middle" });
box(220, 190, 100, 420, C.ink, { fill: "#fff", radius: 18 });
text(270, 390, ["API", "Gateway"], { size: 19, anchor: "middle" });
arrow([[175, 410], [215, 410]], C.ink);

// 1 Task service
box(400, 140, 330, 104, C.orange, { dashed: true });
badge(404, 142, "1", C.orange, C.orangeFill);
text(424, 178, ["create · edit · pause · delete tasks", "tenant from the JWT", "Idempotency-Key on POST"], { size: 16, color: C.orange, lineHeight: 1.35 });
service(430, 280, 240, 72, "TASK SERVICE", C.orange, C.orangeFill, "1");
arrow([[322, 316], [426, 316]], C.ink);

// 9 Job service
service(430, 500, 240, 72, "JOB SERVICE", C.orange, C.orangeFill, "9");
arrow([[322, 536], [426, 536]], C.ink);
text(430, 604, ["status · history · DLQ inbox · retry", "reads MySQL + Cassandra,", "dedupes by job_id"], { size: 16, color: C.muted, lineHeight: 1.35 });

// MySQL for tasks
logo("mysql", 790, 248, 120, C.mysql);
arrow([[672, 316], [782, 316]], C.ink);

// 2 Tasks table
box(950, 150, 360, 330, C.ink, { fill: "#fff", radius: 18 });
badge(954, 152, "2", C.purple, C.purpleFill);
text(980, 196, "tasks", { size: 24 });
text(980, 236, [
  "id, tenant_id, name",
  "schedule: at | cron + timezone",
  "priority",
  "request, secret_ref",
  "timeout, retry policy",
  "status: active | paused | deleted",
  "next_run_at",
  "version, created_at, updated_at",
], { size: 17, lineHeight: 1.45 });
text(1345, 190, "INDEX", { size: 17, color: C.green });
text(1345, 222, ["1. (tenant_id, id)", "2. (status, next_run_at)", "3. UNIQUE (tenant_id,", "    idempotency_key)"], { size: 17, color: C.green, lineHeight: 1.45 });

// 3 Scheduler
arrow([[1130, 482], [1130, 556]], C.ink, { label: "due tasks", labelAt: [1190, 526] });
service(1020, 560, 230, 76, "SCHEDULER", C.purple, C.purpleFill, "3");
box(1330, 520, 420, 156, C.purple, { dashed: true });
badge(1334, 522, "3", C.purple, C.purpleFill);
text(1356, 560, [
  "a. every 5 s: active tasks due in 30 s",
  "b. one tx: INSERT the jobs as pending",
  "    + advance next_run_at",
  "c. UNIQUE (task_id, scheduled_at)",
], { size: 16, color: C.purple, lineHeight: 1.4 });

// 4 Jobs hot table
arrow([[1135, 638], [1135, 744]], C.ink, { label: "INSERT pending", labelAt: [1215, 700] });
box(880, 750, 590, 372, C.ink, { radius: 22 });
badge(884, 752, "4", C.blue, C.blueFill);
logo("mysql", 898, 800, 120, C.mysql);
box(1030, 772, 420, 330, C.ink, { fill: "#fff", radius: 16 });
text(1054, 814, "jobs (hot table + queue)", { size: 23 });
text(1054, 852, [
  "id = Idempotency-Key",
  "tenant_id, task_id, scheduled_at",
  "request, timeout, retry (copied)",
  "status: pending | running |",
  "           succeeded | dead",
  "attempt / max_attempts",
  "next_attempt_at",
  "worker_id, lease_expires_at",
  "result: http_status, error, body",
], { size: 17, lineHeight: 1.42 });
text(1505, 790, "INDEX", { size: 17, color: C.green });
text(1505, 822, [
  "1. (status, priority, next_attempt_at)",
  "    claim",
  "2. (status, lease_expires_at)",
  "    reconciler",
  "3. (status, finished_at)  archiver",
  "4. UNIQUE (task_id, scheduled_at)",
  "5. (tenant_id, status)  DLQ",
], { size: 17, color: C.green, lineHeight: 1.45 });

// Job service reads the hot table
arrow([[672, 536], [800, 536], [800, 790], [876, 790]], C.muted, { dashed: true, label: "read", labelAt: [770, 680] });

// 5 Executors
box(60, 700, 400, 214, C.blue, { dashed: true });
badge(64, 702, "5", C.blue, C.blueFill);
text(84, 740, [
  "a. claim as many as free workers,",
  "    one poll ahead (SKIP LOCKED)",
  "b. running + lease + attempt + 1",
  "c. COMMIT, wait until its time",
  "d. call with the job's timeout",
  "e. save only if still theirs",
  "    (worker_id + attempt)",
], { size: 16, color: C.blue, lineHeight: 1.38 });
service(480, 760, 230, 76, "EXECUTOR", C.blue, C.blueFill, "5");
service(500, 810, 230, 76, "EXECUTOR", C.blue, C.blueFill);
service(520, 860, 230, 76, "EXECUTOR", C.blue, C.blueFill);
arrow([[754, 900], [876, 900]], C.ink, { both: true, label: "claim / save", labelAt: [815, 884] });
arrow([[260, 916], [260, 960]], C.blue);
box(60, 964, 420, 188, C.red, { radius: 12 });
text(84, 1000, "CLAIM (exclusion)", { size: 17, color: C.red });
text(84, 1034, [
  "SELECT id FROM jobs",
  "WHERE status = 'pending'",
  "  AND next_attempt_at <= NOW() + lead",
  "ORDER BY priority, next_attempt_at",
  "LIMIT free_workers",
  "FOR UPDATE SKIP LOCKED",
], { size: 16, lineHeight: 1.36 });

// SSRF firewall: the executor's own dialer, drawn as a firewall on the way out
function firewall(x, y, w, h) {
  add(`<path d="${roundedPath(x, y, w, h, 10, 0.6)}" fill="#fff0f0" stroke="none"/>`);
  for (let row = 0, top = y + 8; top < y + h - 8; row++, top += 16) {
    sketchLine(x + 6, top, x + w - 6, top, "#ffc9c9", { width: 1.5 });
    for (let bx = x + 6 + (row % 2) * 22; bx < x + w - 6; bx += 44) sketchLine(bx, top, bx, Math.min(top + 16, y + h - 8), "#ffc9c9", { width: 1.5 });
  }
  box(x, y, w, h, C.red, { radius: 10 });
}
arrow([[630, 940], [630, 1004]], C.ink);
firewall(500, 1010, 360, 116);
add(`<rect x="540" y="1022" width="280" height="88" rx="10" fill="#fff0f0" opacity="0.92"/>`);
text(680, 1048, "SSRF FIREWALL", { size: 20, color: C.red, anchor: "middle" });
text(680, 1074, ["public IPs only · ports 80, 443", "no proxy · no redirects"], { size: 16, color: C.ink, anchor: "middle", lineHeight: 1.35 });
text(680, 1150, "(inside the executor's dialer)", { size: 15, color: C.muted, anchor: "middle" });
arrow([[520, 1126], [456, 1196]], C.red, { dashed: true });
text(70, 1214, ["x  10.0.0.0/8 · 127.0.0.1", "x  169.254.169.254 (cloud metadata)"], { size: 16, color: C.red, lineHeight: 1.4 });

// 6 External endpoints: the tenants' own APIs on the public internet
box(180, 1290, 780, 200, C.gray, { dashed: true, radius: 22 });
text(204, 1474, "public internet · tenant APIs", { size: 17, color: C.gray });
text(940, 1474, "each one dedupes by Idempotency-Key", { size: 15, color: C.gray, anchor: "end" });
arrow([[600, 1164], [320, 1330]], C.ink, { label: "HTTPS", labelAt: [430, 1236] });
arrow([[680, 1164], [574, 1330]], C.ink);
arrow([[760, 1164], [828, 1330]], C.ink);
function server(x, y) {
  for (let i = 0; i < 3; i++) {
    add(`<path d="${roundedPath(x, y + i * 19, 40, 15, 4, 0.5)}" fill="${C.grayFill}" stroke="${C.ink}" stroke-width="1.6"/>`);
    add(`<circle cx="${x + 32}" cy="${y + i * 19 + 7.5}" r="2.4" fill="${C.green}"/>`);
  }
}
[
  [200, "api.acme-shop.com", "POST /webhooks/orders"],
  [454, "billing.payflow.io", "POST /v1/invoices/run"],
  [708, "crm.bluebird.dev", "POST /reports/daily"],
].forEach(([x, host, route]) => {
  box(x, 1334, 238, 96, C.ink, { fill: "#fff", radius: 12 });
  server(x + 12, 1352);
  text(x + 62, 1372, host, { size: 16 });
  text(x + 62, 1400, route, { size: 14, color: C.muted });
});

badge(194, 1524, "6", C.gray, C.grayFill);
text(220, 1530, "headers on every call: Idempotency-Key: job_id · X-Scheduled-At · X-Attempt", { size: 17, color: C.muted });

// 7 Reconciler
arrow([[1100, 1236], [1100, 1126]], C.ink, { dashed: true, both: true });
service(990, 1240, 220, 72, "RECONCILER", C.blue, C.blueFill, "7");
text(990, 1344, ["running + expired lease", "-> pending | dead"], { size: 16, color: C.muted, lineHeight: 1.35 });

// 8 Archiver
arrow([[1340, 1126], [1340, 1236]], C.ink);
service(1250, 1240, 220, 72, "ARCHIVER", C.blue, C.blueFill, "8");
text(1250, 1344, ["terminal jobs -> Cassandra,", "then DELETE from hot"], { size: 16, color: C.muted, lineHeight: 1.35 });
arrow([[1472, 1276], [1560, 1276]], C.ink, { label: "write first", labelAt: [1516, 1260] });

// Cassandra
logo("apachecassandra", 1570, 1214, 110, C.cassandra, "Cassandra");
text(1712, 1222, [
  "job_history",
  "PK ((tenant_id, task_id, month),",
  "     scheduled_at DESC, job_id)",
  "job_by_id",
  "PK ((tenant_id, job_id))",
  "TTL 30 days",
], { size: 16, color: C.cassandra, lineHeight: 1.4 });

const svg = `<svg xmlns="http://www.w3.org/2000/svg" width="${W}" height="${H}" viewBox="0 0 ${W} ${H}">
<style>@font-face{font-family:Virgil;src:url(data:font/woff2;base64,${font}) format("woff2")}text{font-family:Virgil,"Chalkboard SE",cursive}</style>
<rect width="100%" height="100%" fill="#ffffff"/>
${parts.join("\n")}
</svg>`;
fs.writeFileSync(here + "distributed-scheduler.svg", svg);
console.log(`wrote distributed-scheduler.svg (${W}x${H}, ${(svg.length / 1024).toFixed(0)} KB)`);
