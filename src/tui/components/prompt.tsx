import { useState } from "react";
import { Box, Text, useInput } from "ink";
import TextInput from 'ink-text-input';
import type { Theme } from "../hooks/useAgent";

export const Prompt = ({ onSubmit, theme }: { onSubmit: (value: string) => void, theme: Theme }) => {
  const [value, setValue] = useState("");

  useInput((input, key) => {
    if (key.return) {
      onSubmit(value);
      setValue("");
    } 
  });

  const bgColor = theme === "dark" ? "#1E1E1E" : "#D3D3D3";
  const promptColor = theme === "dark" ? "#808080" : "#999999";
  const textColor = theme === "dark" ? "#FFFFFF" : "#000000";

  return (
    <Box backgroundColor={bgColor} padding={1} width="100%">
      <Text color={promptColor}>{"> "}</Text>
      <Box flexGrow={1}>
        <TextInput value={value} onChange={setValue}/>
      </Box>
    </Box>
  );
};
