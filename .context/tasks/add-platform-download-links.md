---
title: Add download links for all platforms
status: done
created: 2026-03-15
---

## Description

Add actual download links for all 4 platform binaries (Linux x64, macOS x64, macOS arm64, Windows x64) to the website. The download links should point to GitHub releases with dynamic version handling.

## Acceptance Criteria

- [x] Add download link for Linux x64 (barney-linux-x64)
- [x] Add download link for macOS x64 (barney-macos-x64)
- [x] Add download link for macOS arm64 (barney-macos-arm64)
- [x] Add download link for Windows x64 (barney-windows-x64.exe)
- [x] Links follow GitHub release URL pattern: `https://github.com/anomalyco/opencode/releases/download/v[version]/[binary]`
- [x] Add a version indicator or placeholder for dynamic versioning

## Technical Plan

1. Update web/index.html to add download links for all 4 platforms
2. Use the GitHub release download pattern:
   - Linux: `https://github.com/anomalyco/opencode/releases/latest/download/barney-linux-x64`
   - macOS x64: `https://github.com/anomalyco/opencode/releases/latest/download/barney-macos-x64`
   - macOS arm64: `https://github.com/anomalyco/opencode/releases/latest/download/barney-macos-arm64`
   - Windows: `https://github.com/anomalyco/opencode/releases/latest/download/barney-windows-x64.exe`
3. Create a dedicated downloads section with clear platform labels

## Sub-tasks

- [x] Add download section to website with platform links
- [x] Verify links follow correct pattern

## Devlog

- [2026-03-15] Added downloads section with 4 platform download cards (Linux x64, macOS x64, macOS ARM64, Windows x64)
- [2026-03-15] Used `latest` release URL pattern for all downloads
- [2026-03-15] Added styled download cards matching site aesthetic with hover effects
