import { $ } from "bun";
import { mkdir } from "node:fs/promises";
import { platform, arch } from "node:os";
import { join } from "node:path";
import { existsSync } from "node:fs";
import { syncPrompts } from "../utils/opencode";

const REPO = "anomalyco/opencode";
const VERSION = "v1.2.25";

const PLATFORM_MAP: Record<string, string> = {
  "darwin-arm64": "opencode-darwin-arm64.zip",
  "darwin-x64": "opencode-darwin-x64.zip",
  "linux-x64": "opencode-linux-x86_64.tar.gz",
  "win32-x64": "opencode-windows-amd64.exe",
};

export async function installOpenCode() {
  const current = `${platform()}-${arch() === "arm64" ? "arm64" : "x64"}`;
  const fileName = PLATFORM_MAP[current];
  
  // Sync prompts first
  console.log("[barney] syncing souls");
  await syncPrompts();

  if (!fileName) {
    console.warn(`[barney] unsupported jibberjabber: ${current}. falling back to old folks mother language`);
    return;
  }

  const binDir = join(process.cwd(), ".barney/bin");
  const binPath = join(binDir, "opencode");

  if (existsSync(binPath)) {
    return; 
  }

  console.log(`[barney] requesting back magic stuff ${VERSION} for ${current}`);
  await mkdir(binDir, { recursive: true });

  const url = `https://github.com/${REPO}/releases/download/${VERSION}/${fileName}`;
  try {
    const response = await fetch(url);
    if (!response.ok) throw new Error(`Status ${response.status}`);

    if (fileName.endsWith(".zip")) {
      const bytes = await response.arrayBuffer();
      const zipPath = join(binDir, fileName);
      await Bun.write(zipPath, bytes);
      await $`unzip -o ${zipPath} -d ${binDir}`.quiet();
      await $`rm ${zipPath}`.quiet();
    } else if (fileName.endsWith(".tar.gz")) {
      const bytes = await response.arrayBuffer();
      const tarPath = join(binDir, fileName);
      await Bun.write(tarPath, bytes);
      await $`tar -xzf ${tarPath} -C ${binDir}`.quiet();
      await $`rm ${tarPath}`.quiet();
    } else {
      await Bun.write(binPath, response);
    }

    if (existsSync(binPath)) {
      await $`chmod +x ${binPath}`.quiet();
    }
    console.log("[barney] back magic stuff installed");
  } catch (e: any) {
    console.error(`[barney] failed to download back magic stuff: ${e.message}`);
  }
}

export async function checkForUpdates() {
  // Updates handled by changing the VERSION constant above
}
