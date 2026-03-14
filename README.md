# Barney 🦖

A smart, minimalistic AI CLI assistant built with Bun and OpenCode.

## Features

- **Headless OpenCode Integration**: Transparent AI assistance directly from
  your terminal.
- **TUI (Terminal User Interface)**: Real-time logs, tasks, and an interactive
  prompt powered by Ink.
- **Multi-mode Operation**:
  - `ghost`: Passive monitoring.
  - `coworker`: Watches your changes and offers insights.
  - `auto`: Independently works on tasks defined in `.context/tasks/*.md`.
- **Role-based Persona**: Focused as a `developer` (expandable).

## Installation

```bash
bun install
```

## Usage

### Development

```bash
bun dev
```

### Build Binary

```bash
bun build
```

## Structure

- `.barney/`: Local configuration and OpenCode settings.
- `.context/tasks/`: Markdown files defining tasks for `auto` mode.
- `src/`: Core logic, TUI, and engine components.

## Dependencies

- [Bun](https://bun.sh)
- [Ink](https://github.com/vadimdemedes/ink)
- [OpenCode AI](https://opencode.ai)
