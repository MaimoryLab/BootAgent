#!/usr/bin/env node
import { createHash } from "node:crypto";
import { lstatSync, readdirSync, readFileSync, readlinkSync, writeFileSync } from "node:fs";
import { join, relative } from "node:path";

const [home, destination] = process.argv.slice(2);
if (!home || !destination) {
  console.error("usage: snapshot-home.mjs HOME DESTINATION");
  process.exit(2);
}

const targets = [
  ".codex/config.toml",
  ".claude/settings.json",
  ".config/opencode/opencode.jsonc",
  ".config/kilo/kilo.jsonc",
  ".oneagent",
  ".aider.conf.yml",
];

function digest(file) {
  return createHash("sha256").update(readFileSync(file)).digest("hex");
}

function entries(path) {
  const result = [];
  const info = lstatSync(path);
  result.push({ path: relative(home, path), mode: info.mode & 0o777, size: info.size, ...(info.isSymbolicLink() ? { symlink: readlinkSync(path) } : {}), ...(info.isFile() ? { sha256: digest(path) } : {}) });
  if (info.isDirectory()) {
    for (const name of readdirSync(path).sort()) result.push(...entries(join(path, name)));
  }
  return result;
}

const snapshot = {};
for (const target of targets) {
  const path = join(home, target);
  try {
    snapshot[target] = entries(path);
  } catch (error) {
    if (error?.code === "ENOENT") snapshot[target] = null;
    else throw error;
  }
}
writeFileSync(destination, `${JSON.stringify(snapshot, null, 2)}\n`);
