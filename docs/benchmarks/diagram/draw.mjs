// Draws the benchmark platform in the same hand-drawn style as docs/diagram/draw.mjs, reusing its font and logos.
import fs from "node:fs";

const here = new URL(".", import.meta.url).pathname;
const shared = here + "../../diagram/";
const font = fs.readFileSync(shared + "Virgil.woff2").toString("base64");
const logoPath = name => fs.readFileSync(shared + name + ".svg", "utf8").match(/ d="([^"]+)"/)[1];

const W = 2040, H = 1680;
const C = {
  ink: "#1e1e1e", muted: "#495057", gray: "#868e96", grayFill: "#f1f3f5",
  orange: "#e8590c", orangeFill: "#fff4e6",
  blue: "#1971c2", blueFill: "#e7f5ff", blueStrong: "#d0ebff",
  green: "#2f9e44", greenFill: "#ebfbee",
  teal: "#0c8599", tealFill: "#e3fafc",
  red: "#e03131", redFill: "#fff5f5",
  yellow: "#f08c00", yellowFill: "#fff9db",
  mysql: "#4479A1", cassandra: "#1287B1",
};

// Seeded random, so every render jitters the same way.
let seed = 11;
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

function arrow(points, color, { dashed = false } = {}) {
  for (let i = 0; i < points.length - 1; i++) sketchLine(...points[i], ...points[i + 1], color, { dashed });
  const [x1, y1] = points.at(-2), [x2, y2] = points.at(-1);
  const angle = Math.atan2(y2 - y1, x2 - x1);
  for (const side of [-1, 1]) {
    const a = angle + Math.PI + side * 0.42;
    sketchLine(x2, y2, x2 + 15 * Math.cos(a), y2 + 15 * Math.sin(a), color);
  }
}

function logo(name, x, y, size, color) {
  add(`<svg x="${x}" y="${y}" width="${size}" height="${size}" viewBox="0 0 24 24"><path fill="${color}" d="${logoPath(name)}"/></svg>`);
}

// A layer: a container with its label in the top left corner.
function layer(x, y, w, h, color, fill, label, { dashed = false, size = 21 } = {}) {
  box(x, y, w, h, color, { fill, dashed, radius: 18 });
  text(x + 24, y + 38, label, { size, color });
}

function pod(x, y, name, detail, { color = C.blue, fill = "#fff", system = false } = {}) {
  box(x, y, 236, system ? 54 : 96, color, { fill, dashed: system, radius: 10 });
  text(x + 16, y + (system ? 34 : 32), name, { size: system ? 16 : 19, color: system ? C.gray : color });
  if (!system) text(x + 16, y + 58, detail, { size: 15, color: C.muted, lineHeight: 1.35 });
}

function note(x, y, w, h, title, lines) {
  box(x, y, w, h, C.yellow, { fill: C.yellowFill, radius: 18 });
  text(x + 26, y + 42, title, { size: 20, color: C.orange });
  text(x + 26, y + 80, lines, { size: 17, lineHeight: 1.45 });
}

// Title
text(60, 70, "Benchmark platform: a small AWS on one PC", { size: 38 });
text(62, 106, "EKS + RDS shapes, emulated with k3d (Kubernetes in Docker) and docker compose inside WSL2", { size: 18, color: C.gray });

// The layers, from the outside in
layer(40, 140, 1960, 1150, C.ink, "#fff", "PC · Windows 11 · 12 CPU");
layer(70, 200, 1900, 1070, C.green, C.greenFill, "WSL2 · Ubuntu 24.04 · 15 GB RAM · kernel 5.15");
layer(100, 260, 1840, 860, C.teal, C.tealFill, "Docker Engine 29 · the runtime of every container below");
layer(130, 320, 1780, 780, C.orange, C.orangeFill, "VPC = docker network 172.28.0.0/16", { dashed: true });

// docker compose: the databases and the latency proxy
layer(160, 380, 470, 690, C.muted, "#fff", "docker compose · plays RDS", { size: 19 });
box(190, 440, 410, 150, C.red, { fill: C.redFill, radius: 12 });
text(212, 478, "Toxiproxy · .12", { size: 22, color: C.red });
text(212, 512, ["+1 ms on every reply", ":3306 -> MySQL   :9042 -> Cassandra"], { size: 17, color: C.muted, lineHeight: 1.45 });

logo("mysql", 214, 650, 92, C.mysql);
text(326, 676, "MySQL 8.4 · .10", { size: 21, color: C.mysql });
text(326, 708, ["2 vCPU · 4 GB", "tasks + jobs (the queue)"], { size: 16, color: C.muted, lineHeight: 1.45 });

logo("apachecassandra", 214, 860, 92, C.cassandra);
text(326, 886, "Cassandra 5 · .11", { size: 21, color: C.cassandra });
text(326, 918, ["2 vCPU · 2 GB", "job history"], { size: 16, color: C.muted, lineHeight: 1.45 });

arrow([[188, 515], [174, 515], [174, 696], [206, 696]], C.red);
arrow([[174, 696], [174, 906], [206, 906]], C.red);

// Kubernetes cluster
layer(660, 380, 1220, 690, C.blue, C.blueFill, "Kubernetes cluster (k3d) · plays EKS · v1.35", { size: 19 });
box(1500, 396, 360, 44, C.blue, { fill: "#fff", dashed: true, radius: 10 });
text(1680, 425, "load balancer -> WSL :8080 :8081 :18090", { size: 15, color: C.blue, anchor: "middle" });
arrow([[662, 515], [604, 515]], C.red);

const nodes = [
  { name: "node-0", role: "control plane · no app pods", color: C.gray, pods: [
    ["local-path-provisioner", "", { system: true }],
  ] },
  { name: "node-1", role: "worker · 4 GB RAM", color: C.blue, pods: [
    ["task-service", ["0.5 vCPU · 1 GiB", "tasks API"]],
    ["archiver", ["0.5 vCPU · 1 GiB", "old jobs -> Cassandra"]],
    ["metrics-server", "", { system: true }],
  ] },
  { name: "node-2", role: "worker · 4 GB RAM", color: C.blue, pods: [
    ["scheduler", ["0.5 vCPU · 1 GiB", "tasks -> jobs"]],
    ["target", ["2 vCPU · 512 MiB", "fake customer endpoint"], { color: C.orange, fill: C.orangeFill }],
    ["coredns", "", { system: true }],
  ] },
  { name: "node-3", role: "worker · 4 GB RAM", color: C.blue, pods: [
    ["executor", ["0.5 vCPU · 1 GiB", "10,000 workers"], { fill: C.blueStrong }],
    ["job-service", ["0.5 vCPU · 1 GiB", "jobs API"]],
    ["reconciler", ["0.5 vCPU · 1 GiB", "expired leases"]],
  ] },
];
nodes.forEach((node, i) => {
  const x = 680 + i * 296, y = 460;
  box(x, y, 280, 540, node.color, { fill: "#fff", radius: 14 });
  text(x + 20, y + 40, node.name, { size: 24, color: node.color });
  text(x + 20, y + 70, node.role, { size: 15, color: C.muted });
  node.pods.forEach(([name, detail, options = {}], j) => pod(x + 22, y + 100 + j * 116, name, detail, options));
});
text(680, 1040, "pod: 0.5 vCPU · 1 GiB = a Fargate task · pods are placed by Kubernetes · k3d calls node-0 server-0 and node-1..3 agent-0..2", { size: 15, color: C.muted });

// Bench scripts, in WSL next to Docker
box(100, 1140, 1840, 110, C.ink, { fill: "#fff", radius: 14 });
text(124, 1180, "bench/scripts · run in WSL", { size: 21 });
text(124, 1218, "up.sh builds it all · seed.sh writes the tasks into MySQL · latency.sh talks to Toxiproxy · collect.sh samples CPU, memory and disk · results.sh reads MySQL and the target", { size: 16, color: C.muted });

// Notes
note(40, 1320, 630, 170, "Why k3d", [
  "Real Kubernetes (k3s), its nodes are containers.",
  "Same objects as EKS: Deployments, Services,",
  "CPU and memory limits on every pod.",
]);
note(705, 1320, 630, 170, "Why Toxiproxy", [
  "On one PC the database is ~0.05 ms away.",
  "On AWS it sits in another AZ, ~1 ms away.",
  "Toxiproxy adds that 1 ms, with no code change.",
]);
note(1370, 1320, 630, 170, "Sized like AWS", [
  "Each service = a Fargate task (0.5 vCPU, 1 GiB).",
  "MySQL = RDS db.t4g.medium (2 vCPU, 4 GB),",
  "with fsync on every commit, as on RDS.",
]);

box(40, 1515, 1960, 130, C.red, { fill: C.redFill, radius: 18 });
text(66, 1556, "WARNING: one machine, not AWS", { size: 21, color: C.red });
text(66, 1594, [
  "Every container shares the same 12 CPUs and the same disk. The disk has no IOPS cap (the WSL kernel cannot throttle it), so fsync is faster than on RDS.",
  "Trust the comparisons between runs (1 vs 3 executors, before vs after a change) more than the absolute numbers.",
], { size: 17, color: C.red, lineHeight: 1.45 });

const svg = `<svg xmlns="http://www.w3.org/2000/svg" width="${W}" height="${H}" viewBox="0 0 ${W} ${H}">
<style>@font-face{font-family:Virgil;src:url(data:font/woff2;base64,${font}) format("woff2")}text{font-family:Virgil,"Chalkboard SE",cursive}</style>
<rect width="100%" height="100%" fill="#ffffff"/>
${parts.join("\n")}
</svg>`;
fs.writeFileSync(here + "platform.svg", svg);
console.log(`wrote platform.svg (${W}x${H}, ${(svg.length / 1024).toFixed(0)} KB)`);
