import { join } from "node:path";
import { existsSync } from "node:fs";

export async function getOpenCodeBin() {
  const localBin = join(process.cwd(), ".barney/bin/opencode");
  if (existsSync(localBin)) {
    return localBin;
  }
  return "opencode"; // Fallback to global
}
