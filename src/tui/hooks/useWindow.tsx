import { useStdout } from "ink";
import { useState, useEffect } from "react";

export const useWindow = (callback?: () => void) => {
  const {stdout} = useStdout();
  const [rows, setRows] = useState(0);
  const [cols, setCols] = useState(0);

  useEffect(() => {
    const onResize = () => {
      setState(stdout.rows || 24, stdout.columns || 80);
      callback?.();
    };
    stdout.on?.('resize', onResize);
    return () => { stdout.off?.('resize', onResize); };
  }, [stdout]);

  useEffect(() => {
    setState(stdout.rows || 24, stdout.columns || 80);
  }, []);

  const setState = (rows: number, cols: number) => {
    setRows(rows);
    setCols(cols);
  }
  
  return { rows, cols }
}