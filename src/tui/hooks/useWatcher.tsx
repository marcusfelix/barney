import { useEffect } from "react";
import { coworkerWatch, autoWatch, debounce } from "../../utils/files";
import type { Mode } from "./useAgent";

interface WatcherProps {
  mode: Mode;
  onCoworkerChange: (filename: string) => void;
  onAutoTaskChange: (filename: string, data: any) => void;
}

export function useWatcher({ mode, onCoworkerChange, onAutoTaskChange }: WatcherProps) {
  useEffect(() => {
    let watcher: any;

    if (mode === "coworker") {
      const debouncedChange = debounce(onCoworkerChange, 1000);
      watcher = coworkerWatch(debouncedChange);
    } else if (mode === "auto") {
      watcher = autoWatch(onAutoTaskChange);
    }

    return () => {
      if (watcher && watcher.close) watcher.close();
    };
  }, [mode, onCoworkerChange, onAutoTaskChange]);
}
