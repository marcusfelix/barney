// Agent state manager and logic
// this file handle the state, and based on that trigger calls to opencode
// state:
// - mode (default: ghost): 
//    - coworker (only watches files changed by user), 
//      - as coworker, the agent will only message the user. it does not change any file
//    - auto(work on tasks independently), 
//      - as auto, the agent will work on tasks independently. it will change files
//    - ghost (do nothing)
// - role (default: developer): developer, reviewer (this set the system prompt and skills. see roles folder)
// - status (default: idle): working, idle (based on opencode activity)

// when mode and role is changed, .barney/opencode.json should be overwritten and opencode should be rebooted. see: utils/opencode.ts
// opencode.json example:
// {
//   "$schema": "https://opencode.ai/config.json",
//   "instructions": ["roles/developer.md", "modes/auto.md"]
// }
