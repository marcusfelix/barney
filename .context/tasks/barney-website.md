---
title: Generate Barney CLI website
status: done
created: 2026-03-14
---

## Description

Redesign a single-page website for Barney CLI in the `/web` directory. The
website should be styled as coder minimalistic vscode monospaced font dark theme
and inspired on dracula theme. Show instructions on how to download binaries
from GitHub releases.

## Acceptance Criteria

- [x] Single HTML page in `/web` directory
- [x] Coder minimalistic vscode monospaced font dark theme and inspired on
      dracula theme
- [x] Download instructions based on GitHub workflow release pipeline
- [x] Shows all 4 platform binaries (Linux x64, macOS x64, macOS arm64, Windows
      x64)
- [x] Responsive and accessible

## Technical Plan

1. Create `/web` directory (already exists)
2. Design flat, minimal single-page with dinosaur theme
3. Include download links pattern:
   `https://github.com/[owner]/[repo]/releases/download/v[version]/[binary]`
4. Use distinctive typography and cohesive color scheme
5. Keep it simple but memorable

## Sub-tasks

- [x] Update this task to standard format
- [x] Create /web directory structure
- [x] Design and implement single-page website
- [x] Verify implementation

## Devlog

- [2026-03-14] Read workflow and README to understand download mechanism
- [2026-03-14] Download links follow:
  `https://github.com/[owner]/[repo]/releases/download/v[version]/[binary]`
- [2026-03-14] Found existing website, needs redesign with Flintstones dinosaur
  theme
- [2026-03-14] Binary names from workflow: barney-linux-x64, barney-macos-x64,
  barney-macos-arm64, barney-windows-x64.exe
- [2026-03-14] Redesigned with Flintstones aesthetic: cave background, earth
  tones, Bangers font, cartoon dino personality

## PR Reference
