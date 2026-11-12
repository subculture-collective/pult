#!/usr/bin/env node
// Runs the pult binary from the platform package npm installed beside this
// one (an optional dependency picked by the "os" and "cpu" fields).
"use strict";

const { spawnSync } = require("node:child_process");
const { constants } = require("node:os");

const scope = "@SCOPE@";
const pkg = `${scope}/pult-${process.platform}-${process.arch}`;
let bin;
try {
  bin = require.resolve(`${pkg}/bin/pult`);
} catch {
  console.error(
    `pult: ${pkg} is not installed. Pult's npm packages cover Linux and macOS on x64 and arm64; ` +
      "if your system is one of those, reinstall without --omit=optional or --no-optional.",
  );
  process.exit(1);
}

const r = spawnSync(bin, process.argv.slice(2), { stdio: "inherit" });
if (r.error) {
  console.error(`pult: ${r.error.message}`);
  process.exit(1);
}
if (r.signal) {
  process.kill(process.pid, r.signal);
  process.exit(128 + (constants.signals[r.signal] || 0));
}
process.exit(r.status);
