#!/usr/bin/env node
// Benchmarks the shell pipelines used by stickydisk's Go cache trimming
// (du, find|sort, xargs rm, sudo rm -rf) against pure-Node fs equivalents,
// on the real sticky-disk Go caches.
//
// Non-destructive ops (size, list) run directly on the live GOCACHE and
// GOMODCACHE. Destructive ops (evict, bulk delete, wipe) run on cp -a
// copies placed in WORK_DIR on the same disk. Every measurement runs cold
// (page/dentry caches dropped) unless marked warm. Node variants execute in
// a child process so UV_THREADPOOL_SIZE can vary per run; the child times
// only the operation, mirroring in-process use by the action.
//
// Usage: orchestrator (no args) reads GOCACHE_DIR, GOMODCACHE_DIR, WORK_DIR,
// COPY_BUDGET_GB. Internal: `shell-vs-node.mjs op <name> <args...>`.
import { promisify } from "util";
import { exec, execFile } from "child_process";
import fs from "fs/promises";
import os from "os";
import path from "path";

const execAsync = promisify(exec);
const execFileAsync = promisify(execFile);
const MAX_BUFFER = 1 << 30;
const q = (s) => `'${s.replaceAll("'", `'\\''`)}'`;
const MAX_AGE_DAYS = 7;

// In-flight op pool width used by the node ops; the conc phase sweeps it.
const IO_CONC = parseInt(process.env.BENCH_IO_CONC || "64", 10);

// --- shared walk helpers ---

async function listDirFiles(root) {
  const files = [];
  const pending = [root];
  while (pending.length) {
    const batch = pending.splice(0, IO_CONC);
    await Promise.all(
      batch.map(async (d) => {
        const entries = await fs.readdir(d, { withFileTypes: true });
        for (const e of entries) {
          const p = path.join(d, e.name);
          if (e.isDirectory()) pending.push(p);
          else if (e.isFile()) files.push(p);
        }
      }),
    );
  }
  return files;
}

async function mapPool(items, conc, fn) {
  const out = new Array(items.length);
  let next = 0;
  await Promise.all(
    Array.from({ length: conc }, async () => {
      for (;;) {
        const i = next++;
        if (i >= items.length) break;
        out[i] = await fn(items[i], i);
      }
    }),
  );
  return out;
}

// --- ops (each returns a JSON-able result) ---

const ops = {
  async size_shell(dir) {
    const { stdout } = await execAsync(`du -sB1 ${q(dir)} | cut -f1`);
    return { bytes: parseInt(stdout.trim(), 10) };
  },

  async size_node(dir) {
    const files = await listDirFiles(dir);
    const stats = await mapPool(files, IO_CONC, (f) => fs.stat(f));
    let bytes = 0;
    for (const s of stats) bytes += s.blocks * 512;
    return { bytes, files: files.length };
  },

  async list_shell(dir) {
    const { stdout } = await execAsync(
      `find ${q(dir)} -type f -printf '%T@\\t%s\\t%p\\n' | sort -n`,
      { maxBuffer: MAX_BUFFER },
    );
    // Parse exactly like the PR's trimBuildCacheLru.
    let entries = 0;
    for (const line of stdout.split("\n")) {
      if (!line) continue;
      const [, sizeStr, ...pathParts] = line.split("\t");
      if (isNaN(parseInt(sizeStr, 10)) || !pathParts.join("\t")) continue;
      entries++;
    }
    return { entries };
  },

  async list_node(dir) {
    const files = await listDirFiles(dir);
    const stats = await mapPool(files, IO_CONC, (f) => fs.stat(f));
    const entries = files.map((f, i) => ({
      path: f,
      mtimeMs: stats[i].mtimeMs,
      size: stats[i].size,
    }));
    entries.sort((a, b) => a.mtimeMs - b.mtimeMs);
    return { entries: entries.length };
  },

  async evict_shell(dir) {
    const { stdout } = await execAsync(
      `find ${q(dir)} -type f -mtime +${MAX_AGE_DAYS} -print -delete | wc -l`,
      { maxBuffer: MAX_BUFFER },
    );
    return { deleted: parseInt(stdout.trim(), 10) };
  },

  async evict_node(dir) {
    const cutoff = Date.now() - (MAX_AGE_DAYS + 1) * 86400 * 1000;
    const files = await listDirFiles(dir);
    const stats = await mapPool(files, IO_CONC, (f) => fs.stat(f));
    let deleted = 0;
    await mapPool(files, IO_CONC, async (f, i) => {
      if (stats[i].mtimeMs < cutoff) {
        await fs.unlink(f);
        deleted++;
      }
    });
    return { deleted };
  },

  async delete_shell(listFile) {
    await execAsync(`xargs -a ${q(listFile)} -d '\\n' rm -f --`, {
      maxBuffer: MAX_BUFFER,
    });
    return {};
  },

  async delete_node(listFile) {
    const files = (await fs.readFile(listFile, "utf8")).split("\n").filter(Boolean);
    await mapPool(files, IO_CONC, (f) => fs.unlink(f));
    return { deleted: files.length };
  },

  async wipe_shell(dir) {
    await execAsync(`sudo rm -rf ${q(dir)}`);
    return {};
  },

  // Must run under sudo (mod cache files are read-only).
  async wipe_node(dir) {
    await fs.rm(dir, { recursive: true, force: true });
    return {};
  },

  // End-to-end trim as the PR does it: three separate tree passes
  // (find -delete, du, find|sort).
  async trim_shell(dir) {
    const { stdout: evictOut } = await execAsync(
      `find ${q(dir)} -type f -mtime +${MAX_AGE_DAYS} -print -delete | wc -l`,
      { maxBuffer: MAX_BUFFER },
    );
    const { stdout: duOut } = await execAsync(`du -sB1 ${q(dir)} | cut -f1`);
    const { stdout: listOut } = await execAsync(
      `find ${q(dir)} -type f -printf '%T@\\t%s\\t%p\\n' | sort -n`,
      { maxBuffer: MAX_BUFFER },
    );
    let entries = 0;
    for (const line of listOut.split("\n")) {
      if (!line) continue;
      const [, sizeStr, ...pathParts] = line.split("\t");
      if (isNaN(parseInt(sizeStr, 10)) || !pathParts.join("\t")) continue;
      entries++;
    }
    return {
      deleted: parseInt(evictOut.trim(), 10),
      bytes: parseInt(duOut.trim(), 10),
      entries,
    };
  },

  // Same work in a single walk: one stat pass yields the stale set, the
  // surviving size, and the LRU ordering.
  async trim_node(dir) {
    const cutoff = Date.now() - (MAX_AGE_DAYS + 1) * 86400 * 1000;
    const files = await listDirFiles(dir);
    const stats = await mapPool(files, IO_CONC, (f) => fs.stat(f));
    const stale = [];
    const kept = [];
    let bytes = 0;
    for (let i = 0; i < files.length; i++) {
      if (stats[i].mtimeMs < cutoff) {
        stale.push(files[i]);
      } else {
        kept.push({ path: files[i], mtimeMs: stats[i].mtimeMs });
        bytes += stats[i].blocks * 512;
      }
    }
    await mapPool(stale, IO_CONC, (f) => fs.unlink(f));
    kept.sort((a, b) => a.mtimeMs - b.mtimeMs);
    return { deleted: stale.length, bytes, entries: kept.length };
  },
};

// --- child op-runner mode ---

if (process.argv[2] === "op") {
  const [name, ...args] = process.argv.slice(3);
  const t0 = process.hrtime.bigint();
  const result = await ops[name](...args);
  const ms = Number(process.hrtime.bigint() - t0) / 1e6;
  console.log(JSON.stringify({ ms: +ms.toFixed(1), result }));
  process.exit(0);
}

// --- orchestrator ---

const GOCACHE_DIR = process.env.GOCACHE_DIR;
const GOMODCACHE_DIR = process.env.GOMODCACHE_DIR;
const WORK_DIR = process.env.WORK_DIR;
const COPY_BUDGET_BYTES =
  parseFloat(process.env.COPY_BUDGET_GB || "8") * 2 ** 30;
if (!GOCACHE_DIR || !GOMODCACHE_DIR || !WORK_DIR) {
  console.error("GOCACHE_DIR, GOMODCACHE_DIR and WORK_DIR are required");
  process.exit(1);
}

async function dropCaches() {
  await execAsync(`sync && sudo sh -c 'echo 3 > /proc/sys/vm/drop_caches'`);
}

function record(fields) {
  console.log(JSON.stringify(fields));
  results.push(fields);
}
const results = [];

// Shell variants run via execAsync in this process, exactly like the action.
async function timeShell(bench, opName, arg, { cold }) {
  if (cold) await dropCaches();
  const t0 = process.hrtime.bigint();
  const result = await ops[opName](arg);
  const ms = +(Number(process.hrtime.bigint() - t0) / 1e6).toFixed(1);
  record({ bench, variant: "shell", cold, ms, result });
  return result;
}

// Node variants run in a child so UV_THREADPOOL_SIZE applies; the child
// reports its own timing, excluding process startup.
async function timeNode(bench, opName, arg, { cold, tp, conc, sudo = false }) {
  if (cold) await dropCaches();
  const argv = [process.argv[1], "op", opName, arg];
  const cmd = sudo ? "sudo" : process.execPath;
  const args = sudo ? [process.execPath, ...argv] : argv;
  const env = { ...process.env, UV_THREADPOOL_SIZE: String(tp) };
  if (conc) env.BENCH_IO_CONC = String(conc);
  const { stdout } = await execFileAsync(cmd, args, {
    maxBuffer: MAX_BUFFER,
    env,
  });
  const { ms, result } = JSON.parse(stdout);
  const variant = conc ? `node-tp${tp}-c${conc}` : `node-tp${tp}`;
  record({ bench, variant, cold, ms, result });
  return result;
}

async function describe(label, dir) {
  const { stdout: duOut } = await execAsync(`du -sh ${q(dir)} | cut -f1`);
  const { stdout: countOut } = await execAsync(
    `find ${q(dir)} -type f | wc -l`,
  );
  console.log(
    `# ${label}: ${dir} ${duOut.trim()} apparent, ${countOut.trim()} files`,
  );
}

// Copies top-level entries of src (sorted) into dst until budgetBytes.
async function boundedCopy(src, dst, budgetBytes) {
  await execAsync(`sudo rm -rf ${q(dst)}`);
  await fs.mkdir(dst, { recursive: true });
  const entries = (await fs.readdir(src)).sort();
  let copied = 0;
  for (const name of entries) {
    const from = path.join(src, name);
    const { stdout } = await execAsync(`du -sB1 ${q(from)} | cut -f1`);
    const bytes = parseInt(stdout.trim(), 10);
    if (copied > 0 && copied + bytes > budgetBytes) continue;
    await execAsync(`cp -a ${q(from)} ${q(path.join(dst, name))}`);
    copied += bytes;
    if (copied >= budgetBytes) break;
  }
  return copied;
}

// Ages every 3rd file (sorted path order, deterministic across copies) past
// the eviction cutoff so the evict benches have real work to do.
async function preAge(dir) {
  const files = (await listDirFiles(dir)).sort();
  const old = new Date(Date.now() - 10 * 86400 * 1000);
  const aged = files.filter((_, i) => i % 3 === 0);
  await mapPool(aged, IO_CONC, (f) => fs.utimes(f, old, old));
  return aged.length;
}

const PHASES = process.env.PHASES || "all";
const has = (p) => PHASES === "all" || PHASES.split(",").includes(p);

console.log(
  `# node ${process.version}, ${os.cpus().length} cpus, ${os.type()} ${os.release()}, phases=${PHASES}`,
);
await describe("GOCACHE", GOCACHE_DIR);
await describe("GOMODCACHE", GOMODCACHE_DIR);
await fs.mkdir(WORK_DIR, { recursive: true });
const tree = path.join(WORK_DIR, "tree");

// Phase 1: non-destructive on the live caches.
if (has("nondestructive")) {
  for (const [label, dir] of [
    ["gocache", GOCACHE_DIR],
    ["gomodcache", GOMODCACHE_DIR],
  ]) {
    for (const op of ["size", "list"]) {
      const bench = `${op}_${label}`;
      await timeShell(bench, `${op}_shell`, dir, { cold: true });
      await timeNode(bench, `${op}_node`, dir, { cold: true, tp: 4 });
      await timeNode(bench, `${op}_node`, dir, { cold: true, tp: 16 });
      for (let i = 0; i < 2; i++) {
        await timeShell(bench, `${op}_shell`, dir, { cold: false });
        await timeNode(bench, `${op}_node`, dir, { cold: false, tp: 4 });
        await timeNode(bench, `${op}_node`, dir, { cold: false, tp: 16 });
      }
    }
  }
}

// Phase 2: destructive, on bounded copies of the live GOCACHE.
if (has("destructive")) {
  const copied = await boundedCopy(GOCACHE_DIR, tree, COPY_BUDGET_BYTES);
  console.log(`# destructive copy: ${(copied / 2 ** 30).toFixed(1)} GiB`);

  const aged = await preAge(tree);
  console.log(`# pre-aged ${aged} files past ${MAX_AGE_DAYS} days`);
  const evictShell = await timeShell("evict", "evict_shell", tree, {
    cold: true,
  });

  await boundedCopy(GOCACHE_DIR, tree, COPY_BUDGET_BYTES);
  await preAge(tree);
  const evictNode = await timeNode("evict", "evict_node", tree, {
    cold: true,
    tp: 16,
  });
  if (evictShell.deleted !== evictNode.deleted) {
    console.log(
      `# WARNING: evict parity mismatch shell=${evictShell.deleted} node=${evictNode.deleted}`,
    );
  }

  // Bulk delete: oldest 40% by (mtime, path), same relative list on each copy.
  await boundedCopy(GOCACHE_DIR, tree, COPY_BUDGET_BYTES);
  const files = await listDirFiles(tree);
  const stats = await mapPool(files, IO_CONC, (f) => fs.stat(f));
  const rel = files
    .map((f, i) => ({ rel: path.relative(tree, f), mtimeMs: stats[i].mtimeMs }))
    .sort((a, b) => a.mtimeMs - b.mtimeMs || (a.rel < b.rel ? -1 : 1))
    .slice(0, Math.floor(files.length * 0.4))
    .map((e) => e.rel);
  console.log(`# bulk delete: ${rel.length} of ${files.length} files`);
  const listFile = path.join(WORK_DIR, "trim-list");

  await fs.writeFile(listFile, rel.map((r) => path.join(tree, r)).join("\n"));
  await timeShell("delete", "delete_shell", listFile, { cold: true });

  await boundedCopy(GOCACHE_DIR, tree, COPY_BUDGET_BYTES);
  await timeNode("delete", "delete_node", listFile, { cold: true, tp: 16 });

  // Wipe of a build-cache-shaped tree (writable files).
  await boundedCopy(GOCACHE_DIR, tree, COPY_BUDGET_BYTES);
  await timeShell("wipe_gocache", "wipe_shell", tree, { cold: true });
  await boundedCopy(GOCACHE_DIR, tree, COPY_BUDGET_BYTES);
  await timeNode("wipe_gocache", "wipe_node", tree, {
    cold: true,
    tp: 16,
    sudo: true,
  });

  // Wipe of a mod-cache-shaped tree (read-only files; both sides need root).
  const modTree = path.join(WORK_DIR, "mod-tree");
  await boundedCopy(GOMODCACHE_DIR, modTree, COPY_BUDGET_BYTES);
  await timeShell("wipe_gomodcache", "wipe_shell", modTree, { cold: true });
  await boundedCopy(GOMODCACHE_DIR, modTree, COPY_BUDGET_BYTES);
  await timeNode("wipe_gomodcache", "wipe_node", modTree, {
    cold: true,
    tp: 16,
    sudo: true,
  });
}

// Phase: IO_CONC sweep at fixed tp16 — how wide the in-flight op pool must
// be relative to the threadpool. Cold walk+stat on both live caches, then an
// unlink-pool sweep on identical copies.
if (has("conc")) {
  const CONCS = [8, 16, 32, 64, 128, 256];
  for (const [label, dir] of [
    ["gocache", GOCACHE_DIR],
    ["gomodcache", GOMODCACHE_DIR],
  ]) {
    for (let iter = 0; iter < 2; iter++) {
      await timeShell(`size_${label}_conc`, "size_shell", dir, { cold: true });
      for (const conc of CONCS) {
        await timeNode(`size_${label}_conc`, "size_node", dir, {
          cold: true,
          tp: 16,
          conc,
        });
      }
    }
  }

  let rel = null;
  const listFile = path.join(WORK_DIR, "trim-list");
  for (const conc of ["shell", 16, 64, 256]) {
    await boundedCopy(GOCACHE_DIR, tree, COPY_BUDGET_BYTES);
    if (!rel) {
      const files = await listDirFiles(tree);
      const stats = await mapPool(files, IO_CONC, (f) => fs.stat(f));
      rel = files
        .map((f, i) => ({
          rel: path.relative(tree, f),
          mtimeMs: stats[i].mtimeMs,
        }))
        .sort((a, b) => a.mtimeMs - b.mtimeMs || (a.rel < b.rel ? -1 : 1))
        .slice(0, Math.floor(files.length * 0.4))
        .map((e) => e.rel);
      console.log(`# conc delete sweep: ${rel.length} of ${files.length} files`);
    }
    await fs.writeFile(listFile, rel.map((r) => path.join(tree, r)).join("\n"));
    if (conc === "shell") {
      await timeShell("delete_conc", "delete_shell", listFile, { cold: true });
    } else {
      await timeNode("delete_conc", "delete_node", listFile, {
        cold: true,
        tp: 16,
        conc,
      });
    }
  }
}

// Phase 3: end-to-end trim scenario — the PR's three shell passes vs a
// single-pass node walk, with a threadpool sweep.
if (has("trim")) {
  const runs = [
    ["shell", () => timeShell("trim", "trim_shell", tree, { cold: true })],
    ...[8, 16, 32, 64].map((tp) => [
      `node-tp${tp}`,
      () => timeNode("trim", "trim_node", tree, { cold: true, tp }),
    ]),
  ];
  const outcomes = [];
  for (const [, run] of runs) {
    const copied = await boundedCopy(GOCACHE_DIR, tree, COPY_BUDGET_BYTES);
    if (outcomes.length === 0) {
      console.log(`# trim copy: ${(copied / 2 ** 30).toFixed(1)} GiB`);
    }
    await preAge(tree);
    outcomes.push(await run());
  }
  const counts = new Set(outcomes.map((o) => `${o.deleted}/${o.entries}`));
  if (counts.size !== 1) {
    console.log(`# WARNING: trim parity mismatch: ${[...counts].join(" ")}`);
  }
}

await execAsync(`sudo rm -rf ${q(WORK_DIR)}`);

console.log("\n# --- summary (ms) ---");
const benches = [...new Set(results.map((r) => r.bench))];
for (const bench of benches) {
  for (const cold of [true, false]) {
    const rows = results.filter((r) => r.bench === bench && r.cold === cold);
    if (!rows.length) continue;
    const parts = rows.map((r) => `${r.variant}=${r.ms}`);
    console.log(`${bench} ${cold ? "cold" : "warm"}: ${parts.join(" ")}`);
  }
}
