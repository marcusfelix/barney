import { $ } from "bun";
import { writeFileSync, existsSync, mkdirSync, readFileSync } from "node:fs";
import { join, dirname } from "node:path";
import { getOpenCodeBin } from "../engine/index";

// Import directly to ensure Bun's compiler embeds this file in the binary
import PROMPT_FILES from "./prompts-manifest.json";

const CONFIG_PATH = ".barney/opencode.json";
const BARNEY_ROOT = ".barney";

/**
 * Synchronizes embedded prompt assets from the binary to the local .barney/ directory.
 */
export async function syncPrompts() {
  const root = process.cwd();
  const destBase = join(root, BARNEY_ROOT);
  
  if (!existsSync(destBase)) mkdirSync(destBase, { recursive: true });

  for (const [relativePath, base64Content] of Object.entries(PROMPT_FILES)) {
    // relativePath starts with "prompts/"
    const fullPath = join(destBase, relativePath as string);
    const dir = dirname(fullPath);
    
    if (!existsSync(dir)) mkdirSync(dir, { recursive: true });
    // Strictly decode from base64 string
    writeFileSync(fullPath, Buffer.from(base64Content as string, "base64"));
  }
}

/**
 * Executes a specific task using the local OpenCode binary.
 */
export async function runTask(task: string) {
  try {
    const bin = await getOpenCodeBin();
    const configPath = join(process.cwd(), CONFIG_PATH);
    const result = await $`OPENCODE_CONFIG=${configPath} ${bin} run "${task}"`.quiet().text();
    return result || "Done.";
  } catch (e: any) {
    return `Error: ${e.message}`;
  }
}

/**
 * Updates the OpenCode configuration based on the selected role and mode.
 */
export async function updateConfig(role: string, mode: string, skills?: string[]) {
  try {
    const root = process.cwd();
    const promptsPath = join(root, BARNEY_ROOT, "prompts");
    
    // Core instructions
    const instructions = [
      join(BARNEY_ROOT, `prompts/roles/${role}/agents.md`),
      join(BARNEY_ROOT, `prompts/modes/${mode}.md`)
    ];

    // Skills handling
    const roleJsonPath = join(promptsPath, `roles/${role}/role.json`);
    
    if (existsSync(roleJsonPath)) {
      try {
        const roleConfig = JSON.parse(readFileSync(roleJsonPath, "utf8"));
        const roleSkills = Array.isArray(roleConfig.skills) ? roleConfig.skills : [];
        
        // Filter provided skills by role-allowed skills, or use all role skills if none provided
        const targetSkills = skills 
          ? skills.filter(s => roleSkills.includes(s))
          : roleSkills;

        targetSkills.forEach((s: string) => {
          const skillPath = join(promptsPath, `skills/${s}/SKILL.md`);
          if (existsSync(skillPath)) {
            instructions.push(join(BARNEY_ROOT, `prompts/skills/${s}/SKILL.md`));
          }
        });
      } catch (e) {
        // Skip invalid role configs
      }
    }

    const config = {
      "$schema": "https://opencode.ai/config.json",
      "instructions": instructions
    };
    
    writeFileSync(join(root, CONFIG_PATH), JSON.stringify(config, null, 2));
    return `[barney] i'm ${role}/${mode} with ${instructions.length - 2} skills`;
  } catch (e: any) {
    return `[barney] i'm lost ${e.message}`;
  }
}

/**
 * Reboots/Reloads the OpenCode binary to pick up configuration changes.
 */
export async function reboot() {
  try {
    const bin = await getOpenCodeBin();
    const configPath = join(process.cwd(), CONFIG_PATH);
    await $`OPENCODE_CONFIG=${configPath} ${bin} --version`.quiet();
    return "[barney] ready to rawrk";
  } catch (e: any) {
    return `[barney] i'm lost ${e.message}`;
  }
}
