import { Box, Text } from "ink";
import { ScrollView } from "ink-scroll-view";

export interface Task {
  name: string;
  status: "todo" | "done" | "in-progress";
}

export const Tasks = ({ tasks }: { tasks: Task[] }) => (
  <Box flexDirection="column" width={30} paddingX={1} gap={1}>
    <Text color="gray">To Do</Text>
    <ScrollView>
      {tasks.map((task, i) => (
        <Box key={i} flexDirection="row" gap={1}>
          <Box flexShrink={0} width={1}>
            <Text color={task.status === "done" ? "gray" : task.status === "in-progress" ? "yellow" : "white"}>•</Text>
          </Box>
          <Text wrap="truncate-end">{task.name}</Text>
        </Box>
      ))}
    </ScrollView>
  </Box>
);
