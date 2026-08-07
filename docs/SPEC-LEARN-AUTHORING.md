# Authoring `spec.Learn` Blocks

The generator emits the self-learning loop into every CLI whose spec carries an enabled `learn:` block. With the block populated, the printed CLI's `recall` command resolves taught queries through entity-aware substitution: teach one query, recall a variant via alias, and (after a few teaches in the same shape) automatically generalize through pattern substitution against the entity-lookup seeds.

Without seed data, the loop ships as a benign no-op. This guide explains how to populate `learn:` so a printed CLI actually pays off.

For background, see the predecessor plan: `docs/plans/2026-05-23-002-feat-generator-wide-self-learning-cli-plan.md`.

## When to add a `learn:` block

Add `learn:` when the CLI's killer flow is "free-text query → identify resource → operate on it." Most CLIs in the printing-press library fall in this bucket (search-heavy media/sports/commerce/sales). Skip the block for pure-action CLIs (place an order, send a notification) where there's no discovery walk to compress.

Three signs the block will pay off:

- The CLI has a `search` or `topic`-style command that fans out across multiple endpoints
- Agents in real traffic ask for the same entity by multiple names (team nicknames, alias surnames, brand variations)
- The CLI's identifiers follow a stable shape (regex-matchable IDs, slugs, tickers)

If only the first applies, add the block with `ticker_patterns` and `stopwords` only; leave `entity_lookup_seeds` empty until the alias shape settles in real usage.

## Block shape

```yaml
learn:
  enabled: true
  ticker_patterns:
    - "<regex 1>"
    - "<regex 2>"
  stopwords:
    - <word>
    - <word>
  entity_lookup_seeds:
    <seed_kind>:
      - canonical: <canonical name>
        aliases: [<alias 1>, <alias 2>, ...]
```

All three fields under `enabled: true` are optional. Each does a distinct job in the recall path:

- `ticker_patterns` — regexes the entity extractor uses to recognize CLI-specific identifiers (game IDs, slugs, internal codes). Tokens matching any ticker pattern get classified as `Tickers`, separate from `Entities`, so a stray slug in a free-text query doesn't pollute alias-matching.
- `stopwords` — domain-specific filler words stripped from queries before entity matching. Merged with a built-in default set (English articles, prepositions, question words).
- `entity_lookup_seeds` — canonical-name + aliases mapping. Recall consults this to resolve "Niners" → "San Francisco 49ers" before searching learnings. Pattern extraction uses seed kinds (e.g., `nfl_team`) to generalize across taught queries.

## Sourcing each field

### Ticker patterns

Look at the CLI's identifier shapes across each resource type. If the API uses stable formats — alphanumeric IDs, slugs, tickers — they're ticker pattern candidates. The check: does this token shape appear in agent free-text queries (as in, would the agent paste an ID into recall)?

For example:
- Kalshi: `^KX[A-Z0-9]+(-[A-Z0-9]+)*$` (event tickers like `KXMENWORLDCUP-26`)
- Polymarket: `^will-[a-z0-9-]+$` (market slugs)
- ESPN: `^[0-9]{9}$` (9-digit event IDs), see worked example below

Anchor every regex with `^...$` — without anchors a stray "will" in a sports query would match an unrelated Polymarket pattern and corrupt classification.

### Stopwords

Add words that appear in nearly every query for this domain but carry no identity signal. Sports CLIs need "vs", "game", "tonight"; news CLIs need "article", "story", "today"; finance CLIs need "ticker", "price", "today".

The default English stopword set already covers articles ("the", "a"), prepositions ("of", "to"), question words ("what", "who"), and modal verbs ("will", "can"). Don't add those. The block lists only the domain extension on top.

Whitespace-only entries are silently dropped at parse time; case is normalized to lowercase.

### Entity lookup seeds

This is where most authoring time goes. Each seed kind groups canonical entities of one type — `nfl_team`, `nba_team`, `country_iso2`, `person`, `product`. The recall path uses the kind name as a substitution slot in pattern extraction: a taught query about `{nfl_team}` generalizes to every entry in the `nfl_team` seed set.

Two rules:

1. **Canonical names match upstream API responses exactly.** If the API returns "San Francisco 49ers", that's the canonical. Future per-CLI sync data alignment depends on this.
2. **Aliases capture how agents type the entity.** Nicknames, abbreviations, last-word-only variants, common misspellings.

For domains where the entity set is finite and stable (sports teams, ISO countries, finite product categories), seed the full set at print time. For domains where it's open-ended (people, companies, arbitrary search topics), seed the most-common cases and let teach traffic populate the rest.

## Worked example: ESPN

ESPN's killer flow is "give me odds/info for `<team>` in `<sport>`," and agents query teams by 2-4 different names each. Without seeds, recall caches the exact phrasing only; with seeds, one teach generalizes across every team.

### Block

```yaml
learn:
  enabled: true
  ticker_patterns:
    # ESPN event IDs (game IDs)
    - "^[0-9]{9}$"
    # ESPN athlete IDs (URL-style)
    - "^a-[0-9]+$"
    # ESPN team URL identifiers
    - "^[a-z]{2,4}-[a-z]+$"
  stopwords:
    - vs
    - v
    - versus
    - game
    - games
    - match
    - matches
    - tonight
    - today
    - yesterday
    - tomorrow
    - weekend
    - schedule
    - scoreboard
    - score
    - scores
    - result
    - results
    - winner
    - stats
    - standings
    - lineup
    - roster
  entity_lookup_seeds:
    nfl_team:
      - {canonical: "Arizona Cardinals",     aliases: ["Cardinals", "Cards", "ARI", "AZ"]}
      - {canonical: "Atlanta Falcons",       aliases: ["Falcons", "ATL"]}
      - {canonical: "Baltimore Ravens",      aliases: ["Ravens", "BAL"]}
      - {canonical: "Buffalo Bills",         aliases: ["Bills", "BUF"]}
      - {canonical: "Carolina Panthers",     aliases: ["Panthers", "CAR"]}
      - {canonical: "Chicago Bears",         aliases: ["Bears", "CHI"]}
      - {canonical: "Cincinnati Bengals",    aliases: ["Bengals", "CIN"]}
      - {canonical: "Cleveland Browns",      aliases: ["Browns", "CLE"]}
      - {canonical: "Dallas Cowboys",        aliases: ["Cowboys", "DAL", "America's Team"]}
      - {canonical: "Denver Broncos",        aliases: ["Broncos", "DEN"]}
      - {canonical: "Detroit Lions",         aliases: ["Lions", "DET"]}
      - {canonical: "Green Bay Packers",     aliases: ["Packers", "GB", "Pack"]}
      - {canonical: "Houston Texans",        aliases: ["Texans", "HOU"]}
      - {canonical: "Indianapolis Colts",    aliases: ["Colts", "IND"]}
      - {canonical: "Jacksonville Jaguars",  aliases: ["Jaguars", "Jags", "JAX"]}
      - {canonical: "Kansas City Chiefs",    aliases: ["Chiefs", "KC"]}
      - {canonical: "Las Vegas Raiders",     aliases: ["Raiders", "LV", "Vegas"]}
      - {canonical: "Los Angeles Chargers",  aliases: ["Chargers", "LAC", "Bolts"]}
      - {canonical: "Los Angeles Rams",      aliases: ["Rams", "LAR", "LA Rams"]}
      - {canonical: "Miami Dolphins",        aliases: ["Dolphins", "MIA", "Fins"]}
      - {canonical: "Minnesota Vikings",     aliases: ["Vikings", "MIN", "Vikes"]}
      - {canonical: "New England Patriots",  aliases: ["Patriots", "NE", "Pats"]}
      - {canonical: "New Orleans Saints",    aliases: ["Saints", "NO"]}
      - {canonical: "New York Giants",       aliases: ["Giants", "NYG", "G-Men"]}
      - {canonical: "New York Jets",         aliases: ["Jets", "NYJ"]}
      - {canonical: "Philadelphia Eagles",   aliases: ["Eagles", "PHI", "Birds"]}
      - {canonical: "Pittsburgh Steelers",   aliases: ["Steelers", "PIT"]}
      - {canonical: "San Francisco 49ers",   aliases: ["49ers", "Niners", "SF", "SF 49ers"]}
      - {canonical: "Seattle Seahawks",      aliases: ["Seahawks", "SEA", "Hawks"]}
      - {canonical: "Tampa Bay Buccaneers",  aliases: ["Buccaneers", "Bucs", "TB"]}
      - {canonical: "Tennessee Titans",      aliases: ["Titans", "TEN"]}
      - {canonical: "Washington Commanders", aliases: ["Commanders", "WAS"]}

    nba_team:
      - {canonical: "Atlanta Hawks",            aliases: ["Hawks", "ATL"]}
      - {canonical: "Boston Celtics",           aliases: ["Celtics", "BOS", "Cs"]}
      - {canonical: "Brooklyn Nets",            aliases: ["Nets", "BKN"]}
      - {canonical: "Charlotte Hornets",        aliases: ["Hornets", "CHA"]}
      - {canonical: "Chicago Bulls",            aliases: ["Bulls", "CHI"]}
      - {canonical: "Cleveland Cavaliers",      aliases: ["Cavaliers", "Cavs", "CLE"]}
      - {canonical: "Dallas Mavericks",         aliases: ["Mavericks", "Mavs", "DAL"]}
      - {canonical: "Denver Nuggets",           aliases: ["Nuggets", "DEN", "Nugs"]}
      - {canonical: "Detroit Pistons",          aliases: ["Pistons", "DET"]}
      - {canonical: "Golden State Warriors",    aliases: ["Warriors", "GS", "Dubs", "GSW"]}
      - {canonical: "Houston Rockets",          aliases: ["Rockets", "HOU"]}
      - {canonical: "Indiana Pacers",           aliases: ["Pacers", "IND"]}
      - {canonical: "LA Clippers",              aliases: ["Clippers", "LAC", "Clips"]}
      - {canonical: "Los Angeles Lakers",       aliases: ["Lakers", "LAL", "LA Lakers"]}
      - {canonical: "Memphis Grizzlies",        aliases: ["Grizzlies", "Grizz", "MEM"]}
      - {canonical: "Miami Heat",               aliases: ["Heat", "MIA"]}
      - {canonical: "Milwaukee Bucks",          aliases: ["Bucks", "MIL"]}
      - {canonical: "Minnesota Timberwolves",   aliases: ["Timberwolves", "Wolves", "MIN", "T-Wolves"]}
      - {canonical: "New Orleans Pelicans",     aliases: ["Pelicans", "Pels", "NOP"]}
      - {canonical: "New York Knicks",          aliases: ["Knicks", "NY", "NYK"]}
      - {canonical: "Oklahoma City Thunder",    aliases: ["Thunder", "OKC"]}
      - {canonical: "Orlando Magic",            aliases: ["Magic", "ORL"]}
      - {canonical: "Philadelphia 76ers",       aliases: ["76ers", "Sixers", "PHI"]}
      - {canonical: "Phoenix Suns",             aliases: ["Suns", "PHX"]}
      - {canonical: "Portland Trail Blazers",   aliases: ["Trail Blazers", "Blazers", "POR"]}
      - {canonical: "Sacramento Kings",         aliases: ["Kings", "SAC"]}
      - {canonical: "San Antonio Spurs",        aliases: ["Spurs", "SAS"]}
      - {canonical: "Toronto Raptors",          aliases: ["Raptors", "TOR"]}
      - {canonical: "Utah Jazz",                aliases: ["Jazz", "UTA"]}
      - {canonical: "Washington Wizards",       aliases: ["Wizards", "WAS", "Wiz"]}

    mlb_team:
      - {canonical: "Arizona Diamondbacks",     aliases: ["Diamondbacks", "D-backs", "ARI"]}
      - {canonical: "Atlanta Braves",           aliases: ["Braves", "ATL"]}
      - {canonical: "Baltimore Orioles",        aliases: ["Orioles", "Os", "BAL"]}
      - {canonical: "Boston Red Sox",           aliases: ["Red Sox", "BOS", "Sox"]}
      - {canonical: "Chicago Cubs",             aliases: ["Cubs", "CHC"]}
      - {canonical: "Chicago White Sox",        aliases: ["White Sox", "ChiSox", "CHW"]}
      - {canonical: "Cincinnati Reds",          aliases: ["Reds", "CIN"]}
      - {canonical: "Cleveland Guardians",      aliases: ["Guardians", "CLE"]}
      - {canonical: "Colorado Rockies",         aliases: ["Rockies", "COL"]}
      - {canonical: "Detroit Tigers",           aliases: ["Tigers", "DET"]}
      - {canonical: "Houston Astros",           aliases: ["Astros", "HOU", "Stros"]}
      - {canonical: "Kansas City Royals",       aliases: ["Royals", "KC"]}
      - {canonical: "Los Angeles Angels",       aliases: ["Angels", "LAA", "Halos"]}
      - {canonical: "Los Angeles Dodgers",      aliases: ["Dodgers", "LAD"]}
      - {canonical: "Miami Marlins",            aliases: ["Marlins", "MIA"]}
      - {canonical: "Milwaukee Brewers",        aliases: ["Brewers", "MIL", "Brew Crew"]}
      - {canonical: "Minnesota Twins",          aliases: ["Twins", "MIN"]}
      - {canonical: "New York Mets",            aliases: ["Mets", "NYM"]}
      - {canonical: "New York Yankees",         aliases: ["Yankees", "NYY", "Yanks", "Bombers"]}
      - {canonical: "Oakland Athletics",        aliases: ["Athletics", "As", "OAK"]}
      - {canonical: "Philadelphia Phillies",    aliases: ["Phillies", "PHI"]}
      - {canonical: "Pittsburgh Pirates",       aliases: ["Pirates", "PIT", "Bucs"]}
      - {canonical: "San Diego Padres",         aliases: ["Padres", "SD", "Pads"]}
      - {canonical: "San Francisco Giants",     aliases: ["Giants", "SF"]}
      - {canonical: "Seattle Mariners",         aliases: ["Mariners", "SEA", "Ms"]}
      - {canonical: "St. Louis Cardinals",      aliases: ["Cardinals", "Cards", "STL"]}
      - {canonical: "Tampa Bay Rays",           aliases: ["Rays", "TB", "TBR"]}
      - {canonical: "Texas Rangers",            aliases: ["Rangers", "TEX"]}
      - {canonical: "Toronto Blue Jays",        aliases: ["Blue Jays", "TOR", "Jays"]}
      - {canonical: "Washington Nationals",     aliases: ["Nationals", "Nats", "WSH"]}

    mls_team:
      - {canonical: "Atlanta United",           aliases: ["ATL", "ATL UTD"]}
      - {canonical: "Austin FC",                aliases: ["ATX"]}
      - {canonical: "Charlotte FC",             aliases: ["CLT"]}
      - {canonical: "Chicago Fire",             aliases: ["Fire", "CHI"]}
      - {canonical: "FC Cincinnati",            aliases: ["Cincy", "CIN"]}
      - {canonical: "Colorado Rapids",          aliases: ["Rapids", "COL"]}
      - {canonical: "Columbus Crew",            aliases: ["Crew", "CLB"]}
      - {canonical: "D.C. United",              aliases: ["DC"]}
      - {canonical: "FC Dallas",                aliases: ["Dallas", "DAL"]}
      - {canonical: "Houston Dynamo",           aliases: ["Dynamo", "HOU"]}
      - {canonical: "Inter Miami",              aliases: ["Miami", "MIA"]}
      - {canonical: "LA Galaxy",                aliases: ["Galaxy", "LAG"]}
      - {canonical: "Los Angeles FC",           aliases: ["LAFC", "LA FC"]}
      - {canonical: "Minnesota United",         aliases: ["Loons", "MIN", "MNU"]}
      - {canonical: "CF Montreal",              aliases: ["Montreal", "MTL"]}
      - {canonical: "Nashville SC",             aliases: ["Nashville", "NSH"]}
      - {canonical: "New England Revolution",   aliases: ["Revolution", "Revs", "NE"]}
      - {canonical: "New York City FC",         aliases: ["NYCFC", "NYC"]}
      - {canonical: "New York Red Bulls",       aliases: ["Red Bulls", "RBNY"]}
      - {canonical: "Orlando City",             aliases: ["Orlando", "ORL"]}
      - {canonical: "Philadelphia Union",       aliases: ["Union", "PHI"]}
      - {canonical: "Portland Timbers",         aliases: ["Timbers", "POR"]}
      - {canonical: "Real Salt Lake",           aliases: ["RSL"]}
      - {canonical: "San Diego FC",             aliases: ["SD"]}
      - {canonical: "San Jose Earthquakes",     aliases: ["Earthquakes", "Quakes", "SJ"]}
      - {canonical: "Seattle Sounders",         aliases: ["Sounders", "SEA"]}
      - {canonical: "Sporting Kansas City",     aliases: ["SKC", "Sporting KC"]}
      - {canonical: "St. Louis City SC",        aliases: ["STL"]}
      - {canonical: "Toronto FC",               aliases: ["TFC", "Toronto"]}
      - {canonical: "Vancouver Whitecaps",      aliases: ["Whitecaps", "VAN"]}
```

### What this unlocks at runtime

With the block above populated, ESPN's loop generalizes across leagues:

1. Agent teaches one query: `teach --query "Niners game tonight" --resource <event-id> --resource-type events`
2. The query normalizes to `niners game tonight`. Entity extraction recognizes "Niners" → "San Francisco 49ers" via the `nfl_team` seed.
3. Pattern extraction (fires after 3 structurally-similar teaches) emits `{nfl_team} game tonight` → events template.
4. Agent recalls `Cowboys game tonight` (never directly taught): entity extraction recognizes "Cowboys" → "Dallas Cowboys", pattern Apply substitutes, returns the right event. Zero discovery walk.

The same chain works for `Lakers game tonight` (via `nba_team`), `Yankees game tonight` (via `mlb_team`), and so on, as long as one query per league has been taught with the relevant entity.

## Local validation workflow

```bash
cd ~/cli-printing-press
git checkout <branch with your spec changes>

# Regenerate the CLI from the updated spec
cli-printing-press generate <name>

# Inspect the emitted learn_init.go — seeds should be baked in verbatim
less ~/printing-press/library/<name>/internal/cli/learn_init.go

# Compile + test the generated CLI
cd ~/printing-press/library/<name>
go build ./...
go test ./...

# Smoke-test with a fresh HOME — without this, stale user data from
# a prior CLI version can mask real issues
HOME=/tmp/learn-test-$(uuidgen) /tmp/<name>-pp-cli teach \
  --query "Niners game tonight" \
  --resource <some-event-id> \
  --resource-type events

HOME=/tmp/learn-test-$(uuidgen) /tmp/<name>-pp-cli recall \
  "49ers game tonight" --json
# Should resolve "49ers" → "San Francisco 49ers" via alias and return
# the same event the teach cached above.
```

## Library sweep workflow (when the CLI is already published)

If the CLI is already published in `printing-press-library` and you want to refresh its `learn_init.go` with new seed data without a full reprint:

1. Update the spec in `cli-printing-press` as above (the source of truth)
2. Run the library sweep tool against the published entry:
   ```bash
   cd ~/printing-press-library
   GO111MODULE=off go build -o /tmp/sweep-learn-install ./tools/sweep-learn-install
   SWEEP_LIBRARY_ROOT=library /tmp/sweep-learn-install
   # The sweep regenerates learn_init.go with current seed data
   ```
3. Verify the published CLI builds and tests pass
4. Land the changes via the standard library PR flow (no direct edits to published artifacts)

The sweep tool is byte-for-byte parity with the generator emission for the learn package files, so the regenerated `learn_init.go` matches what a fresh print would produce.

## Common pitfalls

- **Empty `aliases` list.** Valid but pointless — entry resolves only on canonical match. Either drop the entry or add real aliases.
- **Duplicate canonical entries within a kind.** Spec parser rejects at parse time. Each seed kind gets one row per canonical name.
- **Whitespace-only aliases.** Silently dropped. Use real strings.
- **Domain identifiers in the canonical or alias text.** The purity gate (`scripts/verify-learn-purity.sh`) doesn't scan spec.Learn — but downstream library tests may. Keep the data clean.
- **Ticker patterns without anchors.** A pattern like `[A-Z]{3}` matches any uppercase trigram in any query, including in stopwords. Always anchor with `^...$`.
- **Treating seeds as canonical truth.** They're a starting point. Real teach traffic is what populates the long tail. Seed only the high-frequency cases; let learnings fill in the rest.

## Related references

- Plan: `docs/plans/2026-05-23-002-feat-generator-wide-self-learning-cli-plan.md`
- Spec field definitions: `internal/spec/spec.go` (search for `LearnConfig`)
- Validation rules: `internal/spec/spec.go` (search for `validateLearn`)
- Generator emission: `internal/generator/templates/learn_init.go.tmpl`
- Runtime resolution: emitted `internal/learn/entities/extract.go`, `internal/learn/lookups/store.go`, `internal/learn/patterns/apply.go`
