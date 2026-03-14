import { watch, readFileSync, writeFileSync, existsSync, mkdirSync } from "node:fs";

export function ensureDir(dir: string) {
  if (!existsSync(dir)) mkdirSync(dir, { recursive: true });
}

export function parseFrontmatter(content: string) {
  const match = content.match(/^---\s*([\s\S]*?)\s*---\s*([\s\S]*)$/);
  if (!match) return { data: {}, content };

  const data: Record<string, string> = {};
  const metadata = match[1] ?? "";
  const body = match[2] ?? "";

  metadata.split("\n").forEach(line => {
    const parts = line.split(":");
    const key = parts[0]?.trim();
    if (key && parts.length > 1) {
      data[key] = parts.slice(1).join(":").trim();
    }
  });

  return { data, content: body };
}

export function debounce<T extends (...args: any[]) => any>(fn: T, ms: number) {
  let timeoutId: ReturnType<typeof setTimeout> | undefined;
  return (...args: Parameters<T>) => {
    if (timeoutId) clearTimeout(timeoutId);
    timeoutId = setTimeout(() => fn(...args), ms);
  };
}

export function coworkerWatch(callback: (filename: string) => void) {
  return watch(".", { recursive: true }, (event, filename) => {
    if (!filename) return;
    if (
      filename.includes("node_modules") || 
      filename.includes(".git") || 
      filename.includes(".barney") ||
      filename.startsWith(".context/tasks")
    ) return;
    callback(filename);
  });
}

export function autoWatch(callback: (filename: string, data: any) => void) {
  const dir = ".context/tasks";
  ensureDir(dir);
  return watch(dir, { recursive: true }, (event, filename) => {
    if (!filename || !filename.endsWith(".md")) return;
    try {
      const content = readFileSync(`${dir}/${filename}`, "utf8");
      const { data } = parseFrontmatter(content);
      callback(filename, data);
    } catch (e) {
      // File might be temporarily unavailable or deleted
    }
  });
}
