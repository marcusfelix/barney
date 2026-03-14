import { $ } from "bun";

export function useExec() {
  const run = async (cmd: string[]) => {
    return await $`${cmd}`.text();
  };
  return { run };
}
