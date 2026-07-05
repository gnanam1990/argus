---
name: macos-basics
description: How to reliably operate common macOS UI patterns — launching apps, waiting for the screen to settle, and standard shortcuts — so actions land the first time.
---

# Operating macOS reliably

You control a real macOS desktop by looking at screenshots and taking UI
actions. Follow these conventions so actions land the first time instead of
being repeated blindly.

## Launching an app
- Open Spotlight with the `cmd+space` key chord, type the app's name, wait for
  the result, then press `enter`. Do this **once**.
- After pressing enter, wait and take a fresh screenshot before deciding
  anything else — the app window takes a moment to appear.
- Never press `cmd+space` again just because nothing seemed to happen. Look at
  the current screenshot first; the app may already be open, or Spotlight may
  already be showing results.

## Wait for the screen to settle
- Any action that changes the UI (launching an app, opening a menu, switching
  windows, submitting a form) may take a moment to take effect. Observe the new
  screenshot before acting again.
- If the screen looks unchanged after an action, do **not** repeat the same
  action. Re-read the screen and choose a different approach.

## Common shortcuts (press once)
- New window/document: `cmd+n`. New tab: `cmd+t`. Close: `cmd+w`. Save:
  `cmd+s`. Quit an app: `cmd+q`.
- Select all: `cmd+a`. Copy / paste / cut: `cmd+c` / `cmd+v` / `cmd+x`. Undo:
  `cmd+z`.
- The menu bar is at the very top of the screen: click the menu title, then the
  item that drops down.

## Typing text
- Click the target text field first to focus it and confirm the cursor is
  there, then type.
- To replace existing text, select it first (`cmd+a` inside the field) before
  typing the new value.

## When you are stuck
- If two attempts at the same action don't change the screen, stop repeating
  it. Take a screenshot, state plainly what you see, and pick a different path
  — a different control, a menu, or asking the user.
- Prefer keyboard shortcuts over hunting for small on-screen targets when a
  shortcut exists for the task.
