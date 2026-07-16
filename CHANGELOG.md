# Changelog

## [4.29.0](https://github.com/mvanhorn/cli-printing-press/compare/v4.28.0...v4.29.0) (2026-07-16)


### Features

* **auth:** perms-check persisted token reads in generated config.Load / LoadCredentials ([#3596](https://github.com/mvanhorn/cli-printing-press/issues/3596)) ([63c3dc7](https://github.com/mvanhorn/cli-printing-press/commit/63c3dc748e0d4fcc1d899c167765ebdee98b3049))
* **generator:** add xml response_format for XML-backed APIs ([#3065](https://github.com/mvanhorn/cli-printing-press/issues/3065)) ([cdf7f4c](https://github.com/mvanhorn/cli-printing-press/commit/cdf7f4cde83353de2d9389b5b2253e57204ebb9a))
* **peloton:** preserve managed oauth generation ([#3609](https://github.com/mvanhorn/cli-printing-press/issues/3609)) ([3f3c219](https://github.com/mvanhorn/cli-printing-press/commit/3f3c2190706006faff27e75c28c68c153c3bae5e))


### Bug Fixes

* **cli:** allow credential-free OAuth login probes ([#3624](https://github.com/mvanhorn/cli-printing-press/issues/3624)) ([57ee95a](https://github.com/mvanhorn/cli-printing-press/commit/57ee95a28772770d60854f42579d7d61273d9d5a))
* **cli:** avoid reserved store schema collisions ([#3640](https://github.com/mvanhorn/cli-printing-press/issues/3640)) ([e279ac8](https://github.com/mvanhorn/cli-printing-press/commit/e279ac88fe581dd483ed051126c80e83107bdb02))
* **cli:** continue cursor sync across short pages ([#3617](https://github.com/mvanhorn/cli-printing-press/issues/3617)) ([be192ba](https://github.com/mvanhorn/cli-printing-press/commit/be192ba687b142bf205cdef80aefd17e1f9317b6))
* **cli:** dedupe responsePathForResource switch cases ([#3530](https://github.com/mvanhorn/cli-printing-press/issues/3530)) ([5a23446](https://github.com/mvanhorn/cli-printing-press/commit/5a2344634f9f26527b21e36028b6995443a581fc))
* **cli:** drop dangling ": " from APIError.Error() on empty response body ([#3545](https://github.com/mvanhorn/cli-printing-press/issues/3545)) ([a2f518f](https://github.com/mvanhorn/cli-printing-press/commit/a2f518f6825885fc79481c836e433d57318bb557)), closes [#2945](https://github.com/mvanhorn/cli-printing-press/issues/2945)
* **cli:** exit non-zero on missing positional and unknown subcommand ([#3633](https://github.com/mvanhorn/cli-printing-press/issues/3633)) ([06446fc](https://github.com/mvanhorn/cli-printing-press/commit/06446fc16373dd7573ffe39c8d9619afe73c43b8))
* **cli:** follow generated pagination cursors safely ([#3601](https://github.com/mvanhorn/cli-printing-press/issues/3601)) ([4c9300f](https://github.com/mvanhorn/cli-printing-press/commit/4c9300f82f6fdef404099949add7adf0ef35eed8))
* **cli:** gate nested required fields by parent ([#3585](https://github.com/mvanhorn/cli-printing-press/issues/3585)) ([7f7e0c4](https://github.com/mvanhorn/cli-printing-press/commit/7f7e0c4e9bb657877ba4621b11cbb6f5b8870023))
* **cli:** gate zero-sync data surfaces ([#3659](https://github.com/mvanhorn/cli-printing-press/issues/3659)) ([ecf2493](https://github.com/mvanhorn/cli-printing-press/commit/ecf249395db2d003b7c7642f203411df21fb9fe2))
* **cli:** handle HAL pagination envelopes and size page parameters ([#3651](https://github.com/mvanhorn/cli-printing-press/issues/3651)) ([6c57441](https://github.com/mvanhorn/cli-printing-press/commit/6c574414e26cae4ab457c23fa6daba2db62b0c68))
* **cli:** harden generated local-store behavior ([#3658](https://github.com/mvanhorn/cli-printing-press/issues/3658)) ([81b4570](https://github.com/mvanhorn/cli-printing-press/commit/81b457094c714a65238611012b3fa33e2b28ccbc))
* **cli:** harden generated MCP handlers ([#3586](https://github.com/mvanhorn/cli-printing-press/issues/3586)) ([339fdc6](https://github.com/mvanhorn/cli-printing-press/commit/339fdc6fb245357f8ba492e2fca6e59b2cc4e59a))
* **cli:** harden generated store file permissions ([#3613](https://github.com/mvanhorn/cli-printing-press/issues/3613)) ([459a867](https://github.com/mvanhorn/cli-printing-press/commit/459a867a6a3ac62cc7bf1e65c448ee6ef556d9d0))
* **cli:** honor explicit quiet for teach JSON output ([#3648](https://github.com/mvanhorn/cli-printing-press/issues/3648)) ([61041c5](https://github.com/mvanhorn/cli-printing-press/commit/61041c5784e6fd2f4b8304c9d4b737193ee250f3))
* **cli:** honor paginated HTML responses ([#3627](https://github.com/mvanhorn/cli-printing-press/issues/3627)) ([3713207](https://github.com/mvanhorn/cli-printing-press/commit/3713207fd5ebd749df51481adaa8be787622283e))
* **cli:** honor pp:typed-exit-codes in live dogfood and complete learn-loop annotations ([#3556](https://github.com/mvanhorn/cli-printing-press/issues/3556)) ([19ffc5d](https://github.com/mvanhorn/cli-printing-press/commit/19ffc5d129054c4d612a11067f4155c7e313e2eb))
* **cli:** improve which ranking and Google Discovery generation ([#3476](https://github.com/mvanhorn/cli-printing-press/issues/3476)) ([800d893](https://github.com/mvanhorn/cli-printing-press/commit/800d8936129bb9f8f644f32f957e0ba73d6947fc))
* **cli:** keep browser-http ALPN on HTTP/1.1 ([#3599](https://github.com/mvanhorn/cli-printing-press/issues/3599)) ([3623e16](https://github.com/mvanhorn/cli-printing-press/commit/3623e16ebc7560346cf5b090800e0c2bb5b49ba0))
* **cli:** normalize brand-named auth env vars ([#3595](https://github.com/mvanhorn/cli-printing-press/issues/3595)) ([91539d1](https://github.com/mvanhorn/cli-printing-press/commit/91539d107808ebe3f0c87f5e957a68d7183e6543))
* **cli:** package manifest-selected manuscripts safely ([#3582](https://github.com/mvanhorn/cli-printing-press/issues/3582)) ([16b21f6](https://github.com/mvanhorn/cli-printing-press/commit/16b21f66411e9908677acd4a34e99b846bdb0ef3))
* **cli:** parse protocol-relative server URLs ([#3625](https://github.com/mvanhorn/cli-printing-press/issues/3625)) ([a4c888d](https://github.com/mvanhorn/cli-printing-press/commit/a4c888d81df1acd7df30ce07cefd42161970d013))
* **cli:** persist OAuth token expiry in generated credentials ([#3533](https://github.com/mvanhorn/cli-printing-press/issues/3533)) ([e73f2f9](https://github.com/mvanhorn/cli-printing-press/commit/e73f2f9fe8d86e640ed72cd15ea1d33131b1c2f1))
* **cli:** prefer credential URLs in auth inference ([#3641](https://github.com/mvanhorn/cli-printing-press/issues/3641)) ([2a159c2](https://github.com/mvanhorn/cli-printing-press/commit/2a159c271c4d5a8dfc6448d2d3967a0083ea5902))
* **cli:** preserve generated credential aliases ([#3603](https://github.com/mvanhorn/cli-printing-press/issues/3603)) ([facf37a](https://github.com/mvanhorn/cli-printing-press/commit/facf37a65c6c2303105def21c2f86480f7a71081))
* **cli:** preserve hierarchical path parameter slashes ([#3654](https://github.com/mvanhorn/cli-printing-press/issues/3654)) ([949c807](https://github.com/mvanhorn/cli-printing-press/commit/949c8072386128ad372278f7aba777253b4794a8))
* **cli:** prevent unsafe request retries ([#3615](https://github.com/mvanhorn/cli-printing-press/issues/3615)) ([cf0458a](https://github.com/mvanhorn/cli-printing-press/commit/cf0458a835d05caf6bd70e6fb0cad38735576c3f))
* **cli:** rebuild stale live dogfood binaries ([#3579](https://github.com/mvanhorn/cli-printing-press/issues/3579)) ([7a5b90e](https://github.com/mvanhorn/cli-printing-press/commit/7a5b90e624a302df159e45f4d2ec91a052cd0671))
* **cli:** redact credential tokens from proof samples ([#3580](https://github.com/mvanhorn/cli-printing-press/issues/3580)) ([3cc7089](https://github.com/mvanhorn/cli-printing-press/commit/3cc708940b522f485503c9f4a987603018cabe6b))
* **cli:** refresh staged artifacts before promote ([#3612](https://github.com/mvanhorn/cli-printing-press/issues/3612)) ([947b1e9](https://github.com/mvanhorn/cli-printing-press/commit/947b1e97328ecf07a94b68e72c1a9340b83c21eb))
* **cli:** report generated batch failures ([#3616](https://github.com/mvanhorn/cli-printing-press/issues/3616)) ([065aee4](https://github.com/mvanhorn/cli-printing-press/commit/065aee414805de4601850bbfacc6e3ad35cb09ef))
* **cli:** require patched Go generator release ([#3610](https://github.com/mvanhorn/cli-printing-press/issues/3610)) ([1f77334](https://github.com/mvanhorn/cli-printing-press/commit/1f77334ddc5a76add34b65e91ce675fe9ccd77df))
* **cli:** scrub generated terminal output ([#3614](https://github.com/mvanhorn/cli-printing-press/issues/3614)) ([f839f1c](https://github.com/mvanhorn/cli-printing-press/commit/f839f1c2a2f7b398cc5c0e2cfca646d0d20b0dc6))
* **cli:** scrub generated terminal output ([#3614](https://github.com/mvanhorn/cli-printing-press/issues/3614)) ([c732ccc](https://github.com/mvanhorn/cli-printing-press/commit/c732ccc146a1c2f3bdbb453b12c8a00e4bf4219e))
* **cli:** serialize OpenAPI array query params ([#3583](https://github.com/mvanhorn/cli-printing-press/issues/3583)) ([e4389b4](https://github.com/mvanhorn/cli-printing-press/commit/e4389b4e2080bc52a5fd71cb7e5c0925dbcbfd48))
* **cli:** support structured binary response output ([#3628](https://github.com/mvanhorn/cli-printing-press/issues/3628)) ([5ed7dfe](https://github.com/mvanhorn/cli-printing-press/commit/5ed7dfe56aa0237f8ded8bd3058fec2aabb511ce))
* **cli:** use authoritative resource paths for data commands ([#3661](https://github.com/mvanhorn/cli-printing-press/issues/3661)) ([2621ebc](https://github.com/mvanhorn/cli-printing-press/commit/2621ebcde49b435ee105de7135841c366f699412))
* **cli:** validate JSON body flag shapes ([#3647](https://github.com/mvanhorn/cli-printing-press/issues/3647)) ([f232a7b](https://github.com/mvanhorn/cli-printing-press/commit/f232a7b6839a879a53e912cae35c460145c58a82))
* **cli:** verify generated TLS certificates by default ([#3605](https://github.com/mvanhorn/cli-printing-press/issues/3605)) ([18022d9](https://github.com/mvanhorn/cli-printing-press/commit/18022d9e44d4d6eb29de136cd20718875631a0e9))
* **cli:** warn on unknown --select fields ([#3652](https://github.com/mvanhorn/cli-printing-press/issues/3652)) ([620ea2f](https://github.com/mvanhorn/cli-printing-press/commit/620ea2fb340e7c12a84dfc06a96cf8b029d1a9a8))
* **skills:** Phase 0 registry lookup uses schema v2 .entries key ([#3608](https://github.com/mvanhorn/cli-printing-press/issues/3608)) ([50521b1](https://github.com/mvanhorn/cli-printing-press/commit/50521b184de7a4a851594a62e69ec4bab6eba9a3))
* **skills:** preserve runtime version layout on reprints ([#3578](https://github.com/mvanhorn/cli-printing-press/issues/3578)) ([9b81492](https://github.com/mvanhorn/cli-printing-press/commit/9b81492424fb46245fda0310fe39f8cdc515672c))

## [4.28.0](https://github.com/mvanhorn/cli-printing-press/compare/v4.27.1...v4.28.0) (2026-07-09)


### Features

* **cli:** add x-pp-example extension to override endpoint help examples ([#3380](https://github.com/mvanhorn/cli-printing-press/issues/3380)) ([2b45036](https://github.com/mvanhorn/cli-printing-press/commit/2b4503631b4fad9f04b069c42915a4cb9d7b2d05))
* **cli:** self-improving printed CLIs - default-on self-healing learn loop ([#3434](https://github.com/mvanhorn/cli-printing-press/issues/3434)) ([c19a4ee](https://github.com/mvanhorn/cli-printing-press/commit/c19a4ee30059997351dd6d735fe59f8d3a3d3912))
* **generator:** composite-aware ReconcilePartition seen-set + cascade ([#3376](https://github.com/mvanhorn/cli-printing-press/issues/3376)) ([8d0983b](https://github.com/mvanhorn/cli-printing-press/commit/8d0983b2dc64f374bde30299ae39c13dd988e99c))
* **generator:** header-driven rate limiter, parallel per-parent fan-out, membership skip, composite path-param fix ([#3429](https://github.com/mvanhorn/cli-printing-press/issues/3429)) ([d4910e6](https://github.com/mvanhorn/cli-printing-press/commit/d4910e603317e91831b282d6eba2700e2060409f))
* **generator:** surface sub-resources in MCP context taxonomy ([#3478](https://github.com/mvanhorn/cli-printing-press/issues/3478)) ([b7598b3](https://github.com/mvanhorn/cli-printing-press/commit/b7598b3fd75911abf755cdb02edec5b0a3c0d5a1))


### Bug Fixes

* **ci:** guard Go toolchain floor drift ([#3512](https://github.com/mvanhorn/cli-printing-press/issues/3512)) ([03bb829](https://github.com/mvanhorn/cli-printing-press/commit/03bb8298ef530c9e8a9814fc89f8440cc48ffd0d))
* clamp sync page size to pagination param's declared maximum ([#3441](https://github.com/mvanhorn/cli-printing-press/issues/3441)) ([1df157c](https://github.com/mvanhorn/cli-printing-press/commit/1df157c421d71f9eaad24c930365d37f92a323a2)), closes [#3440](https://github.com/mvanhorn/cli-printing-press/issues/3440)
* **cli:** back off before retrying transient network errors ([#3424](https://github.com/mvanhorn/cli-printing-press/issues/3424)) ([f4118a3](https://github.com/mvanhorn/cli-printing-press/commit/f4118a3737489ae11c8f9618031089f3cd7afefa))
* **cli:** bump Go toolchain floor to 1.26.5 ([#3506](https://github.com/mvanhorn/cli-printing-press/issues/3506)) ([c48a046](https://github.com/mvanhorn/cli-printing-press/commit/c48a0466e33e1f7ed826a5e64e006e9090384f8c))
* **cli:** calibrate GraphQL generator checks ([#3413](https://github.com/mvanhorn/cli-printing-press/issues/3413)) ([8601d16](https://github.com/mvanhorn/cli-printing-press/commit/8601d160dbafdc98bcba6ac0bc3314d8169245a6))
* **cli:** defang full-shape example credentials in secret-scrubbing reference ([#3480](https://github.com/mvanhorn/cli-printing-press/issues/3480)) ([e089073](https://github.com/mvanhorn/cli-printing-press/commit/e089073c6f976f75555ed753925fca92dc9ee6ef))
* **cli:** gate partial failure helpers by emitted callers ([#3470](https://github.com/mvanhorn/cli-printing-press/issues/3470)) ([f5a4795](https://github.com/mvanhorn/cli-printing-press/commit/f5a4795d7394725ecd675d509429ef7820fc6832))
* **cli:** harden publish artifact staging ([#3414](https://github.com/mvanhorn/cli-printing-press/issues/3414)) ([4a2b023](https://github.com/mvanhorn/cli-printing-press/commit/4a2b02329e71e64cc7ffe14ff9af569d0dfa3495))
* **cli:** honor directly-held env bearer in OAuth2 authcode AuthHeader ([#3490](https://github.com/mvanhorn/cli-printing-press/issues/3490)) ([7301f67](https://github.com/mvanhorn/cli-printing-press/commit/7301f67b91723e2d40288bec8d60010bd69c33c2))
* **cli:** honor plan-mode dry runs ([#3410](https://github.com/mvanhorn/cli-printing-press/issues/3410)) ([93c3442](https://github.com/mvanhorn/cli-printing-press/commit/93c344255fbf63e8943e7d95beb339735fb136c6))
* **cli:** hydrate scalar-id sync resources ([#3515](https://github.com/mvanhorn/cli-printing-press/issues/3515)) ([72335ba](https://github.com/mvanhorn/cli-printing-press/commit/72335baf69685bb2f5b44780f7b6f831517b1d51))
* **cli:** implement PKCE for secretless OAuth authorization-code login ([#3460](https://github.com/mvanhorn/cli-printing-press/issues/3460)) ([6e43fd5](https://github.com/mvanhorn/cli-printing-press/commit/6e43fd5d5dc458fa91b910aa9e4ddb4efb1dfacb))
* **cli:** keep local datastore MCP off HTTP endpoints ([#3411](https://github.com/mvanhorn/cli-printing-press/issues/3411)) ([d9a82a9](https://github.com/mvanhorn/cli-printing-press/commit/d9a82a9433bdb8bb1895c56a1b07e988d505d063))
* **cli:** order path-param positionals by URL path, not declaration order ([#3390](https://github.com/mvanhorn/cli-printing-press/issues/3390)) ([a64d829](https://github.com/mvanhorn/cli-printing-press/commit/a64d8295b60e9d6cd13bf27c94a9f6687501ae0c)), closes [#3369](https://github.com/mvanhorn/cli-printing-press/issues/3369)
* **cli:** remove vendor-specific profile wording ([#3430](https://github.com/mvanhorn/cli-printing-press/issues/3430)) ([ea8d6fb](https://github.com/mvanhorn/cli-printing-press/commit/ea8d6fbe8fec8d0c5e658ace31c1f89c6d0168a1))
* **cli:** route browser cookie auth through jar path ([#3408](https://github.com/mvanhorn/cli-printing-press/issues/3408)) ([d30d066](https://github.com/mvanhorn/cli-printing-press/commit/d30d0662468577cf7f70c7504b1c28e49cc90fc7))
* **cli:** skip soft lookup error probes ([#3468](https://github.com/mvanhorn/cli-printing-press/issues/3468)) ([6037ad2](https://github.com/mvanhorn/cli-printing-press/commit/6037ad292c4977c4e383d0f96554e3585e9fa9ef))
* **cli:** support array request bodies from stdin ([#3484](https://github.com/mvanhorn/cli-printing-press/issues/3484)) ([956c095](https://github.com/mvanhorn/cli-printing-press/commit/956c095b9a26ab3fae6ccd4c56da7570c5f29c0c))
* **cli:** use default page size for bare --all offset pagination ([#3519](https://github.com/mvanhorn/cli-printing-press/issues/3519)) ([d33b276](https://github.com/mvanhorn/cli-printing-press/commit/d33b276d8b9f96a98237a8256644106348749bda))
* **cli:** use unstamped generated CLI version defaults ([#3524](https://github.com/mvanhorn/cli-printing-press/issues/3524)) ([d09a23b](https://github.com/mvanhorn/cli-printing-press/commit/d09a23b456f372b24a3a34e6a53c79a5216e22f1))
* **generator:** auto-detect all Chrome channels in auth login --chrome ([#3528](https://github.com/mvanhorn/cli-printing-press/issues/3528)) ([3f11b8d](https://github.com/mvanhorn/cli-printing-press/commit/3f11b8d1fbd9e23ee13978177d603d267c7883cc))
* **generator:** mark generated security suppressions ([#3485](https://github.com/mvanhorn/cli-printing-press/issues/3485)) ([f05a0fd](https://github.com/mvanhorn/cli-printing-press/commit/f05a0fdf6d03fc1bb346bfd5f758d18bb91cd311))
* **generator:** stop mid-word truncation of root Short/Long ([#3363](https://github.com/mvanhorn/cli-printing-press/issues/3363)) ([6c4d9c4](https://github.com/mvanhorn/cli-printing-press/commit/6c4d9c43a403f29b07dead04832d3d5db6ec97d2))
* **skills:** block proposal PR fallback for real artifact work ([#3412](https://github.com/mvanhorn/cli-printing-press/issues/3412)) ([95636bf](https://github.com/mvanhorn/cli-printing-press/commit/95636bff0c83ccfc90f27aa1ec153240a99ac207))
* **skills:** preflight Go currency and disk space ([#3409](https://github.com/mvanhorn/cli-printing-press/issues/3409)) ([ff9e58e](https://github.com/mvanhorn/cli-printing-press/commit/ff9e58e85b19190ec7905e7c89f017bbbdeaf4d4))
* **skills:** raise supported version floor ([#3498](https://github.com/mvanhorn/cli-printing-press/issues/3498)) ([3b1f7c3](https://github.com/mvanhorn/cli-printing-press/commit/3b1f7c3f1cf67fa5caf397f1f89eee4d72097577))

## [4.27.1](https://github.com/mvanhorn/cli-printing-press/compare/v4.27.0...v4.27.1) (2026-06-30)


### Bug Fixes

* **cli:** add examples to jobs commands ([#3356](https://github.com/mvanhorn/cli-printing-press/issues/3356)) ([a264fd5](https://github.com/mvanhorn/cli-printing-press/commit/a264fd5eae790ea66a7769e0fe42b940ec8de9b4))
* **cli:** emit live dogfood tier annotations ([#3355](https://github.com/mvanhorn/cli-printing-press/issues/3355)) ([1f65658](https://github.com/mvanhorn/cli-printing-press/commit/1f6565860a98c69eee20d993a5b5fe9202c942b0))
* **cli:** preserve reprint runtime version layout ([#3389](https://github.com/mvanhorn/cli-printing-press/issues/3389)) ([39fa634](https://github.com/mvanhorn/cli-printing-press/commit/39fa63410ea6b3c7d5ac04b926897864c7e84776))
* **skills:** route description review fixes through research ([#3386](https://github.com/mvanhorn/cli-printing-press/issues/3386)) ([4407f1a](https://github.com/mvanhorn/cli-printing-press/commit/4407f1a899efd8c9e0e13570e64dc80eb5d28761))


### Code Refactoring

* **cli:** remove embedded catalog ([#3385](https://github.com/mvanhorn/cli-printing-press/issues/3385)) ([74fcff8](https://github.com/mvanhorn/cli-printing-press/commit/74fcff8ddc06e21925829096849ad07df7ca31b0))

## [4.27.0](https://github.com/mvanhorn/cli-printing-press/compare/v4.26.1...v4.27.0) (2026-06-27)


### Features

* **generator:** sync deletion reconciliation (dependent per-parent mark-and-sweep) ([#3206](https://github.com/mvanhorn/cli-printing-press/issues/3206)) ([407433a](https://github.com/mvanhorn/cli-printing-press/commit/407433ac5d85beb3b4b5bd0936bd408e1bf9fe58))
* **generator:** tenant resolver + flat tenant-scoped reconcile ([#3341](https://github.com/mvanhorn/cli-printing-press/issues/3341)) ([9c3219c](https://github.com/mvanhorn/cli-printing-press/commit/9c3219c108ab9c64921cad30b04f68c6751e3c31))


### Bug Fixes

* **cli:** bound sync dogfood and profiler defaults ([#3322](https://github.com/mvanhorn/cli-printing-press/issues/3322)) ([5858b98](https://github.com/mvanhorn/cli-printing-press/commit/5858b98a92045b8af68f7597edc43233c45fae2d))
* **cli:** correct generated body request serialization ([#3313](https://github.com/mvanhorn/cli-printing-press/issues/3313)) ([246113a](https://github.com/mvanhorn/cli-printing-press/commit/246113acdfdb15db39b1e897f3c85bf6ac589ea3))
* **cli:** correct generated pagination defaults ([#3299](https://github.com/mvanhorn/cli-printing-press/issues/3299)) ([0bb7820](https://github.com/mvanhorn/cli-printing-press/commit/0bb78200b1cc520499ecadb9df6fa11dfb8680f4))
* **cli:** gate generated command surfaces ([#3333](https://github.com/mvanhorn/cli-printing-press/issues/3333)) ([b15220f](https://github.com/mvanhorn/cli-printing-press/commit/b15220f0d8bcbc1bb07fb793d71f22b923b5450e))
* **cli:** harden browser-session auth imports ([#3348](https://github.com/mvanhorn/cli-printing-press/issues/3348)) ([95529b7](https://github.com/mvanhorn/cli-printing-press/commit/95529b721ef96f23595b15bd9e560663c550dc1d))
* **cli:** harden generated auth runtime ([#3298](https://github.com/mvanhorn/cli-printing-press/issues/3298)) ([103c25a](https://github.com/mvanhorn/cli-printing-press/commit/103c25a231564dcf744521224465c13c7c643df6))
* **cli:** harden generated read and search extraction ([#3321](https://github.com/mvanhorn/cli-printing-press/issues/3321)) ([99a9e41](https://github.com/mvanhorn/cli-printing-press/commit/99a9e4103b3c3c604e0cc96cbe7245e426ddf6d7))
* **cli:** harden generated sync identity runtime ([#3312](https://github.com/mvanhorn/cli-printing-press/issues/3312)) ([37f640b](https://github.com/mvanhorn/cli-printing-press/commit/37f640b9d88e58ee6be82e042ef54ecc41e214bc))
* **cli:** harden provenance and sniff edge cases ([#3320](https://github.com/mvanhorn/cli-printing-press/issues/3320)) ([4a259d6](https://github.com/mvanhorn/cli-printing-press/commit/4a259d670a3f1d2d0d9a015237ee0efe9b0b9c14))
* **cli:** harden response-path store extraction ([#3300](https://github.com/mvanhorn/cli-printing-press/issues/3300)) ([b8d9b21](https://github.com/mvanhorn/cli-printing-press/commit/b8d9b21ccea71e12726f193dcc5cca4c25df1613))
* **cli:** harden scorer verify gates ([#3319](https://github.com/mvanhorn/cli-printing-press/issues/3319)) ([3ded211](https://github.com/mvanhorn/cli-printing-press/commit/3ded211ba2f8ea287ee8b2af900dccfd18c832d3))
* **cli:** persist scorer gates and live polish status ([#3314](https://github.com/mvanhorn/cli-printing-press/issues/3314)) ([161be9c](https://github.com/mvanhorn/cli-printing-press/commit/161be9c836c4f6368c784153566e992759997cf3))
* **cli:** preserve implemented novel command scaffolds ([#3309](https://github.com/mvanhorn/cli-printing-press/issues/3309)) ([e847c1d](https://github.com/mvanhorn/cli-printing-press/commit/e847c1d319567cbe84565753d07fa64469ef4d5f))
* **cli:** preserve mcp mirror positional args ([#3332](https://github.com/mvanhorn/cli-printing-press/issues/3332)) ([a2f0842](https://github.com/mvanhorn/cli-printing-press/commit/a2f084239b445ef0ebba98ac98a128993722d8e5))
* **cli:** reduce scorer dogfood false failures ([#3344](https://github.com/mvanhorn/cli-printing-press/issues/3344)) ([eafd6ba](https://github.com/mvanhorn/cli-printing-press/commit/eafd6ba9a1390a0a81a6d72cd691ea83bad46eb2))
* **cli:** register parent-scoped sync templates ([#3331](https://github.com/mvanhorn/cli-printing-press/issues/3331)) ([f1510cd](https://github.com/mvanhorn/cli-printing-press/commit/f1510cd13504a21299ef914f2f4606ddce9f7372))
* **cli:** support POST HTML table sniffing ([#3347](https://github.com/mvanhorn/cli-printing-press/issues/3347)) ([a6e7162](https://github.com/mvanhorn/cli-printing-press/commit/a6e716223af1b94dc967be1bce69caf5e6f1b782))
* **cli:** sync narrative docs and MCP descriptions ([#3346](https://github.com/mvanhorn/cli-printing-press/issues/3346)) ([76490b8](https://github.com/mvanhorn/cli-printing-press/commit/76490b83e8a1795bd1a14250baf52ba0ef032080))
* **skills:** harden skill flow scope and CRLF handling ([#3330](https://github.com/mvanhorn/cli-printing-press/issues/3330)) ([7e56e63](https://github.com/mvanhorn/cli-printing-press/commit/7e56e6376bd9fe6fc58209ed005d59d437939085))

## [4.26.1](https://github.com/mvanhorn/cli-printing-press/compare/v4.26.0...v4.26.1) (2026-06-26)


### Bug Fixes

* **cli:** calibrate live dogfood fixtures ([#3290](https://github.com/mvanhorn/cli-printing-press/issues/3290)) ([5d70d76](https://github.com/mvanhorn/cli-printing-press/commit/5d70d7606f814642c8f495606f9f43c68d52d735))
* **cli:** improve phase five and skill validation feedback ([#3288](https://github.com/mvanhorn/cli-printing-press/issues/3288)) ([b9aa369](https://github.com/mvanhorn/cli-printing-press/commit/b9aa369b03978ee279a0beafc157abea2b0d819b))
* **cli:** treat nested help as success ([#3289](https://github.com/mvanhorn/cli-printing-press/issues/3289)) ([f4f125d](https://github.com/mvanhorn/cli-printing-press/commit/f4f125d1856449232fec5501f72967755f5802fd))

## [4.26.0](https://github.com/mvanhorn/cli-printing-press/compare/v4.25.1...v4.26.0) (2026-06-26)


### Features

* **cli:** add internal endpoint command fixtures ([#3278](https://github.com/mvanhorn/cli-printing-press/issues/3278)) ([f945d67](https://github.com/mvanhorn/cli-printing-press/commit/f945d67023ceff23712d5144ad7412430e15da0e))


### Bug Fixes

* **cli:** align cobratree MCP tool surface ([#3281](https://github.com/mvanhorn/cli-printing-press/issues/3281)) ([3270718](https://github.com/mvanhorn/cli-printing-press/commit/3270718bfd9b2d364c4a966e7833ed11c66c9284))
* **cli:** calibrate dogfood auth checks ([#3267](https://github.com/mvanhorn/cli-printing-press/issues/3267)) ([d39ee1c](https://github.com/mvanhorn/cli-printing-press/commit/d39ee1c05d2118ad5432d2b98496211e9c23a8b6))
* **cli:** classify bot-wall reachability bodies ([#3279](https://github.com/mvanhorn/cli-printing-press/issues/3279)) ([b66706e](https://github.com/mvanhorn/cli-printing-press/commit/b66706e7ab3411c7062fc6de9140e3f83810ad76))
* **cli:** emit patch go directives ([#3280](https://github.com/mvanhorn/cli-printing-press/issues/3280)) ([32329b5](https://github.com/mvanhorn/cli-printing-press/commit/32329b5571a28033d65ef3880c7951e6bf93d4a5))
* **cli:** harden browser sniff HAR quality ([#3271](https://github.com/mvanhorn/cli-printing-press/issues/3271)) ([7d7a695](https://github.com/mvanhorn/cli-printing-press/commit/7d7a695d79e8580094e0171f83a077facf6e529b))
* **cli:** harden publish secret and PII scans ([#3270](https://github.com/mvanhorn/cli-printing-press/issues/3270)) ([79bddbb](https://github.com/mvanhorn/cli-printing-press/commit/79bddbbc4fbd9860199072b581c402e8141912ed))
* **cli:** preserve novel command test ownership ([#3277](https://github.com/mvanhorn/cli-printing-press/issues/3277)) ([26af081](https://github.com/mvanhorn/cli-printing-press/commit/26af08161edff63128fd5202424646db9bd2160c))
* **cli:** suppress scorer verifier false positives ([#3272](https://github.com/mvanhorn/cli-printing-press/issues/3272)) ([ce71036](https://github.com/mvanhorn/cli-printing-press/commit/ce71036a222180a50295ffa10cdd6a73c30db761))
* **skills:** guard publish reprint workflows ([#3266](https://github.com/mvanhorn/cli-printing-press/issues/3266)) ([5c3d695](https://github.com/mvanhorn/cli-printing-press/commit/5c3d69509ab60f0423871572ccccb64c8dbe67ff))

## [4.25.1](https://github.com/mvanhorn/cli-printing-press/compare/v4.25.0...v4.25.1) (2026-06-25)


### Bug Fixes

* **cli:** add resumable MCP cursors ([#3230](https://github.com/mvanhorn/cli-printing-press/issues/3230)) ([8155e8f](https://github.com/mvanhorn/cli-printing-press/commit/8155e8fcda3346735d62c761bbe17a89bef5a07b))
* **cli:** attach Authorization header for cookie-typed auth with a real header credential ([#3201](https://github.com/mvanhorn/cli-printing-press/issues/3201)) ([9a3919d](https://github.com/mvanhorn/cli-printing-press/commit/9a3919d6f62cfb8c6f76d5f1bbc61be281d227da)), closes [#3200](https://github.com/mvanhorn/cli-printing-press/issues/3200)
* **cli:** backfill empty creator handle on regen + emit store bare-id accessor ([#3232](https://github.com/mvanhorn/cli-printing-press/issues/3232)) ([104ed22](https://github.com/mvanhorn/cli-printing-press/commit/104ed220f2adb71e09e9f31ebf99e30bd12b8293))
* **cli:** bound MCP store result envelopes ([#3234](https://github.com/mvanhorn/cli-printing-press/issues/3234)) ([c5e42bb](https://github.com/mvanhorn/cli-printing-press/commit/c5e42bbdfd8e320e8c060326eadb26cce4ecff25))
* **cli:** centralize MCP output bounds ([#3214](https://github.com/mvanhorn/cli-printing-press/issues/3214)) ([7f6431c](https://github.com/mvanhorn/cli-printing-press/commit/7f6431cd0b45d41c521ab799b0ea98bd0f2d3e1d))
* **cli:** correct SQLite pragma order to avoid SQLITE_BUSY on concurrent first-run (closes [#2926](https://github.com/mvanhorn/cli-printing-press/issues/2926)) ([#3165](https://github.com/mvanhorn/cli-printing-press/issues/3165)) ([82af5ef](https://github.com/mvanhorn/cli-printing-press/commit/82af5ef6d75e1651617e8dc2806d1851408f850e))
* **cli:** derive cookie domain from base_url when spec omits cookie_domain ([#3199](https://github.com/mvanhorn/cli-printing-press/issues/3199)) ([9c9280d](https://github.com/mvanhorn/cli-printing-press/commit/9c9280d1c9007fd1ac54bba555ea8905d924d64b))
* **cli:** derive sql tool example resource_type from the spec ([#3184](https://github.com/mvanhorn/cli-printing-press/issues/3184)) ([29659f2](https://github.com/mvanhorn/cli-printing-press/commit/29659f269504f5b8a8ee1dca70596e18c1e76c67)), closes [#2960](https://github.com/mvanhorn/cli-printing-press/issues/2960)
* **cli:** harden generated auth client retries ([#3248](https://github.com/mvanhorn/cli-printing-press/issues/3248)) ([aa05aea](https://github.com/mvanhorn/cli-printing-press/commit/aa05aeabc217fb0bce50fbf3bd166a6faf707197))
* **cli:** harden generator correctness ([#3251](https://github.com/mvanhorn/cli-printing-press/issues/3251)) ([24294a1](https://github.com/mvanhorn/cli-printing-press/commit/24294a14f0b82a3209fb4afb1dd86db6c2cadcfa))
* **cli:** harden publish packaging ([#3250](https://github.com/mvanhorn/cli-printing-press/issues/3250)) ([6c0b5d8](https://github.com/mvanhorn/cli-printing-press/commit/6c0b5d85ab5d82d4a67f4073278833d34bf4fb1e))
* **cli:** harden recipe and reprint flows ([#3252](https://github.com/mvanhorn/cli-printing-press/issues/3252)) ([83f6a45](https://github.com/mvanhorn/cli-printing-press/commit/83f6a4534a9091f0397d727d7361be98f49bf453))
* **cli:** harden validation gates ([#3249](https://github.com/mvanhorn/cli-printing-press/issues/3249)) ([a9bbdd4](https://github.com/mvanhorn/cli-printing-press/commit/a9bbdd48c3a80df10fd236508886865f1e13c979))
* **cli:** honor live dogfood happy args overlays ([#3264](https://github.com/mvanhorn/cli-printing-press/issues/3264)) ([f9dbafa](https://github.com/mvanhorn/cli-printing-press/commit/f9dbafa467559bf4bc3f2b1326ec093a9564227c))
* **cli:** honor output format flags across generated commands ([#3235](https://github.com/mvanhorn/cli-printing-press/issues/3235)) ([43033b0](https://github.com/mvanhorn/cli-printing-press/commit/43033b037980ad76023ded4cc24fcd52b7970ef9))
* **cli:** index 3-letter all-caps acronyms in generated FTS ([#3185](https://github.com/mvanhorn/cli-printing-press/issues/3185)) ([e2a9b50](https://github.com/mvanhorn/cli-printing-press/commit/e2a9b50142aa996036cd17589ba162e7e8d59f38)), closes [#3098](https://github.com/mvanhorn/cli-printing-press/issues/3098)
* **cli:** preserve generated query params ([#3240](https://github.com/mvanhorn/cli-printing-press/issues/3240)) ([65db052](https://github.com/mvanhorn/cli-printing-press/commit/65db0525c0ce40045dfb24ba6cdfc3bc141b77a1))
* **cli:** project wrapped-array output ([#3239](https://github.com/mvanhorn/cli-printing-press/issues/3239)) ([3c25b3e](https://github.com/mvanhorn/cli-printing-press/commit/3c25b3ef223f12bb993be06d0e7e1c42e191f745))
* **cli:** support OData conditions sync filters ([#3262](https://github.com/mvanhorn/cli-printing-press/issues/3262)) ([9c62eee](https://github.com/mvanhorn/cli-printing-press/commit/9c62eee18f9b0174ef437e84c298f8d0db6fa8ee))
* **cli:** unify generated agent envelopes ([#3260](https://github.com/mvanhorn/cli-printing-press/issues/3260)) ([49f2111](https://github.com/mvanhorn/cli-printing-press/commit/49f21113817ee895295572ea857393f3b493c8d5))
* **substack:** route draft writer endpoints globally ([#3191](https://github.com/mvanhorn/cli-printing-press/issues/3191)) ([36c4be0](https://github.com/mvanhorn/cli-printing-press/commit/36c4be02130544effc0673d2a0e33e22df01adc2))

## [4.25.0](https://github.com/mvanhorn/cli-printing-press/compare/v4.24.0...v4.25.0) (2026-06-21)


### Features

* **cli:** add Apify actor reachability audit ([#2867](https://github.com/mvanhorn/cli-printing-press/issues/2867)) ([b0c5b0b](https://github.com/mvanhorn/cli-printing-press/commit/b0c5b0ba1f33c256d9cd47b761da93750eb657a8))
* **cli:** emit SQL-query-endpoint sync by construction (query_sync / x-pp-query) ([#3019](https://github.com/mvanhorn/cli-printing-press/issues/3019)) ([d700bf2](https://github.com/mvanhorn/cli-printing-press/commit/d700bf2b30c5c8b82a040c784f994b3c102e8f04))
* **cli:** fold recurring Greptile findings into printed-CLI defaults ([#3005](https://github.com/mvanhorn/cli-printing-press/issues/3005)) ([11a1752](https://github.com/mvanhorn/cli-printing-press/commit/11a1752b2eb84a95e62ad95096e9baecca680efe))
* **cli:** four-kind path resolution with credential relocation for generated CLIs ([#2633](https://github.com/mvanhorn/cli-printing-press/issues/2633)) ([973174b](https://github.com/mvanhorn/cli-printing-press/commit/973174b6aa848a116039994cce9b6e57987170bd))
* **generator:** emit CLAUDE.md (@AGENTS.md) for printed CLIs ([#2959](https://github.com/mvanhorn/cli-printing-press/issues/2959)) ([70cc74c](https://github.com/mvanhorn/cli-printing-press/commit/70cc74cff3e6b5bdc68a590f876aeefe5e9c1328))
* **publish:** lightweight blobless+sparse library clone + force-add MCP source ([#3152](https://github.com/mvanhorn/cli-printing-press/issues/3152)) ([30d2017](https://github.com/mvanhorn/cli-printing-press/commit/30d2017a176c566c65fc5b40ed77db01c7ae9c88))


### Bug Fixes

* **catalog:** add health category ([#2868](https://github.com/mvanhorn/cli-printing-press/issues/2868)) ([9f9ff30](https://github.com/mvanhorn/cli-printing-press/commit/9f9ff305978e6e65b50b64d398076d4312cc9111))
* **ci:** honor latest codeowner review state ([#3068](https://github.com/mvanhorn/cli-printing-press/issues/3068)) ([33ab3d1](https://github.com/mvanhorn/cli-printing-press/commit/33ab3d1b441e2f31a82076f616664ced0af64a52))
* **ci:** warn when ready-to-merge lacks codeowner approval ([#3061](https://github.com/mvanhorn/cli-printing-press/issues/3061)) ([49f05bb](https://github.com/mvanhorn/cli-printing-press/commit/49f05bb783b358971fca9c494ba03a8870d029ce))
* **cli:** allow synthetic phase5 external credential skips ([#2866](https://github.com/mvanhorn/cli-printing-press/issues/2866)) ([89b27fc](https://github.com/mvanhorn/cli-printing-press/commit/89b27fcc268ebcc4f69e7da7f904040846de9ebf))
* **cli:** bind MCP array/object body params natively (WithArray/WithObject) ([#3075](https://github.com/mvanhorn/cli-printing-press/issues/3075)) ([47897c7](https://github.com/mvanhorn/cli-printing-press/commit/47897c782d173e94f6c6c54a7ed32029e8cab523))
* **cli:** block equals-form MCP root flags ([#3025](https://github.com/mvanhorn/cli-printing-press/issues/3025)) ([8606544](https://github.com/mvanhorn/cli-printing-press/commit/86065448654ca4ce7a1d3eb2b1174c6503cff0be))
* **cli:** emit Cobra Example in novel-feature scaffold ([#3153](https://github.com/mvanhorn/cli-printing-press/issues/3153)) ([0edc724](https://github.com/mvanhorn/cli-printing-press/commit/0edc724d372a6c42e48677e465437d55c2bdaee4))
* **cli:** emit local data layer for syncable APIs ([#2881](https://github.com/mvanhorn/cli-printing-press/issues/2881)) ([a6da532](https://github.com/mvanhorn/cli-printing-press/commit/a6da5322b69039beae067d7eeb9a4fd69bf5f099))
* **cli:** filter telemetry hosts in browser sniff ([#2865](https://github.com/mvanhorn/cli-printing-press/issues/2865)) ([08864e4](https://github.com/mvanhorn/cli-printing-press/commit/08864e4169a3c6ab40ebecf643773815ea93a96a))
* **cli:** flag oauth2 refresh missing client id ([#2902](https://github.com/mvanhorn/cli-printing-press/issues/2902)) ([4ceaada](https://github.com/mvanhorn/cli-printing-press/commit/4ceaada8c78f7d5dc7ab541f55b47803581c6e15))
* **cli:** floor x/sys&gt;=v0.46.0, x/crypto&gt;=v0.53.0, quic-go&gt;=v0.60.0 + 0700 dirs in printed CLIs ([#3017](https://github.com/mvanhorn/cli-printing-press/issues/3017)) ([a164251](https://github.com/mvanhorn/cli-printing-press/commit/a164251692068ad3d04bcb257837f496edfd5319))
* **cli:** guard read-only mcp positional write sinks ([#3037](https://github.com/mvanhorn/cli-printing-press/issues/3037)) ([cbcec34](https://github.com/mvanhorn/cli-printing-press/commit/cbcec34513dba1cb65666bb203273a34e7394cd1))
* **cli:** guard unresolved path params ([#3027](https://github.com/mvanhorn/cli-printing-press/issues/3027)) ([f8476f5](https://github.com/mvanhorn/cli-printing-press/commit/f8476f52b0fad3a301c0d56a29f20416aa792ca9))
* **cli:** honor auth preference in dogfood manifest ([#2878](https://github.com/mvanhorn/cli-printing-press/issues/2878)) ([99e73ac](https://github.com/mvanhorn/cli-printing-press/commit/99e73ac5ce882d8776bae92585d2b5eed7efacb4))
* **cli:** ignore dangling OpenAPI security refs ([#3026](https://github.com/mvanhorn/cli-printing-press/issues/3026)) ([fc3e340](https://github.com/mvanhorn/cli-printing-press/commit/fc3e34098790009a630b6c57ee0c118e4859cc15))
* **cli:** make dogfood --live + promote gate cookie-auth aware ([#3107](https://github.com/mvanhorn/cli-printing-press/issues/3107)) ([b343f4d](https://github.com/mvanhorn/cli-printing-press/commit/b343f4da15acea3c9d3307a6ea27af2ebf879239))
* **cli:** make generated auth hints scheme-aware ([#2875](https://github.com/mvanhorn/cli-printing-press/issues/2875)) ([8222735](https://github.com/mvanhorn/cli-printing-press/commit/822273573f2a9a0c8572a2839cdbc97241cc5e55))
* **cli:** normalize live gate example branch ([#2996](https://github.com/mvanhorn/cli-printing-press/issues/2996)) ([2752d9b](https://github.com/mvanhorn/cli-printing-press/commit/2752d9bb46800bfdc33b3f2e1e910be1e1ab60e9))
* **cli:** normalize store read-only branch ([#3047](https://github.com/mvanhorn/cli-printing-press/issues/3047)) ([543cfc5](https://github.com/mvanhorn/cli-printing-press/commit/543cfc54ee7c70a9391474f5085ba80a48c623e2))
* **cli:** preserve authored generated descriptions ([#2870](https://github.com/mvanhorn/cli-printing-press/issues/2870)) ([a97cddc](https://github.com/mvanhorn/cli-printing-press/commit/a97cddc7d769dc88b529d76b1adc3afededa7898))
* **cli:** preserve git metadata in regen-merge apply ([#3110](https://github.com/mvanhorn/cli-printing-press/issues/3110)) ([c2d050d](https://github.com/mvanhorn/cli-printing-press/commit/c2d050de6f0896c3eda2fdf417b014f9e119d7d7))
* **cli:** preserve legacy reprint version layout ([#3111](https://github.com/mvanhorn/cli-printing-press/issues/3111)) ([ddadd9b](https://github.com/mvanhorn/cli-printing-press/commit/ddadd9b4125bd250cc71969f3cf198867f9f3e36))
* **cli:** preserve Windows companion CLI fallback ([#2876](https://github.com/mvanhorn/cli-printing-press/issues/2876)) ([a113f4c](https://github.com/mvanhorn/cli-printing-press/commit/a113f4c2b95e63d8289f34d0bb8b370cc22a6828))
* **cli:** prune phantom which index entries ([#3115](https://github.com/mvanhorn/cli-printing-press/issues/3115)) ([fa188e4](https://github.com/mvanhorn/cli-printing-press/commit/fa188e4eb2bf10e9b9ecfff0b5b27874dcdbf2e1))
* **cli:** qualify FTS ORDER BY rank to the FTS table ([#3095](https://github.com/mvanhorn/cli-printing-press/issues/3095)) ([541d7e8](https://github.com/mvanhorn/cli-printing-press/commit/541d7e8340e7838c19ccfdb6f9fc1731b8f72978))
* **cli:** redact credential JSON keys in artifacts ([#3034](https://github.com/mvanhorn/cli-printing-press/issues/3034)) ([133e97d](https://github.com/mvanhorn/cli-printing-press/commit/133e97dc8d79c8ed1eb25e18fad94d254c13e046))
* **cli:** route local collection reads through list store ([#3113](https://github.com/mvanhorn/cli-printing-press/issues/3113)) ([2c8c293](https://github.com/mvanhorn/cli-printing-press/commit/2c8c293227dfd1b66628eee7ccb1dbe5cc58c6c2))
* **cli:** route Substack writer endpoints globally ([#3018](https://github.com/mvanhorn/cli-printing-press/issues/3018)) ([66a6c8a](https://github.com/mvanhorn/cli-printing-press/commit/66a6c8aa6b5f8c40f92a55c4a478a6417b16c458))
* **cli:** skip live dogfood placeholder fixture reads ([#3036](https://github.com/mvanhorn/cli-printing-press/issues/3036)) ([bd338f1](https://github.com/mvanhorn/cli-printing-press/commit/bd338f11a34abeac22c8b1a2040792b722cff8fd))
* **cli:** skip novel wiring on command file collisions ([#3024](https://github.com/mvanhorn/cli-printing-press/issues/3024)) ([07d6d24](https://github.com/mvanhorn/cli-printing-press/commit/07d6d245c2fa311cb17fcccf9b10ecb32f49750e))
* **cli:** stop dogfood classifier mislabeling help failures as transport_error ([#3154](https://github.com/mvanhorn/cli-printing-press/issues/3154)) ([14f97f4](https://github.com/mvanhorn/cli-printing-press/commit/14f97f42dd39ca324464ab2e77d41cb26d0d2493))
* **cli:** strip raw sniff captures from packaged manuscripts ([#3033](https://github.com/mvanhorn/cli-printing-press/issues/3033)) ([f85808e](https://github.com/mvanhorn/cli-printing-press/commit/f85808e8cadec0da3b017721f8a8b09f8ae93c9b))
* **cli:** strip trailing punctuation from verify-skill flag tokens ([#2964](https://github.com/mvanhorn/cli-printing-press/issues/2964)) ([4194bef](https://github.com/mvanhorn/cli-printing-press/commit/4194befbc125847a6960f589d5406157a01a27b7))
* **cli:** strip wrapping quotes from verify-skill prose flag tokens ([#3108](https://github.com/mvanhorn/cli-printing-press/issues/3108)) ([9555de6](https://github.com/mvanhorn/cli-printing-press/commit/9555de65a0211a4176f6ff82049bacc321a7c167))
* **cli:** sync --dry-run must not mutate sync_state (dependent resources + cursor clears) ([#2946](https://github.com/mvanhorn/cli-printing-press/issues/2946)) ([a27740e](https://github.com/mvanhorn/cli-printing-press/commit/a27740eb474c9424857b032614be93009c581d00))
* **cli:** unwrap HAL embedded sync envelopes ([#3114](https://github.com/mvanhorn/cli-printing-press/issues/3114)) ([8622b62](https://github.com/mvanhorn/cli-printing-press/commit/8622b621bb3965975e01af784f9fd099a81fb663))
* **cli:** use schema hints in generated examples ([#2874](https://github.com/mvanhorn/cli-printing-press/issues/2874)) ([175105f](https://github.com/mvanhorn/cli-printing-press/commit/175105f2be66a2afeaa118cad903f3db7e608789))
* **cli:** warn on unenriched promote manifests ([#2871](https://github.com/mvanhorn/cli-printing-press/issues/2871)) ([3c14fcb](https://github.com/mvanhorn/cli-printing-press/commit/3c14fcbe21be17a1bbdd868ec680335ecc8642a6))
* **cli:** wire seeded cookie jar for cookie-auth client transport ([#3106](https://github.com/mvanhorn/cli-printing-press/issues/3106)) ([0a89d57](https://github.com/mvanhorn/cli-printing-press/commit/0a89d57c43be4cba314e1d551a9e3188a8ba9b83))
* **dogfood:** skip top-level login alias in live dogfood matrix ([#3073](https://github.com/mvanhorn/cli-printing-press/issues/3073)) ([2d2860b](https://github.com/mvanhorn/cli-printing-press/commit/2d2860b3cee8a89fd19e4a400f79b54b3e2de684))
* **generator:** mark tail command no-error-path-probe ([#2969](https://github.com/mvanhorn/cli-printing-press/issues/2969)) ([13cd7d0](https://github.com/mvanhorn/cli-printing-press/commit/13cd7d0dd842c74461036c2ac31fd326df0be787)), closes [#2968](https://github.com/mvanhorn/cli-printing-press/issues/2968)
* **generator:** omit optional query params with empty/invalid spec defaults; trim enum tokens ([#3077](https://github.com/mvanhorn/cli-printing-press/issues/3077)) ([920f7e2](https://github.com/mvanhorn/cli-printing-press/commit/920f7e2080de8bdb7002a6107a969c9fa9bcd215))
* **generator:** qualify FTS ORDER BY rank ([#3023](https://github.com/mvanhorn/cli-printing-press/issues/3023)) ([46a5e97](https://github.com/mvanhorn/cli-printing-press/commit/46a5e978fd74b4b9104aacd70d5bf41dc14c5d28)), closes [#2973](https://github.com/mvanhorn/cli-printing-press/issues/2973)
* **generator:** refresh authorization-code bearer auth ([#3064](https://github.com/mvanhorn/cli-printing-press/issues/3064)) ([0e7ecc5](https://github.com/mvanhorn/cli-printing-press/commit/0e7ecc59d15c77c599f2d3e20b9b4a65465524c6))
* **generator:** reject empty positional path-param values with a clear error ([#3087](https://github.com/mvanhorn/cli-printing-press/issues/3087)) ([383eb6d](https://github.com/mvanhorn/cli-printing-press/commit/383eb6d78e4e13bb9213e99d1f8aa95e5c75bbe6))
* **skills:** resolve amend publish status by slug ([#3032](https://github.com/mvanhorn/cli-printing-press/issues/3032)) ([147119f](https://github.com/mvanhorn/cli-printing-press/commit/147119f56adb8c59515868a0e29a544e0f81179c))
* skip permission test on Windows where Getuid is unsupported ([#2900](https://github.com/mvanhorn/cli-printing-press/issues/2900)) ([b3fbb2e](https://github.com/mvanhorn/cli-printing-press/commit/b3fbb2ec8c119d022b7f35e2dd49fba31fff6853))
* **test:** drain captured stdout pipe to prevent deadlock ([#3072](https://github.com/mvanhorn/cli-printing-press/issues/3072)) ([69915cd](https://github.com/mvanhorn/cli-printing-press/commit/69915cd00edc6ad803d43a6733a831db1a3fa496))

## [4.24.0](https://github.com/mvanhorn/cli-printing-press/compare/v4.23.1...v4.24.0) (2026-06-08)


### Features

* **pipeline:** emit CLI release ledger skeletons ([#2859](https://github.com/mvanhorn/cli-printing-press/issues/2859)) ([e13b00d](https://github.com/mvanhorn/cli-printing-press/commit/e13b00d2481415eca53daedb81ade007b776f821))

## [4.23.1](https://github.com/mvanhorn/cli-printing-press/compare/v4.23.0...v4.23.1) (2026-06-08)


### Bug Fixes

* **cli:** add timeout helper for novel commands ([#2808](https://github.com/mvanhorn/cli-printing-press/issues/2808)) ([2767cf3](https://github.com/mvanhorn/cli-printing-press/commit/2767cf3ba0747140db9652bc6b1508e8c870c7e5))
* **cli:** allow adaptive limiter backoff to floor ([#2814](https://github.com/mvanhorn/cli-printing-press/issues/2814)) ([eac5598](https://github.com/mvanhorn/cli-printing-press/commit/eac5598e24cb6ea1c623b34ddbce8909ba2ce0ad))
* **cli:** dedupe multi-spec duplicate endpoint commands ([#2828](https://github.com/mvanhorn/cli-printing-press/issues/2828)) ([69a8327](https://github.com/mvanhorn/cli-printing-press/commit/69a83275029d200d0e3663602918eb27b5b8897d))
* **cli:** derive live dogfood fixtures from store ([#2826](https://github.com/mvanhorn/cli-printing-press/issues/2826)) ([b2f47e5](https://github.com/mvanhorn/cli-printing-press/commit/b2f47e54b5d280c332040600069f01903a0e51df))
* **cli:** fail dogfood on missing data-source strategy ([#2805](https://github.com/mvanhorn/cli-printing-press/issues/2805)) ([0f3d988](https://github.com/mvanhorn/cli-printing-press/commit/0f3d9887c9c10914ed083547631e36c83778bb17))
* **cli:** harden force regen merge ([#2812](https://github.com/mvanhorn/cli-printing-press/issues/2812)) ([cc3692f](https://github.com/mvanhorn/cli-printing-press/commit/cc3692ff27195ac37e90ef7f72228c27041e8b03))
* **cli:** make graphql latest-only choose newest page ([#2823](https://github.com/mvanhorn/cli-printing-press/issues/2823)) ([c3e044b](https://github.com/mvanhorn/cli-printing-press/commit/c3e044ba0bc3ddc1de75b27e0abd45398e13d667))
* **cli:** pace generated MCP clients ([#2809](https://github.com/mvanhorn/cli-printing-press/issues/2809)) ([477ed2f](https://github.com/mvanhorn/cli-printing-press/commit/477ed2f458232d938f775e163d6374570bb0b0fe))
* **cli:** reject multi-statement sql mcp queries ([#2811](https://github.com/mvanhorn/cli-printing-press/issues/2811)) ([c3d4e1e](https://github.com/mvanhorn/cli-printing-press/commit/c3d4e1e646c5b8d0e3737ca50f62ccebf714e355))
* **cli:** route shipcheck verify for HTML sync stubs ([#2825](https://github.com/mvanhorn/cli-printing-press/issues/2825)) ([59f3574](https://github.com/mvanhorn/cli-printing-press/commit/59f3574c5e500d4b4ac302c4b68c1e09548052d5))
* **cli:** scope global params from env ([#2827](https://github.com/mvanhorn/cli-printing-press/issues/2827)) ([95b099e](https://github.com/mvanhorn/cli-printing-press/commit/95b099e530c075fd2333309eec509a5c958e9692))
* **cli:** skip idless resources in default sync ([#2830](https://github.com/mvanhorn/cli-printing-press/issues/2830)) ([50054e1](https://github.com/mvanhorn/cli-printing-press/commit/50054e1ebb03f74aab70f51df28cdae53c4f8206))
* **cli:** skip required-query dependent sync ([#2831](https://github.com/mvanhorn/cli-printing-press/issues/2831)) ([4d28f0f](https://github.com/mvanhorn/cli-printing-press/commit/4d28f0ff1d62a7c0a6c28b1e73e74a2adc74a6aa))
* **cli:** stamp printed MCP versions in bundles ([#2817](https://github.com/mvanhorn/cli-printing-press/issues/2817)) ([e4bc665](https://github.com/mvanhorn/cli-printing-press/commit/e4bc6654dfc33ffc8a3ac5b540f68c3c6ee0b65e))
* **cli:** support local sqlite specs without base urls ([#2807](https://github.com/mvanhorn/cli-printing-press/issues/2807)) ([b36e7bc](https://github.com/mvanhorn/cli-printing-press/commit/b36e7bcca3a3223cfd4cec795075117ded79a467))
* **cli:** support next_token sync pagination ([#2818](https://github.com/mvanhorn/cli-printing-press/issues/2818)) ([d0a5d24](https://github.com/mvanhorn/cli-printing-press/commit/d0a5d243af188f32edba40b16d4c54cc1b8d951d))
* **cli:** synthesize html page sync ids ([#2835](https://github.com/mvanhorn/cli-printing-press/issues/2835)) ([88617bb](https://github.com/mvanhorn/cli-printing-press/commit/88617bb94d0271bc031af50b49935bda8cfe5fff))
* **skills:** add blocked API journal flow ([#2806](https://github.com/mvanhorn/cli-printing-press/issues/2806)) ([7d445b1](https://github.com/mvanhorn/cli-printing-press/commit/7d445b1f5bb95c5ec54486c7ab07760ffa59a632))
* **skills:** add sqlite missing-mirror guard guidance ([#2819](https://github.com/mvanhorn/cli-printing-press/issues/2819)) ([393347a](https://github.com/mvanhorn/cli-printing-press/commit/393347a2449a24dbc2f0aae71609df1a35b37a22))
* **skills:** rebuild stale repo preflight binary ([#2810](https://github.com/mvanhorn/cli-printing-press/issues/2810)) ([cfe9e8f](https://github.com/mvanhorn/cli-printing-press/commit/cfe9e8ffda9516181e7faaec48fadb2d8646cdb9))

## [4.23.0](https://github.com/mvanhorn/cli-printing-press/compare/v4.22.1...v4.23.0) (2026-06-07)


### Features

* **cli:** port self-teaching loop into generator templates ([#2440](https://github.com/mvanhorn/cli-printing-press/issues/2440)) ([eb7ef10](https://github.com/mvanhorn/cli-printing-press/commit/eb7ef10deee696b7a962873b7d2c93a5b8d6b67e))


### Bug Fixes

* **cli:** annotate read-only framework MCP commands ([#2700](https://github.com/mvanhorn/cli-printing-press/issues/2700)) ([1b7129b](https://github.com/mvanhorn/cli-printing-press/commit/1b7129b8b954d0d8e65a274e98833b58e4ab9393))
* **cli:** apply api key auth prefixes ([#2764](https://github.com/mvanhorn/cli-printing-press/issues/2764)) ([2e59ab3](https://github.com/mvanhorn/cli-printing-press/commit/2e59ab31d8dfbb3be2ccb48b717a71eae93838f9))
* **cli:** bound typed MCP endpoint responses ([#2771](https://github.com/mvanhorn/cli-printing-press/issues/2771)) ([a71b3c2](https://github.com/mvanhorn/cli-printing-press/commit/a71b3c2b4c95c26b4c2a4358ba3db1e6d680b940))
* **cli:** carry spec-declared query-param defaults into typed MCP bindings ([#2689](https://github.com/mvanhorn/cli-printing-press/issues/2689)) ([4a6d1fc](https://github.com/mvanhorn/cli-printing-press/commit/4a6d1fc0acc51239268b0858cf5b46fe2bc130f3))
* **cli:** continue offset pagination after full pages ([#2765](https://github.com/mvanhorn/cli-printing-press/issues/2765)) ([b994cde](https://github.com/mvanhorn/cli-printing-press/commit/b994cde6c0e710238a2e78e73c3c5cb0449299e5))
* **cli:** dedup intentEndpoints map + 0600/0700 cache perms + cobratree shell-arg quoting ([#2697](https://github.com/mvanhorn/cli-printing-press/issues/2697)) ([dede887](https://github.com/mvanhorn/cli-printing-press/commit/dede887e828be33acbfa24364b12ad3bcaaf314a))
* **cli:** format generated numeric params without exponents ([#2772](https://github.com/mvanhorn/cli-printing-press/issues/2772)) ([592f874](https://github.com/mvanhorn/cli-printing-press/commit/592f8744556482766d8824682cdd29306d40b8ac))
* **cli:** format MCP numeric params without exponent ([#2767](https://github.com/mvanhorn/cli-printing-press/issues/2767)) ([ad36c24](https://github.com/mvanhorn/cli-printing-press/commit/ad36c2468ec0ecaf5b06a946ebed32663aa5cb34))
* **cli:** gate novel-feature Help guard on positional + non-placeholder parent Short ([#2694](https://github.com/mvanhorn/cli-printing-press/issues/2694)) ([505998c](https://github.com/mvanhorn/cli-printing-press/commit/505998c1f05535285ba740857727d8ea07ab2adc))
* **cli:** gate sync search hint, dogfood-safe tail follow, 400 argument-missing warning ([#2702](https://github.com/mvanhorn/cli-printing-press/issues/2702)) ([68b47fd](https://github.com/mvanhorn/cli-printing-press/commit/68b47fd0fe0d7a6116ac6773bf46bcaf5e62f624))
* **cli:** guard regen against stale Printing Press binaries ([#2758](https://github.com/mvanhorn/cli-printing-press/issues/2758)) ([d0e9b32](https://github.com/mvanhorn/cli-printing-press/commit/d0e9b32abd601faf13f5b4530f6daea7879edb75))
* **cli:** honor explicit has_more:false in sync page-int fallback + add metadata envelope key ([#2696](https://github.com/mvanhorn/cli-printing-press/issues/2696)) ([b5f7fe3](https://github.com/mvanhorn/cli-printing-press/commit/b5f7fe33afd468c4715b853353a48d737ed562f8))
* **cli:** inject MCP server version via ldflag instead of hardcoded 1.0.0 ([#2699](https://github.com/mvanhorn/cli-printing-press/issues/2699)) ([d0ae8dc](https://github.com/mvanhorn/cli-printing-press/commit/d0ae8dc1b36e13c0f10e5943df2947369ea99270))
* **cli:** keep defaulted high-frequency query params in global filter ([#2678](https://github.com/mvanhorn/cli-printing-press/issues/2678)) ([a12e75c](https://github.com/mvanhorn/cli-printing-press/commit/a12e75ca8f734ce2d81079e79ec7218633f32e0e))
* **cli:** make BLE backend opt-in for default builds ([#2766](https://github.com/mvanhorn/cli-printing-press/issues/2766)) ([c5c8c93](https://github.com/mvanhorn/cli-printing-press/commit/c5c8c931d8cbdab61088de457e5a2e91c12384e0))
* **cli:** make generated store list zero limit unbounded ([#2762](https://github.com/mvanhorn/cli-printing-press/issues/2762)) ([76326a9](https://github.com/mvanhorn/cli-printing-press/commit/76326a9d366878bc71b0382c3da78f409c9eff0b))
* **cli:** pass --db to data-pipeline sql probes and WARN-skip the sync gate for pure-API CLIs ([#2691](https://github.com/mvanhorn/cli-printing-press/issues/2691)) ([a856a73](https://github.com/mvanhorn/cli-printing-press/commit/a856a7384263eb5e963c62980a7eaf549ccb6051))
* **cli:** platform-conditional --help validation timeout + accurate pipeline.Init doc ([#2695](https://github.com/mvanhorn/cli-printing-press/issues/2695)) ([3166d73](https://github.com/mvanhorn/cli-printing-press/commit/3166d73fecd4092b0cfc4c88d7b1a70b9f1ce0ae))
* **cli:** prefer Chrome profiles with auth cookies ([#2681](https://github.com/mvanhorn/cli-printing-press/issues/2681)) ([067e947](https://github.com/mvanhorn/cli-printing-press/commit/067e9476b165083f681bc7f51bd3aa2dcb826ed1))
* **cli:** prefer header api keys over oauth alternatives ([#2756](https://github.com/mvanhorn/cli-printing-press/issues/2756)) ([4055e24](https://github.com/mvanhorn/cli-printing-press/commit/4055e24d4af74501bb60bbf39e02ce9d242a8248))
* **cli:** preserve dependent child-parent store rows ([#2761](https://github.com/mvanhorn/cli-printing-press/issues/2761)) ([28674a5](https://github.com/mvanhorn/cli-printing-press/commit/28674a5e80a0a57dd01579e80c29efd72c8e174a))
* **cli:** preserve manifest spec name in transient mcp-sync dirs ([#2698](https://github.com/mvanhorn/cli-printing-press/issues/2698)) ([c36f8b4](https://github.com/mvanhorn/cli-printing-press/commit/c36f8b46eb2b78039d77b90751aa73057054c6eb))
* **cli:** preserve nested compact list payloads ([#2752](https://github.com/mvanhorn/cli-printing-press/issues/2752)) ([5478daa](https://github.com/mvanhorn/cli-printing-press/commit/5478daa05ccc9a0fe076367059117ea9fe6a7b72))
* **cli:** recognize Google "Login Required" 401 envelope and rebuild staged binary in dogfood --live ([#2690](https://github.com/mvanhorn/cli-printing-press/issues/2690)) ([991c35d](https://github.com/mvanhorn/cli-printing-press/commit/991c35d6136b35708a375a97a7e98a24677dfae8))
* **cli:** redact token= credentials + accurate SQL tool schema description ([#2701](https://github.com/mvanhorn/cli-printing-press/issues/2701)) ([aee10fa](https://github.com/mvanhorn/cli-printing-press/commit/aee10fac925535243a4ecd8af2ac67cf5d8c2f5d))
* **cli:** reject missing analytics group-by fields ([#2751](https://github.com/mvanhorn/cli-printing-press/issues/2751)) ([bfe4759](https://github.com/mvanhorn/cli-printing-press/commit/bfe4759f82ddc534c7364ff1662d548462e250c8))
* **cli:** sanitize generated local search ([#2770](https://github.com/mvanhorn/cli-printing-press/issues/2770)) ([5c58694](https://github.com/mvanhorn/cli-printing-press/commit/5c5869413a2369ebc3d0e11f24d28e221fb75edc))
* **cli:** scorer multi-spec path_validity + docsync novel-feature Go-surface resync and drop-warning ([#2693](https://github.com/mvanhorn/cli-printing-press/issues/2693)) ([298c46f](https://github.com/mvanhorn/cli-printing-press/commit/298c46fdba1b5b24d68041074638da8c4aa58894))
* **cli:** skip colliding novel command stubs ([#2773](https://github.com/mvanhorn/cli-printing-press/issues/2773)) ([5931c78](https://github.com/mvanhorn/cli-printing-press/commit/5931c781db17e186b4376ae6bd86afda1509f452))
* **cli:** strip *.test on publish, dedup -pp slug, crowd-sniff drop summary ([#2703](https://github.com/mvanhorn/cli-printing-press/issues/2703)) ([2051f56](https://github.com/mvanhorn/cli-printing-press/commit/2051f5681f46954979c797e6c4769247bcc350a1))
* **cli:** support standalone pycookiecheat ([#2725](https://github.com/mvanhorn/cli-printing-press/issues/2725)) ([3d70c16](https://github.com/mvanhorn/cli-printing-press/commit/3d70c169ef8c240257ca1f925489d4e86e05769e))
* **cli:** verify-skill detects alias-receiver flags and fixes positional/flag-value tokenization ([#2692](https://github.com/mvanhorn/cli-printing-press/issues/2692)) ([6a98b16](https://github.com/mvanhorn/cli-printing-press/commit/6a98b163541cce72d41d24387602034a402ce9e5))
* **generator:** don't persist env-sourced credentials into config.toml ([#2710](https://github.com/mvanhorn/cli-printing-press/issues/2710)) ([#2720](https://github.com/mvanhorn/cli-printing-press/issues/2720)) ([b63fb35](https://github.com/mvanhorn/cli-printing-press/commit/b63fb3555650451e5e659b29f8c91f915e04c1a4))
* **generator:** emit toolchain floor and pin validate govulncheck to module toolchain ([#2709](https://github.com/mvanhorn/cli-printing-press/issues/2709)) ([7f6382d](https://github.com/mvanhorn/cli-printing-press/commit/7f6382d4049f389954d5681b8eb1c50eb8324275))
* **generator:** JSON-safe GraphQL sync_warning/sync_error events ([#2675](https://github.com/mvanhorn/cli-printing-press/issues/2675)) ([#2715](https://github.com/mvanhorn/cli-printing-press/issues/2715)) ([ffeb210](https://github.com/mvanhorn/cli-printing-press/commit/ffeb210dda7ea2ec60ede9e54af241bac4e9b90a))
* **generator:** make store list zero limit unlimited ([#2757](https://github.com/mvanhorn/cli-printing-press/issues/2757)) ([276c76c](https://github.com/mvanhorn/cli-printing-press/commit/276c76c9ee8ac7c6de42a983d1928b6f61fd2196))
* **generator:** prevent sync data-loss defaults ([#2760](https://github.com/mvanhorn/cli-printing-press/issues/2760)) ([4ba4967](https://github.com/mvanhorn/cli-printing-press/commit/4ba496719efde7a8ba24768a00a77a824a729c37))
* **generator:** quote local FTS search terms ([#2755](https://github.com/mvanhorn/cli-printing-press/issues/2755)) ([b36d1aa](https://github.com/mvanhorn/cli-printing-press/commit/b36d1aa633ea719bca69d00ef851e5682eefca77))
* **generator:** resolve three silent sync data-loss paths ([#2327](https://github.com/mvanhorn/cli-printing-press/issues/2327), [#2569](https://github.com/mvanhorn/cli-printing-press/issues/2569), [#2621](https://github.com/mvanhorn/cli-printing-press/issues/2621)) ([#2713](https://github.com/mvanhorn/cli-printing-press/issues/2713)) ([adc9f51](https://github.com/mvanhorn/cli-printing-press/commit/adc9f51f147f6a5926bf52072526a7ce87ed1f02))
* **generator:** rewrite bare-root self-imports on publish --module-path ([#2578](https://github.com/mvanhorn/cli-printing-press/issues/2578)) ([#2721](https://github.com/mvanhorn/cli-printing-press/issues/2721)) ([a85d722](https://github.com/mvanhorn/cli-printing-press/commit/a85d722ab65c03ffafb3ff3ba657b112ba7f1387))
* **generator:** surface row + query errors in store ResolveByName ([#2708](https://github.com/mvanhorn/cli-printing-press/issues/2708)) ([#2716](https://github.com/mvanhorn/cli-printing-press/issues/2716)) ([1539622](https://github.com/mvanhorn/cli-printing-press/commit/1539622844da493089560d72af610d2a5c3253b3))
* **generator:** treat null resource array with zero count as an empty sync page ([#2656](https://github.com/mvanhorn/cli-printing-press/issues/2656)) ([#2719](https://github.com/mvanhorn/cli-printing-press/issues/2719)) ([826432a](https://github.com/mvanhorn/cli-printing-press/commit/826432a2d2cb5021f9608874023346e408c13455))
* **scorer:** SKIP data-pipeline gate for no-sync and local-datastore CLIs ([#2622](https://github.com/mvanhorn/cli-printing-press/issues/2622), [#2672](https://github.com/mvanhorn/cli-printing-press/issues/2672)) ([#2717](https://github.com/mvanhorn/cli-printing-press/issues/2717)) ([748da72](https://github.com/mvanhorn/cli-printing-press/commit/748da7244fd5f716f62c3677971ca74a505d3ebc))
* **scorer:** strip shipcheck report JSON on publish package ([#2658](https://github.com/mvanhorn/cli-printing-press/issues/2658)) ([#2718](https://github.com/mvanhorn/cli-printing-press/issues/2718)) ([a5e1929](https://github.com/mvanhorn/cli-printing-press/commit/a5e19298897babf407069fca4673e565888d2899))
* **skill:** align novel-command skeleton with generator's newNovel&lt;X&gt;Cmd scaffold ([#2704](https://github.com/mvanhorn/cli-printing-press/issues/2704)) ([53a59ee](https://github.com/mvanhorn/cli-printing-press/commit/53a59ee63523efeddfff10d6951cbd7c9c5f8f6b))
* **skills:** make run ids collision-proof ([#2759](https://github.com/mvanhorn/cli-printing-press/issues/2759)) ([c262d56](https://github.com/mvanhorn/cli-printing-press/commit/c262d56a08de1460d0727922309ccd63c5cc6d55))

## [4.22.1](https://github.com/mvanhorn/cli-printing-press/compare/v4.22.0...v4.22.1) (2026-06-05)


### Bug Fixes

* **ci:** raise Mergify max_parallel_checks to 5 and exclude draft PRs ([#2663](https://github.com/mvanhorn/cli-printing-press/issues/2663)) ([842f612](https://github.com/mvanhorn/cli-printing-press/commit/842f6126adf2c07460a7e786a0ff834b19cfbb40))

## [4.22.0](https://github.com/mvanhorn/cli-printing-press/compare/v4.21.0...v4.22.0) (2026-06-05)


### Features

* **catalog:** add shipping tracker blueprint ([#2649](https://github.com/mvanhorn/cli-printing-press/issues/2649)) ([4bfd960](https://github.com/mvanhorn/cli-printing-press/commit/4bfd960b73f96d0ec4c4935ab6059b0426427a47))


### Bug Fixes

* **cli:** surface row errors in generated MCP sql tool ([#2650](https://github.com/mvanhorn/cli-printing-press/issues/2650)) ([2af370c](https://github.com/mvanhorn/cli-printing-press/commit/2af370c7525c45be9bff818dc018988a387b9b8b))
* **generator:** rely on installer default bin directory ([#2654](https://github.com/mvanhorn/cli-printing-press/issues/2654)) ([0a9969c](https://github.com/mvanhorn/cli-printing-press/commit/0a9969cc3c32313a04c6c27cebb36b79b8a0b42a))

## [4.21.0](https://github.com/mvanhorn/cli-printing-press/compare/v4.20.1...v4.21.0) (2026-06-05)


### Features

* **catalog:** add Plane project-management catalog entry ([#2598](https://github.com/mvanhorn/cli-printing-press/issues/2598)) ([91245f6](https://github.com/mvanhorn/cli-printing-press/commit/91245f67e1cde1c0f319e8d552f4b2fc81524216))
* **cli:** add BLE device-sniff and device-spec CLI generation ([#2601](https://github.com/mvanhorn/cli-printing-press/issues/2601)) ([4e0e8f8](https://github.com/mvanhorn/cli-printing-press/commit/4e0e8f845aaec9031cb3543aeec8190185d8aa96))


### Bug Fixes

* **ci:** bump Go to 1.26.4 to clear GO-2026-5037 / GO-2026-5039 stdlib advisories ([#2612](https://github.com/mvanhorn/cli-printing-press/issues/2612)) ([a4fcff7](https://github.com/mvanhorn/cli-printing-press/commit/a4fcff797bb914fea6e9fd3d8608f0bc661b28f5))
* **cli:** bump emitted go directive to 1.26.4 ([#2627](https://github.com/mvanhorn/cli-printing-press/issues/2627)) ([a21d796](https://github.com/mvanhorn/cli-printing-press/commit/a21d7969f2decf0efe47cd54cd6099087fe3d56f))
* **cli:** emit GraphQL-aware import for GraphQL specs, not REST POST ([#2618](https://github.com/mvanhorn/cli-printing-press/issues/2618)) ([7ba6fb0](https://github.com/mvanhorn/cli-printing-press/commit/7ba6fb0af5abd25f445c22b1da5f56476c3c029f))
* **cli:** emit valid JSON for sync_warning events ([#2643](https://github.com/mvanhorn/cli-printing-press/issues/2643)) ([dec1fc9](https://github.com/mvanhorn/cli-printing-press/commit/dec1fc9336b0b8112381bb1339fcc9f41c663836))
* **cli:** live dogfood happy-path honors pp:happy-args ([#2642](https://github.com/mvanhorn/cli-printing-press/issues/2642)) ([d2ca5a2](https://github.com/mvanhorn/cli-printing-press/commit/d2ca5a2cb3f31423658314340149d0cb05f2c86a))
* **cli:** preserve creator attribution on reprint ([#2634](https://github.com/mvanhorn/cli-printing-press/issues/2634)) ([4e261dc](https://github.com/mvanhorn/cli-printing-press/commit/4e261dca6d6e40a447a0a1685de5bc817210676a))
* **cli:** preserve live dogfood refresh credentials ([#2638](https://github.com/mvanhorn/cli-printing-press/issues/2638)) ([29aba15](https://github.com/mvanhorn/cli-printing-press/commit/29aba15f6bc78f1a38b1a159f5b83fa7b34313d7))

## [4.20.1](https://github.com/mvanhorn/cli-printing-press/compare/v4.20.0...v4.20.1) (2026-06-01)


### Bug Fixes

* **patches:** emit per-patch directory layout (generator + skills) ([#2581](https://github.com/mvanhorn/cli-printing-press/issues/2581)) ([a826d31](https://github.com/mvanhorn/cli-printing-press/commit/a826d31617d635c8fd7717e9edf0ebf213797af0))

## [4.20.0](https://github.com/mvanhorn/cli-printing-press/compare/v4.19.1...v4.20.0) (2026-06-01)


### Features

* **catalog:** add tidycal ([#2565](https://github.com/mvanhorn/cli-printing-press/issues/2565)) ([683d849](https://github.com/mvanhorn/cli-printing-press/commit/683d849c430ae25cd632873e33e88be1cf0a4738))
* **skills:** add currency-version floor that hard-blocks generation on stale binaries ([#2567](https://github.com/mvanhorn/cli-printing-press/issues/2567)) ([ef0ec66](https://github.com/mvanhorn/cli-printing-press/commit/ef0ec661c1d01618d61c39de72c1c33eb5e56083))


### Bug Fixes

* **cli:** exclude scalar-array and sampler endpoints from sync selection ([#2549](https://github.com/mvanhorn/cli-printing-press/issues/2549)) ([54ecd9e](https://github.com/mvanhorn/cli-printing-press/commit/54ecd9e5205012991335fd6096e8f29d2803f465))

## [4.19.1](https://github.com/mvanhorn/cli-printing-press/compare/v4.19.0...v4.19.1) (2026-05-31)


### Bug Fixes

* **cli:** always emit RequestBaseURL() so novel commands keep BasePath ([#2520](https://github.com/mvanhorn/cli-printing-press/issues/2520)) ([6db6972](https://github.com/mvanhorn/cli-printing-press/commit/6db6972089b35c24e7071c7c5ed7c640c4b528a7))
* **cli:** ASCII-fold in CamelIdentifier so non-ASCII names don't corrupt identifiers ([#2555](https://github.com/mvanhorn/cli-printing-press/issues/2555)) ([0c18120](https://github.com/mvanhorn/cli-printing-press/commit/0c1812055a5e674ffa495485f0b44eba502947ef))
* **cli:** bound remote spec fetch with a timeout and size cap ([#2558](https://github.com/mvanhorn/cli-printing-press/issues/2558)) ([443d9ae](https://github.com/mvanhorn/cli-printing-press/commit/443d9aed6d6b66a5ec0848fe9784b5337062c0ea))
* **cli:** filter raw HARs from publishable manuscripts ([#2525](https://github.com/mvanhorn/cli-printing-press/issues/2525)) ([a5f6e70](https://github.com/mvanhorn/cli-printing-press/commit/a5f6e70aff26e583fa23b8e2871356352c808eac))
* **cli:** ignore scalar siblings in sync envelope fallback ([#2532](https://github.com/mvanhorn/cli-printing-press/issues/2532)) ([2f9fb81](https://github.com/mvanhorn/cli-printing-press/commit/2f9fb81b70a69d77d6778fda4e9c5290b535ec0f))
* **cli:** normalize publish package metadata ([#2523](https://github.com/mvanhorn/cli-printing-press/issues/2523)) ([79c7c82](https://github.com/mvanhorn/cli-printing-press/commit/79c7c82d249b16487c2742a1937787949573f298))
* **cli:** parse comment-led helper call files ([#2524](https://github.com/mvanhorn/cli-printing-press/issues/2524)) ([0cfc64b](https://github.com/mvanhorn/cli-printing-press/commit/0cfc64bc11dbfae6cf70e929e18ce231addfd1e3))
* **cli:** pass context to async job no-cache polling ([#2456](https://github.com/mvanhorn/cli-printing-press/issues/2456)) ([1872817](https://github.com/mvanhorn/cli-printing-press/commit/1872817ad58713d24cd86921892a4b6d08e50995))
* **cli:** preserve module imports on force regen ([#2531](https://github.com/mvanhorn/cli-printing-press/issues/2531)) ([e3801c7](https://github.com/mvanhorn/cli-printing-press/commit/e3801c7b489936975d15f9e00a9306f2b467fdb1))
* **cli:** preserve sync cursor on max-pages cap ([#2543](https://github.com/mvanhorn/cli-printing-press/issues/2543)) ([f7037d8](https://github.com/mvanhorn/cli-printing-press/commit/f7037d8a4bef4117b72e29f07d1f4624978f58de))
* **cli:** preserve typed tables for paginated union resources ([#2540](https://github.com/mvanhorn/cli-printing-press/issues/2540)) ([5b0efb8](https://github.com/mvanhorn/cli-printing-press/commit/5b0efb87bce9f01dc91eb54d123ed0d8362238a9))
* **cli:** re-inject lost registrations into their source function ([#2559](https://github.com/mvanhorn/cli-printing-press/issues/2559)) ([288b4a5](https://github.com/mvanhorn/cli-printing-press/commit/288b4a5d584ef93fcb35e3e19adf5590486c68be))
* **cli:** rebuild FTS after rowid migration ([#2542](https://github.com/mvanhorn/cli-printing-press/issues/2542)) ([79c8acf](https://github.com/mvanhorn/cli-printing-press/commit/79c8acf3661a9bee030adfa46fe10c5e27812e52))
* **cli:** require explicit global scope flags ([#2530](https://github.com/mvanhorn/cli-printing-press/issues/2530)) ([6f5518f](https://github.com/mvanhorn/cli-printing-press/commit/6f5518f963fbdda977b524e1c53d42a1b577dfa9))
* **cli:** resolve GraphQL custom root operation types ([#2556](https://github.com/mvanhorn/cli-printing-press/issues/2556)) ([b0b4147](https://github.com/mvanhorn/cli-printing-press/commit/b0b41473ed9c4b27a61ee550d4e04ee6cf653d3f))
* **cli:** skip unsynced local live-check samples ([#2533](https://github.com/mvanhorn/cli-printing-press/issues/2533)) ([50c4504](https://github.com/mvanhorn/cli-printing-press/commit/50c450472491456117ddd89b72b6c5f5865ffbdf))
* **cli:** stop gating promote on ASIN examples ([#2522](https://github.com/mvanhorn/cli-printing-press/issues/2522)) ([6bfd1be](https://github.com/mvanhorn/cli-printing-press/commit/6bfd1be3f2f770a0cd3c13489cf098e4f4677c0d))
* **cli:** trim client credential env vars for auth login ([#2537](https://github.com/mvanhorn/cli-printing-press/issues/2537)) ([2742ef7](https://github.com/mvanhorn/cli-printing-press/commit/2742ef7aa36d911c5ec53847ebd29f0fcb53eeb0))
* **skills:** route rebuilt-novel reprints through swap ([#2527](https://github.com/mvanhorn/cli-printing-press/issues/2527)) ([c7ff7f8](https://github.com/mvanhorn/cli-printing-press/commit/c7ff7f80e79b807dcdd58e50aede3cc7563ebfd6))
* **skills:** update polish gosec fallback pin ([#2529](https://github.com/mvanhorn/cli-printing-press/issues/2529)) ([9548233](https://github.com/mvanhorn/cli-printing-press/commit/95482338b05ec15a04a01840782075966b11c9dd))

## [4.19.0](https://github.com/mvanhorn/cli-printing-press/compare/v4.18.1...v4.19.0) (2026-05-28)


### Features

* **cli:** creator + contributors attribution model ([#2432](https://github.com/mvanhorn/cli-printing-press/issues/2432)) ([d7bf1bf](https://github.com/mvanhorn/cli-printing-press/commit/d7bf1bf2c0df9d497a7c19d0ae6ba34fa25dd5db))


### Bug Fixes

* **cli:** drop journal_mode from read-only store DSN ([#2407](https://github.com/mvanhorn/cli-printing-press/issues/2407)) ([4bf1659](https://github.com/mvanhorn/cli-printing-press/commit/4bf16593ef55fb5e11598d9558dbd9b770d91f3e))
* **cli:** guard empty PROXY_ARGS expansion under bash 3.2 ([#2439](https://github.com/mvanhorn/cli-printing-press/issues/2439)) ([ad28280](https://github.com/mvanhorn/cli-printing-press/commit/ad282805a9523a8f52e2209cec600aa9b03caad9)), closes [#2438](https://github.com/mvanhorn/cli-printing-press/issues/2438)
* **cli:** pin golang.org/x/net to a patched version after generation ([#2410](https://github.com/mvanhorn/cli-printing-press/issues/2410)) ([470bd19](https://github.com/mvanhorn/cli-printing-press/commit/470bd19d417f8276b0396e334d52dd38576e1285))

## [4.18.1](https://github.com/mvanhorn/cli-printing-press/compare/v4.18.0...v4.18.1) (2026-05-27)


### Bug Fixes

* **cli:** apply store SQLite pragmas via modernc _pragma= DSN syntax ([#2399](https://github.com/mvanhorn/cli-printing-press/issues/2399)) ([d8b6b5f](https://github.com/mvanhorn/cli-printing-press/commit/d8b6b5f758bc979f1a361b9dd7121dcf4ae0d22a))
* **cli:** gofmt Go source after publish-time module-path rewrite ([#2405](https://github.com/mvanhorn/cli-printing-press/issues/2405)) ([6183f1b](https://github.com/mvanhorn/cli-printing-press/commit/6183f1b271be92d1dca92acd996ad383d8bfb8c6))

## [4.18.0](https://github.com/mvanhorn/cli-printing-press/compare/v4.17.0...v4.18.0) (2026-05-26)


### Features

* **cli:** add oauth2_refresh auth type ([#2355](https://github.com/mvanhorn/cli-printing-press/issues/2355)) ([29b9f88](https://github.com/mvanhorn/cli-printing-press/commit/29b9f8860b17a11c605727d8bfca8b50c2e595f1))


### Bug Fixes

* **cli:** auto-apply large MCP orchestration default ([#2365](https://github.com/mvanhorn/cli-printing-press/issues/2365)) ([0c7a263](https://github.com/mvanhorn/cli-printing-press/commit/0c7a26351fc125be92537618fe4f15d0b4906db3))
* **cli:** detect turnstile reachability interstitials ([#2341](https://github.com/mvanhorn/cli-printing-press/issues/2341)) ([0f8083f](https://github.com/mvanhorn/cli-printing-press/commit/0f8083fec3a8f5fd685ed5c024523286b522f815))
* **cli:** emit CSV parse helpers for response specs ([#2358](https://github.com/mvanhorn/cli-printing-press/issues/2358)) ([0ba4bf5](https://github.com/mvanhorn/cli-printing-press/commit/0ba4bf5b922f5a6a65ccf2663739df764e72bb2d))
* **cli:** emit happy args annotations ([#2359](https://github.com/mvanhorn/cli-printing-press/issues/2359)) ([7074a42](https://github.com/mvanhorn/cli-printing-press/commit/7074a4209ce298bb83c194c15433180e88de3986))
* **cli:** guard template prose interpolation ([#2351](https://github.com/mvanhorn/cli-printing-press/issues/2351)) ([1419384](https://github.com/mvanhorn/cli-printing-press/commit/14193843ec2e480ccb8f5a9643931f6222f9f745))
* **cli:** handle reserved search resources ([#2360](https://github.com/mvanhorn/cli-printing-press/issues/2360)) ([ef0cd96](https://github.com/mvanhorn/cli-printing-press/commit/ef0cd963e03425ca1a664d77b3282d9709badc7f))
* **cli:** honor docs batch filter flags client-side ([#2352](https://github.com/mvanhorn/cli-printing-press/issues/2352)) ([6182abf](https://github.com/mvanhorn/cli-printing-press/commit/6182abfeb4283abe3c7ccd962b7d3aa7c420a25c))
* **cli:** keep sole query input param from global-param filter ([#2241](https://github.com/mvanhorn/cli-printing-press/issues/2241)) ([518b3eb](https://github.com/mvanhorn/cli-printing-press/commit/518b3ebd982185fafa085370c2732fd793f288ee)), closes [#982](https://github.com/mvanhorn/cli-printing-press/issues/982)
* **cli:** parse multiline GraphQL fields for promoted resources ([#2367](https://github.com/mvanhorn/cli-printing-press/issues/2367)) ([26edea9](https://github.com/mvanhorn/cli-printing-press/commit/26edea9434622383f44a83e74a12bc794f361231))
* **cli:** preserve dispatch param defaults in examples ([#2346](https://github.com/mvanhorn/cli-printing-press/issues/2346)) ([623200b](https://github.com/mvanhorn/cli-printing-press/commit/623200b5df3df644df8c74cbb60837c14d9d477c))
* **cli:** preserve numeric sync resource ids ([#2350](https://github.com/mvanhorn/cli-printing-press/issues/2350)) ([8095b32](https://github.com/mvanhorn/cli-printing-press/commit/8095b322bbabe6b74827ce937aead1825efdeeac))
* **cli:** promote global path template vars ([#2366](https://github.com/mvanhorn/cli-printing-press/issues/2366)) ([3824012](https://github.com/mvanhorn/cli-printing-press/commit/3824012af97ca8abcaadc30deec3fe141fa9dcbb))
* **cli:** promote global scope query params ([#2356](https://github.com/mvanhorn/cli-printing-press/issues/2356)) ([1ee18ed](https://github.com/mvanhorn/cli-printing-press/commit/1ee18ed64fe769087ee853b5ea781dd8d82de2c1))
* **cli:** respect live dogfood auth tiers ([#2364](https://github.com/mvanhorn/cli-printing-press/issues/2364)) ([d9d0157](https://github.com/mvanhorn/cli-printing-press/commit/d9d0157677638efa33cf11777d888965ba2e713e))
* **cli:** skip unfixturable mutating dogfood examples ([#2357](https://github.com/mvanhorn/cli-printing-press/issues/2357)) ([e49f5f1](https://github.com/mvanhorn/cli-printing-press/commit/e49f5f1f55c04d765d3f5de284c398b41dbb20ad))
* **skills:** escalate 200 challenge shells ([#2340](https://github.com/mvanhorn/cli-printing-press/issues/2340)) ([f2eaf74](https://github.com/mvanhorn/cli-printing-press/commit/f2eaf749ac5b05d96bc55fba35e522e3ac1a590e))

## [4.17.0](https://github.com/mvanhorn/cli-printing-press/compare/v4.16.0...v4.17.0) (2026-05-26)


### Features

* **catalog:** add region language metadata ([#2311](https://github.com/mvanhorn/cli-printing-press/issues/2311)) ([205c0ac](https://github.com/mvanhorn/cli-printing-press/commit/205c0ace11f02adf834389bcd238767479351f95))
* **cli:** add MCP generate overrides ([#2245](https://github.com/mvanhorn/cli-printing-press/issues/2245)) ([d522d5f](https://github.com/mvanhorn/cli-printing-press/commit/d522d5fa7ae807351f8732ac87349f0eda248e00))
* **cli:** add per-endpoint role gates ([#2314](https://github.com/mvanhorn/cli-printing-press/issues/2314)) ([a255eb1](https://github.com/mvanhorn/cli-printing-press/commit/a255eb1a9f92199c8353731042aaba9ab2093365))
* **cli:** add WebSocket streaming sync scaffold ([#2244](https://github.com/mvanhorn/cli-printing-press/issues/2244)) ([f20cbad](https://github.com/mvanhorn/cli-printing-press/commit/f20cbad064a4727f1cd5f81fbc267dc7f637cc97))
* **cli:** emit novel feature command stubs ([#2302](https://github.com/mvanhorn/cli-printing-press/issues/2302)) ([2bb2c4a](https://github.com/mvanhorn/cli-printing-press/commit/2bb2c4a122a1643575385ddf1a6622b091c0b4cb))
* **skills:** add raw docs fetch helper ([#2295](https://github.com/mvanhorn/cli-printing-press/issues/2295)) ([f67ad83](https://github.com/mvanhorn/cli-printing-press/commit/f67ad83f6f0933224b970cd17656bb73a53b0b6a))


### Bug Fixes

* **ci:** add generated output compile gate ([#2319](https://github.com/mvanhorn/cli-printing-press/issues/2319)) ([fc04a3e](https://github.com/mvanhorn/cli-printing-press/commit/fc04a3e6f23460ff20ea273de995c2088e38b1e5))
* **cli:** accept accounted phase5 skips in gate ([#2232](https://github.com/mvanhorn/cli-printing-press/issues/2232)) ([1771a27](https://github.com/mvanhorn/cli-printing-press/commit/1771a27f922ce4cf138023421cd7e87f6443b550))
* **cli:** accept LAN-only phase5 skips ([#2264](https://github.com/mvanhorn/cli-printing-press/issues/2264)) ([d55da88](https://github.com/mvanhorn/cli-printing-press/commit/d55da88a4d66328fe98cfad51de1887809bbe752))
* **cli:** accept Windows staged live-check binaries ([#2250](https://github.com/mvanhorn/cli-printing-press/issues/2250)) ([9783acd](https://github.com/mvanhorn/cli-printing-press/commit/9783acd5d226c0b446d71427923b6bf88f69bde0))
* **cli:** apply research canonical auth env vars ([#2298](https://github.com/mvanhorn/cli-printing-press/issues/2298)) ([6ee70c9](https://github.com/mvanhorn/cli-printing-press/commit/6ee70c9f8f9868b60ff564c006629d8499d0fc1f))
* **cli:** backfill publish printer attribution ([#2233](https://github.com/mvanhorn/cli-printing-press/issues/2233)) ([dc19c7c](https://github.com/mvanhorn/cli-printing-press/commit/dc19c7c7ad8f5cddbde2dff81b686f27d2b087d6))
* **cli:** derive header sibling auth env vars ([#2254](https://github.com/mvanhorn/cli-printing-press/issues/2254)) ([5488950](https://github.com/mvanhorn/cli-printing-press/commit/5488950412448109d5eaca466cd1cc42ccc4ac4e))
* **cli:** detect unsafe chrome impersonation ([#2253](https://github.com/mvanhorn/cli-printing-press/issues/2253)) ([91cd511](https://github.com/mvanhorn/cli-printing-press/commit/91cd5118d6fa36527832abf2c78ada537fcceb94))
* **cli:** emit extractResponseData helper only for envelope-shaped responses ([#2257](https://github.com/mvanhorn/cli-printing-press/issues/2257)) ([591d9e7](https://github.com/mvanhorn/cli-printing-press/commit/591d9e7f8656d603f37c761f5da66290561c1a20)), closes [#1345](https://github.com/mvanhorn/cli-printing-press/issues/1345)
* **cli:** make agentcookie soft + stop publishing plans ([#2333](https://github.com/mvanhorn/cli-printing-press/issues/2333)) ([d0b7578](https://github.com/mvanhorn/cli-printing-press/commit/d0b757839900829c426c8dfaf242e50d1c8d68ce))
* **cli:** narrow pii-audit fixture scope ([#2246](https://github.com/mvanhorn/cli-printing-press/issues/2246)) ([b94ec3b](https://github.com/mvanhorn/cli-printing-press/commit/b94ec3bc8f20bf5c1c1ce993d139205d8533aeff))
* **cli:** print help on empty invocation of required-input commands ([#2243](https://github.com/mvanhorn/cli-printing-press/issues/2243)) ([0a469f3](https://github.com/mvanhorn/cli-printing-press/commit/0a469f303d9d9414f3b9d0b1a4ec1cf8c27e6e89)), closes [#1289](https://github.com/mvanhorn/cli-printing-press/issues/1289)
* **cli:** recognize slice/map/typed pflag variants in verify-skill flag scanner ([#2222](https://github.com/mvanhorn/cli-printing-press/issues/2222)) ([bf6a610](https://github.com/mvanhorn/cli-printing-press/commit/bf6a610aa511ff6f671898b7423561c88f4e6d61)), closes [#1255](https://github.com/mvanhorn/cli-printing-press/issues/1255)
* **cli:** reject invalid enum flag values with the valid set ([#2255](https://github.com/mvanhorn/cli-printing-press/issues/2255)) ([4f8a53c](https://github.com/mvanhorn/cli-printing-press/commit/4f8a53c12afd6f7192626fb65f29e2a08cbc0d58)), closes [#1139](https://github.com/mvanhorn/cli-printing-press/issues/1139)
* **cli:** render skill anti-triggers from research ([#2306](https://github.com/mvanhorn/cli-printing-press/issues/2306)) ([72ffcd7](https://github.com/mvanhorn/cli-printing-press/commit/72ffcd709f82c8c6c60fef6cc2cb7e2262001152))
* **cli:** run publish govulncheck with module toolchain ([#2238](https://github.com/mvanhorn/cli-printing-press/issues/2238)) ([ee5e1be](https://github.com/mvanhorn/cli-printing-press/commit/ee5e1beca0b316e17caa50f023c6dbcc098e5a02))
* **cli:** send in:cookie security credential as cookie not header ([#2239](https://github.com/mvanhorn/cli-printing-press/issues/2239)) ([b0b19ce](https://github.com/mvanhorn/cli-printing-press/commit/b0b19ced91336c54e062101a23b4a00104bef0ee)), closes [#981](https://github.com/mvanhorn/cli-printing-press/issues/981)
* **cli:** shape-aware auth-status env var placeholders ([#2242](https://github.com/mvanhorn/cli-printing-press/issues/2242)) ([993a409](https://github.com/mvanhorn/cli-printing-press/commit/993a4096bf1f7b4fc5acd483fb50b08a4e47e563)), closes [#1329](https://github.com/mvanhorn/cli-printing-press/issues/1329)
* **cli:** short-circuit oauth2 auth login under verify env ([#2223](https://github.com/mvanhorn/cli-printing-press/issues/2223)) ([0b6279a](https://github.com/mvanhorn/cli-printing-press/commit/0b6279a4b93e107bf1ffdac5f26c629f9a7e4c79)), closes [#1059](https://github.com/mvanhorn/cli-printing-press/issues/1059)
* **cli:** skip auth-flow inputs as bearer tokens ([#2247](https://github.com/mvanhorn/cli-printing-press/issues/2247)) ([bfd4428](https://github.com/mvanhorn/cli-printing-press/commit/bfd44284484de781351c095519a0332419031704))
* **cli:** skip promotion when body exceeds the flag-depth cap ([#2258](https://github.com/mvanhorn/cli-printing-press/issues/2258)) ([061aab8](https://github.com/mvanhorn/cli-printing-press/commit/061aab8cd00fd43f213bf67579149aa1c4ff978b)), closes [#1520](https://github.com/mvanhorn/cli-printing-press/issues/1520)
* **cli:** strip =value suffix from --flag=value tokens in verify-skill recipe parser ([#2256](https://github.com/mvanhorn/cli-printing-press/issues/2256)) ([72137f9](https://github.com/mvanhorn/cli-printing-press/commit/72137f9db86994f6ffd3024618b7b9d24ae1da7c)), closes [#1316](https://github.com/mvanhorn/cli-printing-press/issues/1316)
* **cli:** sync narrative value props during dogfood ([#2292](https://github.com/mvanhorn/cli-printing-press/issues/2292)) ([1149c6c](https://github.com/mvanhorn/cli-printing-press/commit/1149c6caa4af5041e5a0617de94bf82d3d520f16))
* **cli:** synthesize Google Discovery API key auth ([#2301](https://github.com/mvanhorn/cli-printing-press/issues/2301)) ([0104716](https://github.com/mvanhorn/cli-printing-press/commit/0104716a665a8684fd772c064dce5b2a0116ed3a))
* **cli:** synthesize resource-aware MCP descriptions for summary-less operations ([#2267](https://github.com/mvanhorn/cli-printing-press/issues/2267)) ([474dbf4](https://github.com/mvanhorn/cli-printing-press/commit/474dbf42538e74ab30acd86691e5d5ed2bae2ef1)), closes [#1315](https://github.com/mvanhorn/cli-printing-press/issues/1315)
* **skills:** add gosec gate to polish diagnostics ([#2300](https://github.com/mvanhorn/cli-printing-press/issues/2300)) ([cc645eb](https://github.com/mvanhorn/cli-printing-press/commit/cc645eb88855f655ab07da2398762f949498e5a0))
* **skills:** add LAN reachability carve-out ([#2290](https://github.com/mvanhorn/cli-printing-press/issues/2290)) ([778192b](https://github.com/mvanhorn/cli-printing-press/commit/778192bd4bcd44b649c0e4ee77cfecab2b12c5b4))
* **skills:** clarify bearer-token auth enrichment ([#2251](https://github.com/mvanhorn/cli-printing-press/issues/2251)) ([81c8a29](https://github.com/mvanhorn/cli-printing-press/commit/81c8a2947c1953f03b3f9846f23216ef0390cde8))
* **skills:** detect truncated Go stdlib in preflight ([#2294](https://github.com/mvanhorn/cli-printing-press/issues/2294)) ([8c26a9e](https://github.com/mvanhorn/cli-printing-press/commit/8c26a9e3d0c9f955dd35fc1dd4e0d1999b53be7e))
* **skills:** document multi-positional RunE shape so partial args fail with exit 2 ([#2272](https://github.com/mvanhorn/cli-printing-press/issues/2272)) ([0d21ad3](https://github.com/mvanhorn/cli-printing-press/commit/0d21ad34a97ee2e03382900bdae119a9a119bfc1)), closes [#965](https://github.com/mvanhorn/cli-printing-press/issues/965)
* **skills:** gate large MCP endpoint surfaces ([#2231](https://github.com/mvanhorn/cli-printing-press/issues/2231)) ([b395352](https://github.com/mvanhorn/cli-printing-press/commit/b3953529135904397a84dd4a9dc7a1abaa491264))
* **skills:** hold sub-60 reprints on missing transcendence rows ([#2236](https://github.com/mvanhorn/cli-printing-press/issues/2236)) ([d0acb24](https://github.com/mvanhorn/cli-printing-press/commit/d0acb2470f581ae6527e0b639c32fbf107c020e6))
* **skills:** initialize transcendence collector slices with make([]T, 0) ([#2268](https://github.com/mvanhorn/cli-printing-press/issues/2268)) ([3132ea4](https://github.com/mvanhorn/cli-printing-press/commit/3132ea4b7322e2887c26b1c0f602e7bba0af0f1e)), closes [#1344](https://github.com/mvanhorn/cli-printing-press/issues/1344)
* **skills:** route polish data inserts through typed helpers ([#2230](https://github.com/mvanhorn/cli-printing-press/issues/2230)) ([a2f4659](https://github.com/mvanhorn/cli-printing-press/commit/a2f4659bc39e408c87ffc019f947dc6243f078f2))

## [4.16.0](https://github.com/mvanhorn/cli-printing-press/compare/v4.15.0...v4.16.0) (2026-05-25)


### Features

* **catalog:** add IT Glue Printing Press spec ([#2063](https://github.com/mvanhorn/cli-printing-press/issues/2063)) ([48ec8d0](https://github.com/mvanhorn/cli-printing-press/commit/48ec8d01ae539ba73f63bf2dac962fc4b9264814))
* **cli:** add OAuth device-code auth generation ([#2171](https://github.com/mvanhorn/cli-printing-press/issues/2171)) ([15739cc](https://github.com/mvanhorn/cli-printing-press/commit/15739cc74bb61425dd2dab65f76a36ab2f6a30bb))
* **cli:** hook agentcookie secrets bus into the generator ([#1972](https://github.com/mvanhorn/cli-printing-press/issues/1972)) ([8bae9aa](https://github.com/mvanhorn/cli-printing-press/commit/8bae9aaedc591e8ae2e3f0a9a3cebd60e3004aef))
* **skills:** add oauth grant reachability probe ([#2148](https://github.com/mvanhorn/cli-printing-press/issues/2148)) ([1fa5614](https://github.com/mvanhorn/cli-printing-press/commit/1fa56147f11fb9a595e4effb65d94b1262d87b22))
* **skills:** capture reachability 4xx body hints ([#2150](https://github.com/mvanhorn/cli-printing-press/issues/2150)) ([9d72564](https://github.com/mvanhorn/cli-printing-press/commit/9d725647fbb9b8b48ac4ef98b91b69b26d9cc92a))
* **skills:** offer reprint spec enrichment before handoff ([#2154](https://github.com/mvanhorn/cli-printing-press/issues/2154)) ([163e4bc](https://github.com/mvanhorn/cli-printing-press/commit/163e4bcafa9178c5a74807c8a85de239ddc8213f))


### Bug Fixes

* **ci:** clarify review feedback gates ([#2146](https://github.com/mvanhorn/cli-printing-press/issues/2146)) ([eb4decb](https://github.com/mvanhorn/cli-printing-press/commit/eb4decb7b1d61adc94c3f99cfac8d1a6872c20a6))
* **cli:** accept nested internal body schemas ([#2161](https://github.com/mvanhorn/cli-printing-press/issues/2161)) ([08eaf12](https://github.com/mvanhorn/cli-printing-press/commit/08eaf12a6b33e6b32ef4b020a7e5c904df32e837))
* **cli:** add data-source strategy contract ([#2174](https://github.com/mvanhorn/cli-printing-press/issues/2174)) ([552e5e8](https://github.com/mvanhorn/cli-printing-press/commit/552e5e86242b66b8aa359e47cb77c63fd273cd5a))
* **cli:** add editable store migration extras hook ([#2172](https://github.com/mvanhorn/cli-printing-press/issues/2172)) ([79c0c6f](https://github.com/mvanhorn/cli-printing-press/commit/79c0c6f044df8abda502b0cbf8cff6a4e120d834))
* **cli:** allow reserved PII placeholders ([#2159](https://github.com/mvanhorn/cli-printing-press/issues/2159)) ([deeeb71](https://github.com/mvanhorn/cli-printing-press/commit/deeeb714956e68a6bfe7283171e2372ed16b9106))
* **cli:** allowlist reserved synthetic values and GitHub IDs in PII scanner ([#2165](https://github.com/mvanhorn/cli-printing-press/issues/2165)) ([77b5a16](https://github.com/mvanhorn/cli-printing-press/commit/77b5a163dd98a2693e9f2d30beb63881052a1462)), closes [#1589](https://github.com/mvanhorn/cli-printing-press/issues/1589) [#1343](https://github.com/mvanhorn/cli-printing-press/issues/1343) [#1477](https://github.com/mvanhorn/cli-printing-press/issues/1477)
* **cli:** annotate help-only parent-grouper commands mcp:read-only ([#2158](https://github.com/mvanhorn/cli-printing-press/issues/2158)) ([e594d2c](https://github.com/mvanhorn/cli-printing-press/commit/e594d2c655d05f60d2a8953688fccc5203e49391))
* **cli:** count parent-grouper scorecard commands ([#2176](https://github.com/mvanhorn/cli-printing-press/issues/2176)) ([4c42770](https://github.com/mvanhorn/cli-printing-press/commit/4c42770d3a56d60de08ec5a49368ee8974d8d044))
* **cli:** credit positional type fidelity ([#2178](https://github.com/mvanhorn/cli-printing-press/issues/2178)) ([2e4aded](https://github.com/mvanhorn/cli-printing-press/commit/2e4aded43523f7ff191e362a1d5570dbe2b71682))
* **cli:** default generated sync max-pages to unlimited ([#2151](https://github.com/mvanhorn/cli-printing-press/issues/2151)) ([9b30c01](https://github.com/mvanhorn/cli-printing-press/commit/9b30c01602005c7b77d00fa8271e183e601ce53c))
* **cli:** derive BaseURL from spec source URL for relative-only servers ([#2175](https://github.com/mvanhorn/cli-printing-press/issues/2175)) ([c0e5b4a](https://github.com/mvanhorn/cli-printing-press/commit/c0e5b4a098f485c2ebd984f9167ec7604ac996d1)), closes [#1383](https://github.com/mvanhorn/cli-printing-press/issues/1383)
* **cli:** derive MCPBinary from api_name slug not spec title ([#2149](https://github.com/mvanhorn/cli-printing-press/issues/2149)) ([140b418](https://github.com/mvanhorn/cli-printing-press/commit/140b4181f8e7409f30dee08a0ca956d44de1fd5d)), closes [#1473](https://github.com/mvanhorn/cli-printing-press/issues/1473)
* **cli:** detect Mailchimp/Linear/Anthropic key shapes in secret scanner ([#2162](https://github.com/mvanhorn/cli-printing-press/issues/2162)) ([6e6fc29](https://github.com/mvanhorn/cli-printing-press/commit/6e6fc29292e550a9767d34ebf5f9bf651fc6bb35)), closes [#1593](https://github.com/mvanhorn/cli-printing-press/issues/1593)
* **cli:** infer sniffed response type stubs ([#2164](https://github.com/mvanhorn/cli-printing-press/issues/2164)) ([bebc256](https://github.com/mvanhorn/cli-printing-press/commit/bebc25645b3cff2cdfc2fe77af883837239ea9d9))
* **cli:** make scoreVision 10/10 reachable ([#2163](https://github.com/mvanhorn/cli-printing-press/issues/2163)) ([8208ab8](https://github.com/mvanhorn/cli-printing-press/commit/8208ab8ef86807956dcd09446e80ce64fb638e11)), closes [#1721](https://github.com/mvanhorn/cli-printing-press/issues/1721)
* **cli:** raise generated CLI default request timeout to 60s ([#2157](https://github.com/mvanhorn/cli-printing-press/issues/2157)) ([dba4b9e](https://github.com/mvanhorn/cli-printing-press/commit/dba4b9e22325aac2651ec0e0b9a86bb5a0259aca)), closes [#1626](https://github.com/mvanhorn/cli-printing-press/issues/1626)
* **cli:** reject placeholder URLs when inferring auth key URL ([#2169](https://github.com/mvanhorn/cli-printing-press/issues/2169)) ([e0edc96](https://github.com/mvanhorn/cli-printing-press/commit/e0edc96d2611f11775f7111c7015b6e7686f4cb1)), closes [#2011](https://github.com/mvanhorn/cli-printing-press/issues/2011)
* **cli:** restore canonical prediction-goat learn loop (engine + schema + package + CLI) ([#2200](https://github.com/mvanhorn/cli-printing-press/issues/2200)) ([cc7e69c](https://github.com/mvanhorn/cli-printing-press/commit/cc7e69cc7b166bf33926084f93b79e75530a69cf))
* **cli:** reword UpsertBatch cache-index warning to not imply data loss ([#2221](https://github.com/mvanhorn/cli-printing-press/issues/2221)) ([b5267a8](https://github.com/mvanhorn/cli-printing-press/commit/b5267a8898099b2aa00e7e2c107954e5dbf36eec)), closes [#1224](https://github.com/mvanhorn/cli-printing-press/issues/1224)
* **cli:** store only basename for local spec_path in manifest ([#2156](https://github.com/mvanhorn/cli-printing-press/issues/2156)) ([4a92662](https://github.com/mvanhorn/cli-printing-press/commit/4a926627bba5305572396d0cd75c070969db9e41)), closes [#1587](https://github.com/mvanhorn/cli-printing-press/issues/1587)
* **cli:** store synced_at as RFC3339 UTC so SQLite strftime works ([#2173](https://github.com/mvanhorn/cli-printing-press/issues/2173)) ([b01b62e](https://github.com/mvanhorn/cli-printing-press/commit/b01b62eb5b6a78291fb355d5a645741015be1955)), closes [#1588](https://github.com/mvanhorn/cli-printing-press/issues/1588)
* **cli:** streamline oauth login setup ([#2177](https://github.com/mvanhorn/cli-printing-press/issues/2177)) ([7709698](https://github.com/mvanhorn/cli-printing-press/commit/7709698f51c5cd95943eaccbce7750951b14f180))
* **cli:** support csv array body fields ([#2160](https://github.com/mvanhorn/cli-printing-press/issues/2160)) ([1f8d390](https://github.com/mvanhorn/cli-printing-press/commit/1f8d39043438409335fe0117a7588de69fd298b8))
* **cli:** unwrap nested data list envelopes ([#2126](https://github.com/mvanhorn/cli-printing-press/issues/2126)) ([1a9aec8](https://github.com/mvanhorn/cli-printing-press/commit/1a9aec84a76775137ffd3d66551fbc7179f184fc))
* **cli:** wire GraphQL query list variable ([#2152](https://github.com/mvanhorn/cli-printing-press/issues/2152)) ([c9e39d7](https://github.com/mvanhorn/cli-printing-press/commit/c9e39d7f657cfa183b1125d964ba5b8a975e32d1))
* **generator:** preserve required bool flag presence ([#1940](https://github.com/mvanhorn/cli-printing-press/issues/1940)) ([5157252](https://github.com/mvanhorn/cli-printing-press/commit/515725221f309d915777d752f445af8e10c76dba))
* **skills:** rerun live dogfood during publish ([#2155](https://github.com/mvanhorn/cli-printing-press/issues/2155)) ([d8f806e](https://github.com/mvanhorn/cli-printing-press/commit/d8f806e20e192df08a7278488537db6779ad92b3))
* **skills:** route polish instrumentation through client ([#2168](https://github.com/mvanhorn/cli-printing-press/issues/2168)) ([5929945](https://github.com/mvanhorn/cli-printing-press/commit/59299458b9c9f194d49a3150e363425bf081cfc2))
* **skills:** warn on multi-spec directories ([#2167](https://github.com/mvanhorn/cli-printing-press/issues/2167)) ([93d43b5](https://github.com/mvanhorn/cli-printing-press/commit/93d43b55da54bafe176fd1ee158c1cd0186f807a))


### Code Refactoring

* **cli:** collapse Recall *entities.Config into Opts.EntityConfig ([#2189](https://github.com/mvanhorn/cli-printing-press/issues/2189)) ([0c3f032](https://github.com/mvanhorn/cli-printing-press/commit/0c3f03212d2dd926ace4af106a826b429fb49a63))

## [4.15.0](https://github.com/mvanhorn/cli-printing-press/compare/v4.14.0...v4.15.0) (2026-05-25)


### Features

* **cli:** generator-wide self-learning CLI loop ([#2085](https://github.com/mvanhorn/cli-printing-press/issues/2085)) ([23b6239](https://github.com/mvanhorn/cli-printing-press/commit/23b6239c3626687d1401847f0f7722e898a213c7))
* **cli:** sync-param-drop resolves helper-function key enrichment ([#1881](https://github.com/mvanhorn/cli-printing-press/issues/1881)) ([9beda0d](https://github.com/mvanhorn/cli-printing-press/commit/9beda0d27b6929709b8fa806fe466d9ece50f355))


### Bug Fixes

* **catalog:** require catalog PR justification ([#2093](https://github.com/mvanhorn/cli-printing-press/issues/2093)) ([bc3c5ee](https://github.com/mvanhorn/cli-printing-press/commit/bc3c5eedc7adbfd793cbb783888db760edec94d1))
* **cli:** advance has-more numeric pagination ([#2107](https://github.com/mvanhorn/cli-printing-press/issues/2107)) ([c640b50](https://github.com/mvanhorn/cli-printing-press/commit/c640b501819a5f38817ebde541ae08d6c54bd0c8))
* **cli:** broaden write-through cache envelope keys to match sync ([#1919](https://github.com/mvanhorn/cli-printing-press/issues/1919)) ([98f47a5](https://github.com/mvanhorn/cli-printing-press/commit/98f47a5cebefc560312a89362a7345220c03023a))
* **cli:** cap wide generated store tables ([#2086](https://github.com/mvanhorn/cli-printing-press/issues/2086)) ([dd388bb](https://github.com/mvanhorn/cli-printing-press/commit/dd388bb850e533e2121d952c27de1ec99c91d1a2))
* **cli:** catch zero-row mock syncs ([#2135](https://github.com/mvanhorn/cli-printing-press/issues/2135)) ([b498612](https://github.com/mvanhorn/cli-printing-press/commit/b498612b06613fc3b3f0d51642112e1481aaa680))
* **cli:** count helper-backed workflow client calls ([#2076](https://github.com/mvanhorn/cli-printing-press/issues/2076)) ([861be98](https://github.com/mvanhorn/cli-printing-press/commit/861be987d3e7d0283337126927e4a6e111c7a521))
* **cli:** derive version-command binary name from cmd.Root().Name() ([#1913](https://github.com/mvanhorn/cli-printing-press/issues/1913)) ([729027c](https://github.com/mvanhorn/cli-printing-press/commit/729027c9800e9f747fef52c1418bbee2917f6c2d)), closes [#1585](https://github.com/mvanhorn/cli-printing-press/issues/1585)
* **cli:** detect plural wrapper list envelopes ([#2078](https://github.com/mvanhorn/cli-printing-press/issues/2078)) ([2998977](https://github.com/mvanhorn/cli-printing-press/commit/2998977c5649b5e1b376e41f4d34844d6e2cef67))
* **cli:** fall back synthetic anchors to local lists ([#2079](https://github.com/mvanhorn/cli-printing-press/issues/2079)) ([d057ef0](https://github.com/mvanhorn/cli-printing-press/commit/d057ef09a88a9a133f13f2aa29ba9880b3488c47))
* **cli:** follow one-hop client helpers in reimplementation check ([#2048](https://github.com/mvanhorn/cli-printing-press/issues/2048)) ([bb830d0](https://github.com/mvanhorn/cli-printing-press/commit/bb830d0290fb42413c12264e7340ceaa63d1a568))
* **cli:** harden sniffed auth inference ([#2096](https://github.com/mvanhorn/cli-printing-press/issues/2096)) ([6f20d3d](https://github.com/mvanhorn/cli-printing-press/commit/6f20d3d9f185e4ae45665bdc057114221c2e630c))
* **cli:** hold rate-limiter lock through lastRequest reservation ([#1907](https://github.com/mvanhorn/cli-printing-press/issues/1907)) ([617c894](https://github.com/mvanhorn/cli-printing-press/commit/617c894b618553c6d92d0f66c1e878f790706643)), closes [#1669](https://github.com/mvanhorn/cli-printing-press/issues/1669)
* **cli:** honor absolute endpoint paths ([#2117](https://github.com/mvanhorn/cli-printing-press/issues/2117)) ([99f6625](https://github.com/mvanhorn/cli-printing-press/commit/99f6625bb97901c0b619c58dd7a4c9cd4fcf17c2))
* **cli:** ignore shell operators in verify-skill positionals ([#2095](https://github.com/mvanhorn/cli-printing-press/issues/2095)) ([5acaa2a](https://github.com/mvanhorn/cli-printing-press/commit/5acaa2adac858e2e9f9301827c7cee97dea5f7e5))
* **cli:** include auth flow env vars in mcpb manifests ([#2106](https://github.com/mvanhorn/cli-printing-press/issues/2106)) ([64c1b1a](https://github.com/mvanhorn/cli-printing-press/commit/64c1b1a3c47de439c845551bc6eb296c9f884765))
* **cli:** infer path-param resource id fields ([#2134](https://github.com/mvanhorn/cli-printing-press/issues/2134)) ([1a8d9c6](https://github.com/mvanhorn/cli-printing-press/commit/1a8d9c6f5ec0cdb7763546f5bb4c47ac2293409c))
* **cli:** let force regen preserve hand-authored files ([#2077](https://github.com/mvanhorn/cli-printing-press/issues/2077)) ([3ec8516](https://github.com/mvanhorn/cli-printing-press/commit/3ec851698c5da5a6d28fb79d4887dc280a027839))
* **cli:** merge spec date-time fields into stale timestamp allowlist ([#1920](https://github.com/mvanhorn/cli-printing-press/issues/1920)) ([1979823](https://github.com/mvanhorn/cli-printing-press/commit/1979823b0e6f5aff0c49c0af3f76312069301f0f))
* **cli:** normalize GraphQL sync since timestamps ([#2136](https://github.com/mvanhorn/cli-printing-press/issues/2136)) ([eeed86c](https://github.com/mvanhorn/cli-printing-press/commit/eeed86cf670cf7677460cd678bc0b0c89d628110))
* **cli:** preserve colliding body params in MCP ([#2127](https://github.com/mvanhorn/cli-printing-press/issues/2127)) ([5474578](https://github.com/mvanhorn/cli-printing-press/commit/5474578cec6685080e8b5ce0ee2ddadcad707d67))
* **cli:** preserve docs-derived request paths ([#2124](https://github.com/mvanhorn/cli-printing-press/issues/2124)) ([d3f7af5](https://github.com/mvanhorn/cli-printing-press/commit/d3f7af5708b0ff009d903e882241c22c8d347275))
* **cli:** preserve force-regen provenance ([#2118](https://github.com/mvanhorn/cli-printing-press/issues/2118)) ([9adbf4d](https://github.com/mvanhorn/cli-printing-press/commit/9adbf4d517dff8f092bbf3f629c6c569bfb95601))
* **cli:** preserve generated auth source provenance ([#2115](https://github.com/mvanhorn/cli-printing-press/issues/2115)) ([062a3b3](https://github.com/mvanhorn/cli-printing-press/commit/062a3b3785ef923b32b8c173927343dfc46fa9d7))
* **cli:** preserve OpenAPI multi-query auth credentials ([#2062](https://github.com/mvanhorn/cli-printing-press/issues/2062)) ([212faad](https://github.com/mvanhorn/cli-printing-press/commit/212faad6a5982207875cf0f1a3db4b012c8019c1))
* **cli:** preserve templated server URL schemes ([#2114](https://github.com/mvanhorn/cli-printing-press/issues/2114)) ([267d4f7](https://github.com/mvanhorn/cli-printing-press/commit/267d4f79650a2d513ba4ca4abcb4e190d15be40d))
* **cli:** preserve windows exe names in mcpb bundles ([#2097](https://github.com/mvanhorn/cli-printing-press/issues/2097)) ([7d8e8ca](https://github.com/mvanhorn/cli-printing-press/commit/7d8e8ca97969f2e62cd56fb0cfeac2c4281081f7))
* **cli:** recognize literal-format composed auth in dogfood auth_check ([#1993](https://github.com/mvanhorn/cli-printing-press/issues/1993)) ([196dba3](https://github.com/mvanhorn/cli-printing-press/commit/196dba3a2b432df332c507ad18ae54cba8085cee))
* **cli:** recognize local-datastore scorer shape ([#2137](https://github.com/mvanhorn/cli-printing-press/issues/2137)) ([2034e9c](https://github.com/mvanhorn/cli-printing-press/commit/2034e9cd5eee89ad756d9f87f24ad6a39a7efbec))
* **cli:** route analytics output through cobra writer ([#2113](https://github.com/mvanhorn/cli-printing-press/issues/2113)) ([8e4e0ec](https://github.com/mvanhorn/cli-printing-press/commit/8e4e0ec88122660e7ea8fc841dab5b67b655575e))
* **cli:** route GraphQL reads around verify gate ([#2105](https://github.com/mvanhorn/cli-printing-press/issues/2105)) ([0ce8645](https://github.com/mvanhorn/cli-printing-press/commit/0ce86458ab862abb83ed252abcbba211af06b66b))
* **cli:** scope traffic-analysis protection evidence ([#2047](https://github.com/mvanhorn/cli-printing-press/issues/2047)) ([e6292dd](https://github.com/mvanhorn/cli-printing-press/commit/e6292ddd2bc853fad1e55930ef8c79140772eccb))
* **cli:** skip live dogfood required-param fixture gaps ([#2098](https://github.com/mvanhorn/cli-printing-press/issues/2098)) ([31b5d2d](https://github.com/mvanhorn/cli-printing-press/commit/31b5d2d54bae24360a27ab3ff3dd8edef3dbc999))
* **cli:** strip custom auth header on cross-host redirects ([#1915](https://github.com/mvanhorn/cli-printing-press/issues/1915)) ([2d339ba](https://github.com/mvanhorn/cli-printing-press/commit/2d339bac8246b2a817c568a1bcf6e575d8855776)), closes [#1672](https://github.com/mvanhorn/cli-printing-press/issues/1672)
* **cli:** support body wire-name overrides ([#2075](https://github.com/mvanhorn/cli-printing-press/issues/2075)) ([280b272](https://github.com/mvanhorn/cli-printing-press/commit/280b2722d39770a94fe531e5ffc3c08bc21a826a))
* **cli:** support Entra client credentials generation ([#2088](https://github.com/mvanhorn/cli-printing-press/issues/2088)) ([076bf6a](https://github.com/mvanhorn/cli-printing-press/commit/076bf6aae02dd381732891809e4bee6f7e4f8e5c))
* **cli:** support query wire-name overrides ([#2073](https://github.com/mvanhorn/cli-printing-press/issues/2073)) ([73dc29b](https://github.com/mvanhorn/cli-printing-press/commit/73dc29b660a6a886078f1f0835e11eab3a671ab2))
* **cli:** suppress phantom [] MCP/CLI parameters ([#1952](https://github.com/mvanhorn/cli-printing-press/issues/1952)) ([2922c86](https://github.com/mvanhorn/cli-printing-press/commit/2922c86c9b8b5080e48a98cfc20cf7efcadd41e8))
* **cli:** sync chained dependent resources ([#2051](https://github.com/mvanhorn/cli-printing-press/issues/2051)) ([abe5f8a](https://github.com/mvanhorn/cli-printing-press/commit/abe5f8abb17d6728d937f818068b0e84d9ef6a2b))
* **cli:** treat side-effect narrative skips as warnings ([#2087](https://github.com/mvanhorn/cli-printing-press/issues/2087)) ([16fc4ed](https://github.com/mvanhorn/cli-printing-press/commit/16fc4ed25296f44b1e01bb7b820ec64a61429e0c))
* **cli:** unify digit-leading command identifiers ([#2128](https://github.com/mvanhorn/cli-printing-press/issues/2128)) ([8faa180](https://github.com/mvanhorn/cli-printing-press/commit/8faa180c60caeb45338fa86f1ed4a182d650e297))
* **cli:** wire http.CookieJar into composed-auth client template ([#1992](https://github.com/mvanhorn/cli-printing-press/issues/1992)) ([2342203](https://github.com/mvanhorn/cli-printing-press/commit/234220381969f642296324664a4a8165d9462e62))
* **skills:** distinguish RunE missing input from help ([#2046](https://github.com/mvanhorn/cli-printing-press/issues/2046)) ([fce8cff](https://github.com/mvanhorn/cli-printing-press/commit/fce8cfff4da61e32f1ffa6b8f96d3a0a0dfe5f5a))
* **skills:** require fresh publish turn after polish ([#2104](https://github.com/mvanhorn/cli-printing-press/issues/2104)) ([c1b03ed](https://github.com/mvanhorn/cli-printing-press/commit/c1b03ed2ec2abb77ee68af49266cd6b25b7e36ac))

## [4.14.0](https://github.com/mvanhorn/cli-printing-press/compare/v4.13.1...v4.14.0) (2026-05-24)


### Features

* **cli:** emit cliutil.ParseDurationLoose for day/week duration flags ([#1953](https://github.com/mvanhorn/cli-printing-press/issues/1953)) ([e35c466](https://github.com/mvanhorn/cli-printing-press/commit/e35c46665000fab24f753475c67517cdd2927642))
* **cli:** emit cliutil.ParseODataDate for OData v3 /Date(ms)/ literals ([#1954](https://github.com/mvanhorn/cli-printing-press/issues/1954)) ([476028b](https://github.com/mvanhorn/cli-printing-press/commit/476028bdcb0a54c6740fec63a26d275b5f750f38))
* **installer:** add repo bootstrap script ([#2044](https://github.com/mvanhorn/cli-printing-press/issues/2044)) ([a1b96ff](https://github.com/mvanhorn/cli-printing-press/commit/a1b96ff74d135b9001ba878f09dab350d529e3b1))


### Bug Fixes

* **cli:** chomp trailing newline from auth setup instructions ([#1947](https://github.com/mvanhorn/cli-printing-press/issues/1947)) ([f878265](https://github.com/mvanhorn/cli-printing-press/commit/f878265505c4987cf1f7e6a9d018947bbdd69797))
* **cli:** drop dead invocation path from promoted-command Long ([#1912](https://github.com/mvanhorn/cli-printing-press/issues/1912)) ([f434e2b](https://github.com/mvanhorn/cli-printing-press/commit/f434e2bb272c58382f721d02bdda0d7bdfc36a6e)), closes [#1697](https://github.com/mvanhorn/cli-printing-press/issues/1697)
* **cli:** emit cache stale-after as a direct duration expression ([#1951](https://github.com/mvanhorn/cli-printing-press/issues/1951)) ([501964c](https://github.com/mvanhorn/cli-printing-press/commit/501964c00a8774ecd6093c18f155296bad946220))
* **cli:** format CSV float64 cells without scientific notation ([#1905](https://github.com/mvanhorn/cli-printing-press/issues/1905)) ([dd1ee72](https://github.com/mvanhorn/cli-printing-press/commit/dd1ee72fe3ecf0f06bb125ffa0781d8ef8d3567e))
* **cli:** kebab-case resource names in README/SKILL command examples ([#1948](https://github.com/mvanhorn/cli-printing-press/issues/1948)) ([11bb0b5](https://github.com/mvanhorn/cli-printing-press/commit/11bb0b53d5e97933de357dc312d13229a52284d5))
* **cli:** keep integer-typed time params numeric in examples ([#1918](https://github.com/mvanhorn/cli-printing-press/issues/1918)) ([97e5ec4](https://github.com/mvanhorn/cli-printing-press/commit/97e5ec43237d69049afd5f5537d6584524176ded))
* **cli:** only emit partial-failure helpers when spec has mutation endpoints ([#1991](https://github.com/mvanhorn/cli-printing-press/issues/1991)) ([b607511](https://github.com/mvanhorn/cli-printing-press/commit/b6075117e5415c1069ba7d1502f8d0801dfe2841))
* **cli:** use FNV-1a hash for FTS rowid to avoid collisions ([#1908](https://github.com/mvanhorn/cli-printing-press/issues/1908)) ([258346b](https://github.com/mvanhorn/cli-printing-press/commit/258346b23e451594aa136bcbb05d05f8bf4a74ef))
* **cli:** write feedback.jsonl to XDG data dir ([#1914](https://github.com/mvanhorn/cli-printing-press/issues/1914)) ([709fe74](https://github.com/mvanhorn/cli-printing-press/commit/709fe74cd516dcf2b9a32b3698ed0d7a2e828dfc)), closes [#1640](https://github.com/mvanhorn/cli-printing-press/issues/1640)
* **generator:** support JSON-LD script selectors ([#1877](https://github.com/mvanhorn/cli-printing-press/issues/1877)) ([58380bb](https://github.com/mvanhorn/cli-printing-press/commit/58380bb82be8bbcf21768bd3a84a56d14ed67519))

## [4.13.1](https://github.com/mvanhorn/cli-printing-press/compare/v4.13.0...v4.13.1) (2026-05-24)


### Bug Fixes

* **cli:** align MCP remote transport scoring threshold ([#1979](https://github.com/mvanhorn/cli-printing-press/issues/1979)) ([d0cb79a](https://github.com/mvanhorn/cli-printing-press/commit/d0cb79ae73296c304fb0488605dc8c254242bcd3))
* **cli:** count store helper workflows ([#2010](https://github.com/mvanhorn/cli-printing-press/issues/2010)) ([d78535a](https://github.com/mvanhorn/cli-printing-press/commit/d78535a53ece93e276b1e4d4bc0b3ac005a972b5))
* **cli:** guard client flag in MCP shellouts ([#2003](https://github.com/mvanhorn/cli-printing-press/issues/2003)) ([60d7916](https://github.com/mvanhorn/cli-printing-press/commit/60d7916270d6b814a41d4729e7663be66df97cb5))
* **cli:** include manuscript narratives in PII polish scan ([#2025](https://github.com/mvanhorn/cli-printing-press/issues/2025)) ([8f94f89](https://github.com/mvanhorn/cli-printing-press/commit/8f94f899b84e93113c9a7e9d4486992b983b8d02))
* **cli:** infer template vars from base URLs ([#2002](https://github.com/mvanhorn/cli-printing-press/issues/2002)) ([e72ae7a](https://github.com/mvanhorn/cli-printing-press/commit/e72ae7af805aa468ccddebe9e0edab2bc17306cf))
* **cli:** normalize verify directories ([#1994](https://github.com/mvanhorn/cli-printing-press/issues/1994)) ([b6e7bcc](https://github.com/mvanhorn/cli-printing-press/commit/b6e7bcc1274713346ea85aea081f000b04bce1c2))
* **cli:** parse OpenAPI x-cache extension ([#1995](https://github.com/mvanhorn/cli-printing-press/issues/1995)) ([8ff3242](https://github.com/mvanhorn/cli-printing-press/commit/8ff32425b99f288545017995976501c4c12e6afe))
* **cli:** redact live-check output samples ([#1997](https://github.com/mvanhorn/cli-printing-press/issues/1997)) ([0dab481](https://github.com/mvanhorn/cli-printing-press/commit/0dab48113de1e7d94c06b9d32924a2cc003b00bf))
* **cli:** report malformed research json during publish ([#2008](https://github.com/mvanhorn/cli-printing-press/issues/2008)) ([9cd0c2b](https://github.com/mvanhorn/cli-printing-press/commit/9cd0c2b3a3e1fd33706671bac501570a59d3fdab))
* **cli:** require quoted shell variables in skill bash blocks ([#2004](https://github.com/mvanhorn/cli-printing-press/issues/2004)) ([01afec0](https://github.com/mvanhorn/cli-printing-press/commit/01afec0970fba73cf8d173b3314763f5751ee221))
* **cli:** retry live dogfood auth 401s ([#1996](https://github.com/mvanhorn/cli-printing-press/issues/1996)) ([13f5d99](https://github.com/mvanhorn/cli-printing-press/commit/13f5d99b1e538ab90500f9312380d97bb093af0e))
* **cli:** route wrapper sync events off json stdout ([#1981](https://github.com/mvanhorn/cli-printing-press/issues/1981)) ([58a3c8d](https://github.com/mvanhorn/cli-printing-press/commit/58a3c8dd38ebbfc7fc48f1811ce3149a325bdfdc))
* **cli:** warn on novel feature command depth drift ([#2009](https://github.com/mvanhorn/cli-printing-press/issues/2009)) ([8bcf5ae](https://github.com/mvanhorn/cli-printing-press/commit/8bcf5ae89be7d3d4308ffc0bf47ab523ebc60b8f))
* **skills:** document patches index publish contract ([#1980](https://github.com/mvanhorn/cli-printing-press/issues/1980)) ([c0f33c6](https://github.com/mvanhorn/cli-printing-press/commit/c0f33c65d0f52f904894123a3cb15ab68bc2c451))
* **skills:** honor PRINTING_PRESS_HOME in setup paths ([#2023](https://github.com/mvanhorn/cli-printing-press/issues/2023)) ([23cc0ce](https://github.com/mvanhorn/cli-printing-press/commit/23cc0ce2b3d13d52ea35988fe8ce67806a949f49))
* **skills:** require agent-browser post-install setup ([#2022](https://github.com/mvanhorn/cli-printing-press/issues/2022)) ([bef08f5](https://github.com/mvanhorn/cli-printing-press/commit/bef08f5dfb1f669a9aa1e0113310b6d8a119dc28))
* **skills:** require verify-safe quickstart first step ([#2021](https://github.com/mvanhorn/cli-printing-press/issues/2021)) ([0f30aa5](https://github.com/mvanhorn/cli-printing-press/commit/0f30aa50355d9873f4679056af5a22524a95fdf3))

## [4.13.0](https://github.com/mvanhorn/cli-printing-press/compare/v4.12.0...v4.13.0) (2026-05-23)


### Features

* **cli:** implement CDP WebSocket eval for cookie extraction ([#1869](https://github.com/mvanhorn/cli-printing-press/issues/1869)) ([b0a9ee4](https://github.com/mvanhorn/cli-printing-press/commit/b0a9ee46536d7a7001388be5b6261eb8b6e199da))
* **cli:** refuse --force when novel features detected ([#1880](https://github.com/mvanhorn/cli-printing-press/issues/1880)) ([3b47cbf](https://github.com/mvanhorn/cli-printing-press/commit/3b47cbf2f484b58544ba5bbdfd971178c8d47640))


### Bug Fixes

* **ci:** let code-owner authors satisfy review gate ([#1968](https://github.com/mvanhorn/cli-printing-press/issues/1968)) ([de2654f](https://github.com/mvanhorn/cli-printing-press/commit/de2654ff0c790fcec98ee522a53b17c4fa079841))
* **ci:** move conversation resolution signal to commit status ([#1882](https://github.com/mvanhorn/cli-printing-press/issues/1882)) ([838d409](https://github.com/mvanhorn/cli-printing-press/commit/838d4093a485286030375b63c80b98f335239c7b))
* **cli:** align MCP tool design scoring threshold ([#1927](https://github.com/mvanhorn/cli-printing-press/issues/1927)) ([438ecaf](https://github.com/mvanhorn/cli-printing-press/commit/438ecafb5a80de0bd07456bb45b4dadaac8ee743))
* **cli:** broaden scoreTerminalUX TTY detection beyond isatty literal ([#1786](https://github.com/mvanhorn/cli-printing-press/issues/1786)) ([aa5a0af](https://github.com/mvanhorn/cli-printing-press/commit/aa5a0afa0957303999c619d88fb55c0c1e8f636d)), closes [#1561](https://github.com/mvanhorn/cli-printing-press/issues/1561)
* **cli:** bump golang.org/x/net template pin past GO-2026-5026 ([#1868](https://github.com/mvanhorn/cli-printing-press/issues/1868)) ([9724312](https://github.com/mvanhorn/cli-printing-press/commit/972431270eac5de2240bda23f8ba496eb9a97f3a)), closes [#1865](https://github.com/mvanhorn/cli-printing-press/issues/1865)
* **cli:** clarify live dogfood output truncation ([#1937](https://github.com/mvanhorn/cli-printing-press/issues/1937)) ([5c9e246](https://github.com/mvanhorn/cli-printing-press/commit/5c9e2468c46b3ca32abfa295ca0ce89e95891444))
* **cli:** default sync concurrency by rate class ([#1938](https://github.com/mvanhorn/cli-printing-press/issues/1938)) ([6d1ef4a](https://github.com/mvanhorn/cli-printing-press/commit/6d1ef4a45760a724ec879178af2260ce059b8108))
* **cli:** distinguish verify-skill prose invocations ([#1906](https://github.com/mvanhorn/cli-printing-press/issues/1906)) ([d328375](https://github.com/mvanhorn/cli-printing-press/commit/d328375cf15803f464f2c970d9c5091af0e46694))
* **cli:** emit recipe-backed MCP intent tools ([#1916](https://github.com/mvanhorn/cli-printing-press/issues/1916)) ([3d050f9](https://github.com/mvanhorn/cli-printing-press/commit/3d050f9443217ca86426e869b321de78f22522f4))
* **cli:** exempt sentinel parent groupers from tools audit ([#1935](https://github.com/mvanhorn/cli-printing-press/issues/1935)) ([2308b1a](https://github.com/mvanhorn/cli-printing-press/commit/2308b1a548e0e328012815a3d7ac59c392f917e5))
* **cli:** mask credentials in generated URL output ([#1909](https://github.com/mvanhorn/cli-printing-press/issues/1909)) ([964bda6](https://github.com/mvanhorn/cli-printing-press/commit/964bda67f1ddd54e91194516aab2986bb54e5d96))
* **cli:** preserve typed api error exit codes ([#1894](https://github.com/mvanhorn/cli-printing-press/issues/1894)) ([d25a8e3](https://github.com/mvanhorn/cli-printing-press/commit/d25a8e355e01e49ef9e3882211368c50a4a2c075))
* **cli:** recognize equivalent scorer capability shapes ([#1896](https://github.com/mvanhorn/cli-printing-press/issues/1896)) ([7d15e0e](https://github.com/mvanhorn/cli-printing-press/commit/7d15e0e13b3437b0c0d10b0853fc601b8307f573))
* **cli:** redact PII audit ledger cli dir ([#1866](https://github.com/mvanhorn/cli-printing-press/issues/1866)) ([56e8a37](https://github.com/mvanhorn/cli-printing-press/commit/56e8a3726be90fc84d1c68b756cdf591ee5f2440))
* **cli:** refresh stale staged binary before live-check ([#1910](https://github.com/mvanhorn/cli-printing-press/issues/1910)) ([6c681d0](https://github.com/mvanhorn/cli-printing-press/commit/6c681d052fe65191b946fed17fda1b2af5cd50f4))
* **cli:** render README narrative recipes ([#1928](https://github.com/mvanhorn/cli-printing-press/issues/1928)) ([67ba616](https://github.com/mvanhorn/cli-printing-press/commit/67ba616cc572509a16632a3e96405f010b3ea2ef))
* **cli:** skip auth-tagged resources in sync defaults ([#1939](https://github.com/mvanhorn/cli-printing-press/issues/1939)) ([f7e5424](https://github.com/mvanhorn/cli-printing-press/commit/f7e5424bf2a321fb6c55c49d4df4fb526a2ef24c))
* **cli:** skip JSON cache for binary responses ([#1930](https://github.com/mvanhorn/cli-printing-press/issues/1930)) ([d9a58fb](https://github.com/mvanhorn/cli-printing-press/commit/d9a58fb160c2e65f92a3e4e5c92ddaa96218211c))
* **cli:** stub sync for HTML page-mode CLIs ([#1911](https://github.com/mvanhorn/cli-printing-press/issues/1911)) ([c8ff02c](https://github.com/mvanhorn/cli-printing-press/commit/c8ff02ce522544f492f33a3921b68dbc4a7ceeed))
* **cli:** support GeoJSON FeatureCollection in sync extraction ([#1831](https://github.com/mvanhorn/cli-printing-press/issues/1831)) ([4d42e3f](https://github.com/mvanhorn/cli-printing-press/commit/4d42e3f134931e3d1a9d134f6f13093d251db0eb))
* **cli:** sync narrative docs after dogfood ([#1934](https://github.com/mvanhorn/cli-printing-press/issues/1934)) ([db0017b](https://github.com/mvanhorn/cli-printing-press/commit/db0017b85f399d52eca18de5bb6fac19de63c45f))
* **cli:** sync-param-drop gate detects named-map call argument ([#1874](https://github.com/mvanhorn/cli-printing-press/issues/1874)) ([e0ffa42](https://github.com/mvanhorn/cli-printing-press/commit/e0ffa4214efd511f5c3ab39864997f02007f352f))
* **cli:** use narrative commands for generated examples ([#1897](https://github.com/mvanhorn/cli-printing-press/issues/1897)) ([2c7cbba](https://github.com/mvanhorn/cli-printing-press/commit/2c7cbba2b2fa194ad82a24161701e2dabbc5eced))
* **cli:** use Windows executable names for MCPB companions ([#1895](https://github.com/mvanhorn/cli-printing-press/issues/1895)) ([09a8df5](https://github.com/mvanhorn/cli-printing-press/commit/09a8df5125609dee7fba6388830d18be9bacdcaa))
* **skills:** drive reprint via regen-merge to preserve hand-authored novels ([#1872](https://github.com/mvanhorn/cli-printing-press/issues/1872)) ([eb4ce29](https://github.com/mvanhorn/cli-printing-press/commit/eb4ce29df77f28e2506da9d2034ce4c5e61857af))

## [4.12.0](https://github.com/mvanhorn/cli-printing-press/compare/v4.11.0...v4.12.0) (2026-05-22)


### Features

* **cli:** add sync freshness hints to generated CLIs ([#1819](https://github.com/mvanhorn/cli-printing-press/issues/1819)) ([5f6b1cf](https://github.com/mvanhorn/cli-printing-press/commit/5f6b1cf999960bd6b269838bef5b773fa806fe6b))
* **cli:** add sync-param-drop gate against browser-sniff capture ([#1644](https://github.com/mvanhorn/cli-printing-press/issues/1644)) ([c94e190](https://github.com/mvanhorn/cli-printing-press/commit/c94e1907155f209d60ac1c48842ff82d30b859ed))
* **cli:** add x-path-template-env-vars spec extension ([#1401](https://github.com/mvanhorn/cli-printing-press/issues/1401)) ([f83bd31](https://github.com/mvanhorn/cli-printing-press/commit/f83bd316dac59e456d5201bc45b770a02b8358e4))


### Bug Fixes

* **cli:** accept root mcp in OpenAPI parser ([#1777](https://github.com/mvanhorn/cli-printing-press/issues/1777)) ([4cea5a1](https://github.com/mvanhorn/cli-printing-press/commit/4cea5a1cb89ebdee3109b8bb6a1582b197045928))
* **cli:** add OAuth scope coverage dogfood check ([#1809](https://github.com/mvanhorn/cli-printing-press/issues/1809)) ([ceafd83](https://github.com/mvanhorn/cli-printing-press/commit/ceafd8351a4105d096f2782c52ced71b1b4a1982))
* **cli:** add verifier happy args annotations ([#1841](https://github.com/mvanhorn/cli-printing-press/issues/1841)) ([340d5a0](https://github.com/mvanhorn/cli-printing-press/commit/340d5a0b44d604c1aae4d32bb6e05450e884d751))
* **cli:** allow dogfood error path opt-outs ([#1839](https://github.com/mvanhorn/cli-printing-press/issues/1839)) ([c3a3313](https://github.com/mvanhorn/cli-printing-press/commit/c3a331337efec833ee924771ed43a9f0a5cf2acc))
* **cli:** credit cookie auth in scorecard ([#1787](https://github.com/mvanhorn/cli-printing-press/issues/1787)) ([b7481c0](https://github.com/mvanhorn/cli-printing-press/commit/b7481c06c74e546419aadbb4276d64a49cac1c80))
* **cli:** derive multispec resource names from titles ([#1792](https://github.com/mvanhorn/cli-printing-press/issues/1792)) ([428c720](https://github.com/mvanhorn/cli-printing-press/commit/428c7204063639dfe0e395d3a34b88cfe1b71566))
* **cli:** exclude hidden MCP endpoint mirrors from description gates ([#1826](https://github.com/mvanhorn/cli-printing-press/issues/1826)) ([c60982d](https://github.com/mvanhorn/cli-printing-press/commit/c60982d8f21c9f18ecf6db39ff31736afaa7622a))
* **cli:** honor HTML extraction in sync ([#1793](https://github.com/mvanhorn/cli-printing-press/issues/1793)) ([475a641](https://github.com/mvanhorn/cli-printing-press/commit/475a641f55aa102a97d61a7717bd31d63af54736))
* **cli:** improve generated parent command shorts ([#1805](https://github.com/mvanhorn/cli-printing-press/issues/1805)) ([231b957](https://github.com/mvanhorn/cli-printing-press/commit/231b957fdc894b22b7879b546e4d83dab7680533))
* **cli:** improve generated parent grouper shorts ([#1794](https://github.com/mvanhorn/cli-printing-press/issues/1794)) ([f0f928c](https://github.com/mvanhorn/cli-printing-press/commit/f0f928cb4ebf546e1bac1086a1214441d56168d7))
* **cli:** merge compatible auth metadata across specs ([#1811](https://github.com/mvanhorn/cli-printing-press/issues/1811)) ([f183bd1](https://github.com/mvanhorn/cli-printing-press/commit/f183bd1d5a7f16d46e693e728bec0f16308d6e4c))
* **cli:** normalize FastAPI operation ids ([#1788](https://github.com/mvanhorn/cli-printing-press/issues/1788)) ([a85a397](https://github.com/mvanhorn/cli-printing-press/commit/a85a397eb7ddc8cb70177fc08687dda0a35f3a64))
* **cli:** preflight framework narrative flags ([#1780](https://github.com/mvanhorn/cli-printing-press/issues/1780)) ([0d03604](https://github.com/mvanhorn/cli-printing-press/commit/0d0360405aebd8b51bd5f817233949efe90d513c))
* **cli:** preserve all-apiKey composed auth headers ([#1828](https://github.com/mvanhorn/cli-printing-press/issues/1828)) ([fa8e109](https://github.com/mvanhorn/cli-printing-press/commit/fa8e10995505cca671d29cc3b86f395562d3f956))
* **cli:** preserve generated catalog display names ([#1855](https://github.com/mvanhorn/cli-printing-press/issues/1855)) ([a6d956e](https://github.com/mvanhorn/cli-printing-press/commit/a6d956e1123fc3e0b380b4b5f06b931519a300de))
* **cli:** respect cobratree framework command depth ([#1806](https://github.com/mvanhorn/cli-printing-press/issues/1806)) ([c0e45d1](https://github.com/mvanhorn/cli-printing-press/commit/c0e45d1449ace468ba0d2ec49edadfcef1da7803))
* **cli:** retry busy dogfood command execs ([#1785](https://github.com/mvanhorn/cli-printing-press/issues/1785)) ([fa6d2bb](https://github.com/mvanhorn/cli-printing-press/commit/fa6d2bb48ddc6554ad8f932d2541723821347514))
* **cli:** scope framework collision renames ([#1778](https://github.com/mvanhorn/cli-printing-press/issues/1778)) ([16a01ca](https://github.com/mvanhorn/cli-printing-press/commit/16a01ca1e3e51fc3ffc571c3c3a14a803ff13e6e))
* **cli:** select OpenAPI auth by operation usage ([#1816](https://github.com/mvanhorn/cli-printing-press/issues/1816)) ([21dadbd](https://github.com/mvanhorn/cli-printing-press/commit/21dadbdc9ec7861c38f1e716c49fdfe038633e63))
* **cli:** serialize DELETE request bodies ([#1842](https://github.com/mvanhorn/cli-printing-press/issues/1842)) ([25c503b](https://github.com/mvanhorn/cli-printing-press/commit/25c503b5b090029af9cbde98f73a0156371e273e))
* **cli:** set generation category for non-catalog runs ([#1817](https://github.com/mvanhorn/cli-printing-press/issues/1817)) ([730cd53](https://github.com/mvanhorn/cli-printing-press/commit/730cd536e2f9aafec1ac324f51548e8f7f5f37f9))
* **cli:** stub missing local schema refs in lenient mode ([#1827](https://github.com/mvanhorn/cli-printing-press/issues/1827)) ([2f96ee9](https://github.com/mvanhorn/cli-printing-press/commit/2f96ee92b26db071e5721e3267e981364fcc86b1))
* **cli:** support id-walk sync for capped POST queries ([#1847](https://github.com/mvanhorn/cli-printing-press/issues/1847)) ([95d28c1](https://github.com/mvanhorn/cli-printing-press/commit/95d28c13a4e8b6ad0f8b363fb274c989feb0262b))
* **cli:** tighten scoreTypeFidelity flag-decl regex and drop MarkFlagRequired reward ([#1795](https://github.com/mvanhorn/cli-printing-press/issues/1795)) ([ef00173](https://github.com/mvanhorn/cli-printing-press/commit/ef001733dc537ea574a5253907be53ee15a9dfea))
* **cli:** write mutation responses to local store ([#1818](https://github.com/mvanhorn/cli-printing-press/issues/1818)) ([8de833d](https://github.com/mvanhorn/cli-printing-press/commit/8de833d01112e90f5fe68cdb7e8e0c63b96f1f15))
* **mcp:** correct code-orchestration write-body shapes (double-marshal, query/body split, bare-array) ([#1662](https://github.com/mvanhorn/cli-printing-press/issues/1662)) ([2bbccd6](https://github.com/mvanhorn/cli-printing-press/commit/2bbccd6e49ce3b79fb586b9baf8245c8df0bb1d8))

## [4.11.0](https://github.com/mvanhorn/cli-printing-press/compare/v4.10.1...v4.11.0) (2026-05-21)


### Features

* **cli:** add Supercut Platform catalog entry ([#1731](https://github.com/mvanhorn/cli-printing-press/issues/1731)) ([52c4afc](https://github.com/mvanhorn/cli-printing-press/commit/52c4afcac59e945634499952601d3238bc17c98b))


### Bug Fixes

* **cli:** choose freshest live-check binary ([#1759](https://github.com/mvanhorn/cli-printing-press/issues/1759)) ([0251e59](https://github.com/mvanhorn/cli-printing-press/commit/0251e593e38d7c0682edaf49314adb8ca3628515))
* **cli:** classify captcha prechecks as auth context ([#1747](https://github.com/mvanhorn/cli-printing-press/issues/1747)) ([11e9843](https://github.com/mvanhorn/cli-printing-press/commit/11e98435a411fce0a81dbfa0b807465c73986292))
* **cli:** filter telemetry hosts from browser sniff ([#1766](https://github.com/mvanhorn/cli-printing-press/issues/1766)) ([3a7636b](https://github.com/mvanhorn/cli-printing-press/commit/3a7636bd7e3995389f2868c02aedde271fca67a7))
* **cli:** flatten nested allOf body schemas ([#1761](https://github.com/mvanhorn/cli-printing-press/issues/1761)) ([7bf0698](https://github.com/mvanhorn/cli-printing-press/commit/7bf0698e5ea8e05f8725480801f1eeceb25369b5))
* **cli:** handle setlist retro generator gaps ([#1744](https://github.com/mvanhorn/cli-printing-press/issues/1744)) ([da72283](https://github.com/mvanhorn/cli-printing-press/commit/da7228353e9fb266488b1789fea8ad30f34ffdf0))
* **cli:** inject field selectors during sync ([#1763](https://github.com/mvanhorn/cli-printing-press/issues/1763)) ([93ef629](https://github.com/mvanhorn/cli-printing-press/commit/93ef629e5fdb5ab4aa93e44cff614e94129b92cf))
* **cli:** populate dependent typed parent fks ([#1760](https://github.com/mvanhorn/cli-printing-press/issues/1760)) ([93fd8e6](https://github.com/mvanhorn/cli-printing-press/commit/93fd8e66f6009b37284dbc27876e16b99f2846ec))
* **cli:** preserve required global query params ([#1756](https://github.com/mvanhorn/cli-printing-press/issues/1756)) ([99596b1](https://github.com/mvanhorn/cli-printing-press/commit/99596b1633e127a2a93f749bf14ab3c0998da3af))
* **cli:** reject placeholder auth credentials ([#1767](https://github.com/mvanhorn/cli-printing-press/issues/1767)) ([93b2e1d](https://github.com/mvanhorn/cli-printing-press/commit/93b2e1d29e5d84a193e633aa9985d5689c045991))
* **cli:** score reachable scorer surfaces ([#1764](https://github.com/mvanhorn/cli-printing-press/issues/1764)) ([0c13b00](https://github.com/mvanhorn/cli-printing-press/commit/0c13b00979fe1ccf73165ad49d54823c658f1ee4))
* **cli:** seed narrative template env placeholders ([#1758](https://github.com/mvanhorn/cli-printing-press/issues/1758)) ([a18591a](https://github.com/mvanhorn/cli-printing-press/commit/a18591a414dc73b18eeec31475c4e30e300a32be))
* **cli:** thread generated client context ([#1769](https://github.com/mvanhorn/cli-printing-press/issues/1769)) ([1119cda](https://github.com/mvanhorn/cli-printing-press/commit/1119cda8a74403b4cc040747c7e7f81c58163d8c))
* **skills:** capture Request object browser-sniff bodies ([#1745](https://github.com/mvanhorn/cli-printing-press/issues/1745)) ([b0176d2](https://github.com/mvanhorn/cli-printing-press/commit/b0176d264c5bb7551d0ed32f6eb4ca1c1649e69f))

## [4.10.1](https://github.com/mvanhorn/cli-printing-press/compare/v4.10.0...v4.10.1) (2026-05-21)


### Bug Fixes

* **cli:** allow first force generation without go.mod snapshot ([#1723](https://github.com/mvanhorn/cli-printing-press/issues/1723)) ([aa12f66](https://github.com/mvanhorn/cli-printing-press/commit/aa12f6671c159297b5b87f274d40450aad68f0d1))
* **cli:** preserve generated catalog descriptions ([#1730](https://github.com/mvanhorn/cli-printing-press/issues/1730)) ([482192f](https://github.com/mvanhorn/cli-printing-press/commit/482192f0e945e9b46f9354b1e8560a06bc4ec052))
* **cli:** support POST list sync and auth scoring ([#1729](https://github.com/mvanhorn/cli-printing-press/issues/1729)) ([b29d90a](https://github.com/mvanhorn/cli-printing-press/commit/b29d90a922dbd9f62d61b6593b5720e479c15a51))
* **cli:** surface unsafe paginated all truncation ([#1727](https://github.com/mvanhorn/cli-printing-press/issues/1727)) ([0c43075](https://github.com/mvanhorn/cli-printing-press/commit/0c430755ee82ed588c0f743de0cbcfe94f0aea13))

## [4.10.0](https://github.com/mvanhorn/cli-printing-press/compare/v4.9.0...v4.10.0) (2026-05-21)


### Features

* **ci:** mirror supply-chain hardening from printing-press-library ([#1619](https://github.com/mvanhorn/cli-printing-press/issues/1619)) ([f671b7b](https://github.com/mvanhorn/cli-printing-press/commit/f671b7ba9a572a2540f10931b2d794eebc16cb53))
* **cli:** add auth set-token escape hatch for cookie-auth CLIs ([#1641](https://github.com/mvanhorn/cli-printing-press/issues/1641)) ([275646a](https://github.com/mvanhorn/cli-printing-press/commit/275646a3f87e8f9f7f088ef2cb048a068eabb22b))
* **cli:** add cliutil.LooksLikeJWT shared validator with length floor ([#1602](https://github.com/mvanhorn/cli-printing-press/issues/1602)) ([b5b1dd5](https://github.com/mvanhorn/cli-printing-press/commit/b5b1dd594989b0335adebcae66930237b6cc2e03))
* **cli:** add Quo (formerly OpenPhone) catalog entry and OpenAPI spec ([#1597](https://github.com/mvanhorn/cli-printing-press/issues/1597)) ([1a24381](https://github.com/mvanhorn/cli-printing-press/commit/1a243810db5ff3c379385271b67030beb1a6e556))
* **cli:** add sync --path-context KEY=VAL runtime override for template placeholders ([#1631](https://github.com/mvanhorn/cli-printing-press/issues/1631)) ([766354e](https://github.com/mvanhorn/cli-printing-press/commit/766354e6b82c5a515c0eaceb4f921348b8f26365))
* **cli:** default mcp.transport=[stdio, http] for small APIs ([#1629](https://github.com/mvanhorn/cli-printing-press/issues/1629)) ([14bc18a](https://github.com/mvanhorn/cli-printing-press/commit/14bc18a4dcba96b219a6b82d17321c5a21636ad2))
* **cli:** detect Auth0 SPA in-memory auth + emit CDP extractor ([#1645](https://github.com/mvanhorn/cli-printing-press/issues/1645)) ([015478b](https://github.com/mvanhorn/cli-printing-press/commit/015478bfe179f2d7bfd26e850466c800e7f71540))
* **cli:** flag empty defaultSyncResources at dogfood time ([#1633](https://github.com/mvanhorn/cli-printing-press/issues/1633)) ([042a78d](https://github.com/mvanhorn/cli-printing-press/commit/042a78d494058fb5d80000bba1b1cfc379e1d2b2))
* **cli:** GraphQL credential probe in doctor + actionable copy ([#1701](https://github.com/mvanhorn/cli-printing-press/issues/1701)) ([b534884](https://github.com/mvanhorn/cli-printing-press/commit/b534884fcd9707d1cdcd72698df6f0312efdbfdb))
* **cli:** press-auth companion for cookie capture + generated CLI integration ([#1466](https://github.com/mvanhorn/cli-printing-press/issues/1466)) ([89816d4](https://github.com/mvanhorn/cli-printing-press/commit/89816d49a635bdbc3805b659b027459374a39037))
* **cli:** rename generator binary to cli-printing-press ([#1722](https://github.com/mvanhorn/cli-printing-press/issues/1722)) ([7c4bedd](https://github.com/mvanhorn/cli-printing-press/commit/7c4beddbb622f4d5419ef28efb230ba1d0c19686))
* **skills:** check public library for existing CLI before generating ([#1650](https://github.com/mvanhorn/cli-printing-press/issues/1650)) ([81f07e5](https://github.com/mvanhorn/cli-printing-press/commit/81f07e5342b27b4d0610c7d41db55b5a854d0f95))


### Bug Fixes

* **ci:** allow queue drafts to skip Greptile ([96bc822](https://github.com/mvanhorn/cli-printing-press/commit/96bc822e27be1cf13f372cbf28f341d5e9a71539))
* **ci:** trust queued Greptile gate ([9a47433](https://github.com/mvanhorn/cli-printing-press/commit/9a474335592f2c645754801a145c6cc492edc658))
* **cli:** correct live-check and helper emission ([#1637](https://github.com/mvanhorn/cli-printing-press/issues/1637)) ([8c95570](https://github.com/mvanhorn/cli-printing-press/commit/8c95570116f6c223f77c5b89956073c1d0e7f81d))
* **cli:** create llm prompt temp files privately ([#1674](https://github.com/mvanhorn/cli-printing-press/issues/1674)) ([c5d8682](https://github.com/mvanhorn/cli-printing-press/commit/c5d86825c65118f377d3515e1ce0ef32750c447f))
* **cli:** detect path params in browser-sniff URL grouper ([#1657](https://github.com/mvanhorn/cli-printing-press/issues/1657)) ([2c3271f](https://github.com/mvanhorn/cli-printing-press/commit/2c3271fcf95dc9ede77864f3c78bfde9bb4b6476))
* **cli:** dogfood prefers bundled &lt;dir&gt;/spec.yaml over caller --spec ([#1625](https://github.com/mvanhorn/cli-printing-press/issues/1625)) ([17cd277](https://github.com/mvanhorn/cli-printing-press/commit/17cd27742e434fbf691001dc98702494dde0d4cd))
* **cli:** emit structured cache_warning JSON on auto-refresh failure ([#1632](https://github.com/mvanhorn/cli-printing-press/issues/1632)) ([187b699](https://github.com/mvanhorn/cli-printing-press/commit/187b69928fe8a11d6c045d3fbc9bb63016296c02))
* **cli:** gate ship plans on agent readiness ([#1638](https://github.com/mvanhorn/cli-printing-press/issues/1638)) ([ed17d30](https://github.com/mvanhorn/cli-printing-press/commit/ed17d302df9dafd7b3bfc3ded8099ce1525ec77d))
* **cli:** handle binary-only response endpoints (Accept + base64 envelope) ([#1574](https://github.com/mvanhorn/cli-printing-press/issues/1574)) ([d817807](https://github.com/mvanhorn/cli-printing-press/commit/d8178077ede31c6873f63ac2ae192edeb0e18c97))
* **cli:** harden browser-sniff PII placeholders ([#1639](https://github.com/mvanhorn/cli-printing-press/issues/1639)) ([7eee7df](https://github.com/mvanhorn/cli-printing-press/commit/7eee7dfcb1d294feeeae6a013b4fd9d5bcaaec61))
* **cli:** harden manifest-gen remote spec loader against hangs and silent truncation ([#1699](https://github.com/mvanhorn/cli-printing-press/issues/1699)) ([aecf495](https://github.com/mvanhorn/cli-printing-press/commit/aecf495af1b8be4394d0f41253f3b2cdc639e370))
* **cli:** make live-dogfood runner enum-aware and resilient to empty outcomes ([#1656](https://github.com/mvanhorn/cli-printing-press/issues/1656)) ([cda9b43](https://github.com/mvanhorn/cli-printing-press/commit/cda9b43b60469e072832b5883911795b9faeb44f))
* **cli:** polish press-auth output and make spec warnings testable ([#1710](https://github.com/mvanhorn/cli-printing-press/issues/1710)) ([61f919a](https://github.com/mvanhorn/cli-printing-press/commit/61f919aa13131537828bad36de78eab5e6856aac))
* **cli:** redact shipcheck api key in json ([#1673](https://github.com/mvanhorn/cli-printing-press/issues/1673)) ([4093ef3](https://github.com/mvanhorn/cli-printing-press/commit/4093ef3eef08ad8a88b6ebf16b42ce817a16e113))
* **cli:** require documented flags in verify-skill ([#1707](https://github.com/mvanhorn/cli-printing-press/issues/1707)) ([595ed9e](https://github.com/mvanhorn/cli-printing-press/commit/595ed9ec92aa44146fd0f230484f3af1c1fe0abd))
* **cli:** resolve shipcheck CLI binary name from .printing-press.json ([#1700](https://github.com/mvanhorn/cli-printing-press/issues/1700)) ([9eff1e2](https://github.com/mvanhorn/cli-printing-press/commit/9eff1e208a5b0238d990f1e0774ae40afc8d5ceb))
* **cli:** return failure exit from verify json ([#1693](https://github.com/mvanhorn/cli-printing-press/issues/1693)) ([5261ae7](https://github.com/mvanhorn/cli-printing-press/commit/5261ae735caee1b3ccf541d400636491c9a6730a))
* **cli:** route Swagger 2.0 specs through openapi2conv to avoid OOM on circular refs ([#1630](https://github.com/mvanhorn/cli-printing-press/issues/1630)) ([e798f67](https://github.com/mvanhorn/cli-printing-press/commit/e798f6716a184910bc5e00fa58ddcecbf9e97c9a))
* **cli:** sync pagination, auth aliases, narrative timing, and codex prompt ([#1341](https://github.com/mvanhorn/cli-printing-press/issues/1341)) ([ac3e15b](https://github.com/mvanhorn/cli-printing-press/commit/ac3e15b4e2665106f3c524d60622aed198b4f098))
* **cli:** verify-mode HTTP-verb short-circuit + N1 envelope + doctor + read-only POST bypass ([#1398](https://github.com/mvanhorn/cli-printing-press/issues/1398)) ([75e4d1b](https://github.com/mvanhorn/cli-printing-press/commit/75e4d1b9245ad8fd7b9684b5e75bc910a66d17d1))
* **skills:** Clarify publish skill's PR review loop and terminal state ([#1713](https://github.com/mvanhorn/cli-printing-press/issues/1713)) ([6870329](https://github.com/mvanhorn/cli-printing-press/commit/68703295ffea3f5a6200b1d6b01cf53f70ad7384))
* **skills:** reprint discovers and carries prior patches ([#1649](https://github.com/mvanhorn/cli-printing-press/issues/1649)) ([f9bb82c](https://github.com/mvanhorn/cli-printing-press/commit/f9bb82c892c5628fdb73ae950841d486dc865ca0))
* **skills:** stop phase 4.95 code review from being skipped ([#1655](https://github.com/mvanhorn/cli-printing-press/issues/1655)) ([5cb39a6](https://github.com/mvanhorn/cli-printing-press/commit/5cb39a697616696dd4ea163b10298d0296726cbe))

## [4.9.0](https://github.com/mvanhorn/cli-printing-press/compare/v4.8.0...v4.9.0) (2026-05-18)


### Features

* **catalog:** add OpenRouteService under new maps category ([#1513](https://github.com/mvanhorn/cli-printing-press/issues/1513)) ([43c90ba](https://github.com/mvanhorn/cli-printing-press/commit/43c90ba03553c41837c6959c13d99aafb41d1c8e))
* **ci:** surface unresolved review threads as a status check ([786848e](https://github.com/mvanhorn/cli-printing-press/commit/786848e9882055a626082bab58763aafb371edfe))
* **ci:** surface unresolved review threads as a status check ([21ae8a5](https://github.com/mvanhorn/cli-printing-press/commit/21ae8a5407c27d1984c2db7ae917d2903e56fe00))
* **cli:** add --auth-preference flag for catalog-driven scheme selection ([#865](https://github.com/mvanhorn/cli-printing-press/issues/865)) ([34ca814](https://github.com/mvanhorn/cli-printing-press/commit/34ca81497f6e46ee4fe5f92a30f6f3f15d498e27))
* **cli:** catalog auth_env_vars declares canonical credential env vars ([#1508](https://github.com/mvanhorn/cli-printing-press/issues/1508)) ([fe4a5ec](https://github.com/mvanhorn/cli-printing-press/commit/fe4a5ec38c5e379c1da491725b3e5b6675e3f094))
* **cli:** emit .printing-press-patches.json on every generate ([#1590](https://github.com/mvanhorn/cli-printing-press/issues/1590)) ([c3e1b87](https://github.com/mvanhorn/cli-printing-press/commit/c3e1b87aa90bbca99a3989fc897f9bd10454a340))
* **cli:** expose PRINTING_PRESS_DOGFOOD env signal for long-running commands ([#1493](https://github.com/mvanhorn/cli-printing-press/issues/1493)) ([96fca08](https://github.com/mvanhorn/cli-printing-press/commit/96fca0815d80417d8a3b046ba20e822aa7a792d6))
* **skills:** add /printing-press-amend skill — dogfood + direct-input modes ([#1490](https://github.com/mvanhorn/cli-printing-press/issues/1490)) ([c31632b](https://github.com/mvanhorn/cli-printing-press/commit/c31632b3fbb2fb918e1f29cf0ca1b72a35ca1d56))


### Bug Fixes

* **ci:** address Greptile review on conversation-resolution check ([f55f57a](https://github.com/mvanhorn/cli-printing-press/commit/f55f57a8d908c5555d4aa3d989fb9ec56abf6b0a))
* **ci:** cancel-in-progress=false so cancelled check_runs don't pile up ([906928d](https://github.com/mvanhorn/cli-printing-press/commit/906928d06f63217b56cc3d2948c8809da95b205d))
* **ci:** cancel-in-progress=false so cancelled check_runs don't pile up ([80ba507](https://github.com/mvanhorn/cli-printing-press/commit/80ba507ec22a19d060dbfb487937ceb98ce12a4a))
* **ci:** close jq if-then-else with end so &gt;10-thread path doesn't crash ([f7365c2](https://github.com/mvanhorn/cli-printing-press/commit/f7365c2602d828bae4352c9f316da496d8d36ce1))
* **ci:** conversation-resolution check race + duplicate check_run ([264617a](https://github.com/mvanhorn/cli-printing-press/commit/264617a19094d30be17661bfa5217764a8f80707))
* **ci:** conversation-resolution check race + duplicate check_run ([162d9a3](https://github.com/mvanhorn/cli-printing-press/commit/162d9a32bf983fb88a1df1fe75a61a197090626a))
* **ci:** require all review threads resolved before queue + merge ([a8fa80a](https://github.com/mvanhorn/cli-printing-press/commit/a8fa80a387d532159606f7beb56003fedcdbcbb8))
* **ci:** scope settle delay to synchronize, strip head_sha from PATCH ([5b8a814](https://github.com/mvanhorn/cli-printing-press/commit/5b8a814110264b9e8f4161fdc4617dde36e0df63))
* **ci:** use job exit-code instead of POST/PATCH for check_run state ([9433241](https://github.com/mvanhorn/cli-printing-press/commit/9433241780080b9aa21e0fd23244a1034a5533e0))
* **cli:** always emit boolean body fields, no zero-guard ([#1494](https://github.com/mvanhorn/cli-printing-press/issues/1494)) ([9d3050b](https://github.com/mvanhorn/cli-printing-press/commit/9d3050b5a3a356eaceb0b9cb157bcf9dd69aa1e6))
* **cli:** apply Bearer prefix in composed and cookie AuthHeader paths ([#1500](https://github.com/mvanhorn/cli-printing-press/issues/1500)) ([ca4898a](https://github.com/mvanhorn/cli-printing-press/commit/ca4898a396731026e1f9e33497d966ebf9d2165d))
* **cli:** cap body-flag recursion depth to prevent compiler OOM ([#1522](https://github.com/mvanhorn/cli-printing-press/issues/1522)) ([82ad787](https://github.com/mvanhorn/cli-printing-press/commit/82ad787b8a38591e611fdc968630cda45a27e28f))
* **cli:** detect integer page paginator and advance numerically in sync ([#1519](https://github.com/mvanhorn/cli-printing-press/issues/1519)) ([0c3c8bc](https://github.com/mvanhorn/cli-printing-press/commit/0c3c8bccc55fdb8a63693927eaa3dada519195b0))
* **cli:** detect partial-failure response shape in :mutate handlers, add --allow-partial-failure flag ([#1360](https://github.com/mvanhorn/cli-printing-press/issues/1360)) ([875d64e](https://github.com/mvanhorn/cli-printing-press/commit/875d64e9c238f937223ae8eadb6ba0f76c20d0a3))
* **cli:** drive browser-sniff http_transport from HAR HTTP-version distribution ([#1540](https://github.com/mvanhorn/cli-printing-press/issues/1540)) ([833213f](https://github.com/mvanhorn/cli-printing-press/commit/833213ff69318e47017d87d31030d4e0a08b6e12))
* **cli:** drop // PATCH marker instruction from generated AGENTS.md ([caa6880](https://github.com/mvanhorn/cli-printing-press/commit/caa68801e2c387f89489f9c32c6bd78a1526dcc4))
* **cli:** drop MCP code-orch handler pre-marshal that base64-encoded mutating bodies ([#1521](https://github.com/mvanhorn/cli-printing-press/issues/1521)) ([ff56237](https://github.com/mvanhorn/cli-printing-press/commit/ff562375ba7aaf413707b14d6debb0dd7bd11388))
* **cli:** exempt vendor OpenAPI/Swagger source under .manuscripts from PII scan ([#1496](https://github.com/mvanhorn/cli-printing-press/issues/1496)) ([dc0651a](https://github.com/mvanhorn/cli-printing-press/commit/dc0651ab8bf2cb1f9060b14a31de836df7f91ef3))
* **cli:** filter auth login --chrome cookies by spec auth.cookies allowlist ([#1518](https://github.com/mvanhorn/cli-printing-press/issues/1518)) ([9541740](https://github.com/mvanhorn/cli-printing-press/commit/954174014101d225f83acae04854a7cb4f234936))
* **cli:** match both singular and plural required-flag cobra outputs ([#1413](https://github.com/mvanhorn/cli-printing-press/issues/1413)) ([f5ea270](https://github.com/mvanhorn/cli-printing-press/commit/f5ea270df7583160348d1d4f83fe4798fbde153b))
* **cli:** move extra_commands SKILL.md block to its own top-level section ([#1524](https://github.com/mvanhorn/cli-printing-press/issues/1524)) ([c10d0b5](https://github.com/mvanhorn/cli-printing-press/commit/c10d0b553823e8a63428ed8473d36182c7de1d48))
* **cli:** preserve body/content payload on single-object compact path ([#1509](https://github.com/mvanhorn/cli-printing-press/issues/1509)) ([c7e1b82](https://github.com/mvanhorn/cli-printing-press/commit/c7e1b82b85ef47df1a011100fcb31a42a095b7ac))
* **cli:** refresh auth on redirects via CheckRedirect ([#1497](https://github.com/mvanhorn/cli-printing-press/issues/1497)) ([f3f7043](https://github.com/mvanhorn/cli-printing-press/commit/f3f7043558971d098960ee3f66c3548b7d0b4b15))
* **cli:** route promoted-command file stem through safeResourceFileStem ([#1489](https://github.com/mvanhorn/cli-printing-press/issues/1489)) ([#1506](https://github.com/mvanhorn/cli-printing-press/issues/1506)) ([bf7f346](https://github.com/mvanhorn/cli-printing-press/commit/bf7f346537502883bed2afede9db9d7bac06d915))
* **cli:** scope-aware sync --param dispatch with --global-param fallback ([#1529](https://github.com/mvanhorn/cli-printing-press/issues/1529)) ([4e304df](https://github.com/mvanhorn/cli-printing-press/commit/4e304df5654e4432b48d04f74a28e0e4506108d8))
* **cli:** synthesize undeclared path placeholders in OpenAPI parser ([#1510](https://github.com/mvanhorn/cli-printing-press/issues/1510)) ([bf9014d](https://github.com/mvanhorn/cli-printing-press/commit/bf9014d45ade16baacab07829c784448d19073c6))
* **cli:** validate-narrative splits piped recipes and strips redirects ([#1517](https://github.com/mvanhorn/cli-printing-press/issues/1517)) ([62838d0](https://github.com/mvanhorn/cli-printing-press/commit/62838d047cde2b1ffbd440f54069fbc9b765a584))
* **cli:** writeThroughCache caches single-object responses keyed by non-id PKs ([#1531](https://github.com/mvanhorn/cli-printing-press/issues/1531)) ([c96ac5d](https://github.com/mvanhorn/cli-printing-press/commit/c96ac5da6a9f7b58146ad45ee569c30d9f0ce85c))
* **scorer:** resolve binary at build/stage/bin/ before cliDir ([#1150](https://github.com/mvanhorn/cli-printing-press/issues/1150)) ([#1412](https://github.com/mvanhorn/cli-printing-press/issues/1412)) ([2abbfec](https://github.com/mvanhorn/cli-printing-press/commit/2abbfec04826d9763877490df1612d433f2debce))
* **skills:** add NULL-safe SQL scan guidance to Phase 3 store-query starter ([bca9866](https://github.com/mvanhorn/cli-printing-press/commit/bca9866e8c00e48e40d6a4b56c960da3435a96b4))
* **skills:** add NULL-safe SQL scan guidance to Phase 3 store-query starter ([7396d66](https://github.com/mvanhorn/cli-printing-press/commit/7396d66fe58eaec3c5b557748a07124ef12f066b))
* **skills:** drop cli-skills mirror regen from publish flow ([#1502](https://github.com/mvanhorn/cli-printing-press/issues/1502)) ([f64e991](https://github.com/mvanhorn/cli-printing-press/commit/f64e9913ff259760300b76c01db2e6f1b4ecd4e9))
* **skills:** require codex completion marker before treating delegation as success ([#1523](https://github.com/mvanhorn/cli-printing-press/issues/1523)) ([9963794](https://github.com/mvanhorn/cli-printing-press/commit/996379417bef8df70441a8f180652d6267dbcda4))
* **skills:** retro skill scrubs secrets and PII before posting issue bodies ([#1595](https://github.com/mvanhorn/cli-printing-press/issues/1595)) ([fef027f](https://github.com/mvanhorn/cli-printing-press/commit/fef027fe2e41bba5c695463d86869dbfe985381c))
* **skills:** split SQL example out of go-fenced block in NULL-safe scans guidance ([63f745b](https://github.com/mvanhorn/cli-printing-press/commit/63f745b879731b7e3225b00c020f4d586ca06021))
* **skills:** surface novel-feature hand-code scope at Phase Gate 1.5 ([#1501](https://github.com/mvanhorn/cli-printing-press/issues/1501)) ([793d5b2](https://github.com/mvanhorn/cli-printing-press/commit/793d5b272608d95ff65a4c83d669be2ab1e0ba10))

## [4.8.0](https://github.com/mvanhorn/cli-printing-press/compare/v4.7.0...v4.8.0) (2026-05-16)


### Features

* **cli:** add ElevenLabs generation support ([#1307](https://github.com/mvanhorn/cli-printing-press/issues/1307)) ([5f43de5](https://github.com/mvanhorn/cli-printing-press/commit/5f43de505252859e2ebbe846a4ecffb598e7655d))


### Bug Fixes

* **ci:** switch queue update_method to merge so fork PRs can queue ([#1492](https://github.com/mvanhorn/cli-printing-press/issues/1492)) ([2d0ad5a](https://github.com/mvanhorn/cli-printing-press/commit/2d0ad5ae7a6007034d8e743bebc8515383490aa3))
* **cli:** doctor preserves configured User-Agent when API uses UA as auth credential ([#1361](https://github.com/mvanhorn/cli-printing-press/issues/1361)) ([3bb6d37](https://github.com/mvanhorn/cli-printing-press/commit/3bb6d37bfbe94e675051bc6c453dd32ad9985e07))
* **cli:** expand pm_stale timestamp-field list for non-updated_at APIs ([#1461](https://github.com/mvanhorn/cli-printing-press/issues/1461)) ([55f5b26](https://github.com/mvanhorn/cli-printing-press/commit/55f5b26b0a45e30c3917bd6b14c400e0c5450dad))
* **cli:** probe nested path-param leaves so verify catches `args[]` index bugs ([#1434](https://github.com/mvanhorn/cli-printing-press/issues/1434)) ([5460c32](https://github.com/mvanhorn/cli-printing-press/commit/5460c32aa10373023060fffeceb7f8ada1fcfd5f))
* **cli:** short-circuit sync on dry-run sentinel response ([#1471](https://github.com/mvanhorn/cli-printing-press/issues/1471)) ([f8d3ba8](https://github.com/mvanhorn/cli-printing-press/commit/f8d3ba803f0b483305737ef5259c8e09c3e26c86))
* **cli:** use Chrome User-Agent for synthetic/cookie/composed/session_handshake CLIs ([#1479](https://github.com/mvanhorn/cli-printing-press/issues/1479)) ([d7ccd85](https://github.com/mvanhorn/cli-printing-press/commit/d7ccd8502e41eca152f03e88985527ec2338b839)), closes [#1293](https://github.com/mvanhorn/cli-printing-press/issues/1293)

## [4.7.0](https://github.com/mvanhorn/cli-printing-press/compare/v4.6.1...v4.7.0) (2026-05-15)


### Features

* **cli:** surface per-endpoint evidence and confidence from browser-sniff ([#1397](https://github.com/mvanhorn/cli-printing-press/issues/1397)) ([f32214e](https://github.com/mvanhorn/cli-printing-press/commit/f32214ea6e80bc7f01ae0a51c06bca809a3b9ed9))
* **skills:** contain session-state.json outside DISCOVERY_DIR ([#1425](https://github.com/mvanhorn/cli-printing-press/issues/1425)) ([55b0f16](https://github.com/mvanhorn/cli-printing-press/commit/55b0f16bfb10fcd3ee948e9f87a73392909dc546))


### Bug Fixes

* **cli:** align publish-skill contract tests with api-slug staged path ([#1459](https://github.com/mvanhorn/cli-printing-press/issues/1459)) ([c57345a](https://github.com/mvanhorn/cli-printing-press/commit/c57345a147bfc77d9174468fd3ae2996d156f3f4))
* **cli:** archive merged spec for multi-spec generate runs ([#1432](https://github.com/mvanhorn/cli-printing-press/issues/1432)) ([44f1ff7](https://github.com/mvanhorn/cli-printing-press/commit/44f1ff7956099c218c7531f562d2fbddfa259659))
* **cli:** bypass response cache when polling async jobs ([#1429](https://github.com/mvanhorn/cli-printing-press/issues/1429)) ([acbb3df](https://github.com/mvanhorn/cli-printing-press/commit/acbb3dfe6938d4c820533d63bccfa271a30b1dce))
* **cli:** clear every emitted credential field in ClearTokens ([#1433](https://github.com/mvanhorn/cli-printing-press/issues/1433)) ([2bf7d1e](https://github.com/mvanhorn/cli-printing-press/commit/2bf7d1e759ee602d9ba5fc038dcd2da83c57f07e))
* **cli:** derive doctor auth.verify_path from me-shaped endpoint ([#1431](https://github.com/mvanhorn/cli-printing-press/issues/1431)) ([95f0350](https://github.com/mvanhorn/cli-printing-press/commit/95f0350152285207c99616b3d80869471af9957e)), closes [#1161](https://github.com/mvanhorn/cli-printing-press/issues/1161)
* **cli:** derive doctor probe path from auth.verify_path or me-shaped endpoint ([#1406](https://github.com/mvanhorn/cli-printing-press/issues/1406)) ([d022660](https://github.com/mvanhorn/cli-printing-press/commit/d0226602f7c6d68d59e4b25e87e6940ea48306e6)), closes [#1118](https://github.com/mvanhorn/cli-printing-press/issues/1118)
* **cli:** descend --select into list-envelope responses ([#1379](https://github.com/mvanhorn/cli-printing-press/issues/1379)) ([3bcb9d9](https://github.com/mvanhorn/cli-printing-press/commit/3bcb9d9009596fa273e1fa88da80fa06176fbcc6)), closes [#1143](https://github.com/mvanhorn/cli-printing-press/issues/1143)
* **cli:** drop shell line comments + skip happy_path on missing file fixtures ([#1378](https://github.com/mvanhorn/cli-printing-press/issues/1378)) ([295fd03](https://github.com/mvanhorn/cli-printing-press/commit/295fd03ff06ba754dc0c8a1775497a31f729a7de))
* **cli:** drop unused client import from param-less endpoint command files ([#911](https://github.com/mvanhorn/cli-printing-press/issues/911)) ([72adfef](https://github.com/mvanhorn/cli-printing-press/commit/72adfefd408b12f5d02b8e6aeef2ba85183b0859))
* **cli:** emit structured JSON error from parents invoked without a subcommand in --agent mode ([#1373](https://github.com/mvanhorn/cli-printing-press/issues/1373)) ([fb9125e](https://github.com/mvanhorn/cli-printing-press/commit/fb9125e86965b875d878c2b8460087b8656a896b))
* **cli:** emit WithNumber/WithBoolean for OpenAPI-parsed numeric MCP params ([#1372](https://github.com/mvanhorn/cli-printing-press/issues/1372)) ([1f1b0bd](https://github.com/mvanhorn/cli-printing-press/commit/1f1b0bdb20325f512dce35283d724602c72ebcdd))
* **cli:** extend verify-skill recipe scan to README.md ([#1430](https://github.com/mvanhorn/cli-printing-press/issues/1430)) ([73fe567](https://github.com/mvanhorn/cli-printing-press/commit/73fe56730b2670d8bcb01876c1e64e863f083570))
* **cli:** force sync concurrency=1 under PRINTING_PRESS_VERIFY to avoid SQLITE_BUSY ([#1441](https://github.com/mvanhorn/cli-printing-press/issues/1441)) ([b8b1b6a](https://github.com/mvanhorn/cli-printing-press/commit/b8b1b6a4d4b6a8f037b8dc3f78625a1b676ea402))
* **cli:** handle single-quoted args in validate-narrative shell tokenizer ([#1424](https://github.com/mvanhorn/cli-printing-press/issues/1424)) ([33a6668](https://github.com/mvanhorn/cli-printing-press/commit/33a6668bd88b48ae3f1bf8f2b55c0bdff42b1620)), closes [#1159](https://github.com/mvanhorn/cli-printing-press/issues/1159)
* **cli:** honor explicit "mcp:read-only": "false" in tools-audit ([#1485](https://github.com/mvanhorn/cli-printing-press/issues/1485)) ([ede5c35](https://github.com/mvanhorn/cli-printing-press/commit/ede5c3506e20891804df592e787943f581b41278)), closes [#891](https://github.com/mvanhorn/cli-printing-press/issues/891)
* **cli:** isolate typed-table upsert failures with per-item SAVEPOINT ([#1402](https://github.com/mvanhorn/cli-printing-press/issues/1402)) ([89f38c4](https://github.com/mvanhorn/cli-printing-press/commit/89f38c4ca977c63f5b3cc43ad41627d4f2467ff5))
* **cli:** kebab-case SKILL.md and README endpoint examples ([#1445](https://github.com/mvanhorn/cli-printing-press/issues/1445)) ([bd4dc64](https://github.com/mvanhorn/cli-printing-press/commit/bd4dc64175305b61e6cb4278bba3def0e16d68b4)), closes [#1270](https://github.com/mvanhorn/cli-printing-press/issues/1270)
* **cli:** make cookie-auth login --chrome work on Windows ([#1404](https://github.com/mvanhorn/cli-printing-press/issues/1404)) ([fdca9f7](https://github.com/mvanhorn/cli-printing-press/commit/fdca9f7aa3bd697e3153e4852d7905752cbf5d47))
* **cli:** pick gid/sid/uid before name in PK heuristic ([#1465](https://github.com/mvanhorn/cli-printing-press/issues/1465)) ([01c816d](https://github.com/mvanhorn/cli-printing-press/commit/01c816daf99dacab6a69333c263a003ccf49c956)), closes [#1394](https://github.com/mvanhorn/cli-printing-press/issues/1394)
* **cli:** populate printer + run_id in generated .printing-press.json ([#1442](https://github.com/mvanhorn/cli-printing-press/issues/1442)) ([a1098f1](https://github.com/mvanhorn/cli-printing-press/commit/a1098f1563e66fec8fa61a2b04c77cce5f041c00))
* **cli:** preserve OpenAPI param casing in detected pagination ([#1460](https://github.com/mvanhorn/cli-printing-press/issues/1460)) ([272d8e8](https://github.com/mvanhorn/cli-printing-press/commit/272d8e835a5513f1bef2af2b507462954bbcfba6)), closes [#1353](https://github.com/mvanhorn/cli-printing-press/issues/1353)
* **cli:** propagate PRINTING_PRESS_VERIFY=1 to browser-session-proof probe ([#1458](https://github.com/mvanhorn/cli-printing-press/issues/1458)) ([dbfc611](https://github.com/mvanhorn/cli-printing-press/commit/dbfc61186d17012579de116545ad884dc5fa5142)), closes [#1389](https://github.com/mvanhorn/cli-printing-press/issues/1389)
* **cli:** query generic resources_fts from search empty-type branch ([#1462](https://github.com/mvanhorn/cli-printing-press/issues/1462)) ([35e515c](https://github.com/mvanhorn/cli-printing-press/commit/35e515c12d6ca7dbb3366ba926086915997043ad)), closes [#1390](https://github.com/mvanhorn/cli-printing-press/issues/1390)
* **cli:** research-dir state.json api_name overrides info.title-derived name ([#1476](https://github.com/mvanhorn/cli-printing-press/issues/1476)) ([a9ffa08](https://github.com/mvanhorn/cli-printing-press/commit/a9ffa08f0d373b28406feb94acd6b1aacf11f819)), closes [#1375](https://github.com/mvanhorn/cli-printing-press/issues/1375)
* **cli:** scope HOME/XDG_CONFIG_HOME for verify/dogfood subprocesses ([#1416](https://github.com/mvanhorn/cli-printing-press/issues/1416)) ([c5a51ea](https://github.com/mvanhorn/cli-printing-press/commit/c5a51eadfede71058d5b4b8ab9c0ba7d17a7c82b))
* **cli:** stop generated main.go from printing every error twice ([#1411](https://github.com/mvanhorn/cli-printing-press/issues/1411)) ([46ad86b](https://github.com/mvanhorn/cli-printing-press/commit/46ad86b2b287dc18cbab40b5dfe0997a52e7a3be))
* **cli:** strip root-level binaries from staged publish tree ([#1449](https://github.com/mvanhorn/cli-printing-press/issues/1449)) ([096411c](https://github.com/mvanhorn/cli-printing-press/commit/096411cea4838d7c9dc27be4acfbc5bd4627a076))
* **cli:** substitute server-URL variables from env vars at runtime ([#1448](https://github.com/mvanhorn/cli-printing-press/issues/1448)) ([e36050f](https://github.com/mvanhorn/cli-printing-press/commit/e36050fb97d818fec71258fbb73d65533ac423fb)), closes [#1295](https://github.com/mvanhorn/cli-printing-press/issues/1295)
* **cli:** tighten phone-us PII regex to NANP-valid first digits ([#1405](https://github.com/mvanhorn/cli-printing-press/issues/1405)) ([1596497](https://github.com/mvanhorn/cli-printing-press/commit/15964974d79e0366e641ea1bbb90af11282c9f90))
* **cli:** validate &&-chained narrative recipes segment-by-segment ([#1447](https://github.com/mvanhorn/cli-printing-press/issues/1447)) ([bb5e2de](https://github.com/mvanhorn/cli-printing-press/commit/bb5e2defcb03a91235aa3a39415ea509a5b8dc3b)), closes [#1271](https://github.com/mvanhorn/cli-printing-press/issues/1271)
* **generator:** support single-env basic auth credentials ([#1381](https://github.com/mvanhorn/cli-printing-press/issues/1381)) ([ac83f0f](https://github.com/mvanhorn/cli-printing-press/commit/ac83f0fde393f00dda24638f7e1472643fd33ade))
* **skills:** canonicalize auth env var when slug-derived differs ([#1428](https://github.com/mvanhorn/cli-printing-press/issues/1428)) ([2988814](https://github.com/mvanhorn/cli-printing-press/commit/2988814e0585f9ede348ffbeb80c6cdf219f92d9)), closes [#1157](https://github.com/mvanhorn/cli-printing-press/issues/1157)
* **skills:** correct install-internal-skills.sh path to repo skills/ ([#1342](https://github.com/mvanhorn/cli-printing-press/issues/1342)) ([5a04aca](https://github.com/mvanhorn/cli-printing-press/commit/5a04aca6eede56b642a2b900283e7b41c2a99867)), closes [#869](https://github.com/mvanhorn/cli-printing-press/issues/869)
* **skills:** emit PRINTING_PRESS_BIN marker so later phases survive PATH non-inheritance ([#1399](https://github.com/mvanhorn/cli-printing-press/issues/1399)) ([2698690](https://github.com/mvanhorn/cli-printing-press/commit/2698690ee63c41016fa0f9f86470631ee55e0a8b))
* **skills:** use api-slug in publish Step 6 staged path ([#1444](https://github.com/mvanhorn/cli-printing-press/issues/1444)) ([ce3aeb4](https://github.com/mvanhorn/cli-printing-press/commit/ce3aeb4a209e76989fc8beb18c84da85ceb405f2)), closes [#1233](https://github.com/mvanhorn/cli-printing-press/issues/1233)

## [4.6.1](https://github.com/mvanhorn/cli-printing-press/compare/v4.6.0...v4.6.1) (2026-05-14)


### Bug Fixes

* **cli:** coerce number-typed --limit param to int ([#1370](https://github.com/mvanhorn/cli-printing-press/issues/1370)) ([80f5bc6](https://github.com/mvanhorn/cli-printing-press/commit/80f5bc6ca5d1c5aae2ca0880d19a5cd2396046ab)), closes [#1082](https://github.com/mvanhorn/cli-printing-press/issues/1082)
* **cli:** consume x-tenant-env-var so per-tenant sync paths ship populated ([#1368](https://github.com/mvanhorn/cli-printing-press/issues/1368)) ([51b758c](https://github.com/mvanhorn/cli-printing-press/commit/51b758c713f3c4252697d38d8355a9f3bb4b81f2))
* **cli:** detect cross-cutting novel features in dogfood scorer ([#1364](https://github.com/mvanhorn/cli-printing-press/issues/1364)) ([a601038](https://github.com/mvanhorn/cli-printing-press/commit/a60103844391e29535b12d8a416d558944086192))
* **cli:** drop MCP handler pre-marshal that base64-encoded mutating bodies ([#1357](https://github.com/mvanhorn/cli-printing-press/issues/1357)) ([1fc5f62](https://github.com/mvanhorn/cli-printing-press/commit/1fc5f62bef9c21716e27c092545ed2ca8c6f1aca))
* **cli:** percent-encode path param values in replacePathParam ([#1366](https://github.com/mvanhorn/cli-printing-press/issues/1366)) ([77fce43](https://github.com/mvanhorn/cli-printing-press/commit/77fce43f42bf32c3c9d286ed69b506d93b3635da))
* **cli:** refuse publish package --dest overlay when mirror diverges from source ([#1371](https://github.com/mvanhorn/cli-printing-press/issues/1371)) ([1ff6853](https://github.com/mvanhorn/cli-printing-press/commit/1ff685373907ac9966ceacdea7628a69edf7c235))
* **cli:** skip framework-auto-generated operationIds when deriving command names ([#1367](https://github.com/mvanhorn/cli-printing-press/issues/1367)) ([d697eb5](https://github.com/mvanhorn/cli-printing-press/commit/d697eb5ae9ee18804c77022996f71058c972982d))
* **cli:** strip embedded .git/ and .gitmodules in pipeline.CopyDir ([#1358](https://github.com/mvanhorn/cli-printing-press/issues/1358)) ([0f8dc98](https://github.com/mvanhorn/cli-printing-press/commit/0f8dc989616797be1c1a326dc6d420dc8426289d)), closes [#1304](https://github.com/mvanhorn/cli-printing-press/issues/1304)
* **cli:** wire composed apiKey + bearer sibling headers through parser, templates, and MCP manifest ([#1359](https://github.com/mvanhorn/cli-printing-press/issues/1359)) ([2437cab](https://github.com/mvanhorn/cli-printing-press/commit/2437cab76049ef0cbf81a6e49cf53595c31c8649)), closes [#1303](https://github.com/mvanhorn/cli-printing-press/issues/1303)

## [4.6.0](https://github.com/mvanhorn/cli-printing-press/compare/v4.5.2...v4.6.0) (2026-05-13)


### Features

* **cli:** emit fetchFull&lt;X&gt; companion for GET-embedded paged sub-resources ([#1301](https://github.com/mvanhorn/cli-printing-press/issues/1301)) ([900b335](https://github.com/mvanhorn/cli-printing-press/commit/900b335ce15efab58b8007a8cdb80de2de80ff4e))


### Bug Fixes

* **ci:** collapse Mergify two-step CI to enable in-place mode ([#1336](https://github.com/mvanhorn/cli-printing-press/issues/1336)) ([5ac1de7](https://github.com/mvanhorn/cli-printing-press/commit/5ac1de7c5823ef6b90a210a4ca2135ede7ae1449))
* **cli:** close ResolveByName json_extract injection + dedupe cacheKey AuthHeader call ([#1278](https://github.com/mvanhorn/cli-printing-press/issues/1278)) ([e21feae](https://github.com/mvanhorn/cli-printing-press/commit/e21feae6854f272d8a1d3c12d581da49ec739fb9))
* **cli:** hide const-default query flags from --help ([#1287](https://github.com/mvanhorn/cli-printing-press/issues/1287)) ([ee3638b](https://github.com/mvanhorn/cli-printing-press/commit/ee3638b2e846e38c3fa7f23e4de82aac6d781332)), closes [#1273](https://github.com/mvanhorn/cli-printing-press/issues/1273)
* **cli:** promote write-endpoint params to body when body is empty ([#791](https://github.com/mvanhorn/cli-printing-press/issues/791)) ([ff13530](https://github.com/mvanhorn/cli-printing-press/commit/ff1353099491ede69491a2694763ec88c856eccc))
* **cli:** propagate --timeout to surf transport ResponseHeaderTimeout ([#1212](https://github.com/mvanhorn/cli-printing-press/issues/1212)) ([69cefbe](https://github.com/mvanhorn/cli-printing-press/commit/69cefbe9db5219d8c6e03e51e74f218a36c567f8))
* **cli:** raise API error body truncation cap from 200 to 4096 bytes ([#1285](https://github.com/mvanhorn/cli-printing-press/issues/1285)) ([513ae01](https://github.com/mvanhorn/cli-printing-press/commit/513ae01d9afa7e8a3b7d850db7824d596d118c61))
* **cli:** route in:query params to URL on PUT/DELETE/POST/PATCH handlers ([#1279](https://github.com/mvanhorn/cli-printing-press/issues/1279)) ([b8488ee](https://github.com/mvanhorn/cli-printing-press/commit/b8488ee422472772141c9137b869ae5cbee3492c))
* **cli:** support $-prefixed pagination params for Socrata-style APIs ([#1204](https://github.com/mvanhorn/cli-printing-press/issues/1204)) ([3819b67](https://github.com/mvanhorn/cli-printing-press/commit/3819b67f8118617725671467a7a0f6ee712c4304))
* **skills:** extend retro pre-upload scrub with jurisdiction-specific PII patterns ([#1302](https://github.com/mvanhorn/cli-printing-press/issues/1302)) ([42093ad](https://github.com/mvanhorn/cli-printing-press/commit/42093ad854a0c2505104aba03f92d418c04d3f59)), closes [#1291](https://github.com/mvanhorn/cli-printing-press/issues/1291)
* **skills:** skip publish-validate in mid-pipeline polish ([#1300](https://github.com/mvanhorn/cli-printing-press/issues/1300)) ([8cc7e65](https://github.com/mvanhorn/cli-printing-press/commit/8cc7e655ff4475070ba906f9891682d88ffedfa7))

## [4.5.2](https://github.com/mvanhorn/cli-printing-press/compare/v4.5.1...v4.5.2) (2026-05-12)


### Bug Fixes

* **ci:** fold skill docs into lint gate ([#1226](https://github.com/mvanhorn/cli-printing-press/issues/1226)) ([7010c10](https://github.com/mvanhorn/cli-printing-press/commit/7010c102c0e52860cad09dd745065b16919aedb2))
* **cli:** always quote SQL identifiers emitted by safeSQLName ([#1228](https://github.com/mvanhorn/cli-printing-press/issues/1228)) ([c05d3bc](https://github.com/mvanhorn/cli-printing-press/commit/c05d3bc75d9f098e9a99d6b5cd22ce07cf4e220c))
* **cli:** default Accept to application/json instead of */* ([#1229](https://github.com/mvanhorn/cli-printing-press/issues/1229)) ([e40389e](https://github.com/mvanhorn/cli-printing-press/commit/e40389efd8e3fcfcc673d95accdef6678c455dc9)), closes [#1119](https://github.com/mvanhorn/cli-printing-press/issues/1119)
* **cli:** gate refreshAccessToken emission on Auth.TokenURL non-empty ([#1244](https://github.com/mvanhorn/cli-printing-press/issues/1244)) ([5c359ba](https://github.com/mvanhorn/cli-printing-press/commit/5c359ba6813f20cbcf75d9910bfb70f7aa75935a))
* **cli:** guard nil Phases in LoadState to prevent lock promote panic ([#1193](https://github.com/mvanhorn/cli-printing-press/issues/1193)) ([28e2fa1](https://github.com/mvanhorn/cli-printing-press/commit/28e2fa1b44740135699e1bf4502411ae9e363db6)), closes [#1189](https://github.com/mvanhorn/cli-printing-press/issues/1189)
* **cli:** make .printing-press.json authoritative for parsed.Name in mcp-sync ([#1252](https://github.com/mvanhorn/cli-printing-press/issues/1252)) ([bb9b9cb](https://github.com/mvanhorn/cli-printing-press/commit/bb9b9cb3a71a3fea95df2075c515ae62bec44553))
* **cli:** populate parent FK in generated UpsertBatch typed-table test fixture ([#1253](https://github.com/mvanhorn/cli-printing-press/issues/1253)) ([9e0fe96](https://github.com/mvanhorn/cli-printing-press/commit/9e0fe96c8488f04a81f1de413a55d7244559679b)), closes [#1063](https://github.com/mvanhorn/cli-printing-press/issues/1063)
* **cli:** prioritize bearer + apply root-security filter in scheme selection ([#1238](https://github.com/mvanhorn/cli-printing-press/issues/1238)) ([09d72e6](https://github.com/mvanhorn/cli-printing-press/commit/09d72e68455d0ff1b720d6de369349d45c30112a)), closes [#979](https://github.com/mvanhorn/cli-printing-press/issues/979)
* **cli:** skip dogfood --live error_path probe for mutating commands ([#1225](https://github.com/mvanhorn/cli-printing-press/issues/1225)) ([f1246ee](https://github.com/mvanhorn/cli-printing-press/commit/f1246ee145ea765f8ee66714e35ad14b6a859f4e)), closes [#1219](https://github.com/mvanhorn/cli-printing-press/issues/1219)
* **skills:** generate absolute manuscript URLs in publish PR body ([#1235](https://github.com/mvanhorn/cli-printing-press/issues/1235)) ([a8ad982](https://github.com/mvanhorn/cli-printing-press/commit/a8ad9820e89070e3adf084299c73bb2a5a5b797e))

## [4.5.1](https://github.com/mvanhorn/cli-printing-press/compare/v4.5.0...v4.5.1) (2026-05-12)


### Bug Fixes

* **cli:** accept backtick raw-string Use: in dogfood walkers ([#1169](https://github.com/mvanhorn/cli-printing-press/issues/1169)) ([7ba9c20](https://github.com/mvanhorn/cli-printing-press/commit/7ba9c20a352c473cdffa8db492543a719f1518d2))
* **cli:** credit auth_protocol on structural OAuth surface, not "Bearer " literal ([#1176](https://github.com/mvanhorn/cli-printing-press/issues/1176)) ([e8c03cf](https://github.com/mvanhorn/cli-printing-press/commit/e8c03cffc5241963ce803a30ef29c541ae3f958d)), closes [#941](https://github.com/mvanhorn/cli-printing-press/issues/941)
* **cli:** dedupe nested body-field flatten collisions with sibling scalars ([#1190](https://github.com/mvanhorn/cli-printing-press/issues/1190)) ([c5a5441](https://github.com/mvanhorn/cli-printing-press/commit/c5a5441d3771568ee2d6d07db998af6e0c6f236e)), closes [#1043](https://github.com/mvanhorn/cli-printing-press/issues/1043)
* **cli:** emit stderr truncation warning on single-page list responses ([#1177](https://github.com/mvanhorn/cli-printing-press/issues/1177)) ([6cda260](https://github.com/mvanhorn/cli-printing-press/commit/6cda260642f1af9379c66d2a5e7e5fd4f6364245))
* **cli:** handle PascalCase .NET-shape API responses in sync ([#1135](https://github.com/mvanhorn/cli-printing-press/issues/1135)) ([#1174](https://github.com/mvanhorn/cli-printing-press/issues/1174)) ([84138f9](https://github.com/mvanhorn/cli-printing-press/commit/84138f91da785001da9b105ce647cbfff0bdb386))
* **cli:** index args[] by positional ordinal in path-param emit ([#1211](https://github.com/mvanhorn/cli-printing-press/issues/1211)) ([460d676](https://github.com/mvanhorn/cli-printing-press/commit/460d676ec54a9c231ff98f2047edbe61bffe2693))
* **cli:** make dogfood acceptance marker manifest read best-effort ([#1179](https://github.com/mvanhorn/cli-printing-press/issues/1179)) ([ee13fb1](https://github.com/mvanhorn/cli-printing-press/commit/ee13fb1611755cc2964a506b03bf614a44d190eb)), closes [#963](https://github.com/mvanhorn/cli-printing-press/issues/963)
* **cli:** map Cobra/pflag pre-RunE usage errors to exit code 2 ([#1194](https://github.com/mvanhorn/cli-printing-press/issues/1194)) ([dabf63f](https://github.com/mvanhorn/cli-printing-press/commit/dabf63f8753eeddc10a72b6b74f37a713f137e3c))
* **cli:** preserve novel-command keys in --compact partial-strip path ([#1167](https://github.com/mvanhorn/cli-printing-press/issues/1167)) ([f88a10b](https://github.com/mvanhorn/cli-printing-press/commit/f88a10b3a9fc3738acffc496db4e476a57851c21))
* **cli:** preserve x-mcp config when merging combo specs ([#1187](https://github.com/mvanhorn/cli-printing-press/issues/1187)) ([660829e](https://github.com/mvanhorn/cli-printing-press/commit/660829ed3cce879fc15209d50fe39df96594ff8e)), closes [#1044](https://github.com/mvanhorn/cli-printing-press/issues/1044)
* **cli:** redact $HOME paths from publish-tree JSON artifacts ([#1181](https://github.com/mvanhorn/cli-printing-press/issues/1181)) ([a3c3c65](https://github.com/mvanhorn/cli-printing-press/commit/a3c3c6537f36d371e14d1410c35d702300ca1b53))
* **cli:** refuse to ship CLIs with placeholder base URL ([#1186](https://github.com/mvanhorn/cli-printing-press/issues/1186)) ([c9a35b5](https://github.com/mvanhorn/cli-printing-press/commit/c9a35b5e2423c8d8a21eec6fa126c9b10e85746d))
* **cli:** resolve validation binary path with platform.ExecutablePath ([#1213](https://github.com/mvanhorn/cli-printing-press/issues/1213)) ([7365fcc](https://github.com/mvanhorn/cli-printing-press/commit/7365fcc31c061342fd55c2c84633794a00e1d30a)), closes [#978](https://github.com/mvanhorn/cli-printing-press/issues/978)
* **cli:** route scorer subcommand --spec URLs through generate's fetch path ([#1178](https://github.com/mvanhorn/cli-printing-press/issues/1178)) ([5e3667d](https://github.com/mvanhorn/cli-printing-press/commit/5e3667df2e2aabef62b0110b7557b93df7fae2cf))
* **cli:** scope HTTP cache to &lt;api&gt;/http so invalidateCache spares siblings ([#1166](https://github.com/mvanhorn/cli-printing-press/issues/1166)) ([3697610](https://github.com/mvanhorn/cli-printing-press/commit/36976106f4357fb3dba3242a85ba66b9eb677bc3)), closes [#1126](https://github.com/mvanhorn/cli-printing-press/issues/1126)

## [4.5.0](https://github.com/mvanhorn/cli-printing-press/compare/v4.4.0...v4.5.0) (2026-05-12)


### Features

* **skills:** add native code review phase to printing-press ([#1160](https://github.com/mvanhorn/cli-printing-press/issues/1160)) ([88c5b69](https://github.com/mvanhorn/cli-printing-press/commit/88c5b6966b1cdb1387f919c08b4aa21359d51762))
* **skills:** offer browser-sniff backend install at preflight ([#1132](https://github.com/mvanhorn/cli-printing-press/issues/1132)) ([8990fc2](https://github.com/mvanhorn/cli-printing-press/commit/8990fc242ff1a4ec7c57892e2896f5cd77fd7be2))

## [4.4.0](https://github.com/mvanhorn/cli-printing-press/compare/v4.3.0...v4.4.0) (2026-05-11)


### Features

* **cli:** promote html_scrape reachability mode when captcha-tier protection blocks JSON + SSR sibling carries state blob ([#1065](https://github.com/mvanhorn/cli-printing-press/issues/1065)) ([d7e2d74](https://github.com/mvanhorn/cli-printing-press/commit/d7e2d74f32fcefbb730366c40bb4c33974bc3ead))
* **cli:** sync.walker spec parameter for hierarchical APIs ([#1060](https://github.com/mvanhorn/cli-printing-press/issues/1060)) ([#1074](https://github.com/mvanhorn/cli-printing-press/issues/1074)) ([24962b7](https://github.com/mvanhorn/cli-printing-press/commit/24962b7c862f5f4d6cb3679908615266d4c531e4))


### Bug Fixes

* **catalog:** refresh google-flights entry to reflect fli native port ([#1084](https://github.com/mvanhorn/cli-printing-press/issues/1084)) ([5c4ef3a](https://github.com/mvanhorn/cli-printing-press/commit/5c4ef3a7a4faea5f8f3b3eaaed4e62a8e773396e))
* **ci:** drop cancel-in-progress from pr-title workflow ([#1068](https://github.com/mvanhorn/cli-printing-press/issues/1068)) ([424199a](https://github.com/mvanhorn/cli-printing-press/commit/424199a7a4bd9b90c1787a0a30d1c00228728630))
* **cli:** delegate workflow archive to syncResource ([#1090](https://github.com/mvanhorn/cli-printing-press/issues/1090)) ([3d515c4](https://github.com/mvanhorn/cli-printing-press/commit/3d515c457de657b470035e71bda8b5d8808f45e2))
* **cli:** dispatch typed-table upserts on spec resource key, not snake table name ([#1071](https://github.com/mvanhorn/cli-printing-press/issues/1071)) ([705bad2](https://github.com/mvanhorn/cli-printing-press/commit/705bad2df0c1081ddb5e42a7aa949c5767ae52b0)), closes [#1064](https://github.com/mvanhorn/cli-printing-press/issues/1064)
* **cli:** emit --body-json fallback for oneOf/anyOf request bodies ([#994](https://github.com/mvanhorn/cli-printing-press/issues/994)) ([910c228](https://github.com/mvanhorn/cli-printing-press/commit/910c228d48e4de0bdc21e6ba4b5f39e827ef2f5a)), closes [#977](https://github.com/mvanhorn/cli-printing-press/issues/977)
* **cli:** emit default Accept */* header in generated HTTP clients ([#1112](https://github.com/mvanhorn/cli-printing-press/issues/1112)) ([c626efc](https://github.com/mvanhorn/cli-printing-press/commit/c626efca9403e829bafbee3c160ce7354c47d3f4))
* **cli:** exempt CLI-root vendor spec files from PII audit scope ([#1121](https://github.com/mvanhorn/cli-printing-press/issues/1121)) ([6536cff](https://github.com/mvanhorn/cli-printing-press/commit/6536cfffc1a796604286df2c8fc67d8ff113e1ae))
* **cli:** gate OAuth refresh-token params on x-oauth-refresh-token-mechanism ([#1099](https://github.com/mvanhorn/cli-printing-press/issues/1099)) ([cba06db](https://github.com/mvanhorn/cli-printing-press/commit/cba06db19aa2dd0af1b161686873e98f1ae2a71a))
* **cli:** gofmt rendered .go output in generator emit phase ([#1100](https://github.com/mvanhorn/cli-printing-press/issues/1100)) ([1ca94b9](https://github.com/mvanhorn/cli-printing-press/commit/1ca94b91e8ff917a24f996a7d89aaa89dd1a27d6)), closes [#1080](https://github.com/mvanhorn/cli-printing-press/issues/1080)
* **cli:** honor Spec.BasePath in generated client URL construction ([#1108](https://github.com/mvanhorn/cli-printing-press/issues/1108)) ([c642074](https://github.com/mvanhorn/cli-printing-press/commit/c6420747099278647d33c51c9f64df85d8584fdf))
* **cli:** infer pagination defaults from plain params; preserve cursor=0 in paginatedGet ([#1115](https://github.com/mvanhorn/cli-printing-press/issues/1115)) ([10cc48a](https://github.com/mvanhorn/cli-printing-press/commit/10cc48a822022450b12109c1d13fd09dddb04033)), closes [#927](https://github.com/mvanhorn/cli-printing-press/issues/927)
* **cli:** preserve unknown-shape records in compactListFields ([#1078](https://github.com/mvanhorn/cli-printing-press/issues/1078)) ([dc6fe87](https://github.com/mvanhorn/cli-printing-press/commit/dc6fe8791ec3693a8addac8dab27f0e333c1e326)), closes [#1046](https://github.com/mvanhorn/cli-printing-press/issues/1046)
* **cli:** skip Windows Python Store stub in verify-skill ([#1086](https://github.com/mvanhorn/cli-printing-press/issues/1086)) ([27ad2dd](https://github.com/mvanhorn/cli-printing-press/commit/27ad2dda618d56431cf3fcd826017c3a760c3cc2))
* **cli:** stage runstate manuscripts during lock promote ([#1094](https://github.com/mvanhorn/cli-printing-press/issues/1094)) ([17d1930](https://github.com/mvanhorn/cli-printing-press/commit/17d1930735e5e9d5f26298640e869f978951b886)), closes [#889](https://github.com/mvanhorn/cli-printing-press/issues/889)
* **cli:** suppress max_pages_cap_hit warning under --latest-only ([#1120](https://github.com/mvanhorn/cli-printing-press/issues/1120)) ([5f2bde7](https://github.com/mvanhorn/cli-printing-press/commit/5f2bde7fa62b2235b83cb2092a069f6b6456beaa))
* **cli:** surface API body in sync_error events and add --param passthrough ([#1104](https://github.com/mvanhorn/cli-printing-press/issues/1104)) ([cb3c097](https://github.com/mvanhorn/cli-printing-press/commit/cb3c0974a1c9ab449e50d7a3700a979cb3e0f82c))
* **cli:** unwrap single-key API envelopes before agent provenance envelope ([#1095](https://github.com/mvanhorn/cli-printing-press/issues/1095)) ([ff4b652](https://github.com/mvanhorn/cli-printing-press/commit/ff4b65286008d0f2224befa5b1d0624afe5e1e6e)), closes [#894](https://github.com/mvanhorn/cli-printing-press/issues/894)
* **cli:** validate ListIDs resourceType to close SQL injection ([#1000](https://github.com/mvanhorn/cli-printing-press/issues/1000)) ([242a56b](https://github.com/mvanhorn/cli-printing-press/commit/242a56bc6a2d88cc8b93ac29d90254a7f7d6e1e9))

## [4.3.0](https://github.com/mvanhorn/cli-printing-press/compare/v4.2.2...v4.3.0) (2026-05-11)


### Features

* **cli:** add cliutil.ExtractNumber/ExtractInt for JSON-string-encoded numeric fields ([#1002](https://github.com/mvanhorn/cli-printing-press/issues/1002)) ([182395c](https://github.com/mvanhorn/cli-printing-press/commit/182395caaa631a5fa7721dc0a6f158c557cd473e)), closes [#989](https://github.com/mvanhorn/cli-printing-press/issues/989)
* **cli:** emit nested-object body fields as parent-prefixed flags ([#957](https://github.com/mvanhorn/cli-printing-press/issues/957)) ([ebc8cd8](https://github.com/mvanhorn/cli-printing-press/commit/ebc8cd811e5708e4b43e38b36d42719df09b4fe8))
* **cli:** mechanical PII gate before promote/publish ([#958](https://github.com/mvanhorn/cli-printing-press/issues/958)) ([#1023](https://github.com/mvanhorn/cli-printing-press/issues/1023)) ([a3e07d0](https://github.com/mvanhorn/cli-printing-press/commit/a3e07d0af8cba3d7270f44c231cf00096bb29e5f))
* **cli:** point users at where to get a token (URL + instructions + auth setup --launch) ([#871](https://github.com/mvanhorn/cli-printing-press/issues/871)) ([4fac827](https://github.com/mvanhorn/cli-printing-press/commit/4fac827e8c3e90d77ef2c89017eab6f8c81cf747))


### Bug Fixes

* **cli:** allow runtime override of OAuth2 OIDC URLs ([#970](https://github.com/mvanhorn/cli-printing-press/issues/970)) ([a9e9d00](https://github.com/mvanhorn/cli-printing-press/commit/a9e9d006af266b5b147cd7bd27bd00733be8829d))
* **cli:** bind typed upsert values in column-declaration order ([#1018](https://github.com/mvanhorn/cli-printing-press/issues/1018)) ([fef70ba](https://github.com/mvanhorn/cli-printing-press/commit/fef70ba1e2ccfe93fe2ab51e524dabd898b69cfe)), closes [#1014](https://github.com/mvanhorn/cli-printing-press/issues/1014)
* **cli:** block vendor-prefix secrets during publish ([#852](https://github.com/mvanhorn/cli-printing-press/issues/852)) ([8cf5459](https://github.com/mvanhorn/cli-printing-press/commit/8cf5459c947eacb00a94b9fae465ac761d7dc4ff))
* **cli:** classify doctor HTTP 403 as scope-limited WARN, not invalid FAIL ([#1033](https://github.com/mvanhorn/cli-printing-press/issues/1033)) ([b1dded7](https://github.com/mvanhorn/cli-printing-press/commit/b1dded7d7db1962c2d3f6909c5f863b74367a25d))
* **cli:** emit AccessToken-only AuthHeader for all OAuth2 grants ([#1010](https://github.com/mvanhorn/cli-printing-press/issues/1010)) ([3b05089](https://github.com/mvanhorn/cli-printing-press/commit/3b050899c91b03dcae0466482dbd4d5a4faec9a1))
* **cli:** emit form-encoded request bodies ([#947](https://github.com/mvanhorn/cli-printing-press/issues/947)) ([48cc2a3](https://github.com/mvanhorn/cli-printing-press/commit/48cc2a31b172328d6f6a314014553c3f8e981eb3))
* **cli:** emit multipart requests for upload endpoints ([#904](https://github.com/mvanhorn/cli-printing-press/issues/904)) ([fc291ca](https://github.com/mvanhorn/cli-printing-press/commit/fc291cae1938593d86e4abac349a9a2117ced0a0))
* **cli:** fold per-spec base URL path prefixes on multi-spec merge ([#995](https://github.com/mvanhorn/cli-printing-press/issues/995)) ([6e086c0](https://github.com/mvanhorn/cli-printing-press/commit/6e086c00773dd0e17c088f222d1fda785ad5644e))
* **cli:** force UTF-8 stdio in verify-skill Python subprocess ([#985](https://github.com/mvanhorn/cli-printing-press/issues/985)) ([cb1697e](https://github.com/mvanhorn/cli-printing-press/commit/cb1697ed2fcc914b9f53592ba2b2f65ff7de6488)), closes [#976](https://github.com/mvanhorn/cli-printing-press/issues/976) [#819](https://github.com/mvanhorn/cli-printing-press/issues/819)
* **cli:** gate sync since-param emission per resource ([#1036](https://github.com/mvanhorn/cli-printing-press/issues/1036)) ([dc5e8c5](https://github.com/mvanhorn/cli-printing-press/commit/dc5e8c55f70e7d6e343b6bd5199afaff38d16b6a))
* **cli:** handle empty sync pages ([#903](https://github.com/mvanhorn/cli-printing-press/issues/903)) ([d94d89d](https://github.com/mvanhorn/cli-printing-press/commit/d94d89dc6d8aec8c55212e9907a3543b4e61a94d))
* **cli:** hide raw resource groups when api browser is generated ([#1045](https://github.com/mvanhorn/cli-printing-press/issues/1045)) ([ed693b0](https://github.com/mvanhorn/cli-printing-press/commit/ed693b0d36b52746b8b43e2a075cd55cac44c0c9))
* **cli:** honor --resources filter in dependent sync fan-out ([#1047](https://github.com/mvanhorn/cli-printing-press/issues/1047)) ([e2f3a53](https://github.com/mvanhorn/cli-printing-press/commit/e2f3a53c004d42b94a3ab8957d2817221c23d3c3))
* **cli:** honor auth.prefix on bearer_token specs ([#1054](https://github.com/mvanhorn/cli-printing-press/issues/1054)) ([d2770e1](https://github.com/mvanhorn/cli-printing-press/commit/d2770e1610f5ab38dcee000ff7a941732174be39))
* **cli:** infer resource-prefixed IDField from item-schema properties ([#938](https://github.com/mvanhorn/cli-printing-press/issues/938)) ([6cd57cc](https://github.com/mvanhorn/cli-printing-press/commit/6cd57cc2bb3ec0b728cff7584aa98acfec24f783))
* **cli:** isolate generic resources by type ([#901](https://github.com/mvanhorn/cli-printing-press/issues/901)) ([ff75531](https://github.com/mvanhorn/cli-printing-press/commit/ff7553169890c8517bd174b0eef90cebbca010db))
* **cli:** preserve hand-edits to templated files on --force regen ([#967](https://github.com/mvanhorn/cli-printing-press/issues/967)) ([618fa45](https://github.com/mvanhorn/cli-printing-press/commit/618fa45627609eff27a103bfc91ee28783e128a6))
* **cli:** preserve internal sibling packages on force regen ([#897](https://github.com/mvanhorn/cli-printing-press/issues/897)) ([dceb6e5](https://github.com/mvanhorn/cli-printing-press/commit/dceb6e58f1bce60ea2228c7169eb590228b6c591))
* **cli:** reconcile MCPB manifest against internal/client env reads ([#859](https://github.com/mvanhorn/cli-printing-press/issues/859)) ([#1035](https://github.com/mvanhorn/cli-printing-press/issues/1035)) ([c93ce0c](https://github.com/mvanhorn/cli-printing-press/commit/c93ce0c261efa227ffa916bd74a63026c769f207))
* **cli:** reject control-plane flag injection in MCP shellout ([#1022](https://github.com/mvanhorn/cli-printing-press/issues/1022)) ([4b04f4c](https://github.com/mvanhorn/cli-printing-press/commit/4b04f4c6be2591d3e34f5da2996dcadf3626bf78))
* **cli:** reject reserved placeholder hosts in spec validation ([#984](https://github.com/mvanhorn/cli-printing-press/issues/984)) ([5f8dae1](https://github.com/mvanhorn/cli-printing-press/commit/5f8dae13e1984d46a2bd7f9bf3bf83279fa4c8d6)), closes [#818](https://github.com/mvanhorn/cli-printing-press/issues/818)
* **cli:** route explicit --csv/--quiet/--plain above piped-pipe gate ([#968](https://github.com/mvanhorn/cli-printing-press/issues/968)) ([ab6edbe](https://github.com/mvanhorn/cli-printing-press/commit/ab6edbefb709697a129a42668a16a31c04749bcf)), closes [#918](https://github.com/mvanhorn/cli-printing-press/issues/918)
* **cli:** seed template-var placeholders in verify mode ([#934](https://github.com/mvanhorn/cli-printing-press/issues/934)) ([a1d39bf](https://github.com/mvanhorn/cli-printing-press/commit/a1d39bf3e649ceff452a7b049e95c7d86d898680)), closes [#893](https://github.com/mvanhorn/cli-printing-press/issues/893)
* **cli:** Store.Get propagates sql.ErrNoRows so callers can gate on existence ([#1031](https://github.com/mvanhorn/cli-printing-press/issues/1031)) ([66fd401](https://github.com/mvanhorn/cli-printing-press/commit/66fd401bddf4e27e7de28ee19eccc88791719bef))
* **cli:** sync skips resources with unresolved {key} placeholders ([#1009](https://github.com/mvanhorn/cli-printing-press/issues/1009)) ([5fabad6](https://github.com/mvanhorn/cli-printing-press/commit/5fabad63d2a15ddfb8fcf752082070437156ffb2))
* **cli:** walk parent dirs for research.json in live-check ([#1057](https://github.com/mvanhorn/cli-printing-press/issues/1057)) ([9a07f28](https://github.com/mvanhorn/cli-printing-press/commit/9a07f2872aa5c0bf61b21f7ed0263e928ec4f23e)), closes [#885](https://github.com/mvanhorn/cli-printing-press/issues/885)
* **generator:** preserve multi-spec server prefixes ([#861](https://github.com/mvanhorn/cli-printing-press/issues/861)) ([3e56bed](https://github.com/mvanhorn/cli-printing-press/commit/3e56bedf4b580a67e6bd54bf1705861687740e2f))
* **generator:** rename trailing '_test' stems to avoid Go test-file exclusion ([#1020](https://github.com/mvanhorn/cli-printing-press/issues/1020)) ([#1021](https://github.com/mvanhorn/cli-printing-press/issues/1021)) ([a03a7b8](https://github.com/mvanhorn/cli-printing-press/commit/a03a7b81685062ec0abd6faa5fb182d021ae04d7))
* **generator:** route receiver JSON helper through filters ([#933](https://github.com/mvanhorn/cli-printing-press/issues/933)) ([e952e80](https://github.com/mvanhorn/cli-printing-press/commit/e952e80794ac828dd2a35ff0db7ce91b834cb56c))
* **skills:** forward --research-dir to scorecard --live-check in mid-pipeline polish ([#980](https://github.com/mvanhorn/cli-printing-press/issues/980)) ([e0240ce](https://github.com/mvanhorn/cli-printing-press/commit/e0240ceae66362d78ebf2097bbd66cedf155250a))
* **skills:** gate polish Publish Offer on --standalone, not path detection ([#1017](https://github.com/mvanhorn/cli-printing-press/issues/1017)) ([54f007f](https://github.com/mvanhorn/cli-printing-press/commit/54f007f55798999bc8f8d334ed7f51fd88ec196a)), closes [#1008](https://github.com/mvanhorn/cli-printing-press/issues/1008)
* **skills:** preflight Go toolchain before generation runs ([#973](https://github.com/mvanhorn/cli-printing-press/issues/973)) ([0562bca](https://github.com/mvanhorn/cli-printing-press/commit/0562bca80e8588b02130edeec8dab7d0a3b1ec1d))

## [4.2.2](https://github.com/mvanhorn/cli-printing-press/compare/v4.2.1...v4.2.2) (2026-05-09)


### Bug Fixes

* **cli:** default Surf browser transport to h2 ([#850](https://github.com/mvanhorn/cli-printing-press/issues/850)) ([9f2a4ef](https://github.com/mvanhorn/cli-printing-press/commit/9f2a4efef0c6d912d726d348ebc06af5c63c11f3))
* **cli:** preserve manifest fields during dogfood sync ([#844](https://github.com/mvanhorn/cli-printing-press/issues/844)) ([2d9a304](https://github.com/mvanhorn/cli-printing-press/commit/2d9a304a509f427226d08cbf567f0a7aea5c5df3))
* **cli:** preserve operation server host overrides ([#834](https://github.com/mvanhorn/cli-printing-press/issues/834)) ([1c0d110](https://github.com/mvanhorn/cli-printing-press/commit/1c0d110bb6ef79822ed226d6e961682d27b03c0a))
* **generator:** scope caches by auth identity ([#853](https://github.com/mvanhorn/cli-printing-press/issues/853)) ([1a8f68e](https://github.com/mvanhorn/cli-printing-press/commit/1a8f68eeb91a433554c3ef781de4382ee3d82671))
* **skills:** correct update preflight compatibility ([#854](https://github.com/mvanhorn/cli-printing-press/issues/854)) ([4b29a63](https://github.com/mvanhorn/cli-printing-press/commit/4b29a634250a80f9c80bc2d91a69d72dd71291cd))

## [4.2.1](https://github.com/mvanhorn/cli-printing-press/compare/v4.2.0...v4.2.1) (2026-05-09)


### Bug Fixes

* **catalog:** Align Google Flights catalog with Flight Goat's current wrapper ([#821](https://github.com/mvanhorn/cli-printing-press/issues/821)) ([1970210](https://github.com/mvanhorn/cli-printing-press/commit/19702102e9e0f7306c406a4519b76d280ef514c1))
* **cli:** correct HTTP Basic auth env vars ([#810](https://github.com/mvanhorn/cli-printing-press/issues/810)) ([9bb46da](https://github.com/mvanhorn/cli-printing-press/commit/9bb46da6972900e63e7a954609bc37a87570a63e))
* **cli:** default cookie HTML scrapes to browser transport ([#822](https://github.com/mvanhorn/cli-printing-press/issues/822)) ([7a1d83a](https://github.com/mvanhorn/cli-printing-press/commit/7a1d83ad26f96b81e877831f0a43000eaa37a85c))
* **cli:** handle Windows shipcheck paths ([#783](https://github.com/mvanhorn/cli-printing-press/issues/783)) ([d0086ff](https://github.com/mvanhorn/cli-printing-press/commit/d0086ffe385de40e6d5cbb34c75bdb880abadf04))
* **cli:** stop credential status overclaims ([#779](https://github.com/mvanhorn/cli-printing-press/issues/779)) ([2ff069e](https://github.com/mvanhorn/cli-printing-press/commit/2ff069e0335b4057b480a057a95ca839c20ccb30))
* **cli:** use slash paths for embedded templates ([#794](https://github.com/mvanhorn/cli-printing-press/issues/794)) ([0020b78](https://github.com/mvanhorn/cli-printing-press/commit/0020b78a30b6bf5daa478377c6c26e5f2442987f))
* **skills:** gate polish ship verdict on publish validate ([#789](https://github.com/mvanhorn/cli-printing-press/issues/789)) ([5e04776](https://github.com/mvanhorn/cli-printing-press/commit/5e04776b8b0e943a1b30461884d339ee5e7e67ba))

## [4.2.0](https://github.com/mvanhorn/cli-printing-press/compare/v4.1.0...v4.2.0) (2026-05-09)


### Features

* **cli:** support static config headers ([#769](https://github.com/mvanhorn/cli-printing-press/issues/769)) ([838f5a4](https://github.com/mvanhorn/cli-printing-press/commit/838f5a497975aad11e7c938f982f7a64d96d9a15))


### Bug Fixes

* **cli:** avoid duplicate token auth map keys ([#775](https://github.com/mvanhorn/cli-printing-press/issues/775)) ([de34ec0](https://github.com/mvanhorn/cli-printing-press/commit/de34ec0ecd8ff1d807b049556f138b1131fd081b))

## [4.1.0](https://github.com/mvanhorn/cli-printing-press/compare/v4.0.6...v4.1.0) (2026-05-09)


### Features

* **ci:** add verify-skill drift check ([#766](https://github.com/mvanhorn/cli-printing-press/issues/766)) ([1af0f27](https://github.com/mvanhorn/cli-printing-press/commit/1af0f275a4cc20026e29c8e3d18563759d3764d7))
* **cli:** credit original printer in per-CLI README (closes [#745](https://github.com/mvanhorn/cli-printing-press/issues/745)) ([#748](https://github.com/mvanhorn/cli-printing-press/issues/748)) ([ee8f6ad](https://github.com/mvanhorn/cli-printing-press/commit/ee8f6ad99ce0dd4b6400785a69b014a508534862))
* **cli:** emit AGENTS.md for printed CLIs (fast-track of [#681](https://github.com/mvanhorn/cli-printing-press/issues/681)) ([#728](https://github.com/mvanhorn/cli-printing-press/issues/728)) ([a9b9aa6](https://github.com/mvanhorn/cli-printing-press/commit/a9b9aa659a84a877f42e21bb91167707bb5e46b9))


### Bug Fixes

* **cli:** align generated install and rename metadata ([#749](https://github.com/mvanhorn/cli-printing-press/issues/749)) ([ca580ef](https://github.com/mvanhorn/cli-printing-press/commit/ca580efc6ad47f0f4678b9a54fb1773c7efb33b7))
* **cli:** anchor openapi loader normalization ([#730](https://github.com/mvanhorn/cli-printing-press/issues/730)) ([a4b0eb3](https://github.com/mvanhorn/cli-printing-press/commit/a4b0eb30590d1d322d6895072aa709db5ed46d78))
* **cli:** close redfin retro verification gaps ([#762](https://github.com/mvanhorn/cli-printing-press/issues/762)) ([172c6c2](https://github.com/mvanhorn/cli-printing-press/commit/172c6c299ef48f278faadfa0fbdcef4a39a7c2ec))
* **cli:** guard generated CLI build safety ([#736](https://github.com/mvanhorn/cli-printing-press/issues/736)) ([1d26ae7](https://github.com/mvanhorn/cli-printing-press/commit/1d26ae77985ef52df240018ad23913cb2602acc0))
* **cli:** handle wrapped paginated responses ([#731](https://github.com/mvanhorn/cli-printing-press/issues/731)) ([b4bdcf6](https://github.com/mvanhorn/cli-printing-press/commit/b4bdcf68bc621c3a77e022b54f5ab43c6515a49f))
* **cli:** harden force-generate preservation ([#750](https://github.com/mvanhorn/cli-printing-press/issues/750)) ([c397021](https://github.com/mvanhorn/cli-printing-press/commit/c397021fccf0c31d7e8ed39736bae3e613bb95d5))
* **cli:** hydrate promote state across scopes ([#738](https://github.com/mvanhorn/cli-printing-press/issues/738)) ([dfdf183](https://github.com/mvanhorn/cli-printing-press/commit/dfdf183139a2ae36b2c2750f9f7b0e80f15326c5))
* **cli:** infer bearer auth from prose-only specs ([#753](https://github.com/mvanhorn/cli-printing-press/issues/753)) ([92d303a](https://github.com/mvanhorn/cli-printing-press/commit/92d303ac75d26944c91ffa8a21eb09d05cb909f3))
* **cli:** populate manifest.Category from spec when catalog misses ([#735](https://github.com/mvanhorn/cli-printing-press/issues/735)) ([a00e97b](https://github.com/mvanhorn/cli-printing-press/commit/a00e97b198eec5fc5f53b8877439b2462602e39d))
* **cli:** preserve browser-sniffed request defaults ([#737](https://github.com/mvanhorn/cli-printing-press/issues/737)) ([8721df4](https://github.com/mvanhorn/cli-printing-press/commit/8721df4bf310cac7b0619747af9db366508583c3))
* **cli:** preserve novel cli files on force generate ([#747](https://github.com/mvanhorn/cli-printing-press/issues/747)) ([9675f8b](https://github.com/mvanhorn/cli-printing-press/commit/9675f8b2b39be4639a7bdd2e34cc8fcd7e6b00f7))
* **cli:** route live cache through typed upserts ([#751](https://github.com/mvanhorn/cli-printing-press/issues/751)) ([c332b53](https://github.com/mvanhorn/cli-printing-press/commit/c332b534e59ac6e4d786c48924be81457c85f1a1))
* **cli:** score structural scorer behavior ([#732](https://github.com/mvanhorn/cli-printing-press/issues/732)) ([979510c](https://github.com/mvanhorn/cli-printing-press/commit/979510c21b29be9e93fda3321e60154d2eb5233c))
* **cli:** sync sibling list endpoints ([#756](https://github.com/mvanhorn/cli-printing-press/issues/756)) ([745f2e7](https://github.com/mvanhorn/cli-printing-press/commit/745f2e75f2bc9aff5079ca6de3f232930341c28e))
* **cli:** unscore large code-orch token catalogs ([#755](https://github.com/mvanhorn/cli-printing-press/issues/755)) ([6348c9f](https://github.com/mvanhorn/cli-printing-press/commit/6348c9f25b32a535cbbbd0dd5196bfa843338d55))
* **generator:** use simple backticks in auth_client_credentials.go.tmpl ([#734](https://github.com/mvanhorn/cli-printing-press/issues/734)) ([6436392](https://github.com/mvanhorn/cli-printing-press/commit/6436392108844edace2390b7680eaaf9b4c56a44))

## [4.0.6](https://github.com/mvanhorn/cli-printing-press/compare/v4.0.5...v4.0.6) (2026-05-08)


### Bug Fixes

* **cli:** close MCP sql tool exfiltration vector with allowlist + read-only handle ([#709](https://github.com/mvanhorn/cli-printing-press/issues/709)) ([b196bd8](https://github.com/mvanhorn/cli-printing-press/commit/b196bd813a0a4eb58f763b7d4d38563208c1a5d0))
* **cli:** correct named-array envelope detection and api_key set-token slot ([#706](https://github.com/mvanhorn/cli-printing-press/issues/706)) ([c46cf01](https://github.com/mvanhorn/cli-printing-press/commit/c46cf0157409949d46b958836aadafa8fa281913))
* **cli:** dedupe TypeField identifiers in same struct ([#705](https://github.com/mvanhorn/cli-printing-press/issues/705)) ([d1c3371](https://github.com/mvanhorn/cli-printing-press/commit/d1c3371e13f9f558ff359d3d320a80ebef46d77f)), closes [#697](https://github.com/mvanhorn/cli-printing-press/issues/697)
* **cli:** gate orphan token scaffolding on auth surface ([#704](https://github.com/mvanhorn/cli-printing-press/issues/704)) ([35d7e84](https://github.com/mvanhorn/cli-printing-press/commit/35d7e842f618b0abcaa7127960344267e9f79c71))
* **cli:** parse x-mcp extension in OpenAPI parser ([#702](https://github.com/mvanhorn/cli-printing-press/issues/702)) ([41c87cf](https://github.com/mvanhorn/cli-printing-press/commit/41c87cf866eaff009988036b73dc62a30b9d0738))
* **cli:** shard sub-resource tables per parent on collision ([#707](https://github.com/mvanhorn/cli-printing-press/issues/707)) ([1023054](https://github.com/mvanhorn/cli-printing-press/commit/102305421d740138e3e5a740af3b47a5bc32b7b1))
* **cli:** source store columns from response schema, not query params ([#708](https://github.com/mvanhorn/cli-printing-press/issues/708)) ([9e039c0](https://github.com/mvanhorn/cli-printing-press/commit/9e039c0be787abf49242b668397155f6940525f7))

## [4.0.5](https://github.com/mvanhorn/cli-printing-press/compare/v4.0.4...v4.0.5) (2026-05-08)


### Bug Fixes

* **cli:** add scoped govulncheck gates ([#699](https://github.com/mvanhorn/cli-printing-press/issues/699)) ([ac75b83](https://github.com/mvanhorn/cli-printing-press/commit/ac75b83ccb7804433579d163d0798774c1c7051b))

## [4.0.4](https://github.com/mvanhorn/cli-printing-press/compare/v4.0.3...v4.0.4) (2026-05-08)


### Bug Fixes

* **cli:** dedupe regen-merge registrations by command use ([#690](https://github.com/mvanhorn/cli-printing-press/issues/690)) ([fe625c6](https://github.com/mvanhorn/cli-printing-press/commit/fe625c6bd9b0641401165a0f5659f92edf3737da))
* **cli:** strip leading Markdown headings from CLI descriptions ([#691](https://github.com/mvanhorn/cli-printing-press/issues/691)) ([b188dc2](https://github.com/mvanhorn/cli-printing-press/commit/b188dc2d607e41abb63c104be512607cf4e3436b))

## [4.0.3](https://github.com/mvanhorn/cli-printing-press/compare/v4.0.2...v4.0.3) (2026-05-07)


### Bug Fixes

* **cli:** derive Google Discovery resources from operationIds ([#680](https://github.com/mvanhorn/cli-printing-press/issues/680)) ([39cbc18](https://github.com/mvanhorn/cli-printing-press/commit/39cbc18971a10aba41bbc56a68e1710a03bb063d))

## [4.0.2](https://github.com/mvanhorn/cli-printing-press/compare/v4.0.1...v4.0.2) (2026-05-07)


### Bug Fixes

* **skills:** align publish with library-backed skill mirror ([#683](https://github.com/mvanhorn/cli-printing-press/issues/683)) ([9642032](https://github.com/mvanhorn/cli-printing-press/commit/964203239d54f342d108df9218908d6bef574ee5))

## [4.0.1](https://github.com/mvanhorn/cli-printing-press/compare/v4.0.0...v4.0.1) (2026-05-07)


### Bug Fixes

* **cli:** sync module path with v4 release ([#677](https://github.com/mvanhorn/cli-printing-press/issues/677)) ([4832173](https://github.com/mvanhorn/cli-printing-press/commit/4832173d18b9a20468f2180731f69b1870b4048b))

## [4.0.0](https://github.com/mvanhorn/cli-printing-press/compare/v3.10.0...v4.0.0) (2026-05-07)


### ⚠ BREAKING CHANGES

* **cli:** SKILL.md frontmatter shape changes for every printed CLI on next regen. The library-wide sweep that lands the same shape into already-published library entries is U6 (forthcoming, in printing-press-library).

### Features

* **cli:** add public parameter names ([#648](https://github.com/mvanhorn/cli-printing-press/issues/648)) ([cf91eab](https://github.com/mvanhorn/cli-printing-press/commit/cf91eab3bbbd748107bd31618ef357c3a93c592e))
* **cli:** Hermes/OpenClaw frontmatter alignment for printed CLIs ([#655](https://github.com/mvanhorn/cli-printing-press/issues/655)) ([fd7fa6e](https://github.com/mvanhorn/cli-printing-press/commit/fd7fa6eaec3cd5dfa5fbd1fc357def843b356125))
* **cli:** improve generated money workflows and artifact safety ([#653](https://github.com/mvanhorn/cli-printing-press/issues/653)) ([6d9ab66](https://github.com/mvanhorn/cli-printing-press/commit/6d9ab664b06e1a1c24c0014af14cae4bd40cb0f1))
* **cli:** scope verify-skill flag-names; add canonical-sections check ([#665](https://github.com/mvanhorn/cli-printing-press/issues/665)) ([c69e77c](https://github.com/mvanhorn/cli-printing-press/commit/c69e77c8109b5feec36ed4e17a94ac3eb11237dd))


### Bug Fixes

* **cli:** omit version field from SKILL.md frontmatter ([#656](https://github.com/mvanhorn/cli-printing-press/issues/656)) ([e6aa032](https://github.com/mvanhorn/cli-printing-press/commit/e6aa0326871498719683f74b22389faf3abd70d5))
* **dogfood:** accept quick live passes with skips ([#646](https://github.com/mvanhorn/cli-printing-press/issues/646)) ([c7574bd](https://github.com/mvanhorn/cli-printing-press/commit/c7574bd478eb14e1e26d2421a224bb2be8f0a214))
* **phase5:** explain accepted marker levels ([#647](https://github.com/mvanhorn/cli-printing-press/issues/647)) ([d4d10a8](https://github.com/mvanhorn/cli-printing-press/commit/d4d10a8a5fbf90ed90f9eb60613288d8bd13d7cc))
* **skills:** repair publish generated-artifact flow ([#672](https://github.com/mvanhorn/cli-printing-press/issues/672)) ([d2eca30](https://github.com/mvanhorn/cli-printing-press/commit/d2eca30552147646a520553bcccf42c80c1ebe54))

## [3.10.0](https://github.com/mvanhorn/cli-printing-press/compare/v3.9.1...v3.10.0) (2026-05-06)


### Features

* **cli:** widen auth env-var model with kind/required/sensitive metadata ([#632](https://github.com/mvanhorn/cli-printing-press/issues/632)) ([#639](https://github.com/mvanhorn/cli-printing-press/issues/639)) ([3bc1a23](https://github.com/mvanhorn/cli-printing-press/commit/3bc1a23a1a7108cf097c3b0c8fd184959f83e1e9))
* **skills:** data-driven Phase 6 menu and hold-path support ([#637](https://github.com/mvanhorn/cli-printing-press/issues/637)) ([0310d3f](https://github.com/mvanhorn/cli-printing-press/commit/0310d3fbd4d3b4288685fc5a97d46824a7af478d))
* **skills:** flatten retro issues and dedup against open issues ([#641](https://github.com/mvanhorn/cli-printing-press/issues/641)) ([7eb951e](https://github.com/mvanhorn/cli-printing-press/commit/7eb951e07f994c44846b5a7119b69527274201c9))


### Bug Fixes

* **cli:** correct no-auth 403 hints ([#629](https://github.com/mvanhorn/cli-printing-press/issues/629)) ([013f92a](https://github.com/mvanhorn/cli-printing-press/commit/013f92adc95676bed9d321e7f9d7758d43c52e11))
* **cli:** generate usable oauth auth config ([#617](https://github.com/mvanhorn/cli-printing-press/issues/617)) ([5b489da](https://github.com/mvanhorn/cli-printing-press/commit/5b489da7bc09991d53251be1e847a348b408b016))
* **cli:** harden destructive auth dogfood skips ([#628](https://github.com/mvanhorn/cli-printing-press/issues/628)) ([5004e4b](https://github.com/mvanhorn/cli-printing-press/commit/5004e4b0788dad5975eb07822417f06ca1e62957))
* **cli:** infer bearer auth from inline params ([#634](https://github.com/mvanhorn/cli-printing-press/issues/634)) ([b4a95cb](https://github.com/mvanhorn/cli-printing-press/commit/b4a95cb7d9c803354741dc63ffcc051e4b82d2aa))
* **cli:** kind-aware auth env-vars and promoted-command --limit ([#645](https://github.com/mvanhorn/cli-printing-press/issues/645)) ([0cf04f7](https://github.com/mvanhorn/cli-printing-press/commit/0cf04f73a03c16f87ca3c504bce7cceb3d8ed0e3))
* **cli:** preserve canonical auth env hints ([#633](https://github.com/mvanhorn/cli-printing-press/issues/633)) ([6b5945f](https://github.com/mvanhorn/cli-printing-press/commit/6b5945fdec5af4255b655ebda6a20faf4305b990))
* **cli:** require explicit retry no-ops ([#635](https://github.com/mvanhorn/cli-printing-press/issues/635)) ([cb3ef87](https://github.com/mvanhorn/cli-printing-press/commit/cb3ef87308c006da61b3b5e7305b0b2095540f15))
* **cli:** score composed auth and shell continuations ([#636](https://github.com/mvanhorn/cli-printing-press/issues/636)) ([2035b27](https://github.com/mvanhorn/cli-printing-press/commit/2035b276df2ea411c39d2b36382a2a3c5153d4b8))

## [3.9.1](https://github.com/mvanhorn/cli-printing-press/compare/v3.9.0...v3.9.1) (2026-05-05)


### Bug Fixes

* **cli:** align phase5 quick gate with runner verdict ([#605](https://github.com/mvanhorn/cli-printing-press/issues/605)) ([7359a18](https://github.com/mvanhorn/cli-printing-press/commit/7359a18fcdd04dd26fd9a366837ded51ccfcfdd6))
* **cli:** always derive install module from filesystem in migrate-skill-metadata ([#611](https://github.com/mvanhorn/cli-printing-press/issues/611)) ([1b0dc70](https://github.com/mvanhorn/cli-printing-press/commit/1b0dc70ac4d7e6f5992683c7fe8e5cfa5aa848a7))
* **cli:** default live dogfood happy_path on mutators to --dry-run ([#614](https://github.com/mvanhorn/cli-printing-press/issues/614)) ([24e7e60](https://github.com/mvanhorn/cli-printing-press/commit/24e7e60f1b8d3b4f59119e65f3fcc7586d6ddbb9))
* **cli:** emit ClawHub-compliant nested YAML for SKILL.md metadata ([#609](https://github.com/mvanhorn/cli-printing-press/issues/609)) ([4897c59](https://github.com/mvanhorn/cli-printing-press/commit/4897c593a34fd3cb6f155aad0aec3480e791b857))
* **cli:** emit invalidateCache() in every printed client.go ([#612](https://github.com/mvanhorn/cli-printing-press/issues/612)) ([9c5b882](https://github.com/mvanhorn/cli-printing-press/commit/9c5b882afd42c7d9baf2654e0654005b0af11ec2))
* **cli:** resolveManuscripts prefers API-slug keying over CLI-name ([#615](https://github.com/mvanhorn/cli-printing-press/issues/615)) ([700e467](https://github.com/mvanhorn/cli-printing-press/commit/700e4672f8ad937101cb21af893136badb2eb4e1))
* **cli:** route sync --json events to stdout ([#608](https://github.com/mvanhorn/cli-printing-press/issues/608)) ([6281f2b](https://github.com/mvanhorn/cli-printing-press/commit/6281f2be60295c0f5fd9e3984b7c5ca169da6dd5))
* **cli:** skip destructive-at-auth endpoints in live dogfood ([#613](https://github.com/mvanhorn/cli-printing-press/issues/613)) ([84b22d2](https://github.com/mvanhorn/cli-printing-press/commit/84b22d2817d60f40a5c37ba9486b76739b008d16))
* **cli:** stamp run_id into manifest at generate time ([#606](https://github.com/mvanhorn/cli-printing-press/issues/606)) ([37ee7e3](https://github.com/mvanhorn/cli-printing-press/commit/37ee7e38b6b3b9f6b989fa8a52172cee8cb104c8))

## [3.9.0](https://github.com/mvanhorn/cli-printing-press/compare/v3.8.0...v3.9.0) (2026-05-05)


### Features

* **cli:** add live dogfood matrix runner ([#559](https://github.com/mvanhorn/cli-printing-press/issues/559)) ([e973d5c](https://github.com/mvanhorn/cli-printing-press/commit/e973d5c505e6cab5a5ed163ed736112698d95325))
* **cli:** add spec-driven tier routing ([#570](https://github.com/mvanhorn/cli-printing-press/issues/570)) ([5bc7390](https://github.com/mvanhorn/cli-printing-press/commit/5bc73909b66eaf2c38bdb1970ff37d8067c617f1))
* **cli:** compact repeated MCP parameter descriptions ([#569](https://github.com/mvanhorn/cli-printing-press/issues/569)) ([314b565](https://github.com/mvanhorn/cli-printing-press/commit/314b5650f4025eda9c49fc6cac8fb50b1da29aae))
* **skills:** /printing-press-reprint orchestrator ([#553](https://github.com/mvanhorn/cli-printing-press/issues/553)) ([f362dd4](https://github.com/mvanhorn/cli-printing-press/commit/f362dd44ec94687bb29922ff6b41272d6a8bad3a))
* **skills:** brainstorm novel features via Task subagent ([#533](https://github.com/mvanhorn/cli-printing-press/issues/533)) ([13995da](https://github.com/mvanhorn/cli-printing-press/commit/13995da78c4d061fcd3c26492cd93b25465a774e))
* **skills:** browser-capture fallback options (chrome-MCP, computer-use) ([#529](https://github.com/mvanhorn/cli-printing-press/issues/529)) ([f245038](https://github.com/mvanhorn/cli-printing-press/commit/f2450385474ae29e9a47d79abf3abb315f869504))
* **skills:** retro files parent issue + per-WU sub-issues ([#555](https://github.com/mvanhorn/cli-printing-press/issues/555)) ([aae2af9](https://github.com/mvanhorn/cli-printing-press/commit/aae2af9fed95d1c31d7884e62215179b6440841c))


### Bug Fixes

* **cli:** add client-call reimplementation directive ([#562](https://github.com/mvanhorn/cli-printing-press/issues/562)) ([6242e10](https://github.com/mvanhorn/cli-printing-press/commit/6242e1011630ba45f94d316bb03ab739c284001e))
* **cli:** complete bearer refresh and path handling ([#584](https://github.com/mvanhorn/cli-printing-press/issues/584)) ([ebfa6bf](https://github.com/mvanhorn/cli-printing-press/commit/ebfa6bf1ff71f7b88d6acdf62627da5c784c29d1))
* **cli:** dry-run narrative examples in strict validation ([#550](https://github.com/mvanhorn/cli-printing-press/issues/550)) ([e4e66bd](https://github.com/mvanhorn/cli-printing-press/commit/e4e66bd3d726936797541cf43c074c8264eaf67e))
* **cli:** exempt generated-client source helpers ([#563](https://github.com/mvanhorn/cli-printing-press/issues/563)) ([c0639cf](https://github.com/mvanhorn/cli-printing-press/commit/c0639cf09d1d58cf555c54ea7f451f5f14e73007))
* **cli:** gate publishing on Phase 5 proof ([#558](https://github.com/mvanhorn/cli-printing-press/issues/558)) ([7a14381](https://github.com/mvanhorn/cli-printing-press/commit/7a1438182b97a2707840b3d7813aa758626c69b5))
* **cli:** generator template polish (WU-1, F1+F2+F3+F6) ([#576](https://github.com/mvanhorn/cli-printing-press/issues/576)) ([542835d](https://github.com/mvanhorn/cli-printing-press/commit/542835d393bf3efdc5870ef9c00feedbe0f0edf6))
* **cli:** include cobratree tools in MCP token scoring ([#552](https://github.com/mvanhorn/cli-printing-press/issues/552)) ([74eb876](https://github.com/mvanhorn/cli-printing-press/commit/74eb87692cb040a7b782684442a7557d0530e509))
* **cli:** live-dogfood matrix accuracy (WU-2, F4+F5) ([#577](https://github.com/mvanhorn/cli-printing-press/issues/577)) ([7572b1e](https://github.com/mvanhorn/cli-printing-press/commit/7572b1e09ba4ae8ef4e29fe02e73fa799993e14e))
* **cli:** parse epoch Retry-After headers ([#565](https://github.com/mvanhorn/cli-printing-press/issues/565)) ([e588bf7](https://github.com/mvanhorn/cli-printing-press/commit/e588bf7ed1d14dda8933a9774eb3af115331cf1f))
* **cli:** preserve operation-routing path params ([#567](https://github.com/mvanhorn/cli-printing-press/issues/567)) ([1854212](https://github.com/mvanhorn/cli-printing-press/commit/18542129e8ec10d67735c227efbad8772bac9704))
* **cli:** preserve query params in generated MCP handlers ([#582](https://github.com/mvanhorn/cli-printing-press/issues/582)) ([e9696c9](https://github.com/mvanhorn/cli-printing-press/commit/e9696c9b84fa77863e66acb165c2603d12513612))
* **cli:** reclaim locks from dead owners ([#564](https://github.com/mvanhorn/cli-printing-press/issues/564)) ([34413e9](https://github.com/mvanhorn/cli-printing-press/commit/34413e94ccb06fd0824f78dfce89bd1b887b8d02))
* **cli:** separate sampled probes from live verification ([#560](https://github.com/mvanhorn/cli-printing-press/issues/560)) ([b39a23d](https://github.com/mvanhorn/cli-printing-press/commit/b39a23d08620dc871f581742487f9168c468e996))
* **cli:** session_handshake auth correctness pass ([#534](https://github.com/mvanhorn/cli-printing-press/issues/534)) ([96dd07c](https://github.com/mvanhorn/cli-printing-press/commit/96dd07c06c236e49ef55dd9ba9ae33c6dd65ae9f))
* **cli:** support OpenAPI auth metadata overrides ([#561](https://github.com/mvanhorn/cli-printing-press/issues/561)) ([35fa7af](https://github.com/mvanhorn/cli-printing-press/commit/35fa7afe997c8f3be8461749a86e389e1583e032))
* **cli:** sync root highlights from verified features ([#549](https://github.com/mvanhorn/cli-printing-press/issues/549)) ([8b099ba](https://github.com/mvanhorn/cli-printing-press/commit/8b099ba060cdae07197d40c3b709eef17e04391b))
* **skills:** speed up retro issue filing ([#575](https://github.com/mvanhorn/cli-printing-press/issues/575)) ([708a615](https://github.com/mvanhorn/cli-printing-press/commit/708a6159eefc96147ad73c0665df9088951be64d))
* **skills:** tighten publish and polish workflows ([#568](https://github.com/mvanhorn/cli-printing-press/issues/568)) ([847f7c5](https://github.com/mvanhorn/cli-printing-press/commit/847f7c5b31ef51b309eb9193a17aadada3063788))

## [3.8.0](https://github.com/mvanhorn/cli-printing-press/compare/v3.7.0...v3.8.0) (2026-05-03)


### Features

* **cli:** OAuth2 client_credentials grant + bearer_token AuthHeader precedence fix ([#528](https://github.com/mvanhorn/cli-printing-press/issues/528)) ([593ce97](https://github.com/mvanhorn/cli-printing-press/commit/593ce970a252630f7da42f78dcb885564bf518da))


### Bug Fixes

* **cli:** FedEx retro batch — 5 small fixes (WU-2, WU-3, WU-4, WU-5, WU-7) ([#526](https://github.com/mvanhorn/cli-printing-press/issues/526)) ([e761d04](https://github.com/mvanhorn/cli-printing-press/commit/e761d04d9d0670871e49586352f4bf3e9077f816))

## [3.7.0](https://github.com/mvanhorn/cli-printing-press/compare/v3.6.2...v3.7.0) (2026-05-03)


### Features

* **skills:** pre-generation MCP enrichment for large surfaces ([#522](https://github.com/mvanhorn/cli-printing-press/issues/522)) ([53be120](https://github.com/mvanhorn/cli-printing-press/commit/53be12097d48f1b22fd05aab43c90fa6b52df4e8))
* **skills:** retro defaults to don't-file; adversarial triage gates ([#524](https://github.com/mvanhorn/cli-printing-press/issues/524)) ([3c04a06](https://github.com/mvanhorn/cli-printing-press/commit/3c04a0656324c82ea41bac6191d822904eb24953))

## [3.6.2](https://github.com/mvanhorn/cli-printing-press/compare/v3.6.1...v3.6.2) (2026-05-03)


### Bug Fixes

* **cli:** MCP template ports — NoCache=true + codeOrch stopword filter (refs [#515](https://github.com/mvanhorn/cli-printing-press/issues/515) F1, F4) ([#521](https://github.com/mvanhorn/cli-printing-press/issues/521)) ([734e00d](https://github.com/mvanhorn/cli-printing-press/commit/734e00d14d73c55de921c8840910900b6ff041cd))
* **cli:** scorer respects runtime MCP surface selection (refs [#516](https://github.com/mvanhorn/cli-printing-press/issues/516) WU-A4) ([#519](https://github.com/mvanhorn/cli-printing-press/issues/519)) ([358e46a](https://github.com/mvanhorn/cli-printing-press/commit/358e46a67a8e961d6096f0290d32429fd6cd2960))

## [3.6.1](https://github.com/mvanhorn/cli-printing-press/compare/v3.6.0...v3.6.1) (2026-05-02)


### Bug Fixes

* **cli:** OpenAPI parser walks per-operation servers as fallback ([#511](https://github.com/mvanhorn/cli-printing-press/issues/511)) ([702f54a](https://github.com/mvanhorn/cli-printing-press/commit/702f54a1455919be3516a104266f30ea3216fd60))

## [3.6.0](https://github.com/mvanhorn/cli-printing-press/compare/v3.5.0...v3.6.0) (2026-05-02)


### Features

* **cli:** README template links to release page for binary + MCPB ([#508](https://github.com/mvanhorn/cli-printing-press/issues/508)) ([73468ba](https://github.com/mvanhorn/cli-printing-press/commit/73468bac246ff3116cf2d85693c35a80e5ece7b2))


### Bug Fixes

* **cli:** handle sentry openapi generation ([#507](https://github.com/mvanhorn/cli-printing-press/issues/507)) ([e37cff9](https://github.com/mvanhorn/cli-printing-press/commit/e37cff9d36101695268636badbeb341670f9e9ff))

## [3.5.0](https://github.com/mvanhorn/cli-printing-press/compare/v3.4.3...v3.5.0) (2026-05-02)


### Features

* **cli:** JSON:API support for OpenAPI parser (envelope, page[cursor], x-prefix) ([#505](https://github.com/mvanhorn/cli-printing-press/issues/505)) ([5584818](https://github.com/mvanhorn/cli-printing-press/commit/5584818b5ce40dab67923a0ed5ffb55944bb6757))


### Bug Fixes

* **cli:** honor documented typed exit codes ([#504](https://github.com/mvanhorn/cli-printing-press/issues/504)) ([31e63be](https://github.com/mvanhorn/cli-printing-press/commit/31e63be4587d1b497f409b55d73ad414e73b6dfa))
* **cli:** route discriminator sync to typed tables ([#503](https://github.com/mvanhorn/cli-printing-press/issues/503)) ([97180c0](https://github.com/mvanhorn/cli-printing-press/commit/97180c037ba976b528ebc9e1d9ff7e58ca8b630f))
* **cli:** route resource base urls through mcp surfaces ([#500](https://github.com/mvanhorn/cli-printing-press/issues/500)) ([2817bdc](https://github.com/mvanhorn/cli-printing-press/commit/2817bdc747b2655d34030bafa80a561866574ec1))
* **cli:** strip build/ from publish staging ([#502](https://github.com/mvanhorn/cli-printing-press/issues/502)) ([263be3b](https://github.com/mvanhorn/cli-printing-press/commit/263be3b3d24ecae4a9995fda9b98de14dd7f2dfe))

## [3.4.3](https://github.com/mvanhorn/cli-printing-press/compare/v3.4.2...v3.4.3) (2026-05-02)


### Bug Fixes

* **cli:** README template recommends MCPB drag-and-drop and skill install ([#498](https://github.com/mvanhorn/cli-printing-press/issues/498)) ([0a741d3](https://github.com/mvanhorn/cli-printing-press/commit/0a741d32fad4a930c52378cd2d1d1a111b51bcae))

## [3.4.2](https://github.com/mvanhorn/cli-printing-press/compare/v3.4.1...v3.4.2) (2026-05-02)


### Code Refactoring

* **cli:** reuse generated operation id params ([#496](https://github.com/mvanhorn/cli-printing-press/issues/496)) ([3110d5e](https://github.com/mvanhorn/cli-printing-press/commit/3110d5ec64f3ee56786a5bedea4a063d21ea0fe7))

## [3.4.1](https://github.com/mvanhorn/cli-printing-press/compare/v3.4.0...v3.4.1) (2026-05-02)


### Bug Fixes

* **cli:** normalize lock name so slug and binary form share lock file ([#493](https://github.com/mvanhorn/cli-printing-press/issues/493)) ([820ba7b](https://github.com/mvanhorn/cli-printing-press/commit/820ba7b1fb543ee9b8c663c3f39b5a7f284822db))
* **cli:** preserve Unicode in OpenAPI display_name ([#492](https://github.com/mvanhorn/cli-printing-press/issues/492)) ([67597db](https://github.com/mvanhorn/cli-printing-press/commit/67597dbb1a795ac19b5643f7cf06b266cc48ef28))

## [3.4.0](https://github.com/mvanhorn/cli-printing-press/compare/v3.3.0...v3.4.0) (2026-05-02)


### Features

* **cli:** add curated Shopify wrapper spec ([#476](https://github.com/mvanhorn/cli-printing-press/issues/476)) ([40cefcb](https://github.com/mvanhorn/cli-printing-press/commit/40cefcb5cf8f48a9a7b286f4a1486f8c7680bc5e))


### Bug Fixes

* **cli:** mcp-sync stops conflating API slug with binary name ([#487](https://github.com/mvanhorn/cli-printing-press/issues/487)) ([2899613](https://github.com/mvanhorn/cli-printing-press/commit/2899613de8060e80778fe7ae00ea737d74c5db50))
* **cli:** scorecard APIName drops binary suffix ([#489](https://github.com/mvanhorn/cli-printing-press/issues/489)) ([c76d3ec](https://github.com/mvanhorn/cli-printing-press/commit/c76d3ec98137dd13d566a161f886f9eace8f608d))
* **cli:** scorer accuracy — insight prefix, MCP dir selection, freshness coupling ([#486](https://github.com/mvanhorn/cli-printing-press/issues/486)) ([9f26551](https://github.com/mvanhorn/cli-printing-press/commit/9f265510549d5563804d13872301f9b94831f6b5))
* **generator:** three template defaults polish has to fix manually ([#480](https://github.com/mvanhorn/cli-printing-press/issues/480)) ([#485](https://github.com/mvanhorn/cli-printing-press/issues/485)) ([aab364b](https://github.com/mvanhorn/cli-printing-press/commit/aab364be44d72284b99785cd4a541fddf34dc7e2))
* **scorer:** live-check treats CLI's graceful empty-handling as PASS, not FAIL ([#488](https://github.com/mvanhorn/cli-printing-press/issues/488)) ([bd102cf](https://github.com/mvanhorn/cli-printing-press/commit/bd102cf047bb542288d26e019dde96fc20893b6b))

## [3.3.0](https://github.com/mvanhorn/cli-printing-press/compare/v3.2.1...v3.3.0) (2026-05-02)


### Features

* **generator:** owner precedence chain — preserve attribution on regen ([#470](https://github.com/mvanhorn/cli-printing-press/issues/470)) ([d1a8a46](https://github.com/mvanhorn/cli-printing-press/commit/d1a8a467bf0a64cd75f43b40fee270b22b8186b2))
* **regenmerge:** add TEMPLATED-BODY-DRIFT verdict to catch in-place body edits ([#468](https://github.com/mvanhorn/cli-printing-press/issues/468)) ([abde446](https://github.com/mvanhorn/cli-printing-press/commit/abde446ebe376def6f2380d9c400a2132da3e07b))


### Bug Fixes

* **browsersniff:** accept v2-shape traffic-analysis.json on load ([#474](https://github.com/mvanhorn/cli-printing-press/issues/474)) ([#478](https://github.com/mvanhorn/cli-printing-press/issues/478)) ([63c9618](https://github.com/mvanhorn/cli-printing-press/commit/63c9618cfc9d61745189c84b96df96bfe188601d))
* **regenmerge:** correct owner rewrite + skip injection on preserved hosts ([#477](https://github.com/mvanhorn/cli-printing-press/issues/477)) ([6d45d22](https://github.com/mvanhorn/cli-printing-press/commit/6d45d2287817140223dd9e851738886633197bfe)), closes [#471](https://github.com/mvanhorn/cli-printing-press/issues/471) [#472](https://github.com/mvanhorn/cli-printing-press/issues/472)

## [3.2.1](https://github.com/mvanhorn/cli-printing-press/compare/v3.2.0...v3.2.1) (2026-05-01)


### Bug Fixes

* **generator:** gate table creation on hasDomainUpsert ([#465](https://github.com/mvanhorn/cli-printing-press/issues/465) follow-up) ([#467](https://github.com/mvanhorn/cli-printing-press/issues/467)) ([05092ed](https://github.com/mvanhorn/cli-printing-press/commit/05092ed1ed8fbbadec5193dcaba1d71d841a5e55))
* **generator:** wrapWithProvenance handles non-JSON response bodies ([#464](https://github.com/mvanhorn/cli-printing-press/issues/464)) ([eeb12ea](https://github.com/mvanhorn/cli-printing-press/commit/eeb12ea0c1c1cd18bff2489bab8e003f18a5e767))
* **openapi:** preserve params on single-endpoint specs ([#465](https://github.com/mvanhorn/cli-printing-press/issues/465)) ([45a7722](https://github.com/mvanhorn/cli-printing-press/commit/45a7722b182d8b046317b2ba5a0bf1d2bd5d3ab4))

## [3.2.0](https://github.com/mvanhorn/cli-printing-press/compare/v3.1.0...v3.2.0) (2026-05-01)


### Features

* **cli:** add regen-merge subcommand for library template sweeps ([#461](https://github.com/mvanhorn/cli-printing-press/issues/461)) ([4c7bd42](https://github.com/mvanhorn/cli-printing-press/commit/4c7bd42dc92036a642d5f9393d3e454d597227bc))


### Bug Fixes

* **cli:** unbreak HasStore + non-GET promoted commands ([#425](https://github.com/mvanhorn/cli-printing-press/issues/425)) ([#459](https://github.com/mvanhorn/cli-printing-press/issues/459)) ([a3ea32e](https://github.com/mvanhorn/cli-printing-press/commit/a3ea32edaf982e8d258339dbf2c0b006ea53c4ae))

## [3.1.0](https://github.com/mvanhorn/cli-printing-press/compare/v3.0.1...v3.1.0) (2026-05-01)


### Features

* **cli,skills:** five remaining retro WUs from postman-explore — store concurrency, narrative validator, proxy joiner, browser-sniff docs ([#440](https://github.com/mvanhorn/cli-printing-press/issues/440)) ([d24f60e](https://github.com/mvanhorn/cli-printing-press/commit/d24f60edc2fae62bf963b1ec1465ac73497cc139))
* **cli,skills:** three retro WUs from postman-explore — POST-as-query detection, novel-command --select, scorecard YAML ([#424](https://github.com/mvanhorn/cli-printing-press/issues/424)) ([8ceea6a](https://github.com/mvanhorn/cli-printing-press/commit/8ceea6ad65a0b27784f0134e20cb55827e21b645))
* **cli:** add validate-narrative subcommand to replace bash recipe ([#433](https://github.com/mvanhorn/cli-printing-press/issues/433)) ([#456](https://github.com/mvanhorn/cli-printing-press/issues/456)) ([8a548d0](https://github.com/mvanhorn/cli-printing-press/commit/8a548d098ccca4474367c5cf8268759c412e766b))
* **cli:** cost-based throttling primitives for GraphQL CLIs ([#434](https://github.com/mvanhorn/cli-printing-press/issues/434)) ([de3a4e7](https://github.com/mvanhorn/cli-printing-press/commit/de3a4e7c07e1182ebb8b1e921f50ad5e7cb453d9))
* **cli:** framework-collision detection in resource-name extraction ([#444](https://github.com/mvanhorn/cli-printing-press/issues/444)) ([6d357b3](https://github.com/mvanhorn/cli-printing-press/commit/6d357b3685fce25fecfb303c7e32a9353a22ac0d))
* **cli:** WU-2 sync correctness pass (pagination, ID extraction, exit policy, write serialization) ([#430](https://github.com/mvanhorn/cli-printing-press/issues/430)) ([9a13506](https://github.com/mvanhorn/cli-printing-press/commit/9a13506232e9fd8396e16b1c71ab7dbf07e23345))
* **skills:** preflight gate with interactive upgrade prompt ([#420](https://github.com/mvanhorn/cli-printing-press/issues/420)) ([332e419](https://github.com/mvanhorn/cli-printing-press/commit/332e419b7c7a9de79f4b9b8b877462956a9a499d))


### Bug Fixes

* **ci:** disable golangci-lint remote schema verify ([#432](https://github.com/mvanhorn/cli-printing-press/issues/432)) ([b6c7089](https://github.com/mvanhorn/cli-printing-press/commit/b6c70893260b384cb23e384062e73ce92bf924f7))
* **cli:** cliutil.BuildPath replaces buildProxyPath with proper URL encoding ([#437](https://github.com/mvanhorn/cli-printing-press/issues/437), [#439](https://github.com/mvanhorn/cli-printing-press/issues/439)) ([#443](https://github.com/mvanhorn/cli-printing-press/issues/443)) ([d9472b7](https://github.com/mvanhorn/cli-printing-press/commit/d9472b76b10ae2ab5653713e9fc63a77f20efed5))
* **cli:** emit mcp:read-only + plumb body fields in promoted commands ([#426](https://github.com/mvanhorn/cli-printing-press/issues/426), [#427](https://github.com/mvanhorn/cli-printing-press/issues/427)) ([#449](https://github.com/mvanhorn/cli-printing-press/issues/449)) ([6aee620](https://github.com/mvanhorn/cli-printing-press/commit/6aee620512ba397f65f8217247515fb77a8452ba))
* **cli:** emit StringVar for cursor/page/timestamp pagination flags ([#435](https://github.com/mvanhorn/cli-printing-press/issues/435)) ([1556ff1](https://github.com/mvanhorn/cli-printing-press/commit/1556ff1b78f0d6dd7e60ce0e07562769dd2fe80d))
* **cli:** migrate 15 templates off flags.printJSON + dogfood regression guard ([#428](https://github.com/mvanhorn/cli-printing-press/issues/428)) ([#447](https://github.com/mvanhorn/cli-printing-press/issues/447)) ([1e4798f](https://github.com/mvanhorn/cli-printing-press/commit/1e4798f1ed7a8b7782089e3e54b7cca1e9d167c9))
* **cli:** pagination cursor lookup recurses into well-known wrapper objects ([#157](https://github.com/mvanhorn/cli-printing-press/issues/157) F3) ([#453](https://github.com/mvanhorn/cli-printing-press/issues/453)) ([a197d41](https://github.com/mvanhorn/cli-printing-press/commit/a197d4146d53ffb17db372b2bc1cea161fc9a9d8))
* **cli:** retro [#421](https://github.com/mvanhorn/cli-printing-press/issues/421) WU-4, WU-6, WU-7 — search empty JSON, live-check tokenizer, raw database/sql carve-out ([#446](https://github.com/mvanhorn/cli-printing-press/issues/446)) ([ba22f30](https://github.com/mvanhorn/cli-printing-press/commit/ba22f30adacf032a4a180a339859e6d664d094c4))
* **cli:** store.OpenWithContext + fast-path version check ([#436](https://github.com/mvanhorn/cli-printing-press/issues/436), [#438](https://github.com/mvanhorn/cli-printing-press/issues/438)) ([#441](https://github.com/mvanhorn/cli-printing-press/issues/441)) ([c11d0d9](https://github.com/mvanhorn/cli-printing-press/commit/c11d0d952d388055772722080dd35ef0dd5dfeee))
* **skills:** add Phase 3 starter templates for novel feature commands ([#451](https://github.com/mvanhorn/cli-printing-press/issues/451)) ([00f8321](https://github.com/mvanhorn/cli-printing-press/commit/00f83219a2f94e3cb8335121053df77f076a0c40))
* **skills:** keep absorb gate showcase as its own turn ([#418](https://github.com/mvanhorn/cli-printing-press/issues/418)) ([30370eb](https://github.com/mvanhorn/cli-printing-press/commit/30370ebb31ba7ee81e75e1fd4de8961bf2a835df))
* **skills:** sharpen retro cardinal rule to name over-fitted machine changes ([#422](https://github.com/mvanhorn/cli-printing-press/issues/422)) ([337840c](https://github.com/mvanhorn/cli-printing-press/commit/337840c84eaabe4f6dfcd9f34c52910b851d9e6c))
* **skills:** tighten human-time-estimate cardinal rule + clean up violations ([#452](https://github.com/mvanhorn/cli-printing-press/issues/452)) ([c78cb18](https://github.com/mvanhorn/cli-printing-press/commit/c78cb18130c28183c79507d42279c99e854e7995))


### Code Refactoring

* **cli:** extract body-map block to a shared helper ([#450](https://github.com/mvanhorn/cli-printing-press/issues/450)) ([#457](https://github.com/mvanhorn/cli-printing-press/issues/457)) ([2c59d1c](https://github.com/mvanhorn/cli-printing-press/commit/2c59d1c60de40cc75af5f5b38106afa9fa7ca6ab))

## [3.0.1](https://github.com/mvanhorn/cli-printing-press/compare/v3.0.0...v3.0.1) (2026-04-30)


### Bug Fixes

* **cli:** repair v3 release — version regex, module path, CI gate ([#415](https://github.com/mvanhorn/cli-printing-press/issues/415)) ([4e1db1e](https://github.com/mvanhorn/cli-printing-press/commit/4e1db1e7612431f7d711f12d45c29557910238cf))
* **skills:** drop context: fork from output-review so AskUserQuestion works natively ([#417](https://github.com/mvanhorn/cli-printing-press/issues/417)) ([ac98c00](https://github.com/mvanhorn/cli-printing-press/commit/ac98c00ac588d8a5e9d0b7c22c59afc0773d29d3))

## [3.0.0](https://github.com/mvanhorn/cli-printing-press/compare/v2.4.0...v3.0.0) (2026-04-30)


### ⚠ BREAKING CHANGES

* **cli:** remove megamcp aggregate server and Composio plan ([#363](https://github.com/mvanhorn/cli-printing-press/issues/363))

### chore

* **cli:** remove megamcp aggregate server and Composio plan ([#363](https://github.com/mvanhorn/cli-printing-press/issues/363)) ([0e1b850](https://github.com/mvanhorn/cli-printing-press/commit/0e1b850854510005ce60ab2153a024e993c5fdea))


### Features

* **cli,skills:** generator-time MCP description enrichment from spec ([#396](https://github.com/mvanhorn/cli-printing-press/issues/396)) ([2b7a51e](https://github.com/mvanhorn/cli-printing-press/commit/2b7a51e1bec9e81dd3b340f8cfd680126e75af39)), closes [#384](https://github.com/mvanhorn/cli-printing-press/issues/384)
* **cli,skills:** polish ledger per-item enforcement + AGENTS.md guidance ([#389](https://github.com/mvanhorn/cli-printing-press/issues/389)) ([0320269](https://github.com/mvanhorn/cli-printing-press/commit/0320269d9e4211aaa3c3996e41f6407d0fd8a2bf))
* **cli:** add GraphQLEndpointPath and EndpointTemplateVars to APISpec ([73d136f](https://github.com/mvanhorn/cli-printing-press/commit/73d136f732bbd012434458f37b9826dab60842c9))
* **cli:** add tools-audit subcommand and merge polish into a single skill ([#378](https://github.com/mvanhorn/cli-printing-press/issues/378)) ([e4a0089](https://github.com/mvanhorn/cli-printing-press/commit/e4a0089c9580e7aab2ac7c6cb6987625c0830d95))
* **cli:** emit buildURL helper + Config.TemplateVars for templated endpoints ([289e598](https://github.com/mvanhorn/cli-printing-press/commit/289e5988d6615058f9f7a892ec3a4a88ea8f11c2))
* **cli:** emit MCP tools for novel CLI features via shell-out ([#358](https://github.com/mvanhorn/cli-printing-press/issues/358)) ([7f248d5](https://github.com/mvanhorn/cli-printing-press/commit/7f248d5995da3c5687b47f84d5e5087c4e413e3b))
* **cli:** generator emits mcp:read-only on read-only novel commands ([#401](https://github.com/mvanhorn/cli-printing-press/issues/401)) ([8525915](https://github.com/mvanhorn/cli-printing-press/commit/852591596a8d2f175c1a586bc554b57252b1a411))
* **cli:** MCP description overrides + tools-audit scoring + polish skill nibbles ([#382](https://github.com/mvanhorn/cli-printing-press/issues/382)) ([e651d01](https://github.com/mvanhorn/cli-printing-press/commit/e651d01f09118dc55eb1d385d70e12492560a810))
* **cli:** MCP tool surface mirrors Cobra tree at runtime, with mcp-sync backfill ([#367](https://github.com/mvanhorn/cli-printing-press/issues/367)) ([358ff78](https://github.com/mvanhorn/cli-printing-press/commit/358ff782ccf9240514e15f9531850c04927b08a2))
* **cli:** mcp-sync reads display_name from public library registry.json fallback ([#391](https://github.com/mvanhorn/cli-printing-press/issues/391)) ([64f6798](https://github.com/mvanhorn/cli-printing-press/commit/64f67981e8979e53f09880438665d7c81697ad63))
* **cli:** MCPB manifest and bundle support for generated CLIs ([#355](https://github.com/mvanhorn/cli-printing-press/issues/355)) ([af21450](https://github.com/mvanhorn/cli-printing-press/commit/af21450994270738936728f5cfefabb75dadba9b))
* **cli:** per-resource base_url override in spec schema ([#405](https://github.com/mvanhorn/cli-printing-press/issues/405)) ([6586c0a](https://github.com/mvanhorn/cli-printing-press/commit/6586c0a28b135134cab5d5a8f3a4e3156e3c71bc))
* **cli:** split BaseURL from GraphQLEndpointPath in GraphQL parser + client ([699e8ed](https://github.com/mvanhorn/cli-printing-press/commit/699e8ed750158b1644159e06ac0ab894f25c1dc6))
* **cli:** substitute EndpointTemplateVars from env at runtime ([8b470bc](https://github.com/mvanhorn/cli-printing-press/commit/8b470bc790b591fe66e78360a9ea87017cde341b))
* **cli:** substitute EndpointTemplateVars in client.do() + cache key ([8cb57db](https://github.com/mvanhorn/cli-printing-press/commit/8cb57db37db3806997af5520ed79f6d4bc37daf3))
* **cli:** tools-audit checks MCP descriptions in tools-manifest.json ([#381](https://github.com/mvanhorn/cli-printing-press/issues/381)) ([fee6802](https://github.com/mvanhorn/cli-printing-press/commit/fee6802ec499315d197c543d87be2a6b0c79a618))
* **skills:** add /printing-press-import to bring published CLIs into internal library ([#398](https://github.com/mvanhorn/cli-printing-press/issues/398)) ([262ab87](https://github.com/mvanhorn/cli-printing-press/commit/262ab87681c6daeb43fcd5685443874faa5a90cc))
* **skills:** add divergence check to polish skill setup ([#380](https://github.com/mvanhorn/cli-printing-press/issues/380)) ([6727d0d](https://github.com/mvanhorn/cli-printing-press/commit/6727d0d8a5dbbeda949b6e7d436b60b4e7e316dc))


### Bug Fixes

* **ci:** remove redundant generated compile work ([#361](https://github.com/mvanhorn/cli-printing-press/issues/361)) ([a5ba1b4](https://github.com/mvanhorn/cli-printing-press/commit/a5ba1b4a4721477b1524dc2cb891848be23e8642))
* **ci:** shard test lanes ([#362](https://github.com/mvanhorn/cli-printing-press/issues/362)) ([95cf6c8](https://github.com/mvanhorn/cli-printing-press/commit/95cf6c88e4e29444a2947b31ade29b7c29288be9))
* **ci:** speed up generated compile tests and caches ([#360](https://github.com/mvanhorn/cli-printing-press/issues/360)) ([ddacddb](https://github.com/mvanhorn/cli-printing-press/commit/ddacddba7e82c64fb58cc0e7cb82c1d3e720c3eb))
* **cli,skills:** manifest display_name preservation + divergence-check exemptions ([#390](https://github.com/mvanhorn/cli-printing-press/issues/390)) ([401bda5](https://github.com/mvanhorn/cli-printing-press/commit/401bda54e3059fe89fc55dd2be53541a74a57424)), closes [#387](https://github.com/mvanhorn/cli-printing-press/issues/387)
* **cli:** add source rate-limit guardrails ([#366](https://github.com/mvanhorn/cli-printing-press/issues/366)) ([174d23c](https://github.com/mvanhorn/cli-printing-press/commit/174d23c8fb3ab22e6c3cd2073a7fcd7cc2ddf40c))
* **cli:** dogfood agent-context discovery reads stdout only ([#407](https://github.com/mvanhorn/cli-printing-press/issues/407)) ([fe1e6d7](https://github.com/mvanhorn/cli-printing-press/commit/fe1e6d77ecd61a087e6e86b4b513efc137c59ce9))
* **cli:** keep template-var env names out of verifier auth discovery ([a0e1bb4](https://github.com/mvanhorn/cli-printing-press/commit/a0e1bb4aae8d8bf40d2db99210fc48615551e413))
* **cli:** manifest description no longer doubles 'API' when display_name ends in 'API' ([#397](https://github.com/mvanhorn/cli-printing-press/issues/397)) ([fae7e6e](https://github.com/mvanhorn/cli-printing-press/commit/fae7e6e09309dc5ca77ae063849fc60feee85a39)), closes [#393](https://github.com/mvanhorn/cli-printing-press/issues/393)
* **cli:** manifest emission no longer gated by stale mcp_ready label ([#359](https://github.com/mvanhorn/cli-printing-press/issues/359)) ([2e892e3](https://github.com/mvanhorn/cli-printing-press/commit/2e892e368f3c3249a674b18da27402d47debbafa))
* **cli:** mcp-sync auto-fixes spec.yaml name drift for internal YAML specs ([#400](https://github.com/mvanhorn/cli-printing-press/issues/400)) ([d1b9cd2](https://github.com/mvanhorn/cli-printing-press/commit/d1b9cd27835c986ba4754d69c62946c492ad1467))
* **cli:** mcp-sync bumps mcp-go pin when migrating to cobratree surface ([#404](https://github.com/mvanhorn/cli-printing-press/issues/404)) ([7670fe9](https://github.com/mvanhorn/cli-printing-press/commit/7670fe98bc32df56518e5789a9b6f5bd694ce9fa))
* **cli:** mcp-sync handles older library CLI drift (cliutil + name validation) ([#369](https://github.com/mvanhorn/cli-printing-press/issues/369)) ([1a4bcae](https://github.com/mvanhorn/cli-printing-press/commit/1a4bcae8f94654c64686aee6494bf955a70acfba))
* **cli:** mcp-sync preserves manifest brand-casing and hand-edited descriptions ([#373](https://github.com/mvanhorn/cli-printing-press/issues/373)) ([9f57c41](https://github.com/mvanhorn/cli-printing-press/commit/9f57c41d693463110979cfa8d05abbe95600f050))
* **cli:** mcp-sync refreshes .printing-press.json from spec.yaml ([#375](https://github.com/mvanhorn/cli-printing-press/issues/375)) ([40d1ffa](https://github.com/mvanhorn/cli-printing-press/commit/40d1ffadcdaa582a61953a4239a204eda1886334))
* **cli:** mcp-sync skips deliver/suggestFlag prolog blocks when absent ([#376](https://github.com/mvanhorn/cli-printing-press/issues/376)) ([2b9f2fb](https://github.com/mvanhorn/cli-printing-press/commit/2b9f2fbbecac2c8a2c4a622cd4966f0004d8fab7))
* **cli:** OpenAPI parser prefers description over summary on operations ([#411](https://github.com/mvanhorn/cli-printing-press/issues/411)) ([cb91024](https://github.com/mvanhorn/cli-printing-press/commit/cb91024d4b6a216ad15f4f8f4683d1f375c2ce50))
* **cli:** scorecard human output shows N/A for opt-out dimensions ([#406](https://github.com/mvanhorn/cli-printing-press/issues/406)) ([0954bca](https://github.com/mvanhorn/cli-printing-press/commit/0954bcafcc0142cd0517aa2faf48c2e0ba4ce687))
* **cli:** tools-manifest.json classifies reclassified path params as path, not query ([#394](https://github.com/mvanhorn/cli-printing-press/issues/394)) ([cdd9153](https://github.com/mvanhorn/cli-printing-press/commit/cdd9153b15e9c3a746c612fe12195ce3da9f68f1))
* **cli:** transliterate spec strings to ASCII via Unidecode at every chokepoint ([#371](https://github.com/mvanhorn/cli-printing-press/issues/371)) ([283c992](https://github.com/mvanhorn/cli-printing-press/commit/283c9923a771c87c9f8b67f47adebabb7a801e75))
* **cli:** wire auth.optional through CLIManifest to MCPB user_config Required ([#374](https://github.com/mvanhorn/cli-printing-press/issues/374)) ([a158a62](https://github.com/mvanhorn/cli-printing-press/commit/a158a62984b796eb90eb3e09554fe2f8785b88ac))
* **skills:** drop context: fork from polish so AskUserQuestion works natively ([#410](https://github.com/mvanhorn/cli-printing-press/issues/410)) ([0683ef1](https://github.com/mvanhorn/cli-printing-press/commit/0683ef1f2790672f969f946c6fe34fcf2a85b3bd))
* **skills:** polish loads AskUserQuestion via ToolSearch in forked context ([#399](https://github.com/mvanhorn/cli-printing-press/issues/399)) ([2d9efda](https://github.com/mvanhorn/cli-printing-press/commit/2d9efda56103259ab474f4101a38f4f383a6f732))
* **skills:** polish syncs novel_features; publish detects squash-zombie branches ([#414](https://github.com/mvanhorn/cli-printing-press/issues/414)) ([fe5d65c](https://github.com/mvanhorn/cli-printing-press/commit/fe5d65c5178985b2e22331692ac7f2e2b6b4e240))


### Code Refactoring

* **skills:** extract Phase 4.85 into printing-press-output-review sub-skill ([#379](https://github.com/mvanhorn/cli-printing-press/issues/379)) ([a9ca3f8](https://github.com/mvanhorn/cli-printing-press/commit/a9ca3f80e2f9291df7d4006c9009667314eb24fa))

## [2.4.0](https://github.com/mvanhorn/cli-printing-press/compare/v2.3.9...v2.4.0) (2026-04-27)


### Features

* **cli:** add printing-press shipcheck umbrella + Phase 4 enforcement + polish-worker hook ([#353](https://github.com/mvanhorn/cli-printing-press/issues/353)) ([494abe4](https://github.com/mvanhorn/cli-printing-press/commit/494abe48c4c3dd5cb1fb26a6e5321c4f50939776))
* **cli:** add probe-reachability for no-browser challenge classification ([#331](https://github.com/mvanhorn/cli-printing-press/issues/331)) ([09dccfb](https://github.com/mvanhorn/cli-printing-press/commit/09dccfb9e3124cac54be3ad40603a36b5da9079b))
* **cli:** add unknown-command check + sync test for verify-skill ([#339](https://github.com/mvanhorn/cli-printing-press/issues/339)) ([907c698](https://github.com/mvanhorn/cli-printing-press/commit/907c6986b51119eb767a0d82f8d33361b62e1499))
* **cli:** emit promoted-leaf paths in SKILL.md ([#338](https://github.com/mvanhorn/cli-printing-press/issues/338)) ([4219e71](https://github.com/mvanhorn/cli-printing-press/commit/4219e712e6c6014d9eab9b1fca92601c2812994e))
* **cli:** printing-press machine fixes from food52 retro ([#337](https://github.com/mvanhorn/cli-printing-press/issues/337)) ([#342](https://github.com/mvanhorn/cli-printing-press/issues/342)) ([5fa694a](https://github.com/mvanhorn/cli-printing-press/commit/5fa694a07b8f39becc53189c5c9765d98c1c27c7))
* **skills:** reconcile prior novel features on reprint ([#329](https://github.com/mvanhorn/cli-printing-press/issues/329)) ([5966298](https://github.com/mvanhorn/cli-printing-press/commit/5966298e75007394a2f44afafac9eb9292d7acb2))


### Bug Fixes

* **cli:** Cal.com retro [#334](https://github.com/mvanhorn/cli-printing-press/issues/334) — 4 of 5 P1 machine fixes (per-endpoint headers, novel_features, auth set-token, PII scrub) ([#341](https://github.com/mvanhorn/cli-printing-press/issues/341)) ([c74dcd4](https://github.com/mvanhorn/cli-printing-press/commit/c74dcd458c94640c6688f9f4fc5fb15a8af5bb2f))
* **cli:** machine improvements from hackernews retro [#350](https://github.com/mvanhorn/cli-printing-press/issues/350) ([#352](https://github.com/mvanhorn/cli-printing-press/issues/352)) ([0ed2fbd](https://github.com/mvanhorn/cli-printing-press/commit/0ed2fbd809bf93c386022647f583fe6a12abd761))
* **cli:** printing-press P1 machine fixes (issue [#333](https://github.com/mvanhorn/cli-printing-press/issues/333)) ([#335](https://github.com/mvanhorn/cli-printing-press/issues/335)) ([6b2be74](https://github.com/mvanhorn/cli-printing-press/commit/6b2be74d017b862ceab688d1061aa39ab59ff6c8))
* **cli:** recover RunID in NewMinimalState so lock promote enriches novel_features ([#340](https://github.com/mvanhorn/cli-printing-press/issues/340)) ([b25c2b9](https://github.com/mvanhorn/cli-printing-press/commit/b25c2b9019cb48a39b60e7da84c605ad0b3e9166))
* **cli:** score auth prefixes from config ([#332](https://github.com/mvanhorn/cli-printing-press/issues/332)) ([8bad2ef](https://github.com/mvanhorn/cli-printing-press/commit/8bad2efd83dc85c125b69471c8b4336aec7d4dff))
* **cli:** sync dogfood novel features into CLI artifacts ([#344](https://github.com/mvanhorn/cli-printing-press/issues/344)) ([d86d3af](https://github.com/mvanhorn/cli-printing-press/commit/d86d3af1f159e787ee9106eef84571b06fbe5e48))
* **publish:** gate packaging on skill verification ([#348](https://github.com/mvanhorn/cli-printing-press/issues/348)) ([47fe339](https://github.com/mvanhorn/cli-printing-press/commit/47fe3393590de1cb71778de7b7748af3d6e1736c))

## [2.3.9](https://github.com/mvanhorn/cli-printing-press/compare/v2.3.8...v2.3.9) (2026-04-27)


### Bug Fixes

* **ci:** include refactors in release notes ([#328](https://github.com/mvanhorn/cli-printing-press/issues/328)) ([df551b3](https://github.com/mvanhorn/cli-printing-press/commit/df551b3739a078f803cec5d9207a63da2ed85e61))
* **cli:** harden store migrations for generated identifiers ([#308](https://github.com/mvanhorn/cli-printing-press/issues/308)) ([f33d8e5](https://github.com/mvanhorn/cli-printing-press/commit/f33d8e55fbd219ce51ff90a9439dc72324695310))
* **skills:** inherit Codex model from ~/.codex/config.toml instead of hardcoding ([#317](https://github.com/mvanhorn/cli-printing-press/issues/317)) ([b54882a](https://github.com/mvanhorn/cli-printing-press/commit/b54882aa7e3b45b529289def54981dd36c91cc7d))


### Code Refactoring

* **cli:** share generated auth helpers ([#312](https://github.com/mvanhorn/cli-printing-press/issues/312)) ([b8bb5c1](https://github.com/mvanhorn/cli-printing-press/commit/b8bb5c1ee3884526cb4dfec04379ff22a20c559f))
* **cli:** share generated naming helpers ([#327](https://github.com/mvanhorn/cli-printing-press/issues/327)) ([52bde6b](https://github.com/mvanhorn/cli-printing-press/commit/52bde6b4336e9af38fd64d27e528f5e1935ca263))
* **cli:** share verify report finalization ([#319](https://github.com/mvanhorn/cli-printing-press/issues/319)) ([ee923f8](https://github.com/mvanhorn/cli-printing-press/commit/ee923f84f3723d8b4e04daeb1b998093a7d7620a))
* **cli:** simplify generate orchestration ([#325](https://github.com/mvanhorn/cli-printing-press/issues/325)) ([545ffb9](https://github.com/mvanhorn/cli-printing-press/commit/545ffb94eefb98a17b002d226de708589c1e5a58))
* **cli:** simplify refactor-safe helpers ([#311](https://github.com/mvanhorn/cli-printing-press/issues/311)) ([fd2ea4f](https://github.com/mvanhorn/cli-printing-press/commit/fd2ea4f4067a2f29e5fc07ddd28dcc5e1d12b77c))
* **cli:** simplify scorecard dimension bookkeeping ([#326](https://github.com/mvanhorn/cli-printing-press/issues/326)) ([a2fe158](https://github.com/mvanhorn/cli-printing-press/commit/a2fe1581ee495139d4e6134126c4ab48eba2c016))
* **cli:** split generator render stages ([#313](https://github.com/mvanhorn/cli-printing-press/issues/313)) ([79b134a](https://github.com/mvanhorn/cli-printing-press/commit/79b134aa36c72160c6b28c23f016cb15fef112d2))
* **cli:** split generator vision render stages ([#314](https://github.com/mvanhorn/cli-printing-press/issues/314)) ([52a3dbc](https://github.com/mvanhorn/cli-printing-press/commit/52a3dbc5882353749617bdb2d3ab23313c5af5e7))
* **cli:** split scorecard scoring stages ([#315](https://github.com/mvanhorn/cli-printing-press/issues/315)) ([015ad88](https://github.com/mvanhorn/cli-printing-press/commit/015ad88e834e1f6ca2e24cf53e88f4d18fe8dd0b))
* **cli:** split structural verify runtime ([#320](https://github.com/mvanhorn/cli-printing-press/issues/320)) ([7e733aa](https://github.com/mvanhorn/cli-printing-press/commit/7e733aa7715d7fe29405335883bb7e6740260397))
* **cli:** split verify command helpers ([#322](https://github.com/mvanhorn/cli-printing-press/issues/322)) ([b100149](https://github.com/mvanhorn/cli-printing-press/commit/b10014911fc55e8468520affddb3ce5a5af2fb96))
* **cli:** split verify exec helpers ([#321](https://github.com/mvanhorn/cli-printing-press/issues/321)) ([18cf50f](https://github.com/mvanhorn/cli-printing-press/commit/18cf50f130ea3876e4cf17a197ffd5e32b9c4caf))
* **cli:** table-drive dogfood verdict rules ([#318](https://github.com/mvanhorn/cli-printing-press/issues/318)) ([faac0d3](https://github.com/mvanhorn/cli-printing-press/commit/faac0d39a9b7be88054b21ac4c01f3d621553db6))
* **cli:** table-drive fullrun comparison metrics ([#316](https://github.com/mvanhorn/cli-printing-press/issues/316)) ([a5c59b3](https://github.com/mvanhorn/cli-printing-press/commit/a5c59b330452f93d17c0bb5fb336640184e4a20b))

## [2.3.8](https://github.com/mvanhorn/cli-printing-press/compare/v2.3.7...v2.3.8) (2026-04-26)


### Bug Fixes

* **cli:** retro [#301](https://github.com/mvanhorn/cli-printing-press/issues/301) — six improvements from recipe-goat regenerate ([#303](https://github.com/mvanhorn/cli-printing-press/issues/303)) ([8aad8d8](https://github.com/mvanhorn/cli-printing-press/commit/8aad8d858bf7c7e3d7036ad433323dd26b0bdbb2))
* **cli:** retro [#302](https://github.com/mvanhorn/cli-printing-press/issues/302) — nine fixes from pagliacci-pizza regenerate ([#305](https://github.com/mvanhorn/cli-printing-press/issues/305)) ([6181ee3](https://github.com/mvanhorn/cli-printing-press/commit/6181ee36d01366731d3262014877d42c7e9ad302))

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
