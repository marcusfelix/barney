// based on vadimdemedes/ink
// return here the main TUI layout using Ink with
// - message prompt at the bottom
// - scrollview of opencode logs at the center stage
// - right sidebar with the current tasks status
// type: stateful component
// see: useAgent.ts
// this file should handle some logic such:
// - watcher events from useWatcher.ts
//    - update the sidebar based on frontmatter data
//    - call useAgent.ts to run the task if the mode allows it
// - state changes from useAgent.ts
