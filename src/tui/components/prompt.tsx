import { useState } from "react";
import { Box, Text, useInput } from "ink";
import TextInput from 'ink-text-input';

export const Prompt = ({ onSubmit }: { onSubmit: (value: string) => void }) => {
  const [value, setValue] = useState("");

  useInput((input, key) => {
    if (key.return) {
      onSubmit(value);
      setValue("");
    } 
  });

  return (
    <Box backgroundColor="#D3D3D3" padding={1} width="100%">
      <Text color="#999999">{"> "}</Text>
      <TextInput value={value} onChange={setValue} />
    </Box>
  );
};
