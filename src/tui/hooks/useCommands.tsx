import { useCallback } from "react";
import type { Mode, Role, Theme } from "./useAgent";

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
        addLog("[barney] this soul is lost");
        return true;
      }
      const m = parts[1] as Mode;
      if (!["ghost", "coworker", "auto"].includes(m)) {
        addLog(`[barney] this soul is lost`);
        return true;
      }
      setMode(m);
      addLog(`[barney] i'm ${m} now`);
      return true;
    }

    if (value.startsWith("/role")) {
      const parts = value.split(" ");
      if (parts.length < 2) {
        addLog("[barney] this soul is lost");
        return true;
      }
      const r = parts[1] as Role;
      if (!["developer"].includes(r)) {
        addLog(`[barney] this soul is lost`);
        return true;
      }
      setRole(r);
      addLog(`[barney] i'm ${r} now`);
      return true;
    }

    if (value.startsWith("/theme")) {
      const parts = value.split(" ");
      if (parts.length < 2) {
        addLog("[barney] this googles are dark");
        return true;
      }
      const t = parts[1] as Theme;
      if (!["light", "dark"].includes(t)) {
        addLog(`[barney] this googles are dark`);
        return true;
      }
      setTheme(t);
      addLog(`[barney] i can see all ${t} now and the rain is gone`);
      return true;
    }

    return false;
  }, [setMode, setRole, setTheme, addLog]);

  return { handleCommand };
}
