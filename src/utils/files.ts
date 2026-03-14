// export here file utils such watcher (Bun.watch()), read and write
// watch files modes:
// - coworker() (only watches files changed by user. debounce by good amount of time to not spam opencode and catch files done working on)
// - auto(status, role) (watches files but return if its .md and filtered by frontmatter)
// these functions return realtime events