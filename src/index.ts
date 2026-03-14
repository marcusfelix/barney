import { checkRequirements } from "./engine/requirements";
import { installOpenCode, checkForUpdates } from "./engine/setup";
import { ensureDir } from "./utils/files";
import { startTUI } from "./tui/index";
import { $ } from "bun";
import { existsSync, writeFileSync, appendFileSync } from "node:fs";

async function bootstrap() {
  // 1. Setup tools
  await checkRequirements();
  await installOpenCode();
  await checkForUpdates();

  // 2. Project setup
  ensureDir(".barney");
  ensureDir(".context/tasks");

  // 3. .gitignore setup
  if (existsSync(".gitignore")) {
    const gitignore = await $`cat .gitignore`.text();
    if (!gitignore.includes(".barney")) {
      appendFileSync(".gitignore", "# Barney \n.barney\n");
    }
  } else {
    writeFileSync(".gitignore", "# Barney \n.barney\n");
  }

  // 4. Start TUI
  startTUI();
}

bootstrap();
