#!/usr/bin/env node

import assert from "node:assert/strict";
import { readdirSync, readFileSync } from "node:fs";
import { join } from "node:path";
import { fileURLToPath } from "node:url";

const root = fileURLToPath(new URL("../..", import.meta.url));
const expected = readdirSync(join(root, "skills"), { withFileTypes: true })
  .filter((entry) => entry.isDirectory())
  .filter((entry) => {
    try {
      readFileSync(join(root, "skills", entry.name, "SKILL.md"), "utf8");
      return true;
    } catch {
      return false;
    }
  })
  .map((entry) => entry.name)
  .sort();

function assertList(name, actual) {
  assert.deepEqual(
    actual,
    expected,
    `${name} is out of sync with skills/*/SKILL.md. Update the named skill list when adding, renaming, or removing a Printing Press skill.`,
  );
}

function updateBlock(text, marker) {
  const start = `# ${marker}_START`;
  const end = `# ${marker}_END`;
  const startIndex = text.indexOf(start);
  const endIndex = text.indexOf(end);
  assert.notEqual(startIndex, -1, `${marker} start marker missing`);
  assert.notEqual(endIndex, -1, `${marker} end marker missing`);
  assert.ok(endIndex > startIndex, `${marker} end marker must follow start marker`);
  return text.slice(startIndex + start.length, endIndex);
}

function listedSkillsFromBlock(block) {
  return block
    .split(/\r?\n/)
    .map((line) => line.trim())
    .filter(Boolean)
    .map((line) => line.replace(/\\$/, "").trim())
    .filter((line) => line.startsWith("printing-press"))
    .sort();
}

const setupChecks = readFileSync(join(root, "skills/printing-press/references/setup-checks.md"), "utf8");
assertList(
  "setup-checks skill update list",
  listedSkillsFromBlock(updateBlock(setupChecks, "PRINTING_PRESS_SKILL_UPDATE")),
);
