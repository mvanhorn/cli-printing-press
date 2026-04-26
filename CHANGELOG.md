# Changelog

## [2.3.7](https://github.com/mvanhorn/cli-printing-press/compare/v2.3.6...v2.3.7) (2026-04-26)


### Bug Fixes

* **cli:** use /v2 module path so go install reports correct version ([#298](https://github.com/mvanhorn/cli-printing-press/issues/298)) ([1ab789e](https://github.com/mvanhorn/cli-printing-press/commit/1ab789ea583abcf1a8fe5e0d8719a024cab5308c))
* **cli:** use strconv.Atoi for major-version parsing in guard test ([#300](https://github.com/mvanhorn/cli-printing-press/issues/300)) ([0cc7017](https://github.com/mvanhorn/cli-printing-press/commit/0cc70170f37f8dc6c5e38b6253ed03cec64ae5ed))

## [2.3.6](https://github.com/mvanhorn/cli-printing-press/compare/v2.3.5...v2.3.6) (2026-04-26)


### Bug Fixes

* **cli:** normalize routing prefixes in OpenAPI paths ([#296](https://github.com/mvanhorn/cli-printing-press/issues/296)) ([1e06456](https://github.com/mvanhorn/cli-printing-press/commit/1e06456c34869c2e356cd7c10ca7c601d829c93d))

## [2.3.5](https://github.com/mvanhorn/cli-printing-press/compare/v2.3.4...v2.3.5) (2026-04-25)


### Bug Fixes

* **cli:** normalize generated env var prefixes ([#294](https://github.com/mvanhorn/cli-printing-press/issues/294)) ([0aafafa](https://github.com/mvanhorn/cli-printing-press/commit/0aafafa34b7f7ad6d07eb0c6b9afb1b179debd43))

## [2.3.4](https://github.com/mvanhorn/cli-printing-press/compare/v2.3.3...v2.3.4) (2026-04-25)


### Bug Fixes

* **catalog:** update stale spec URLs + add download content-validity check ([#282](https://github.com/mvanhorn/cli-printing-press/issues/282)) ([4a77f46](https://github.com/mvanhorn/cli-printing-press/commit/4a77f46c3dc9965d68e2ae4bbcc3b2002a1a1f9a))
* **cli:** gate publishing on transcendence features ([#293](https://github.com/mvanhorn/cli-printing-press/issues/293)) ([ac74e7d](https://github.com/mvanhorn/cli-printing-press/commit/ac74e7d2d7dcd661fc4532ecf943933591171b76))
* **cli:** make firstCommandExample helper promotion-aware ([#291](https://github.com/mvanhorn/cli-printing-press/issues/291)) ([6ed197c](https://github.com/mvanhorn/cli-printing-press/commit/6ed197c43e2c2bdcedfa66772af8f5b5f24cacbe))

## [2.3.3](https://github.com/mvanhorn/cli-printing-press/compare/v2.3.2...v2.3.3) (2026-04-25)


### Bug Fixes

* **ci:** build from local checkout instead of proxying private module ([#279](https://github.com/mvanhorn/cli-printing-press/issues/279)) ([1a95d78](https://github.com/mvanhorn/cli-printing-press/commit/1a95d78bf77fd72e53948e52c50a0fbfaa972696))
* **cli:** backfill parent_id on store upgrade ([#272](https://github.com/mvanhorn/cli-printing-press/issues/272)) ([#276](https://github.com/mvanhorn/cli-printing-press/issues/276)) ([892de38](https://github.com/mvanhorn/cli-printing-press/commit/892de38b779dc8d86174127185f3af549f1a8479))
* **cli:** dedup colliding flag identifiers in generated commands ([#283](https://github.com/mvanhorn/cli-printing-press/issues/283)) ([72a84a8](https://github.com/mvanhorn/cli-printing-press/commit/72a84a88d671174c814b09fb4f5032cc26f4b867))
* **cli:** extend flag-identifier dedup to request body fields ([#288](https://github.com/mvanhorn/cli-printing-press/issues/288)) ([1552938](https://github.com/mvanhorn/cli-printing-press/commit/15529383018a26747632e65e65e90803edee2ac5))
* **cli:** honor explicit --output flag in generate ([#281](https://github.com/mvanhorn/cli-printing-press/issues/281)) ([6bbae93](https://github.com/mvanhorn/cli-printing-press/commit/6bbae936a0504dabab1febd4d00d3debeeaf50e6))
* **cli:** normalize object-shaped description fields before parsing ([#285](https://github.com/mvanhorn/cli-printing-press/issues/285)) ([6361826](https://github.com/mvanhorn/cli-printing-press/commit/6361826d8414debc288f84cb25d7d74f8ad2d90b))
* **cli:** prepend T to type names that match Go reserved words ([#284](https://github.com/mvanhorn/cli-printing-press/issues/284)) ([1a95a78](https://github.com/mvanhorn/cli-printing-press/commit/1a95a78adf905538a1a649d992ac2d4ad8337821))
* **cli:** refresh stale catalog entries and reject non-spec bodies ([#286](https://github.com/mvanhorn/cli-printing-press/issues/286)) ([ee6bd88](https://github.com/mvanhorn/cli-printing-press/commit/ee6bd881eca4e1eb1563968e54cd082bd725fecb))
* **cli:** treat access-denied sync errors as warnings ([#274](https://github.com/mvanhorn/cli-printing-press/issues/274)) ([#280](https://github.com/mvanhorn/cli-printing-press/issues/280)) ([55114c4](https://github.com/mvanhorn/cli-printing-press/commit/55114c4e344fd9563495745142d41c0551a4ae41))
* **cli:** validate generated JSON string flags ([#278](https://github.com/mvanhorn/cli-printing-press/issues/278)) ([ea0fbe9](https://github.com/mvanhorn/cli-printing-press/commit/ea0fbe94b398179ad083c9ec16ca6c544dfce0a9))

## [2.3.2](https://github.com/mvanhorn/cli-printing-press/compare/v2.3.1...v2.3.2) (2026-04-25)


### Bug Fixes

* **cli:** avoid duplicate batch store upsert generation ([#231](https://github.com/mvanhorn/cli-printing-press/issues/231)) ([24785ee](https://github.com/mvanhorn/cli-printing-press/commit/24785ee5bc564a1c7e5d5cab82385b114719a55d))
* **cli:** dispatch UpsertBatch to typed tables ([#268](https://github.com/mvanhorn/cli-printing-press/issues/268)) ([#271](https://github.com/mvanhorn/cli-printing-press/issues/271)) ([8eea3a0](https://github.com/mvanhorn/cli-printing-press/commit/8eea3a0e4bb02e380458cfc2062ea5a04afa31ca))
* **cli:** stop doctor from claiming valid creds are invalid ([#270](https://github.com/mvanhorn/cli-printing-press/issues/270)) ([6a18477](https://github.com/mvanhorn/cli-printing-press/commit/6a184771b6bcca0c2957bb13ade7515eccdc9eff))
* **megamcp:** reject env var values containing '}' in ApplyAuthFormat ([#252](https://github.com/mvanhorn/cli-printing-press/issues/252)) ([55b3f5e](https://github.com/mvanhorn/cli-printing-press/commit/55b3f5e1510524f70a340436b2071276988eb4e4))

## [2.3.1](https://github.com/mvanhorn/cli-printing-press/compare/v2.3.0...v2.3.1) (2026-04-24)


### Bug Fixes

* **cli:** quote paths in emboss stderr output to prevent shell injection ([#256](https://github.com/mvanhorn/cli-printing-press/issues/256)) ([1645e96](https://github.com/mvanhorn/cli-printing-press/commit/1645e962836b331da44734973016177fbdae3ba4))
* **megamcp:** validate placeholders before substitution in ApplyAuthFormat ([#262](https://github.com/mvanhorn/cli-printing-press/issues/262)) ([2c802af](https://github.com/mvanhorn/cli-printing-press/commit/2c802afe51115d90ef69fc64b00a47fe3d279e60))
* **openapi:** collect paths to delete before mutation in stripBrokenRefs ([#259](https://github.com/mvanhorn/cli-printing-press/issues/259)) ([28ba1e0](https://github.com/mvanhorn/cli-printing-press/commit/28ba1e01d86e4d5b756dab9a4e0fce36bd41c3a9))

## [2.3.0](https://github.com/mvanhorn/cli-printing-press/compare/v2.2.0...v2.3.0) (2026-04-23)


### Features

* **cli:** add machine-owned freshness coverage ([#249](https://github.com/mvanhorn/cli-printing-press/issues/249)) ([4291b3b](https://github.com/mvanhorn/cli-printing-press/commit/4291b3b96f9ddc4306e101d88588f2edd0a41e5b))

## [2.2.0](https://github.com/mvanhorn/cli-printing-press/compare/v2.1.0...v2.2.0) (2026-04-23)


### Features

* **cli:** --deliver routes command output to file or webhook ([f6e7493](https://github.com/mvanhorn/cli-printing-press/commit/f6e74931b899c2188ca6e8b9833717f0aa158d04))
* **cli:** add auth doctor subcommand ([#226](https://github.com/mvanhorn/cli-printing-press/issues/226)) ([72916ac](https://github.com/mvanhorn/cli-printing-press/commit/72916ac1e571975da741706ea519aa25f60c18db))
* **cli:** add http streamable transport to generated MCP servers ([#242](https://github.com/mvanhorn/cli-printing-press/issues/242)) ([bce586b](https://github.com/mvanhorn/cli-printing-press/commit/bce586bc1031f5eefa630ec429e40cdb63950dc0))
* **cli:** add live_api_verification scorecard dimension ([#239](https://github.com/mvanhorn/cli-printing-press/issues/239)) ([440f654](https://github.com/mvanhorn/cli-printing-press/commit/440f654d8262c564e8d2f3bd8cbcdb7691fd1c8b))
* **cli:** add travel and food-and-dining categories ([#187](https://github.com/mvanhorn/cli-printing-press/issues/187)) ([0ac6513](https://github.com/mvanhorn/cli-printing-press/commit/0ac6513428501cc4f2c9b6331ed2dc55cfe3fbf1))
* **cli:** agent_workflow_readiness scorecard dimension ([3ae025f](https://github.com/mvanhorn/cli-printing-press/commit/3ae025f8249f3b15bee9fda18da3699a8416d3f6))
* **cli:** apply Cloudflare Wrangler CLI learnings (naming check, MCP token efficiency, agent-context) ([#216](https://github.com/mvanhorn/cli-printing-press/issues/216)) ([dc5a8cc](https://github.com/mvanhorn/cli-printing-press/commit/dc5a8cc42943e43a1536681763c2cb913e86f531))
* **cli:** async-job detection, --wait flag, jobs command ([1c17285](https://github.com/mvanhorn/cli-printing-press/commit/1c172850d5882ea89cf00d3e74090c47cdcab6de))
* **cli:** auth.optional spec field — doctor INFO not FAIL, README framing, auth cmd names env var ([#211](https://github.com/mvanhorn/cli-printing-press/issues/211)) ([831040d](https://github.com/mvanhorn/cli-printing-press/commit/831040d58463ecc3734ac663fd43988ed149106d))
* **cli:** auto-refresh stale caches before read commands ([#233](https://github.com/mvanhorn/cli-printing-press/issues/233)) ([4c05e1e](https://github.com/mvanhorn/cli-printing-press/commit/4c05e1eee2f5e53c6cd4067ae5c4e8f75b48bc16))
* **cli:** code-orchestration thin surface for large-surface APIs ([#244](https://github.com/mvanhorn/cli-printing-press/issues/244)) ([a4207be](https://github.com/mvanhorn/cli-printing-press/commit/a4207be8bec5cc7a4f32bf039a6ef2b3d2aa3e87))
* **cli:** declare intent-grouped MCP tools in the spec ([#243](https://github.com/mvanhorn/cli-printing-press/issues/243)) ([85d4ace](https://github.com/mvanhorn/cli-printing-press/commit/85d4ace95a6dfcb94e030e1576776da295fa1e9b))
* **cli:** enum validation for params declared with enum constraints ([#208](https://github.com/mvanhorn/cli-printing-press/issues/208)) ([26bc905](https://github.com/mvanhorn/cli-printing-press/commit/26bc905746cfd3e22dabce8ced66082d4f77bd1e))
* **cli:** feedback subcommand for agent-in-band friction reports ([c6223b4](https://github.com/mvanhorn/cli-printing-press/commit/c6223b41f2f77b0defa9ac5e4a590045cb9ead03))
* **cli:** generate &lt;cli&gt; which &lt;capability&gt; resolver in every printed CLI ([#240](https://github.com/mvanhorn/cli-printing-press/issues/240)) ([9dd5632](https://github.com/mvanhorn/cli-printing-press/commit/9dd5632a1a5cdc742d997cc8b764d9169a7c4f81))
* **cli:** generate replayable website CLIs ([#241](https://github.com/mvanhorn/cli-printing-press/issues/241)) ([e741db8](https://github.com/mvanhorn/cli-printing-press/commit/e741db807506a9bf343293a182f0e8958bd9665b))
* **cli:** git-backed snapshot share + cache_freshness scorecard dimension ([#234](https://github.com/mvanhorn/cli-printing-press/issues/234)) ([5e2ed6a](https://github.com/mvanhorn/cli-printing-press/commit/5e2ed6a8a43205eb9c408171be467cc1806b9e75))
* **cli:** HeyGen CLI learnings - async jobs, profiles, --deliver, feedback ([db2ed65](https://github.com/mvanhorn/cli-printing-press/commit/db2ed654f383322bd747dba54207bf43f3cdfc6f))
* **cli:** kind: synthetic spec attribute for multi-source CLIs — closes [#203](https://github.com/mvanhorn/cli-printing-press/issues/203) ([#209](https://github.com/mvanhorn/cli-printing-press/issues/209)) ([caa283e](https://github.com/mvanhorn/cli-printing-press/commit/caa283ed147a40946751f81a9889ad9d32a1bf9b))
* **cli:** machine-output-verification Wave A — cliutil package + dogfood test-presence gate ([#213](https://github.com/mvanhorn/cli-printing-press/issues/213)) ([65dacc2](https://github.com/mvanhorn/cli-printing-press/commit/65dacc25a739492a012f18f48204b050656ff94d))
* **cli:** machine-output-verification Wave B — live-check entity rule + Phase 4.85 agentic output review ([#214](https://github.com/mvanhorn/cli-printing-press/issues/214)) ([270270a](https://github.com/mvanhorn/cli-printing-press/commit/270270ab1da2b80352b59a7bc7be2af35d3e7536))
* **cli:** mcp-audit subcommand + docs for the new MCP surface ([#246](https://github.com/mvanhorn/cli-printing-press/issues/246)) ([38db061](https://github.com/mvanhorn/cli-printing-press/commit/38db061099b28c1d7dd307e0a3aedb83bb1601d1))
* **cli:** named-profile system for repeatable agent contexts ([7140c3e](https://github.com/mvanhorn/cli-printing-press/commit/7140c3e72a63bcaf6057e07149e6817c3c30e267))
* **cli:** patch skips AST mutations owned by colliding features ([#222](https://github.com/mvanhorn/cli-printing-press/issues/222)) ([331809d](https://github.com/mvanhorn/cli-printing-press/commit/331809da1185d0c4758565b90506d33692257d3c))
* **cli:** printing-press patch — AST-inject PR [#218](https://github.com/mvanhorn/cli-printing-press/issues/218) features into published CLIs ([#221](https://github.com/mvanhorn/cli-printing-press/issues/221)) ([16ed5a5](https://github.com/mvanhorn/cli-printing-press/commit/16ed5a566b6cb511c58a190f0a4fc8aa3de2a3ef))
* **cli:** printing-press verify-skill + Phase 4 wiring + Phase 4.8 agentic SKILL reviewer ([#212](https://github.com/mvanhorn/cli-printing-press/issues/212)) ([18cb521](https://github.com/mvanhorn/cli-printing-press/commit/18cb521e6ce4f91b1f463952cdb3f65ddde161e8))
* **cli:** reimplementation gate in absorb scoring and dogfood ([#238](https://github.com/mvanhorn/cli-printing-press/issues/238)) ([00b9a0d](https://github.com/mvanhorn/cli-printing-press/commit/00b9a0d42498d7f810ffc0ef4bac3c06db27ecab))
* **cli:** schema-version gate, doctor cache section, cache/share spec surface ([#232](https://github.com/mvanhorn/cli-printing-press/issues/232)) ([e116a27](https://github.com/mvanhorn/cli-printing-press/commit/e116a27c1fad3226fc9432da5cb972b169585efb))
* **cli:** scorecard --live-check samples novel-feature examples against real targets — closes [#200](https://github.com/mvanhorn/cli-printing-press/issues/200) ([#210](https://github.com/mvanhorn/cli-printing-press/issues/210)) ([df242da](https://github.com/mvanhorn/cli-printing-press/commit/df242da414371f480d394435ad933b9d77b26aec))
* **cli:** scorecard dimensions for remote transport, tool design, surface strategy ([#245](https://github.com/mvanhorn/cli-printing-press/issues/245)) ([2d07a02](https://github.com/mvanhorn/cli-printing-press/commit/2d07a0218423e3090cbdc033cd9998006fc7c309))
* **cli:** support extra_commands: in spec.yaml for hand-written commands ([#227](https://github.com/mvanhorn/cli-printing-press/issues/227)) ([84043f3](https://github.com/mvanhorn/cli-printing-press/commit/84043f3a72fbf2f20dd028a6793c0e0f96db5b0f))
* **scripts:** add verify-skill — static SKILL.md validator ([#194](https://github.com/mvanhorn/cli-printing-press/issues/194)) ([297c3c1](https://github.com/mvanhorn/cli-printing-press/commit/297c3c14f2cd35cd359b503fddc2a8524421a5ce))


### Bug Fixes

* **ci:** allow release-please PR title scope ([#248](https://github.com/mvanhorn/cli-printing-press/issues/248)) ([bc13a5b](https://github.com/mvanhorn/cli-printing-press/commit/bc13a5b4f84aa63b7f935908db1e230895f243dc))
* **cli:** add transitive reachability to dogfood dead function scanner ([#183](https://github.com/mvanhorn/cli-printing-press/issues/183)) ([b436b08](https://github.com/mvanhorn/cli-printing-press/commit/b436b081953eb7a92005249cd0554466a54824de))
* **cli:** authenticate mega MCP library fetches with GITHUB_TOKEN ([97dbcbb](https://github.com/mvanhorn/cli-printing-press/commit/97dbcbbbf404bcd173cadd35c97efcbbc2130241))
* **cli:** authenticate mega MCP library fetches with GITHUB_TOKEN ([1d4e642](https://github.com/mvanhorn/cli-printing-press/commit/1d4e642c31e39638ccde61336826bee35ba5b858))
* **cli:** cross-CLI retro findings - store, sync, scorer, GraphQL templates ([#185](https://github.com/mvanhorn/cli-printing-press/issues/185)) ([e2009bb](https://github.com/mvanhorn/cli-printing-press/commit/e2009bb1fb1d522844baef8e0a8cc7e49506d857))
* **cli:** enrich README and generate SKILL.md so printed CLIs stop looking like scaffolding ([#186](https://github.com/mvanhorn/cli-printing-press/issues/186)) ([1011df3](https://github.com/mvanhorn/cli-printing-press/commit/1011df38524c5dc6ebf78a10fe7cb2f6a367272d))
* **cli:** patch verifies target shape + runs build in target dir ([#224](https://github.com/mvanhorn/cli-printing-press/issues/224)) ([86bdfd2](https://github.com/mvanhorn/cli-printing-press/commit/86bdfd2ef5cc79b48e51fadb3bfef8ce4f1ed32c))
* **cli:** path-aware dogfood novel-feature matcher ([#195](https://github.com/mvanhorn/cli-printing-press/issues/195)) ([126b00d](https://github.com/mvanhorn/cli-printing-press/commit/126b00d055f31b4189123ffe52862b23ba119a2d))
* **cli:** promoted-command presence check uses promoted type ([#196](https://github.com/mvanhorn/cli-printing-press/issues/196)) ([4ebfa08](https://github.com/mvanhorn/cli-printing-press/commit/4ebfa08801e47967a104976ae1f1d73242f19abb))
* **cli:** publish manifest must read spec.yaml alongside spec.json ([#220](https://github.com/mvanhorn/cli-printing-press/issues/220)) ([c5ed436](https://github.com/mvanhorn/cli-printing-press/commit/c5ed4369a52f331e5a3fcccf922c754f2f4b5027))
* **cli:** scope goimports to patched files only ([#223](https://github.com/mvanhorn/cli-printing-press/issues/223)) ([8c54c4c](https://github.com/mvanhorn/cli-printing-press/commit/8c54c4c8223288a49f042651f6c0638d58162501))
* **cli:** support nested --select paths + suppress provenance on non-TTY stdout ([#229](https://github.com/mvanhorn/cli-printing-press/issues/229)) ([ac4d6aa](https://github.com/mvanhorn/cli-printing-press/commit/ac4d6aa22ff7ef42128d005c4bc39c605ff05733))
* **skills:** keep scratch artifacts out of repo docs ([#247](https://github.com/mvanhorn/cli-printing-press/issues/247)) ([d14cefa](https://github.com/mvanhorn/cli-printing-press/commit/d14cefaae705cb5fc51bd426e6ddc1b1bdbd80e2))
* **skills:** Phase 4.85 prompt refinements from calibration dispatch ([#215](https://github.com/mvanhorn/cli-printing-press/issues/215)) ([6f8b0c7](https://github.com/mvanhorn/cli-printing-press/commit/6f8b0c7d6a05151d74131323c18d6bf8405f197b))
* **skills:** retro 2026-04-13 — stop the ship-broken pattern and require mechanical Phase 5 dogfood ([#207](https://github.com/mvanhorn/cli-printing-press/issues/207)) ([3bef9d6](https://github.com/mvanhorn/cli-printing-press/commit/3bef9d6c5318bb1fe57bdadb0bb62adf9dbd67af))

## [2.1.0](https://github.com/mvanhorn/cli-printing-press/compare/v2.0.0...v2.1.0) (2026-04-12)


### Features

* **skills:** add DeepWiki codebase analysis to research phase ([#156](https://github.com/mvanhorn/cli-printing-press/issues/156)) ([6cc5a5f](https://github.com/mvanhorn/cli-printing-press/commit/6cc5a5f6d2d204714c478e404ec97e34659c6657))


### Bug Fixes

* **ci:** make validate-catalog fail loud on missing base ref ([#180](https://github.com/mvanhorn/cli-printing-press/issues/180)) ([8239f28](https://github.com/mvanhorn/cli-printing-press/commit/8239f28bbf5841ce623796fbdf79bbd9761847aa))

## [2.0.0](https://github.com/mvanhorn/cli-printing-press/compare/v1.3.2...v2.0.0) (2026-04-12)


### ⚠ BREAKING CHANGES

* **cli:** decouple printing-press-library into standalone marketplace

### Features

* **cli:** wrapper-only catalog entries for reverse-engineered APIs ([#177](https://github.com/mvanhorn/cli-printing-press/issues/177)) ([096950f](https://github.com/mvanhorn/cli-printing-press/commit/096950fd8bca3d8f1f1375dd93670019e29ea3f1))


### Bug Fixes

* **ci:** auto-sync go install when installed binary exists ([2e21807](https://github.com/mvanhorn/cli-printing-press/commit/2e21807d7bbbf9ccd4de67b6f4fd15501f1862a9))
* **cli:** address remaining ESPN retro findings — verify hints, FTS5, doctor ([#179](https://github.com/mvanhorn/cli-printing-press/issues/179)) ([9da1cab](https://github.com/mvanhorn/cli-printing-press/commit/9da1cabde05d593c628fda65613a92980dd292cb))
* **cli:** address retro findings from yahoo-finance run ([#174](https://github.com/mvanhorn/cli-printing-press/issues/174)) ([#175](https://github.com/mvanhorn/cli-printing-press/issues/175)) ([8357850](https://github.com/mvanhorn/cli-printing-press/commit/8357850dd103e6bb041e51890e3e73c0240f38e7))
* **cli:** decouple CLI version from API version ([68bc76f](https://github.com/mvanhorn/cli-printing-press/commit/68bc76f3ade620d696c08f60f9b8e738e7d9af65))
* **cli:** decouple printing-press-library into standalone marketplace ([cd93b17](https://github.com/mvanhorn/cli-printing-press/commit/cd93b172c28e7a7d37985104161a5e14c79cab54))
* **cli:** default empty version to 1.0.0 and normalize to semver ([f07a795](https://github.com/mvanhorn/cli-printing-press/commit/f07a795de84c2321989880400c6d60f4e0c9e8a5))
* **cli:** double -pp-cli suffix in manifest + Phase 5 skips no-auth APIs ([0656975](https://github.com/mvanhorn/cli-printing-press/commit/065697576de1dd38b658e0a1ecbe92f74222c345)), closes [#173](https://github.com/mvanhorn/cli-printing-press/issues/173)
* **cli:** movie-goat retro — generator param handling, write-through, sync ceiling, scorer ([#172](https://github.com/mvanhorn/cli-printing-press/issues/172)) ([ad9e4ae](https://github.com/mvanhorn/cli-printing-press/commit/ad9e4aee960e981ad11b318b7632444213f714c8))
* **skills:** add combo-CLI priority gate to prevent source inversion ([#176](https://github.com/mvanhorn/cli-printing-press/issues/176)) ([23ccc60](https://github.com/mvanhorn/cli-printing-press/commit/23ccc603fc878ffb0174015a986f77d302dc5197))
* **skills:** add self-vetting gate for transcendence features ([c04b6c1](https://github.com/mvanhorn/cli-printing-press/commit/c04b6c1021dea97be0e5f9616410ba7c712c25f8))
* **skills:** user-first transcendence feature discovery ([a1fae23](https://github.com/mvanhorn/cli-printing-press/commit/a1fae23bafbf90ea542f61ce412e92c261801c3c))
* use git-subdir source for printing-press-library plugin ([#169](https://github.com/mvanhorn/cli-printing-press/issues/169)) ([ac47b18](https://github.com/mvanhorn/cli-printing-press/commit/ac47b18ed7ada1d350f415073208c045876941d2))

## [1.3.2](https://github.com/mvanhorn/cli-printing-press/compare/v1.3.1...v1.3.2) (2026-04-11)


### Bug Fixes

* **skills:** enforce sniff gate with marker file contract ([#166](https://github.com/mvanhorn/cli-printing-press/issues/166)) ([e8aa611](https://github.com/mvanhorn/cli-printing-press/commit/e8aa611cc91077a513328141953567d8677f3489))

## [1.3.1](https://github.com/mvanhorn/cli-printing-press/compare/v1.3.0...v1.3.1) (2026-04-11)


### Bug Fixes

* **cli:** address Kalshi retro findings — --name flag, sync keys, primary key detection ([#163](https://github.com/mvanhorn/cli-printing-press/issues/163)) ([#164](https://github.com/mvanhorn/cli-printing-press/issues/164)) ([ab7f83c](https://github.com/mvanhorn/cli-printing-press/commit/ab7f83c97ef5c5babfe41fdf498df0eeb43bd03d))

## [1.3.0](https://github.com/mvanhorn/cli-printing-press/compare/v1.2.1...v1.3.0) (2026-04-11)


### Features

* **cli:** add --dates sync flag and wrapper-object list detection ([#154](https://github.com/mvanhorn/cli-printing-press/issues/154)) ([49148b4](https://github.com/mvanhorn/cli-printing-press/commit/49148b437ddc577786549c413fe62bced135b43f))
* **cli:** add printing-press-library plugin to marketplace ([#161](https://github.com/mvanhorn/cli-printing-press/issues/161)) ([cbcd67d](https://github.com/mvanhorn/cli-printing-press/commit/cbcd67dbddd83eb3bbc7155c5c0e2f3a06009a15))
* **cli:** apply PostHog agent-first learnings to MCP server generation ([#160](https://github.com/mvanhorn/cli-printing-press/issues/160)) ([8354f52](https://github.com/mvanhorn/cli-printing-press/commit/8354f5283a25461499eee5a8e5a4c605363a39aa))
* **cli:** printing press improvements from agent-capture retro ([#141](https://github.com/mvanhorn/cli-printing-press/issues/141)) ([911dc29](https://github.com/mvanhorn/cli-printing-press/commit/911dc2906ea6d01c644917ce1a8f125f85f7f47e))


### Bug Fixes

* **cli:** always emit usageErr helper ([#162](https://github.com/mvanhorn/cli-printing-press/issues/162)) ([4d3c31f](https://github.com/mvanhorn/cli-printing-press/commit/4d3c31f87eb3306bdcae307a3a6c35c04b3fd028))
* **cli:** GraphQL type dedup, usageErr emission, and FTS5 manual sync ([#149](https://github.com/mvanhorn/cli-printing-press/issues/149)) ([92074e6](https://github.com/mvanhorn/cli-printing-press/commit/92074e672957b2d448fbee65cdc71aad705391c8))
* **cli:** retro fixes from trigger-dev generation ([#159](https://github.com/mvanhorn/cli-printing-press/issues/159)) ([f9e6c10](https://github.com/mvanhorn/cli-printing-press/commit/f9e6c108be3c9b94dd5a9e78c6efaacc55934e23))
* **skills:** correct publish package flag name and staging workflow ([776d433](https://github.com/mvanhorn/cli-printing-press/commit/776d433ca94b32af8be25fbc3694ce3ff0dea1e4))

## [1.2.1](https://github.com/mvanhorn/cli-printing-press/compare/v1.2.0...v1.2.1) (2026-04-09)


### Bug Fixes

* **cli:** deduplicate config env var tags and add operations shorthand ([#150](https://github.com/mvanhorn/cli-printing-press/issues/150)) ([816a9fd](https://github.com/mvanhorn/cli-printing-press/commit/816a9fd6a0b278b5b6f493083a1db14d62cc71d7))
* **cli:** raise resource limit from 50 to 500 and add --max-resources flag ([#152](https://github.com/mvanhorn/cli-printing-press/issues/152)) ([b1128d0](https://github.com/mvanhorn/cli-printing-press/commit/b1128d0e07fd54a217a556233293a1daf2fd35a5))
* **skills:** constrain artifact writes to managed directories ([178ae82](https://github.com/mvanhorn/cli-printing-press/commit/178ae829a8c77c1e5d6c65356f511c7520a69c6d))

## [1.2.0](https://github.com/mvanhorn/cli-printing-press/compare/v1.1.0...v1.2.0) (2026-04-08)


### Features

* **cli:** MCP readiness layer — per-endpoint auth awareness and metadata ([#145](https://github.com/mvanhorn/cli-printing-press/issues/145)) ([51afd77](https://github.com/mvanhorn/cli-printing-press/commit/51afd77877ca1d2e07f8eb56bc831ebf74d62a0c))
* **cli:** mega MCP — generic HTTP proxy with activation model ([#147](https://github.com/mvanhorn/cli-printing-press/issues/147)) ([e041f50](https://github.com/mvanhorn/cli-printing-press/commit/e041f50e7b46f29875e7eee342a0e3081a3868dd))


### Bug Fixes

* **cli:** Dub retro — FTS batch indexing, retry cap, dogfood auth, root dedup, dead code ([#143](https://github.com/mvanhorn/cli-printing-press/issues/143)) ([349580a](https://github.com/mvanhorn/cli-printing-press/commit/349580afbfb388c6c3750f32c8403e599f180adb))
* **cli:** sync version files to 1.1.0 and fix release-please config ([#146](https://github.com/mvanhorn/cli-printing-press/issues/146)) ([3393ada](https://github.com/mvanhorn/cli-printing-press/commit/3393ada0f39ec2b4918d034a632c6259ddc9c900))

## [1.1.0](https://github.com/mvanhorn/cli-printing-press/compare/v1.0.0...v1.1.0) (2026-04-06)


### Features

* **cli:** flow transcendence features into generated READMEs with integrity validation ([#137](https://github.com/mvanhorn/cli-printing-press/issues/137)) ([96b9b42](https://github.com/mvanhorn/cli-printing-press/commit/96b9b42cfd31e579918b77737eb4c0ef0565eaad))
* **cli:** per-endpoint header routing and auth inference from Authorization header params ([#136](https://github.com/mvanhorn/cli-printing-press/issues/136)) ([fb164ad](https://github.com/mvanhorn/cli-printing-press/commit/fb164adc55020060e96d933a8a07e8f06eb60396))
* **cli:** rename What's New Here to Unique Features, move after Quick Start ([1b4b984](https://github.com/mvanhorn/cli-printing-press/commit/1b4b984478b20c7826f3b7943b8738b77cc38821))


### Bug Fixes

* **cli:** ensure Sync is always enabled when Store is true ([b5fddac](https://github.com/mvanhorn/cli-printing-press/commit/b5fddac0514ad6c378e82c1c20698b8b8b12d821))
* **cli:** generate correct library install path in READMEs ([d962947](https://github.com/mvanhorn/cli-printing-press/commit/d9629475f39f8a7f9dfee9931544e02be6caa616))
* **skills:** add Phase 3 Completion Gate to prevent skipping transcendence features ([3ea8601](https://github.com/mvanhorn/cli-printing-press/commit/3ea8601dddf479a1696a5e509c7080aeb52ba2b1))

## 1.0.0 (2026-04-05)


### Features

* **catalog:** add 12 official catalog entries for popular APIs ([9664927](https://github.com/mvanhorn/cli-printing-press/commit/96649272df2b0c3f9a366cb5a056c7a04c594fc6))
* **catalog:** add catalog schema validator with tests ([cd4824a](https://github.com/mvanhorn/cli-printing-press/commit/cd4824a98297751eacbe3d7cfc42095bc9f0c61a))
* **catalog:** add telegram, launchdarkly, sentry from dogfood gauntlet ([0f1beba](https://github.com/mvanhorn/cli-printing-press/commit/0f1beba24f7818d80ddf5c19cb92a4a8e127776c))
* **catalog:** generate Pipedrive CLI from official OpenAPI spec ([451b7cb](https://github.com/mvanhorn/cli-printing-press/commit/451b7cb7e63e659ffbcbd2d02c0da2bc0231d8d4))
* **catalog:** generate Plaid CLI from official OpenAPI spec ([66bc4ea](https://github.com/mvanhorn/cli-printing-press/commit/66bc4ea8e5103a7dac8ce6ba1edd840a64f3ffeb))
* **ci:** automated releases, linting, and commit conventions ([#34](https://github.com/mvanhorn/cli-printing-press/issues/34)) ([c779648](https://github.com/mvanhorn/cli-printing-press/commit/c779648a6781e28628fbab3d48edffca2406792d))
* **cli:** accept name or path in emboss command ([#38](https://github.com/mvanhorn/cli-printing-press/issues/38)) ([cc098e6](https://github.com/mvanhorn/cli-printing-press/commit/cc098e6722de8287090471fdab81beb222616d37))
* **cli:** accept URLs for --spec with local caching ([2e70bc4](https://github.com/mvanhorn/cli-printing-press/commit/2e70bc46b259e0b799d5052b306bb52cd489c745))
* **cli:** add --data-source flag for live/local/auto read resolution ([#119](https://github.com/mvanhorn/cli-printing-press/issues/119)) ([b88aa97](https://github.com/mvanhorn/cli-printing-press/commit/b88aa97b027755a4582e0024c3f52ac7cfe28922))
* **cli:** add --dry-run flag to generate command ([35ec3ca](https://github.com/mvanhorn/cli-printing-press/commit/35ec3caf1a718def49b3370e164c2d7d34e69a70))
* **cli:** add --dry-run flag to generate command ([b494c3b](https://github.com/mvanhorn/cli-printing-press/commit/b494c3b88505448f6ac4b96de517a41c9c1cb750))
* **cli:** add --force flag to generate command ([96f0d51](https://github.com/mvanhorn/cli-printing-press/commit/96f0d5136990c22fb0a803037f32c890289b6704))
* **cli:** add --json flag to generate, print, and vision ([a1fef72](https://github.com/mvanhorn/cli-printing-press/commit/a1fef7250caa5c84293b303fc98a8698a038dac3))
* **cli:** add --json flag to generate, print, and vision commands ([7f5c352](https://github.com/mvanhorn/cli-printing-press/commit/7f5c35202eed5a6f48cd8243462be953e853b886))
* **cli:** add .printing-press.json manifest to published CLIs ([#41](https://github.com/mvanhorn/cli-printing-press/issues/41)) ([6821a43](https://github.com/mvanhorn/cli-printing-press/commit/6821a433a5a5f6a18f35d676058b76ae3a132c2a))
* **cli:** add 'printing-press print' command with plan-per-phase pipeline ([6a76cb2](https://github.com/mvanhorn/cli-printing-press/commit/6a76cb26f76506081eeb9172c7e504a3d7d55468))
* **cli:** add adaptive rate limiting for sniffed APIs ([#62](https://github.com/mvanhorn/cli-printing-press/issues/62)) ([e26505e](https://github.com/mvanhorn/cli-printing-press/commit/e26505e5cfbf450dd648e2eb9194b3da35e514a7))
* **cli:** add Chrome cookie auth for sniff-discovered APIs ([#113](https://github.com/mvanhorn/cli-printing-press/issues/113)) ([b0a3815](https://github.com/mvanhorn/cli-printing-press/commit/b0a3815150d9e30f523d046f5eadfd0a9801b0be))
* **cli:** add crowd-sniff command for community-based API discovery ([#67](https://github.com/mvanhorn/cli-printing-press/issues/67)) ([4a9843d](https://github.com/mvanhorn/cli-printing-press/commit/4a9843dff245d8427c4a28c5da38db5ea80bfc9d))
* **cli:** add discovery/ manuscript directory for sniff provenance ([#70](https://github.com/mvanhorn/cli-printing-press/issues/70)) ([9bad30a](https://github.com/mvanhorn/cli-printing-press/commit/9bad30affe5ccbeda2a3451d34ffa88fc5e6073a))
* **cli:** add Example fields to all Cobra commands ([6bc6abe](https://github.com/mvanhorn/cli-printing-press/commit/6bc6abeb6d2e61c91c396ee2979cf0656a5b607a))
* **cli:** add Example fields to all Cobra commands ([633eefc](https://github.com/mvanhorn/cli-printing-press/commit/633eefc3f9810172f5baebd7ea42af51949edb11))
* **cli:** add name collision detection and resolution to publish workflow ([#128](https://github.com/mvanhorn/cli-printing-press/issues/128)) ([0dac623](https://github.com/mvanhorn/cli-printing-press/commit/0dac62322a973ed494255425de56f7de5c3bb73d))
* **cli:** add printing-press polish --remove-dead-code ([#105](https://github.com/mvanhorn/cli-printing-press/issues/105)) ([2c9d57d](https://github.com/mvanhorn/cli-printing-press/commit/2c9d57d7d28358ba25163bf250d803048ef8f806))
* **cli:** add proxy-envelope client pattern to generator ([#65](https://github.com/mvanhorn/cli-printing-press/issues/65)) ([f0bb0de](https://github.com/mvanhorn/cli-printing-press/commit/f0bb0de244f7e61d43523430b8641ad7180bf4a3))
* **cli:** add publish skill to ship CLIs to printing-press-library ([#54](https://github.com/mvanhorn/cli-printing-press/issues/54)) ([bf14db9](https://github.com/mvanhorn/cli-printing-press/commit/bf14db97ae866a6aaf5cab6830458ba9024d0361))
* **cli:** add smart-default output format to generator templates ([#60](https://github.com/mvanhorn/cli-printing-press/issues/60)) ([283ab9b](https://github.com/mvanhorn/cli-printing-press/commit/283ab9bfeaadaa44a20b8affe68453b870682de7))
* **cli:** add Sources & Inspiration section to generated README ([#72](https://github.com/mvanhorn/cli-printing-press/issues/72)) ([91c87cd](https://github.com/mvanhorn/cli-printing-press/commit/91c87cde728b6c64e8f409c8640859ff2b9aebd3))
* **cli:** add spec_source, auth_required, client_pattern to catalog schema ([#61](https://github.com/mvanhorn/cli-printing-press/issues/61)) ([f5716d9](https://github.com/mvanhorn/cli-printing-press/commit/f5716d9039613a392e903fe32448bea871445aab))
* **cli:** auth onboarding UX for generated CLIs ([#78](https://github.com/mvanhorn/cli-printing-press/issues/78)) ([e113599](https://github.com/mvanhorn/cli-printing-press/commit/e11359923a2611098449a5a93e94b308cfbaab7e))
* **cli:** auto-calibrate endpoint-per-resource limit from spec ([dd5abb1](https://github.com/mvanhorn/cli-printing-press/commit/dd5abb1fc13425e3143bcd596e6b03d3fe077944))
* **cli:** auto-detect OpenAPI vs internal spec format ([6000910](https://github.com/mvanhorn/cli-printing-press/commit/600091039e1c28a28c253db682dfa86c8f1f71d1))
* **cli:** browser auth, composed cookies, smart output, and sniff robustness ([#115](https://github.com/mvanhorn/cli-printing-press/issues/115)) ([6d2d059](https://github.com/mvanhorn/cli-printing-press/commit/6d2d059d35cc1d1d4835d46be2e8e7ff8550a5f8))
* **cli:** detect and emit required API headers from OpenAPI specs ([#125](https://github.com/mvanhorn/cli-printing-press/issues/125)) ([79a9458](https://github.com/mvanhorn/cli-printing-press/commit/79a945857700df516338a59d7b7608452468627c))
* **cli:** differentiate exit codes by failure type ([bac0a0c](https://github.com/mvanhorn/cli-printing-press/commit/bac0a0c01ef8f6d1afbd6c165da429c6d840e481))
* **cli:** differentiate exit codes by failure type ([538e65c](https://github.com/mvanhorn/cli-printing-press/commit/538e65c4ab0053bed3ccea367012e424325ad3ca))
* **cli:** enum sync expansion and generic API prefix stripping ([#118](https://github.com/mvanhorn/cli-printing-press/issues/118)) ([34e354f](https://github.com/mvanhorn/cli-printing-press/commit/34e354f6a8d070d98338607e94220722530b4cce))
* **cli:** generator pipeline improvements — auth inference, verify env, sync paths ([#103](https://github.com/mvanhorn/cli-printing-press/issues/103)) ([6da6fd0](https://github.com/mvanhorn/cli-printing-press/commit/6da6fd098fa6dad43074794157cbb2b265979a30))
* **cli:** hide raw resource commands when promoted exist, add api discovery ([#121](https://github.com/mvanhorn/cli-printing-press/issues/121)) ([4b12b32](https://github.com/mvanhorn/cli-printing-press/commit/4b12b325c3de463d80092d0768b090af1d13faac))
* **cli:** infer API auth from spec description when securitySchemes missing ([#126](https://github.com/mvanhorn/cli-printing-press/issues/126)) ([75ad9cd](https://github.com/mvanhorn/cli-printing-press/commit/75ad9cdbde6f63e245fc95128ec30f6b4b9174ac))
* **cli:** multi-spec composition with --spec repetition ([bb12a60](https://github.com/mvanhorn/cli-printing-press/commit/bb12a6054bf480199989fab7538aad57ce9f0d7c))
* **cli:** non-skippable dogfood gate and deeper data pipeline validation ([#127](https://github.com/mvanhorn/cli-printing-press/issues/127)) ([8ec4fc7](https://github.com/mvanhorn/cli-printing-press/commit/8ec4fc7e86c3627f7410d1f5c7b0f3fa0d210a35))
* **cli:** runstate isolation and lock lifecycle for parallel build safety ([#114](https://github.com/mvanhorn/cli-printing-press/issues/114)) ([10150ad](https://github.com/mvanhorn/cli-printing-press/commit/10150ad3ae217601e0ea2c64662ac7937693db7c))
* **cli:** search body construction, README website links, and profiler param detection ([#120](https://github.com/mvanhorn/cli-printing-press/issues/120)) ([90cf584](https://github.com/mvanhorn/cli-printing-press/commit/90cf58440dee96e7dacb0da383b5a3bd7501f43d))
* **cli:** use local module path at generation, rewrite at publish ([#63](https://github.com/mvanhorn/cli-printing-press/issues/63)) ([244c484](https://github.com/mvanhorn/cli-printing-press/commit/244c4845521378fa1e248ec34d3482469367f02d))
* **dogfood:** add ExampleCheck to validate help example correctness ([714f1bd](https://github.com/mvanhorn/cli-printing-press/commit/714f1bda2cf37f8d76c35d7136a8a0e68f0286a9))
* **dogfood:** add ExampleCheck to validate help example correctness ([c3eacf9](https://github.com/mvanhorn/cli-printing-press/commit/c3eacf940b6ad493609383a66d86529fcf1be16e))
* **dogfood:** add mechanical CLI validation command ([82bae3e](https://github.com/mvanhorn/cli-printing-press/commit/82bae3ee664602548f7db63e61639638f9de67b4))
* **emboss:** add second-pass improvement command for generated CLIs ([efbf4b8](https://github.com/mvanhorn/cli-printing-press/commit/efbf4b8814a2384dadcde0307ac6ad363f933006))
* **emboss:** complete baseline persistence, delta computation, and full-mode UX ([75c37eb](https://github.com/mvanhorn/cli-printing-press/commit/75c37eb6ff4057e7b0e9c34c944dc6f41dcda6a7))
* **generator:** add color and TTY detection to generated CLIs ([3cb6e86](https://github.com/mvanhorn/cli-printing-press/commit/3cb6e869a1a2b80c0bb0295e891e8ba43f8fe1e0))
* **generator:** add compound workflow template with archive and status ([d51d1e8](https://github.com/mvanhorn/cli-printing-press/commit/d51d1e89b6a26a852e20ffe9c7608ef888fe1af2))
* **generator:** add CRUD aliases to generated CLI commands ([5b3d281](https://github.com/mvanhorn/cli-printing-press/commit/5b3d281e58a07552df27881e61f15dabe5a88585))
* **generator:** add Non-Obvious Insight system, domain archetype detection, and entity mapping ([f5369dd](https://github.com/mvanhorn/cli-printing-press/commit/f5369dd0bfebb99a665f0e23244a88741919f098))
* **generator:** add PM workflow and behavioral insight templates ([f55405b](https://github.com/mvanhorn/cli-printing-press/commit/f55405b82af4534bdeeef7ffef585f8abd2fea79))
* **generator:** add schema builder with data gravity scoring and insight scorecard dimension ([63b89f6](https://github.com/mvanhorn/cli-printing-press/commit/63b89f6d34e1d22b3abb3be53986009a32275d37))
* **generator:** Apache 2.0 license on generated CLIs with NOTICE attribution ([86c5d90](https://github.com/mvanhorn/cli-printing-press/commit/86c5d908f7ffe8c8b7a3e015766b1b85fec0cebb))
* **generator:** auto-detect array responses and render as formatted tables ([c5618c0](https://github.com/mvanhorn/cli-printing-press/commit/c5618c0830d4d7e623413af52861fef59469ffe8))
* **generator:** auto-detect pagination and generate --limit/--all flags ([332fe59](https://github.com/mvanhorn/cli-printing-press/commit/332fe5952606d0a2f9b0ef3f03e0666986e12590))
* **generator:** auto-generate usage examples in command help ([e993ba6](https://github.com/mvanhorn/cli-printing-press/commit/e993ba6e248c25b5f4d1986fedc1676a4e0a72da))
* **generator:** generate MCP server alongside CLI from OpenAPI spec ([6a80b5a](https://github.com/mvanhorn/cli-printing-press/commit/6a80b5af941336677a793c7363cf2f1e182bff01))
* **generator:** make generated CLIs agent-native by default ([#43](https://github.com/mvanhorn/cli-printing-press/issues/43)) ([a8a003d](https://github.com/mvanhorn/cli-printing-press/commit/a8a003daf7ca0a28244de3b563eba8fb7d133f27))
* **generator:** OAuth2 auth flow for generated CLIs ([dacda31](https://github.com/mvanhorn/cli-printing-press/commit/dacda31f8213dcd73f748805f325d67b4259d2a6))
* **generator:** retry logic, structured exit codes, and dry-run support ([086f1d4](https://github.com/mvanhorn/cli-printing-press/commit/086f1d4584c13ca8097895ba7294d1b59efa3b5c))
* **generator:** route generated CLI output to shelf/ directory ([34e646c](https://github.com/mvanhorn/cli-printing-press/commit/34e646c26ec91a0df1a3b6e0bf477b7116e68d8e))
* **generator:** sub-resource grouping for nested API paths ([3d07636](https://github.com/mvanhorn/cli-printing-press/commit/3d07636b0334ea1699cc223ec1c5529ca4290802))
* **generator:** wire BuildSchema to store/sync/search templates ([eb59816](https://github.com/mvanhorn/cli-printing-press/commit/eb59816ba04347f0ccee0f0851b3a9cd17942419))
* **generator:** wire vision templates into Generate() ([bba88f6](https://github.com/mvanhorn/cli-printing-press/commit/bba88f65173ffca7263884713d1179052f0e22f8))
* **graphql:** add GraphQL SDL parser for CLI generation ([8c92c19](https://github.com/mvanhorn/cli-printing-press/commit/8c92c19da95f7e9b4ebeae9837caaccd0e28edea))
* **linear:** generate Linear CLI with 12 resources and 45 commands ([0b89293](https://github.com/mvanhorn/cli-printing-press/commit/0b892938f75ed2298441558bdc7298ac2ffc7b00))
* **llm:** add LLM brain before generation - the press understands before it builds ([a1e7af4](https://github.com/mvanhorn/cli-printing-press/commit/a1e7af4e2488de39b399e0033505811e1c600555))
* **llmpolish:** add LLM polish pass - the press is now smart, not just fast ([76d082f](https://github.com/mvanhorn/cli-printing-press/commit/76d082f5a4e560395a524ade55d3af60f15ee0c7))
* **llmpolish:** add LLM Vision Synthesis for domain-aware customization ([611f4b6](https://github.com/mvanhorn/cli-printing-press/commit/611f4b67c4fc1c5ae94106c3d99918ab8caeea32))
* **openapi:** add OpenAPI 3.0+ parser with kin-openapi ([e81fc87](https://github.com/mvanhorn/cli-printing-press/commit/e81fc8723ae8d1f490412dc49777f8dfb4e600ec))
* **openapi:** integration tests + oneline template fix for multiline descriptions ([6011234](https://github.com/mvanhorn/cli-printing-press/commit/6011234c08aea8cf331caa29227ebc25df502d37))
* **parser:** add lenient mode + comprehensive test suite from overnight learnings ([ffb7a7e](https://github.com/mvanhorn/cli-printing-press/commit/ffb7a7e65beffdd501b0e9cfe7efc225370b693b))
* **pipeline:** add 10 new APIs to known specs registry for dogfood gauntlet ([d81e961](https://github.com/mvanhorn/cli-printing-press/commit/d81e961a5877c5d1cda9efbce38bec1c9a277be0))
* **pipeline:** add autonomous dogfood phase with 3-tier test system ([aae7801](https://github.com/mvanhorn/cli-printing-press/commit/aae780175eff65c9f44573f5bb81891c81b0ffad))
* **pipeline:** add ClaimOutputDir for atomic directory claiming ([93b8b4c](https://github.com/mvanhorn/cli-printing-press/commit/93b8b4cc853b100251fa314038b3eb731ed372c7))
* **pipeline:** add comparative analysis scoring and GoReleaser brews section ([1f5871b](https://github.com/mvanhorn/cli-printing-press/commit/1f5871bdcf399184e66d97e864217c5e6b15ae2f))
* **pipeline:** add dogfood automation, anti-AI text filter, and README augmentation ([bc5c5db](https://github.com/mvanhorn/cli-printing-press/commit/bc5c5db85332ca9a13a5eda5f8c3eaa358afb93c))
* **pipeline:** add Phase 4.9 agent readiness review + skill improvements from Cal.com run ([81eceba](https://github.com/mvanhorn/cli-printing-press/commit/81eceba2a175df5983e5632a22133e44a12f53c9))
* **pipeline:** add PhaseAgentReadiness and plugin dependency ([bdc9a90](https://github.com/mvanhorn/cli-printing-press/commit/bdc9a90f563c1a89d37977bafd4a91a016dfe2e9))
* **pipeline:** add pipeline state manager with phase tracking ([e2560ea](https://github.com/mvanhorn/cli-printing-press/commit/e2560ea7bf4d04667d1b36a917e04f26f93794d6))
* **pipeline:** add plan_status field to PhaseState for seed expansion tracking ([fa2459d](https://github.com/mvanhorn/cli-printing-press/commit/fa2459dd9e1697cd3798273581b7143c3d04df42))
* **pipeline:** add Proof-of-Behavior verification phase ([efaec84](https://github.com/mvanhorn/cli-printing-press/commit/efaec84bd19b81e42291da3b06fe902324e5ceee))
* **pipeline:** add Research and Comparative phases with catalog extensions ([466715f](https://github.com/mvanhorn/cli-printing-press/commit/466715fbd7ecb287e4be0a6b76fcc4071841e3b5))
* **pipeline:** atomic auto-incrementing output directories ([fffbe1c](https://github.com/mvanhorn/cli-printing-press/commit/fffbe1ce2f93fde2dd749c1a96418c30e730765d))
* **pipeline:** copy spec into output dir after generation ([a00ebdb](https://github.com/mvanhorn/cli-printing-press/commit/a00ebdb2e41256807f15440b6eeab46af9f79ece))
* **pipeline:** full press run with MakeBestCLI, scorecard fixes, and comparison table ([99de67e](https://github.com/mvanhorn/cli-printing-press/commit/99de67ec0ab380b9531dd3e57212a6485a43ca2c))
* **pipeline:** move mutable runs into scoped runstate ([#30](https://github.com/mvanhorn/cli-printing-press/issues/30)) ([4120dfc](https://github.com/mvanhorn/cli-printing-press/commit/4120dfcb972513055223b58ffcdd115988b29b8e))
* **pipeline:** press intelligence engine - dynamic plans, competitor intel, doc-to-spec, scorecard ([731b0ce](https://github.com/mvanhorn/cli-printing-press/commit/731b0ce7086d1d99034bb016b3cadb29d43dc8b8))
* **pipeline:** ship loop, live API testing, rename Steinberger scoring ([06b9270](https://github.com/mvanhorn/cli-printing-press/commit/06b9270a78a0ca762d12211e2466b139b8a6a61e))
* **pipeline:** spec discovery registry and overlay merge types ([5d456c6](https://github.com/mvanhorn/cli-printing-press/commit/5d456c67fceca750ed074f2642891e73fbd26431))
* **plugin:** add Claude Code plugin manifest ([07d0564](https://github.com/mvanhorn/cli-printing-press/commit/07d0564ae02e374d966b8e09ae5cb25f75e670f9))
* **press:** add Phase 0.7 prediction engine, Discord CLI, 6-artifact pipeline ([6775a86](https://github.com/mvanhorn/cli-printing-press/commit/6775a8629659d361858b76a6117ca92375ef2e92))
* **press:** add Phase 4.5 Dogfood Emulation - spec-derived API testing ([7b5ce38](https://github.com/mvanhorn/cli-printing-press/commit/7b5ce383128238d63a7ec7fd8f8d32695a7775fc))
* **press:** expand Phase 4.5 with report-fix-retest cycle ([ced6cef](https://github.com/mvanhorn/cli-printing-press/commit/ced6cefc07dda9a3eed67371bc73ba35aaf5cc1a))
* **press:** support GraphQL APIs - warn but proceed, don't block ([9e8cc5d](https://github.com/mvanhorn/cli-printing-press/commit/9e8cc5d5d779c1efd498dc89e25637688e69ad73))
* **press:** v2 - depth over breadth, creativity over mechanical ([13860ec](https://github.com/mvanhorn/cli-printing-press/commit/13860ec01423bbca4e1495dcbb81c68cccd1484b))
* printing-press v2 anti-hallucination overhaul ([38829cb](https://github.com/mvanhorn/cli-printing-press/commit/38829cb86383cd6bcf81ac55713e8ff55b97ab95))
* **profiler:** add API Shape Intelligence Engine ([f499c4b](https://github.com/mvanhorn/cli-printing-press/commit/f499c4b57c38df3b0dfc552e99e8353d13142af0))
* **scaffold:** initial project structure with CLI skeleton ([454739b](https://github.com/mvanhorn/cli-printing-press/commit/454739bd04333318247c1fb4c4d57cf47237b072))
* **score:** add standalone `/printing-press-score` skill ([1083ac3](https://github.com/mvanhorn/cli-printing-press/commit/1083ac32f4a7982738558d1508cdddbb201addef))
* **scorecard:** add breadth dimension + fix LLM runner + integration tests ([95e2683](https://github.com/mvanhorn/cli-printing-press/commit/95e26838f64d1a283c0a004272c86ec273fdc093))
* **scorecard:** add Tier 2 domain correctness dimensions ([bba34c4](https://github.com/mvanhorn/cli-printing-press/commit/bba34c4295a27b3b25f1f8358b648b947f3aad90))
* **scorecard:** implement two-tier Vision scoring ([de296d1](https://github.com/mvanhorn/cli-printing-press/commit/de296d1fcd73148c8bee3706bb50d3bc3e3ca838))
* **skill:** add /printing-press Claude Code skill ([412c2fe](https://github.com/mvanhorn/cli-printing-press/commit/412c2fe6241102b19155de6a831ec4ca36940576))
* **skill:** add /printing-press submit workflow for catalog contributions ([45045b9](https://github.com/mvanhorn/cli-printing-press/commit/45045b92fef3f563221a01e369f60f45f2977c08))
* **skill:** add /printing-press-catalog for browsing and installing CLIs ([3ff4500](https://github.com/mvanhorn/cli-printing-press/commit/3ff4500f7b725ffc323279823577304e00a9fede))
* **skill:** add /printing-press-score for standalone CLI scoring ([c08aedc](https://github.com/mvanhorn/cli-printing-press/commit/c08aedc7dcd48958479a39c790750a5ad6478a04))
* **skill:** add 7-principle agent build checklist and Priority 1 review gate to Phase 3 ([6727b86](https://github.com/mvanhorn/cli-printing-press/commit/6727b8603a8a2f2cbc3f3efc361954196531d89c))
* **skill:** add autonomous pipeline workflow with nightnight-style chaining ([eab64ed](https://github.com/mvanhorn/cli-printing-press/commit/eab64edd11e34cb004ec8787b799db62c382c37b))
* **skill:** add autonomous pipeline workflows with nightnight-style chaining ([9734d0c](https://github.com/mvanhorn/cli-printing-press/commit/9734d0c162de56be27db40de87ba5a634f6f1f93))
* **skill:** add opt-in Codex delegation mode for token savings ([ff9f5e6](https://github.com/mvanhorn/cli-printing-press/commit/ff9f5e61082172c54698552bc8b11f9c2d094f26))
* **skill:** add Phase 1.5 Ecosystem Absorb Gate - build the GOAT by stealing every best idea ([49b98a3](https://github.com/mvanhorn/cli-printing-press/commit/49b98a3b15f8a6c2fc3758f53c5219d05dacbecf))
* **skill:** add Phase 4.6 hallucination audit and anti-gaming rules ([7bcacea](https://github.com/mvanhorn/cli-printing-press/commit/7bcacea317ba7bc18127e578f975622f45805d03))
* **skill:** add Phase 4.9 agent readiness review loop to SKILL.md ([5a6c809](https://github.com/mvanhorn/cli-printing-press/commit/5a6c8095b775421a9f4e8ab78ae33b5c89111e90))
* **skill:** add product thesis, market research, naming pass, runtime verify phase ([ad4812f](https://github.com/mvanhorn/cli-printing-press/commit/ad4812f3524f2e79b5e3bfe79117eeea2bfa85b2))
* **skill:** restore plan-execute-plan-execute loop - the press is smart again ([68f6316](https://github.com/mvanhorn/cli-printing-press/commit/68f63167f394fd807374b3779932bcf9a7ca1dbe))
* **skills:** add /printing-press-polish standalone skill ([#90](https://github.com/mvanhorn/cli-printing-press/issues/90)) ([0348797](https://github.com/mvanhorn/cli-printing-press/commit/03487972b10a619115de5059ed8e70441e05e6b6))
* **skills:** add API reachability gate before generation ([#91](https://github.com/mvanhorn/cli-printing-press/issues/91)) ([bed2f97](https://github.com/mvanhorn/cli-printing-press/commit/bed2f97c3afb99c6a9b89ad2c7582fdf1f4191b1))
* **skills:** add browser-use as primary sniff capture backend ([#47](https://github.com/mvanhorn/cli-printing-press/issues/47)) ([82bc5a3](https://github.com/mvanhorn/cli-printing-press/commit/82bc5a376ad35216c6b4c1cb3ad87018de30bcb4))
* **skills:** add browser-use version compatibility check to sniff gate ([#74](https://github.com/mvanhorn/cli-printing-press/issues/74)) ([89ca2ff](https://github.com/mvanhorn/cli-printing-press/commit/89ca2ffdf315a14f9dcc5ff8faabc3f226068e05))
* **skills:** add onboarding briefing and showcase novel features at absorb gate ([#96](https://github.com/mvanhorn/cli-printing-press/issues/96)) ([8311b13](https://github.com/mvanhorn/cli-printing-press/commit/8311b137915be706046c0e17dcc7ef00e8274ca5))
* **skills:** add proactive auth intelligence and session transfer for sniff gate ([#97](https://github.com/mvanhorn/cli-printing-press/issues/97)) ([63970f8](https://github.com/mvanhorn/cli-printing-press/commit/63970f8f878ec9be4e54640ab9f82828cc593f3a))
* **skills:** add URL detection and disambiguation to printing-press skill ([#73](https://github.com/mvanhorn/cli-printing-press/issues/73)) ([cac7eac](https://github.com/mvanhorn/cli-printing-press/commit/cac7eac23f70092027ad0c1ae58495ac11c5c1c6))
* **skills:** add Victorian printing press operator voice ([#95](https://github.com/mvanhorn/cli-printing-press/issues/95)) ([129cdea](https://github.com/mvanhorn/cli-printing-press/commit/129cdea284a45e5aa6b465798b65bb66ff80333d))
* **skills:** auto-brainstorm features before absorb gate ([#87](https://github.com/mvanhorn/cli-printing-press/issues/87)) ([d289a89](https://github.com/mvanhorn/cli-printing-press/commit/d289a89237f2629c2907b6291608ae929f27e385))
* **skills:** auto-suggest novel CLI features before absorb gate ([#50](https://github.com/mvanhorn/cli-printing-press/issues/50)) ([a350f41](https://github.com/mvanhorn/cli-printing-press/commit/a350f4190a8f38c248e384b7bd000cac7cdf58d3))
* **skills:** extract polish protocol into polish-worker agent ([af759a2](https://github.com/mvanhorn/cli-printing-press/commit/af759a2730bc44722497656f67b7eac96a75f838))
* **skills:** implement codex delegation mode in printing-press skill ([#71](https://github.com/mvanhorn/cli-printing-press/issues/71)) ([99b0768](https://github.com/mvanhorn/cli-printing-press/commit/99b0768d60ab3c26e78543e316af16ef3e651300))
* **skills:** integrate sniff into printing-press skill workflow ([#44](https://github.com/mvanhorn/cli-printing-press/issues/44)) ([334a52a](https://github.com/mvanhorn/cli-printing-press/commit/334a52af749fc62b4d5cb5c959b26adc8969a787))
* **skills:** make printing-press-retro a public skill ([#129](https://github.com/mvanhorn/cli-printing-press/issues/129)) ([#131](https://github.com/mvanhorn/cli-printing-press/issues/131)) ([c2076e0](https://github.com/mvanhorn/cli-printing-press/commit/c2076e02461816c849805c016a02c9508fc6d72c))
* **skills:** offer publish after CLI generation completes ([#57](https://github.com/mvanhorn/cli-printing-press/issues/57)) ([4a1bd28](https://github.com/mvanhorn/cli-printing-press/commit/4a1bd2892ce3576b6c0a9b116797903b6413d28c))
* **skills:** offer to install capture tools when sniff gate fires ([#48](https://github.com/mvanhorn/cli-printing-press/issues/48)) ([b2b4732](https://github.com/mvanhorn/cli-printing-press/commit/b2b47320ac4b8da5f8334165dfbe660c7b8c0cca))
* **skills:** populate README source credits from absorb manifest ([1f9148f](https://github.com/mvanhorn/cli-printing-press/commit/1f9148f0cf5a689fadf9c05704499ed4803288e8))
* **skills:** read MCP source code during ecosystem absorb ([#79](https://github.com/mvanhorn/cli-printing-press/issues/79)) ([3093e11](https://github.com/mvanhorn/cli-printing-press/commit/3093e11a0945e20eeceb98cce7cf170280ab97a4))
* **skills:** show existing CLI context and clarify regeneration menu ([#58](https://github.com/mvanhorn/cli-printing-press/issues/58)) ([590a07b](https://github.com/mvanhorn/cli-printing-press/commit/590a07b19c333e87971afad36456f57d397b55a1))
* **skill:** update Workflow 4 to check plan_status for seed vs expanded ([37008dd](https://github.com/mvanhorn/cli-printing-press/commit/37008dd9a0a4c2a4958b09a5aa139eafaa7499c5))
* **skill:** v1.1.0 dual Steinberger analysis, deep research, complex body fields ([bb0acf3](https://github.com/mvanhorn/cli-printing-press/commit/bb0acf3746ceaf06db7d64128e3a6cce2e99cffe))
* **skill:** v2 overhaul - 14 changes from Notion + Linear post-mortems ([854ff60](https://github.com/mvanhorn/cli-printing-press/commit/854ff6014fb5292f804fce41c7856552e805f704))
* **spec:** extend internal format for OpenAPI + add real OpenAPI test fixtures ([4e3888a](https://github.com/mvanhorn/cli-printing-press/commit/4e3888ab4457f32a08fc685ebb018c47891dfedb))
* **spec:** YAML spec parser with validation and Stytch test fixture ([b4ab56b](https://github.com/mvanhorn/cli-printing-press/commit/b4ab56ba6c58477cfa439b67fcd8a551b6543b7c))
* **store:** generate per-resource SQLite tables from profiler output ([bbd0a47](https://github.com/mvanhorn/cli-printing-press/commit/bbd0a47c69619509395da5cf016ec59ef6d09041))
* **templates:** add --human-friendly flag and NDJSON pagination events ([c266b6f](https://github.com/mvanhorn/cli-printing-press/commit/c266b6fb9de9f8c22e6f076a0edf41635d9add03))
* **templates:** add --select flag, error hints, README rewrite, and Owner variable ([780c6b5](https://github.com/mvanhorn/cli-printing-press/commit/780c6b51715d663f9fc70909f84d2723e960e18f))
* **templates:** add flag suggestions, sync NDJSON events, and MCP response quality ([0388262](https://github.com/mvanhorn/cli-printing-press/commit/038826233985fa6d36864e5b2d547242df361a6e))
* **templates:** add structured confirmation envelope for mutating commands ([d91566f](https://github.com/mvanhorn/cli-printing-press/commit/d91566f9c785c5359965affb70eb640853fca3dd))
* **templates:** agent-friendly error messages, truncation hints, wired flags ([64c58d8](https://github.com/mvanhorn/cli-printing-press/commit/64c58d863adb85499f32960375d75249dbc37ac2))
* **templates:** agent-native CLI improvements - stdin, idempotency, --yes, examples ([b862cf4](https://github.com/mvanhorn/cli-printing-press/commit/b862cf4cd5d5e0a64b7b1815f9212e8524996df9))
* **templates:** auto-JSON piping, --no-input, --compact (Ramp CLI learnings) ([300c01b](https://github.com/mvanhorn/cli-printing-press/commit/300c01bdac139f2fdf4c6469ed9e21f3f1aaf2d3))
* **templates:** discrawl-inspired sync performance upgrades ([8e92603](https://github.com/mvanhorn/cli-printing-press/commit/8e926036b9b2096e5c7603270c631e1c5122f431))
* **templates:** Go templates for all generated CLI files ([a78f097](https://github.com/mvanhorn/cli-printing-press/commit/a78f09790928b24c0925fe9330cf0c7d3f88ad79))
* **templates:** MCPorter-inspired template improvements ([92ecaa7](https://github.com/mvanhorn/cli-printing-press/commit/92ecaa736458b9ce3c6c8aa2c05fe11cfa05fcd2))
* **templates:** structured confirmation envelope for mutating commands ([c529789](https://github.com/mvanhorn/cli-printing-press/commit/c529789c1fc71137cd8fbd5abe8c4015b5f54ff6))
* **validate:** quality gates + Clerk/Loops test specs + integration tests ([4b510ed](https://github.com/mvanhorn/cli-printing-press/commit/4b510ed709ec93b893865250b1d98736e052422d))
* **verify:** add runtime verification command with mock server + fix loop ([4f93e79](https://github.com/mvanhorn/cli-printing-press/commit/4f93e79fca1ccbe1e545581ffaeb9f3ca5394803))
* **vision:** add Phase 0 Visionary Research - the press thinks before it prints ([e25a1b4](https://github.com/mvanhorn/cli-printing-press/commit/e25a1b4501f2a531d977d714d8f5e91e4e056830))
* **websniff:** add API endpoint classifier with analytics blocklist ([07f158f](https://github.com/mvanhorn/cli-printing-press/commit/07f158f615a8abc32057986c77660ca7678c4d50))
* **websniff:** add APISpec generator and sniff CLI command ([4f4dd9c](https://github.com/mvanhorn/cli-printing-press/commit/4f4dd9c44a80f0d588249acffa8717a970770609))
* **websniff:** add auth session capture with domain binding and security hardening ([91e04cc](https://github.com/mvanhorn/cli-printing-press/commit/91e04cca82c5701c5967646a860e8fac9ba57b96))
* **websniff:** add captured traffic test fixture generator ([96bc65d](https://github.com/mvanhorn/cli-printing-press/commit/96bc65df2228974659f5a8c8fe1273a33e8075a2))
* **websniff:** add HAR and enriched capture parser ([6c6c9cf](https://github.com/mvanhorn/cli-printing-press/commit/6c6c9cf59a55b6975ddd30bc39b7b8344ca0c9b6))
* **websniff:** add JSON schema inference from captured payloads ([f9b8e14](https://github.com/mvanhorn/cli-printing-press/commit/f9b8e141186b4055481f3dd2e46c6e598a435a3c))
* **websniff:** Sniff Mode - Discover hidden APIs from live web traffic ([8f57084](https://github.com/mvanhorn/cli-printing-press/commit/8f570846813f0ed3adfd7cf5b10a62834ce3ae5e))


### Bug Fixes

* **ci:** add gofmt pre-commit hook and lint pushed files ([#83](https://github.com/mvanhorn/cli-printing-press/issues/83)) ([c4a7dcf](https://github.com/mvanhorn/cli-printing-press/commit/c4a7dcf2e6c18ef5ad88de1751186b1eecac4352))
* **ci:** add post-merge hook to rebuild binary after git pull ([3af5d2d](https://github.com/mvanhorn/cli-printing-press/commit/3af5d2d73393e72231a271ed751248362d399219))
* **ci:** auto-rebuild printing-press binary on worktree creation ([#93](https://github.com/mvanhorn/cli-printing-press/issues/93)) ([ebc132f](https://github.com/mvanhorn/cli-printing-press/commit/ebc132f6b7fa2567c01d1e9facbfa16b0656dc86))
* **ci:** shared build cache and Go module caching to prevent test timeouts ([#33](https://github.com/mvanhorn/cli-printing-press/issues/33)) ([03a1922](https://github.com/mvanhorn/cli-printing-press/commit/03a19223bd036ca4eb0828261b0e443b1cd73c96))
* clean generated CLI artifacts safely ([40a7bf4](https://github.com/mvanhorn/cli-printing-press/commit/40a7bf44369c644dade034eb09556cbe07366d11))
* **cli:** actionable auth errors with env var names, key URLs, and 400 handling ([#92](https://github.com/mvanhorn/cli-printing-press/issues/92)) ([151ec97](https://github.com/mvanhorn/cli-printing-press/commit/151ec979dbcf9529a5e040e09012a30f23862f65))
* **cli:** add --dest flag to publish package for direct repo writes ([#111](https://github.com/mvanhorn/cli-printing-press/issues/111)) ([0993d60](https://github.com/mvanhorn/cli-printing-press/commit/0993d60c5cc9a5206c7508c902b7a2c4326720df))
* **cli:** add primary workflow verification to printing press pipeline ([#112](https://github.com/mvanhorn/cli-printing-press/issues/112)) ([a28d1b4](https://github.com/mvanhorn/cli-printing-press/commit/a28d1b4851c0de9d5b07b57edf581e294faaed7e))
* **cli:** add smart-default table output for POST endpoints ([#66](https://github.com/mvanhorn/cli-printing-press/issues/66)) ([797049f](https://github.com/mvanhorn/cli-printing-press/commit/797049f53c8b16a19e6c599ec115d54bbe1a94be))
* **cli:** address Cal.com retro findings — scoring, templates, parser, dogfood ([#133](https://github.com/mvanhorn/cli-printing-press/issues/133)) ([95c96c4](https://github.com/mvanhorn/cli-printing-press/commit/95c96c4de334f76e6aae8513d9aa48e1c86f7405))
* **cli:** auto-award OAuth2 auth points until generator supports it ([8171672](https://github.com/mvanhorn/cli-printing-press/commit/817167283ff47765d98c7f3abb756b8f7cff575c))
* **cli:** capture explicitOutput before default assignment ([ca5c11f](https://github.com/mvanhorn/cli-printing-press/commit/ca5c11fd50222103636e1c7f99e7877732d1d506))
* **cli:** crowd-sniff and generator improvements from Steam retro ([#82](https://github.com/mvanhorn/cli-printing-press/issues/82)) ([8a8916f](https://github.com/mvanhorn/cli-printing-press/commit/8a8916fdb901df04c192d530d6a6d0fc70297f79))
* **cli:** dogfood uses cobra Use: fields and recursive help walking ([b80d314](https://github.com/mvanhorn/cli-printing-press/commit/b80d31401f6c1c094786d24cab2617c4d76c798b))
* **cli:** filter crowd-sniff auth env var hints by API name relevance ([#86](https://github.com/mvanhorn/cli-printing-press/issues/86)) ([7f02e10](https://github.com/mvanhorn/cli-printing-press/commit/7f02e107ae1e584cfce1047942247b7787abe98d))
* **cli:** fix extractKeyURL known-platform check and publish skill contract test ([c29604c](https://github.com/mvanhorn/cli-printing-press/commit/c29604c068faf9fca99fee5cbf31349e95852a4c))
* **cli:** four generator improvements from Redfin retro ([#89](https://github.com/mvanhorn/cli-printing-press/issues/89)) ([6c3c8db](https://github.com/mvanhorn/cli-printing-press/commit/6c3c8db8c28f9b595379346e351a2726a1ebcf62))
* **cli:** FTS trigger safety, envelope unwrapping, dogfood testing phase ([#124](https://github.com/mvanhorn/cli-printing-press/issues/124)) ([bc32084](https://github.com/mvanhorn/cli-printing-press/commit/bc3208460d267857b6caf13c965fa64292b5271b))
* **cli:** generator improvements from postman-explore retro ([#76](https://github.com/mvanhorn/cli-printing-press/issues/76)) ([fba41de](https://github.com/mvanhorn/cli-printing-press/commit/fba41deb376acd064c39b04603a9594d5e45a515))
* **cli:** generator template improvements — ID typing, dead imports, README cookbook ([#102](https://github.com/mvanhorn/cli-printing-press/issues/102)) ([82a3614](https://github.com/mvanhorn/cli-printing-press/commit/82a3614844064fb71ba9da7be67e2cbcbd2f7ef1))
* **cli:** machine context compensation — scorer, generator, skill improvements ([#104](https://github.com/mvanhorn/cli-printing-press/issues/104)) ([55c5525](https://github.com/mvanhorn/cli-printing-press/commit/55c5525be3d8312a4b71da447760925330d032ca))
* **cli:** propagate ReadDir error in explicitOutput collision guard ([0d4e711](https://github.com/mvanhorn/cli-printing-press/commit/0d4e711c1a624b8ae78f4e61fa8a1f93040345df))
* **cli:** publish skill registry format and manuscript resolution ([#106](https://github.com/mvanhorn/cli-printing-press/issues/106)) ([b037ac0](https://github.com/mvanhorn/cli-printing-press/commit/b037ac015cf20ce5eb645655b484fc501929f598))
* **cli:** README scorer alias, template placeholder, verify env discovery ([9ab8aa1](https://github.com/mvanhorn/cli-printing-press/commit/9ab8aa120f7c319454a7c19416ea09ca5e02f408))
* **cli:** README title, code block spacing, scorer placeholder fix ([6798088](https://github.com/mvanhorn/cli-printing-press/commit/6798088627346de81a510cc340445e49a7ec05f0))
* **cli:** reject external symlinks during copy ([#53](https://github.com/mvanhorn/cli-printing-press/issues/53)) ([5ee774c](https://github.com/mvanhorn/cli-printing-press/commit/5ee774c80c6cb06f535bce2eddfbbb83a131e545))
* **cli:** replace MarkFlagRequired with RunE validation, remove import guards, fix type fidelity ([#130](https://github.com/mvanhorn/cli-printing-press/issues/130)) ([b5c9115](https://github.com/mvanhorn/cli-printing-press/commit/b5c911584e5e2882f00fd46c31a89eb881e23c7e))
* **cli:** scorer behavioral detection — path validity, insight/workflow, dogfood false positives ([#101](https://github.com/mvanhorn/cli-printing-press/issues/101)) ([#101](https://github.com/mvanhorn/cli-printing-press/issues/101)) ([8a5d01c](https://github.com/mvanhorn/cli-printing-press/commit/8a5d01ccd9d71d6dcc4ddcdc9c4c124c7d360c43))
* **cli:** scorer false positives, pagination param plumbing, and catalog proxy_routes ([#117](https://github.com/mvanhorn/cli-printing-press/issues/117)) ([5094562](https://github.com/mvanhorn/cli-printing-press/commit/5094562d123a9f210200c123eefeb68e8aceeab7))
* **cli:** scorer recognizes composed/cookie auth and apiKey header matching ([e9d9daa](https://github.com/mvanhorn/cli-printing-press/commit/e9d9daa53aed760d6051ccf584016389c50631f2))
* **cli:** SQL reserved word safety, promoted subcommands, verify classification ([#122](https://github.com/mvanhorn/cli-printing-press/issues/122)) ([289ac4b](https://github.com/mvanhorn/cli-printing-press/commit/289ac4bf09ebf4fd283b0ef2d4114659b8526dc2))
* **cli:** Steam retro improvements — scorer bugs, cache poisoning, template defaults ([#100](https://github.com/mvanhorn/cli-printing-press/issues/100)) ([1c5d6ca](https://github.com/mvanhorn/cli-printing-press/commit/1c5d6ca9ac61bbdada46189550c7b25967f6dd7a))
* **cli:** sync path resolution for non-paginated list endpoints ([8763a05](https://github.com/mvanhorn/cli-printing-press/commit/8763a05fc175510dce9636ce516bba626866e15a))
* **cli:** unhide sub-resource groups when wired into promoted commands ([26fe572](https://github.com/mvanhorn/cli-printing-press/commit/26fe57271ed5b3b2bfbc6d3205dec259e9179940))
* **cli:** use catalog Homepage for README website link, remove Homebrew section ([729ad07](https://github.com/mvanhorn/cli-printing-press/commit/729ad07d13946eefefe281d06502a94638425846))
* **cli:** write .printing-press.json manifest during generate command ([#68](https://github.com/mvanhorn/cli-printing-press/issues/68)) ([154626e](https://github.com/mvanhorn/cli-printing-press/commit/154626ed337ba85fe92a6a61b1f6698e0b37a562))
* detect short path declarations ([24a2ad1](https://github.com/mvanhorn/cli-printing-press/commit/24a2ad1851d529b2017be955391cc3aba17846a7))
* **generate:** reject non-empty output directory without --force ([138585b](https://github.com/mvanhorn/cli-printing-press/commit/138585bfb0ff9425f5190119da0928d78600ad35))
* **generate:** reject non-empty output directory without --force ([862dfa7](https://github.com/mvanhorn/cli-printing-press/commit/862dfa705efaa7a0375a1de943132be580cbfca1))
* **generator:** 6 dogfooding fixes for production-quality CLI output ([399c967](https://github.com/mvanhorn/cli-printing-press/commit/399c9678ec6e5bd7082dd15a9e87de232928d051))
* **generator:** add PATCH support + skip unexported fields in types ([588c694](https://github.com/mvanhorn/cli-printing-press/commit/588c694e908b978f5446f618ae60748c2bb53e6c))
* **generator:** auth format, module path, and example values ([0063ee7](https://github.com/mvanhorn/cli-printing-press/commit/0063ee7611abe10e8c219f1de00b724a53e86565))
* **generator:** auth mapping for Discord BotToken scheme ([2915ae3](https://github.com/mvanhorn/cli-printing-press/commit/2915ae3e929d8800507fadd2ad2d8e6cde1d377f))
* **generator:** doctor tries health endpoints before reporting status ([651d11e](https://github.com/mvanhorn/cli-printing-press/commit/651d11e183ce380948cf18519b98045d75790b70))
* **generator:** dogfood to Steinberger quality across Petstore, Stytch, Discord ([54e55a6](https://github.com/mvanhorn/cli-printing-press/commit/54e55a663e08f5e3a1c73bc90dc1a887d4343fc7))
* **generator:** harden toCamel, flagName, defaultVal and dedup body params ([1eda222](https://github.com/mvanhorn/cli-printing-press/commit/1eda222ad76f17854897626b7726bbdbb29bd942))
* **generator:** harden toCamel, flagName, types template for special chars in schema names ([d8a93e0](https://github.com/mvanhorn/cli-printing-press/commit/d8a93e0acba4f4827049ef922749783dfb09e2a4))
* **marketplace:** align marketplace.json with Claude Code schema ([8b11924](https://github.com/mvanhorn/cli-printing-press/commit/8b11924281a8166848d3541096061f1dd3639ef1))
* **marketplace:** align marketplace.json with Claude Code schema ([ebe45d5](https://github.com/mvanhorn/cli-printing-press/commit/ebe45d51c26e1bdf63c86a9bcac3c2126c970a50))
* **openapi:** deep sub-resource detection with common prefix collapse ([08dc4ee](https://github.com/mvanhorn/cli-printing-press/commit/08dc4eef606c0b5e161401d49e4796a83b6e0b08))
* **openapi:** filter global query params that appear on &gt;80% of endpoints ([babd0c6](https://github.com/mvanhorn/cli-printing-press/commit/babd0c6659cea75b72a6a5eb7593124eff5c614d))
* **openapi:** handle nullable types in OpenAPI 3.1 specs ([33d0648](https://github.com/mvanhorn/cli-printing-press/commit/33d0648398ed31a6c082976cd47b636d4a67b411))
* **openapi:** smart operationId cleaning for clean command names ([8482343](https://github.com/mvanhorn/cli-printing-press/commit/84823431b7b711e71c96e6e648fa837bdf1d8755))
* **openapi:** Swagger 2.0 detection + resource name sanitization ([4934eb5](https://github.com/mvanhorn/cli-printing-press/commit/4934eb508b01127e0d98fcd8d53984d52152a30c))
* **parser:** check OpenAPI before GraphQL to prevent false positives ([9c4349a](https://github.com/mvanhorn/cli-printing-press/commit/9c4349a3b4471a13e4527e646d1c3d77b87c98e6))
* **parser:** sanitize schema names, cap title length, handle missing servers, resolve URL templates ([3f096bc](https://github.com/mvanhorn/cli-printing-press/commit/3f096bce67197ef073d06199100c397eefad5d2f))
* **pipeline:** 5.5 live test failures auto-trigger fix loop ([0606df6](https://github.com/mvanhorn/cli-printing-press/commit/0606df640a61b3f28b9ea9aaf37eebb3b06d36f0))
* **pipeline:** backfill PlanStatus in migration and persist state ([3a9e977](https://github.com/mvanhorn/cli-printing-press/commit/3a9e977bf9f9278ce35c7bfe606fb5c7f3eb3f86))
* **pipeline:** clean generated cli artifacts ([421c81b](https://github.com/mvanhorn/cli-printing-press/commit/421c81bdcbb83d7a2d35e7c1e1f6d206ae804416))
* **pipeline:** handle remote URLs and YAML-to-JSON conversion in spec copy ([2ce13e7](https://github.com/mvanhorn/cli-printing-press/commit/2ce13e775ad288da78134266cd1b0124d05b5a0a))
* **pipeline:** match short path declarations ([3a850fc](https://github.com/mvanhorn/cli-printing-press/commit/3a850fc59a95c9133178a3f81eb253b2efef01ae))
* **plugin:** remove invalid skills field from plugin.json ([97931f2](https://github.com/mvanhorn/cli-printing-press/commit/97931f23f033ec5f9bd562da6792b2e01bde29e8))
* **plugin:** remove invalid skills field from plugin.json ([b5e6f02](https://github.com/mvanhorn/cli-printing-press/commit/b5e6f02dacf062c890cfa83dcdb7da210ba3c2a4))
* **press:** dynamic API type detection, registry is hint not gate ([b1651b5](https://github.com/mvanhorn/cli-printing-press/commit/b1651b50b60037cfed97b0fa67e3c4fec40c393d))
* **profiler:** improve HighVolume and NeedsSearch heuristics ([a783ac1](https://github.com/mvanhorn/cli-printing-press/commit/a783ac12ca60843c5231dd82cdfba38f375f2170))
* resolve merge conflicts with upstream, fix RunScorecard 4th arg ([fe1038e](https://github.com/mvanhorn/cli-printing-press/commit/fe1038e48d6876506eb67e1fdbbf05ad627d9b9e))
* **review:** extract DefaultOutputDir helper, update main skill and docs ([cf381bb](https://github.com/mvanhorn/cli-printing-press/commit/cf381bbb948bf71f874e68653c237002a3b40400))
* **scorecard:** address code review findings on accuracy changes ([dfef94f](https://github.com/mvanhorn/cli-printing-press/commit/dfef94f4c3805f76716414ea669f85b0fca876e6))
* **scorecard:** fix PassRate units, gate sync path-param credit, tighten empty sync detection ([49f5d8b](https://github.com/mvanhorn/cli-printing-press/commit/49f5d8b7b4235de678b7fde23372c5e33aab63bf))
* **scorecard:** handle unscored auth semantics ([#29](https://github.com/mvanhorn/cli-printing-press/issues/29)) ([7a1c164](https://github.com/mvanhorn/cli-printing-press/commit/7a1c164697aa3c0fd98a6fb602acb2a27c6336f1))
* **scorecard:** improve accuracy for non-trivial CLIs ([1860e15](https://github.com/mvanhorn/cli-printing-press/commit/1860e151efdd4031aa8832f31bd81553a71762b9))
* **scorecard:** improve accuracy for non-trivial CLIs ([1bebcb0](https://github.com/mvanhorn/cli-printing-press/commit/1bebcb0a41f0d58de6e94b322a89c021bfa310b7))
* **scorecard:** replace presence checks with quality-based scoring ([5c6840d](https://github.com/mvanhorn/cli-printing-press/commit/5c6840d59d3003296728cdeb19450883694521fc))
* **score:** preserve spec extension, remove hardcoded repo path ([98977f2](https://github.com/mvanhorn/cli-printing-press/commit/98977f2371823c03ac3193a4aa6ebf9830c1de40))
* **skill:** add 7 improvements from Cal.com CLI run ([bc358ef](https://github.com/mvanhorn/cli-printing-press/commit/bc358ef82ed0c8444306c80017d5737f55b8071a))
* **skill:** always research, polish, and score - never skip the brain ([4ee0895](https://github.com/mvanhorn/cli-printing-press/commit/4ee089526400c638ef4fd92dbbf889af9e85a180))
* **skill:** enforce 2-pass agent readiness reviewer loop ([#28](https://github.com/mvanhorn/cli-printing-press/issues/28)) ([25b07be](https://github.com/mvanhorn/cli-printing-press/commit/25b07be0439846714a802f7b393f7cf59fd386ec))
* **skill:** enforce 5-phase loop with inlined research and phase gates v1.0.0 ([44daea3](https://github.com/mvanhorn/cli-printing-press/commit/44daea3a2821eb3db9f2eb1925ec1c6bc71e11bc))
* **skill:** enforce Phase 4.9 agent readiness review dispatch ([e3a07f7](https://github.com/mvanhorn/cli-printing-press/commit/e3a07f7b167a5f6097ff0c1d9be2c40a5ae32c02))
* **skill:** force agent readiness review to run in foreground ([4b1544f](https://github.com/mvanhorn/cli-printing-press/commit/4b1544f437c175f99d40e3cd065c9a8198cf73f3))
* **skill:** force agent readiness review to run in foreground ([feef5fa](https://github.com/mvanhorn/cli-printing-press/commit/feef5fa560a89d73f984cdcf78eda8fb4aeeefab))
* **skill:** Phase 0.1 auto-detects API tokens before asking ([50ecc63](https://github.com/mvanhorn/cli-printing-press/commit/50ecc63a783991fe2bcf73419ac36acfe20a5387))
* **skill:** Phase 0.1 must WAIT for API key answer before proceeding ([66abf78](https://github.com/mvanhorn/cli-printing-press/commit/66abf7824b10a873f7028121a0397b29ad1fcd37))
* **skill:** preserve codex fix delegation ([17acf81](https://github.com/mvanhorn/cli-printing-press/commit/17acf818c89eb476d21be47528deae3a98597eb7))
* **skill:** preserve codex fix delegation ([0dca872](https://github.com/mvanhorn/cli-printing-press/commit/0dca872ace74f02c0b4b19c66a6af8841736742e))
* **skill:** require interactive API key consent in Phase 0 ([d9801c3](https://github.com/mvanhorn/cli-printing-press/commit/d9801c33565e3add4c3f4a39499b43b8afb89c37))
* **skills:** add cd to publish-repo before gh pr create/edit ([#69](https://github.com/mvanhorn/cli-printing-press/issues/69)) ([788a696](https://github.com/mvanhorn/cli-printing-press/commit/788a696d0f0661fc2aaf5e16fa18b72c41eaf44a))
* **skills:** add mandatory publish checkpoint after archive ([#88](https://github.com/mvanhorn/cli-printing-press/issues/88)) ([dfc20ac](https://github.com/mvanhorn/cli-printing-press/commit/dfc20ac02fbad531b80b667ba801bf06c4d5f9b3))
* **skills:** add mandatory sniff gate checkpoint before absorb gate ([#85](https://github.com/mvanhorn/cli-printing-press/issues/85)) ([5bc0b6b](https://github.com/mvanhorn/cli-printing-press/commit/5bc0b6b8653e12bb3280debdc9e0056acfe077fb))
* **skills:** add secret leak prevention rules ([#107](https://github.com/mvanhorn/cli-printing-press/issues/107)) ([fc94557](https://github.com/mvanhorn/cli-printing-press/commit/fc94557cb3cd5601818f2d2e4a3ad97e58108596))
* **skills:** archive manuscripts unconditionally after shipcheck, not inside publish gate ([#80](https://github.com/mvanhorn/cli-printing-press/issues/80)) ([1f91858](https://github.com/mvanhorn/cli-printing-press/commit/1f918585d634287f19c9e99cc0afd13102ca081c))
* **skills:** auto-install printing-press binary in setup contract ([#46](https://github.com/mvanhorn/cli-printing-press/issues/46)) ([ca67521](https://github.com/mvanhorn/cli-printing-press/commit/ca675212935979f34e5b4b9f9e9f79b14299141c))
* **skills:** auto-polish after dogfood testing when fixes were applied ([e7f756b](https://github.com/mvanhorn/cli-printing-press/commit/e7f756b2238ffb04e9f6ec5147daa2913c156714))
* **skills:** comprehensive README requirements for polish agent ([6e2e1b3](https://github.com/mvanhorn/cli-printing-press/commit/6e2e1b3c982b892fe40f7869a00b8cc98cf6c9d5))
* **skills:** correct briefing copy for CLI output description ([#98](https://github.com/mvanhorn/cli-printing-press/issues/98)) ([23648d4](https://github.com/mvanhorn/cli-printing-press/commit/23648d4791ec756b2b1c590d53047fe597d7773e))
* **skills:** don't call unauthenticated endpoints a "public API" ([#51](https://github.com/mvanhorn/cli-printing-press/issues/51)) ([0f765d1](https://github.com/mvanhorn/cli-printing-press/commit/0f765d10e8caef508e3ac7f32f0ecdcfc97b59eb))
* **skills:** fix authenticated sniff session transfer daemon lifecycle ([#99](https://github.com/mvanhorn/cli-printing-press/issues/99)) ([34a9142](https://github.com/mvanhorn/cli-printing-press/commit/34a91421148bb0c490921cd7de9d67a7f0be7368))
* **skills:** goal-driven sniff strategy replaces page-crawl approach ([#109](https://github.com/mvanhorn/cli-printing-press/issues/109)) ([125188d](https://github.com/mvanhorn/cli-printing-press/commit/125188d19069df3c0ad70c89313e763da6174848))
* **skills:** improve retro issue format — priority sections, F-prefix findings, retro doc artifact ([eae73f4](https://github.com/mvanhorn/cli-printing-press/commit/eae73f44a8b3425512b31b9061f4f762785166c0))
* **skills:** improve retro skill prioritization and skip/do criteria ([#81](https://github.com/mvanhorn/cli-printing-press/issues/81)) ([64ded0a](https://github.com/mvanhorn/cli-printing-press/commit/64ded0aeaa86bc791685181c879fa3696befd61b))
* **skills:** inline dogfood protocol steps to prevent skipping ([a1096f4](https://github.com/mvanhorn/cli-printing-press/commit/a1096f4152450ab16197e6883cb907302a60d0d0))
* **skills:** make sniff gate reliable with CLI-driven browsing and time budget ([#55](https://github.com/mvanhorn/cli-printing-press/issues/55)) ([ccf7b2e](https://github.com/mvanhorn/cli-printing-press/commit/ccf7b2ee15fcc9545268317a92f00153b0f6e986))
* **skills:** merge codex detection into setup contract ([43d21f2](https://github.com/mvanhorn/cli-printing-press/commit/43d21f22d98cfb1914c93852a838919fcf1df64a))
* **skills:** merge codex detection into setup contract ([#84](https://github.com/mvanhorn/cli-printing-press/issues/84)) ([14b9e74](https://github.com/mvanhorn/cli-printing-press/commit/14b9e74f5673005618b90f543a24b3d43f971b96))
* **skills:** polish agent picks useful Quick Start commands, not just working ones ([afcd3bd](https://github.com/mvanhorn/cli-printing-press/commit/afcd3bd61852c6e922b43a8115dd8f4895a464c9))
* **skills:** polish agent reports skipped findings with reasoning ([c4befa7](https://github.com/mvanhorn/cli-printing-press/commit/c4befa79bc45c3d9d27a03967db07750f3a480ff))
* **skills:** polish always runs after shipcheck, not just after dogfood ([d509976](https://github.com/mvanhorn/cli-printing-press/commit/d509976c21c34a0a9d0e1ee854b4feeb33257911))
* **skills:** publish skill checks for merged PRs before reusing branch ([360db30](https://github.com/mvanhorn/cli-printing-press/commit/360db307cef09ac578074f1ed540a6f9d71e1753))
* **skills:** publish skill specifies full PR URL format ([ff465aa](https://github.com/mvanhorn/cli-printing-press/commit/ff465aac373a80e0bc991766d6b74ed0194e6928))
* **skills:** publish skill supports fork-based PRs for external contributors ([#116](https://github.com/mvanhorn/cli-printing-press/issues/116)) ([5026f51](https://github.com/mvanhorn/cli-printing-press/commit/5026f516a186546ce7cf327e8e82653947df30c7))
* **skills:** recommend installing both capture tools in sniff gate ([#52](https://github.com/mvanhorn/cli-printing-press/issues/52)) ([cd61dd8](https://github.com/mvanhorn/cli-printing-press/commit/cd61dd8857d68d9200f8595a9be6bca9d2cd78bd))
* **skills:** require CLI description rewrite after generation ([#64](https://github.com/mvanhorn/cli-printing-press/issues/64)) ([bafe356](https://github.com/mvanhorn/cli-printing-press/commit/bafe35635ab184dcd3b04bdc66b88119c0975feb))
* **skills:** strengthen sniff gate and add absorb gate options ([#49](https://github.com/mvanhorn/cli-printing-press/issues/49)) ([66cbbc9](https://github.com/mvanhorn/cli-printing-press/commit/66cbbc9847de465a1d82be11ddb3a2ea517f2f91))
* **skill:** use AskUserQuestion in Phase 5.9 emboss prompt ([c85c821](https://github.com/mvanhorn/cli-printing-press/commit/c85c821f366205787a1b2f9802bbf5200ce85ced))
* **skill:** use AskUserQuestion tool in Phase 5.9 emboss prompt ([0906e2c](https://github.com/mvanhorn/cli-printing-press/commit/0906e2cc3590b712a58266daebbb932f67dd339d))
* **templates:** guard readme.md.tmpl against empty Auth.EnvVars ([2370b45](https://github.com/mvanhorn/cli-printing-press/commit/2370b45b4d2a18d1ca722109df2669763bfa02c0))
* **templates:** improve doctor health checks and add HTTP response cache ([3b4f89d](https://github.com/mvanhorn/cli-printing-press/commit/3b4f89db580a5e6afb5b2af8d8d86af3ec025c8c))
