import { useCallback } from "react";
import { Mode, Role, Theme } from "./useAgent";

interface UseCommandsProps {
  setMode: (mode: Mode) => void;
  setRole: (role: Role) => void;
  setTheme: (theme: Theme) => void;
  addLog: (log: string) => void;
}

export function useCommands({ setMode, setRole, setTheme, addLog }: UseCommandsProps) {
  const handleCommand = useCallback((value: string): boolean => {
    if (value.startsWith("/mode")) {
      const parts = value.split(" ");
      if (parts.length < 2) {
        addLog("Error: Missing mode. Usage: /mode <ghost|coworker|auto>");
        return true;
      }
      const m = parts[1] as Mode;
      if (!["ghost", "coworker", "auto"].includes(m)) {
        addLog(`Error: Invalid mode '${m}'. Valid: ghost, coworker, auto`);
        return true;
      }
      setMode(m);
      addLog(`Mode command received: ${m}`);
      return true;
    }

    if (value.startsWith("/role")) {
      const parts = value.split(" ");
      if (parts.length < 2) {
        addLog("Error: Missing role. Usage: /role <developer>");
        return true;
      }
      const r = parts[1] as Role;
      if (!["developer"].includes(r)) {
        addLog(`Error: Invalid role '${r}'. Valid: developer`);
        return true;
      }
      setRole(r);
      addLog(`Role command received: ${r}`);
      return true;
    }

    if (value.startsWith("/theme")) {
      const parts = value.split(" ");
      if (parts.length < 2) {
        addLog("Error: Missing theme. Usage: /theme <light|dark>");
        return true;
      }
      const t = parts[1] as Theme;
      if (!["light", "dark"].includes(t)) {
        addLog(`Error: Invalid theme '${t}'. Valid: light, dark`);
        return true;
      }
      setTheme(t);
      addLog(`Theme command received: ${t}`);
      return true;
    }

    return false;
  }, [setMode, setRole, setTheme, addLog]);

  return { handleCommand };
}
