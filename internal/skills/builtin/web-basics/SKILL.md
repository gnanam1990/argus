---
name: web-basics
description: How to reliably operate a web browser — navigating, waiting for pages to load, filling forms, and handling common blockers — so browser tasks don't stall or misfire.
---

# Operating a web browser reliably

You drive a real browser by reading screenshots and taking UI actions. Web
pages load asynchronously and change under you, so pace your actions to what is
actually on screen.

## Navigating
- To go to a site, focus the address bar (`cmd+l` on macOS, `ctrl+l`
  elsewhere), type the URL, then press `enter`. Do this once.
- After navigating, **wait for the page to load** and take a fresh screenshot
  before acting. A blank or spinner state means keep waiting, not re-navigate.
- Use the page's own controls (links, buttons, search box) rather than guessing
  URLs, unless the user gave you a specific address.

## Waiting for the page
- Pages render in stages: layout, images, then dynamic content. If the element
  you need isn't visible yet, wait and re-screenshot before scrolling or
  clicking.
- After submitting a form or clicking a link, the result may take a moment.
  Observe the new page before deciding the action failed.

## Forms and fields
- Click a field to focus it, confirm the cursor is there, then type.
- Select a value in a dropdown by clicking it open, then clicking the option —
  don't assume it changed without seeing it.
- Review a form before submitting: confirm each field holds what you intended.

## Scrolling and finding things
- If the target isn't on screen, scroll the page (not the whole screen) and
  re-read. Use in-page find (`cmd+f` / `ctrl+f`) to locate text quickly.
- Long lists and feeds load more as you scroll; scroll a bit, wait, re-read.

## Common blockers — do not power through
- **Cookie / consent banners:** accept or dismiss only per the user's intent;
  if unsure, prefer the least-committal option and continue.
- **Login walls, paywalls, CAPTCHAs, "are you human" checks, HTTPS "not secure"
  warnings:** stop and hand back to the user. Do not attempt to bypass a
  security or verification barrier.
- **Unexpected pop-ups / new tabs:** read what appeared before clicking; close
  what you didn't intend to open.

## When stuck
- If the same action twice doesn't change the page, stop repeating it.
  Screenshot, describe what you see, and try a different control or ask the
  user.
