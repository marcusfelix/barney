import { $ } from "bun";

const REQUIRED_COMMANDS = ["git", "opencode", "gh"];

export async function checkRequirements() {
  for (const cmd of REQUIRED_COMMANDS) {
    try {
      await $`which ${cmd}`.quiet();
    } catch {
      console.warn(`⚠️ Warning: ${cmd} is not installed.`);
    }
  }
}
