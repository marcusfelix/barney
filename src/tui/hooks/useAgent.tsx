import { useState, useEffect, useCallback } from "react";
import { updateConfig, reboot } from "../../utils/opencode";

export type Mode = "coworker" | "auto" | "ghost";
export type Role = "developer";
export type Status = "working" | "idle";

export function useAgent() {
  const [mode, setMode] = useState<Mode>("ghost");
  const [role, setRole] = useState<Role>("developer");
  const [status, setStatus] = useState<Status>("idle");
  const [logs, setLogs] = useState<string[]>([]);

  const addLog = useCallback((log: string) => {
    setLogs(prev => [...prev.slice(-50), log]);
  }, []);

  useEffect(() => {
    const applyConfig = async () => {
      addLog(`Applying mode: ${mode}, role: ${role}...`);
      const updateMsg = await updateConfig(role, mode);
      addLog(updateMsg);
      const rebootMsg = await reboot();
      addLog(rebootMsg);
    };
    applyConfig();
  }, [mode, role, addLog]);

  return { mode, setMode, role, setRole, status, setStatus, logs, addLog };
}
