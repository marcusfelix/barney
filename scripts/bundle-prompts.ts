import { readdirSync, readFileSync, writeFileSync, statSync } from "node:fs";
import { join, relative } from "node:path";

const SRC_DIR = join(process.cwd(), "src");
const PROMPTS_DIR = join(SRC_DIR, "prompts");
const OUTPUT_FILE = join(process.cwd(), "src/utils/prompts-manifest.json");

function walk(dir: string, fileList: string[] = []) {
  if (!statSync(dir).isDirectory()) return [dir];
  const files = readdirSync(dir);
  for (const file of files) {
    const filePath = join(dir, file);
    if (statSync(filePath).isDirectory()) {
      walk(filePath, fileList);
    } else {
      fileList.push(filePath);
    }
  }
  return fileList;
}

console.log("[barney] packing souls into boxes");
const files = walk(PROMPTS_DIR);
const manifest: Record<string, string> = {};

for (const file of files) {
  const relativePath = relative(SRC_DIR, file);
  // Read as buffer and convert to base64 for binary safety
  manifest[relativePath] = readFileSync(file).toString("base64");
}

writeFileSync(OUTPUT_FILE, JSON.stringify(manifest, null, 2));
console.log(`[barney] packed ${files.length} souls successfully into box.`);
