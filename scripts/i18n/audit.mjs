#!/usr/bin/env node
import fs from "node:fs";
import path from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";

const scriptDir = path.dirname(fileURLToPath(import.meta.url));
const repoRoot = path.resolve(scriptDir, "../..");
const webRoot = path.join(repoRoot, "web");
const typescriptUrl = pathToFileURL(path.join(webRoot, "node_modules/typescript/lib/typescript.js")).href;
const ts = await import(typescriptUrl);

const strict = process.argv.includes("--strict");
const appPath = path.join(webRoot, "src/App.tsx");
const translationsPath = path.join(webRoot, "src/i18n/translations.ts");
const appSource = fs.readFileSync(appPath, "utf8");
const translationsSource = fs.readFileSync(translationsPath, "utf8");
const sourceFile = ts.createSourceFile(appPath, appSource, ts.ScriptTarget.Latest, true, ts.ScriptKind.TSX);

const phrases = new Map();
const dictionaryKeys = new Set(
  [...translationsSource.matchAll(/"((?:\\.|[^"\\])*)"\s*:/g)].map((match) => JSON.parse(`"${match[1]}"`)),
);

walk(sourceFile);

const missing = [...phrases.entries()]
  .filter(([phrase]) => !dictionaryKeys.has(phrase))
  .sort((a, b) => a[1][0] - b[1][0]);

console.log(`i18n audit: ${phrases.size} source phrases, ${dictionaryKeys.size} dictionary keys, ${missing.length} missing.`);
if (missing.length) {
  for (const [phrase, lines] of missing) {
    console.log(`${path.relative(repoRoot, appPath)}:${lines[0]}\t${phrase}`);
  }
}

if (strict && missing.length) {
  process.exitCode = 1;
}

function walk(node) {
  if (ts.isStringLiteral(node) || ts.isNoSubstitutionTemplateLiteral(node)) {
    addPhrase(node.text, node.getStart(sourceFile));
  } else if (ts.isJsxText(node)) {
    addPhrase(node.getText(sourceFile), node.getStart(sourceFile));
  } else if (ts.isTemplateExpression(node)) {
    let text = node.head.text;
    for (const span of node.templateSpans) text += `{}` + span.literal.text;
    addPhrase(text, node.getStart(sourceFile));
  }
  ts.forEachChild(node, walk);
}

function addPhrase(value, position) {
  const phrase = normalizePhrase(value);
  if (!phrase || !/\p{Script=Han}/u.test(phrase)) return;
  const { line } = sourceFile.getLineAndCharacterOfPosition(position);
  const lines = phrases.get(phrase) ?? [];
  lines.push(line + 1);
  phrases.set(phrase, lines);
}

function normalizePhrase(value) {
  return value.replace(/\s+/g, " ").trim();
}
