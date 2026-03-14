import { useCallback } from "react";
import { Mode, Role } from "./useAgent";

interface UseCommandsProps {
  setMode: (mode: Mode) => void;
  setRole: (role: Role) => void;
  addLog: (log: string) => void;
}

export function useCommands({ setMode, setRole, addLog }: UseCommandsProps) {
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

    return false;
  }, [setMode, setRole, addLog]);

  return { handleCommand };
}
