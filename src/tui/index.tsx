import React, { useState, useEffect, useCallback } from "react";
import { Box, Text, render } from "ink";
import { Logs } from "./components/logs";
import { Tasks } from "./components/tasks";
import type {Task} from "./components/tasks";
import { Prompt } from "./components/prompt";
import { useAgent } from "./hooks/useAgent";
import { useWatcher } from "./hooks/useWatcher";
import { useCommands } from "./hooks/useCommands";
import { parseFrontmatter, ensureDir } from "../utils/files";
import { runTask } from "../utils/opencode";
import { readFileSync, readdirSync } from "node:fs";
import { useWindow } from "./hooks/useWindow";

export const BarneyTUI = () => {
  const { mode, setMode, role, setRole, status, setStatus, logs, addLog } = useAgent();
  const [tasks, setTasks] = useState<Task[]>([]);
  const { rows, cols } = useWindow();
  const { handleCommand } = useCommands({ setMode, setRole, addLog });

  const loadTasks = useCallback(() => {
    ensureDir(".context/tasks");
    try {
      const files = readdirSync(".context/tasks");
      const newTasks: Task[] = files
        .filter(f => f.endsWith(".md"))
        .map(f => {
          try {
            const content = readFileSync(`.context/tasks/${f}`, "utf8");
            const { data } = parseFrontmatter(content);
            return { name: f.replace(".md", ""), status: (data.status as any) || "todo" };
          } catch (err) {
            return { name: f.replace(".md", ""), status: "todo" };
          }
        });
      setTasks(newTasks);
    } catch (e) {
      addLog("Error loading tasks directory.");
    }
  }, [addLog]);

  useEffect(() => { loadTasks(); }, [loadTasks]);

  const onCoworkerChange = useCallback((filename: string) => {
    addLog(`Coworker: ${filename} changed`);
  }, [addLog]);

  const onAutoTaskChange = useCallback(async (filename: string, data: any) => {
    addLog(`Task update: ${filename} (${data.status || 'unknown'})`);
    loadTasks();
    
    if (mode === "auto" && status !== "working" && (data.status === "todo" || data.status === "in-progress")) {
      setStatus("working");
      addLog(`Auto: Executing .context/tasks/${filename}`);
      const output = await runTask(`Follow instructions in .context/tasks/${filename}. When finished, update the frontmatter status to 'done'.`);
      addLog(output);
      setStatus("idle");
      loadTasks();
    }
  }, [mode, status, loadTasks, addLog, setStatus]);

  useWatcher({ mode, onCoworkerChange, onAutoTaskChange });

  const handleSubmit = async (value: string) => {
    if (!value.trim()) return;
    if (handleCommand(value)) return;
    
    addLog(`Running: ${value}`);
    setStatus("working");
    const output = await runTask(value);
    addLog(output);
    setStatus("idle");
  };

  return (
    <Box flexDirection="column" height={rows} paddingTop={1}>
      <Box flexGrow={1} paddingBottom={1}>
        <Logs logs={logs} />
        {cols > 100 && <Tasks tasks={tasks} />}
      </Box>
      <Box padding={1} justifyContent="space-between">
        <Text color="#999">Mode: {mode} • Role: {role} • Status: {status}</Text>
      </Box>
      <Prompt onSubmit={handleSubmit} />
    </Box>
  );
};

export const startTUI = async () => {
  process.stdout.write("\x1b[?1049h");
  const {waitUntilExit} = render(<BarneyTUI />);
  await waitUntilExit();
  process.stdout.write("\x1b[?1049l");
}
