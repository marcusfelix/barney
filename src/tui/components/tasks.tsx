import { Box, Text } from "ink";
import { ScrollView } from "ink-scroll-view";
import { Theme } from "../hooks/useAgent";

export interface Task {
  name: string;
  status: "todo" | "done" | "in-progress";
}

export const Tasks = ({ tasks, theme }: { tasks: Task[], theme: Theme }) => {
  const dotColor = (status: string) => {
    if (status === "done") return "gray";
    if (status === "in-progress") return "yellow";
    return theme === "dark" ? "white" : "black";
  };

  const textColor = theme === "dark" ? "#FFFFFF" : "#000000";

  return (
    <Box flexDirection="column" width={30} paddingX={1} gap={1}>
      <Text color="gray">To Do</Text>
      <ScrollView>
        {tasks.map((task, i) => (
          <Box key={i} flexDirection="row" gap={1}>
            <Box flexShrink={0} width={1}>
              <Text color={dotColor(task.status)}>•</Text>
            </Box>
            <Text wrap="truncate-end" color={textColor}>{task.name}</Text>
          </Box>
        ))}
      </ScrollView>
    </Box>
  );
};
