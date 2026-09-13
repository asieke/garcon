# Dashboard navigation and skin

Garcon uses a persistent desktop sidebar with four task groups:

| Group | Views | Scope |
| --- | --- | --- |
| Analytics | Overview, Usage, Cost | Volume and estimated spend |
| Explore | Models, Accounts, Sessions, Activity | Usage dimensions and work patterns |
| Diagnostics | Performance, Latency, Logs | Request behavior and investigation |
| Administration | Settings | Instance connections and browser preferences |

Settings is separated at the foot of navigation and has no usage filter bar. It
continues to use all recorded accounts and harnesses, regardless of the selected
analytics filters, and remains accessible when usage loading fails. Returning to
analytics preserves the current filters during client navigation.

## First use

After `garcon setup` starts and verifies the proxy, an empty dashboard points the
user to Settings → Connect a harness. It explains how to copy the configuration,
restart the tool, and send a first request. A separate note points users with an
existing Supabase project to Settings → Sync; sync remains optional. Once usage
arrives, the normal analytics view replaces this guidance automatically.

An empty filtered result continues to offer “Show all usage”; it must not be
mistaken for a new installation. Settings stays accessible before any usage exists
and includes a distinct-device-name reminder when joining an existing project.

## Design rationale

- [Carbon's left-panel guidance](https://carbondesignsystem.com/components/UI-shell-left-panel/usage/)
  recommends side navigation for larger sets of frequently switched destinations.
  Grouped, labeled links grow vertically without wrapping into rows of tabs.
- [NN/g's Tabs, Used Right](https://www.nngroup.com/articles/tabs-used-right/)
  describes tabs as alternate views in a shared context. Administration is a
  different task context from usage analysis, so it receives its own group.
- [Carbon's global-header pattern](https://v10.carbondesignsystem.com/patterns/global-header/)
  separates product navigation from system functions and includes a skip link.
  Garcon adopts consistent navigation, location cues, and keyboard access.

The content hierarchy is page title and purpose, shared filters, then metrics and
charts. Existing analytics retain their calculations and chart colors. Shared
surface, text, focus, and navigation tokens provide light and dark skins without
external fonts or UI dependencies. Harnesses, accounts and devices use multi-select
listboxes (nothing selected means all; fully keyboard-operable) so new values
cannot expand the toolbar indefinitely. A Device select joins them only
once sync has pulled in rows from a second machine; a single-device install never
shows it. The Logs table adds a Device column whenever sync is enabled, and the
sidebar brand shows the running version (the npm package version for npm installs).

At widths of 760px and below, a disclosure menu replaces the persistent sidebar.
It stays in document flow, closes on navigation or Escape, and supports normal
keyboard traversal without a modal focus trap. Desktop navigation scrolls
independently when the viewport is short. The horizontal header stays pinned as
main content scrolls beneath it. Opening the mobile menu after scrolling returns
to the top so its navigation links remain visible.

## Extending and checking

`web/src/lib/navigation/views.ts` owns groups, labels, descriptions, and valid view
identifiers. Add navigation entries there and a corresponding section renderer
in `+page.svelte`. Views use `?view=cost` style URLs for bookmarks and browser
Back/Forward; missing or unknown view values fall back to Overview. Filters are
session state and are not included in shared URLs.

Run `npm run check` and `npm run build` in `web`. Browser checks should cover all
views at 1440, 768, 390, and 320px; navigation history; direct view URLs; retained
filters across Settings; menu keyboard behavior; light/dark mode; empty usage;
and an unavailable usage API. Use synthetic records for shareable screenshots.
