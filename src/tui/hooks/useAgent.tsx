import { useState, useEffect, useCallback } from "react";
import { updateConfig, reboot } from "../../utils/opencode";
import { dino } from "../components/dino";

export type Mode = "coworker" | "auto" | "ghost";
export type Role = "developer";
export type Status = "working" | "idle";
export type Theme = "light" | "dark";

export function useAgent() {
  const [mode, setMode] = useState<Mode>("ghost");
  const [role, setRole] = useState<Role>("developer");
  const [status, setStatus] = useState<Status>("idle");
  const [theme, setTheme] = useState<Theme>("light");
  const [logs, setLogs] = useState<string[]>([dino, "barney v0.0.1"]);

  const addLog = useCallback((log: string) => {
    setLogs(prev => [...prev.slice(-50), log]);
  }, []);

  useEffect(() => {
    const applyConfig = async () => {
      addLog(`[barney] i'm learning how to be a ${role} and ${mode} now`);
      const updateMsg = await updateConfig(role, mode);
      addLog(updateMsg);
      const rebootMsg = await reboot();
      addLog(rebootMsg);
    };
    applyConfig();
  }, [mode, role, addLog]);

  return { mode, setMode, role, setRole, status, setStatus, logs, addLog, theme, setTheme };
}
