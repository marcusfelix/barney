// export here file utils such watcher (Bun.watch()), read and write
// watch files modes:
// - coworker() (only watches files changed by user. debounce by good amount of time to not spam opencode and catch files done working on. also ignores node_modules and more to keep it cool)
// - auto(status, role) (watches files on .context/tasks/ but return if its .md and filtered by frontmatter)
// these functions return realtime events