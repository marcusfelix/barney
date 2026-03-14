import React, { useRef } from "react";
import { Box, Text, useInput } from "ink";
import { ScrollView, type ScrollViewRef } from "ink-scroll-view";
import { useWindow } from "../hooks/useWindow";

export const Logs = ({ logs }: { logs: string[] }) => {
  const handleResize = () => scrollRef.current?.remeasure();

  const _ = useWindow(handleResize);
  const scrollRef = useRef<ScrollViewRef>(null);

  useInput((input, key) => {
    if (key.upArrow) {
      scrollRef.current?.scrollBy(-1);
    }
    if (key.downArrow) {
      scrollRef.current?.scrollBy(1);
    }
    if (key.pageUp) {
      const height = scrollRef.current?.getViewportHeight() || 1;
      scrollRef.current?.scrollBy(-height);
    }
    if (key.pageDown) {
      const height = scrollRef.current?.getViewportHeight() || 1;
      scrollRef.current?.scrollBy(height);
    }
  });

  return (
    <Box flexDirection="column" flexGrow={1}>
      <ScrollView ref={scrollRef}>
        {logs.map((log, i) => (
          <Text key={i}>{log}</Text>
        ))}
      </ScrollView>
    </Box>
  );
};
