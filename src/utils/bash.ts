import { $ } from "bun";

export const exec = async (command: string) => {
  return await $`${command.split(" ")}`.text();
};

export const run = async (command: string[]) => {
  return await $`${command}`.quiet().text();
};
