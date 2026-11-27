# Printing Press 2.0 for Ardia Browser

Accelerate Ardia Browser agent sessions by **skipping pixels on repeat work**.

Instead of opening a browser tab, rendering a full DOM, and spending 4-70 seconds + 12,000-50,000 tokens on repeat lookups (ESPN scores, Hacker News, Instacart carts, Airbnb wishlists, Amazon orders, flight searches), agents ask `pp` first:

1. **Instant Fast Path (`pp route`):** Matches any URL against installed Cartridges and all 616+ apps in `printing-press-library` in <5ms.
2. **Zero-Popup Live Compiler (`pp record --backend ardia <url>`):** Uses `ARDIA_BACKGROUND=1 ardia browse network har` to capture real traffic in the background (zero focus steal, zero Chrome 144 debugging modals) and compiles a 15KB host-locked `pp-cartridge/v1` tool in seconds.
3. **Ardia Omnibox & Hook Integration:** Type `pp espn` or `pp lake <query>` right from Ardia's address bar, or wire an Ardia hook into `~/.config/ardia/hooks.json`.

## Quick Setup

```bash
pp ardia setup
```

Or install manually into Ardia's plugins directory:

```bash
ardia plugins install ./integrations/ardia-plugin
ardia plugins check printing-press
```

Then enable **Printing Press 2.0** in **Ardia > Settings > Plugins**.

## Ardia `hooks.json` Omnibox Snippet

Print the ready-to-merge hook snippet anytime with `pp ardia hook-snippet`:

```json
{
  "hooks": [
    {
      "name": "printing-press-omnibox",
      "omnibox": {
        "title": "Printing Press 2.0 (150ms Local Tools & Life Lake)",
        "keyword": "pp"
      },
      "run": ["pp", "route", "--json"]
    }
  ]
}
```
