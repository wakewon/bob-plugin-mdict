# Third-party notices and license audit

`MDict for Bob` is distributed under **GPL-3.0-or-later** (see [LICENSE](LICENSE)).
This file records why, and what obligations come with the code it builds on.

The audit below was performed by reading the `LICENSE` file actually present in
each module in the Go module cache — not by trusting a badge, a README string,
or a package registry field. Where those sources disagree, the disagreement is
written down rather than resolved silently.

---

## Why this project is GPL-3.0-or-later

Three facts drove the choice:

1. **The MDX/MDD engine descends from a GPL-3.0 project.**
   `github.com/lib-x/mdx` ships an MIT `LICENSE` file, but its own README states
   it "was originally based on the [terasum/medict](https://github.com/terasum/medict)
   project". `terasum/medict` is GPL-3.0, and its parser sources carry explicit
   GPL headers (`Copyright (C) 2023 Quan Chen`, "either version 3 of the License,
   or (at your option) any later version"). The two codebases share the same
   file names, the same package layout, and the same unusual dependency set
   (`go-ripemd`, `go-lzo`, `op/go-logging`, `rodaine/table`), so the derivation
   is not in doubt.

   Whether the MIT relicensing was authorised is not something this project can
   determine. Licensing our own work under GPL-3.0-or-later is correct under
   either reading: it satisfies the GPL if the code is in fact GPL-derived, and
   it is permitted if the MIT grant is valid, since MIT code may be combined
   into a GPL work.

2. **LZO decompression is GPL.**
   `github.com/rasky/go-lzo` ships `LICENSE.gpl` (GNU GPL version 2) and its
   README says it is "a straightforward port of the original source code" that
   "shares the same license (GPLv2)". The original LZO by Markus Oberhumer is
   GPL-2.0-**or-later**, and a port that claims no new copyright inherits that
   grant, which permits distribution under GPL-3.0. This is the reading this
   project relies on.

   *Residual uncertainty, stated plainly:* the Go port carries no per-file
   headers, so "or later" is inherited rather than restated. If it were ever
   established as GPL-2.0-only, this project — and equally `medict` and
   `lib-x/mdx`, which both depend on it — would need to move to GPL-2.0 or
   replace the LZO implementation. That code path is only reached for
   LZO-compressed MDict files; the four dictionaries this project was developed
   against all use zlib.

3. **A permissive license was never worth the cost.** Rewriting a mature MDX
   parser to obtain an MIT badge would have meant a less compatible reader for
   real dictionaries. Choosing a copyleft license was the cheaper trade.

---

## Dependencies linked into `bob-mdict`

| Module | Version | License | Role |
|---|---|---|---|
| `github.com/lib-x/mdx` | v0.1.23 | MIT as published; GPL-3.0 lineage — see above | MDX/MDD container parsing |
| `github.com/rasky/go-lzo` | 2020-02-03 | GPL-2.0 (inherited from LZO, read as *or later*) | LZO1X decompression of MDict blocks |
| `github.com/c0mm4nd/go-ripemd` | 2020-03-26 | MIT | RIPEMD-128, used by MDict key-block decryption |
| `github.com/op/go-logging` | 2016-03-15 | BSD-3-Clause | Logging inside the engine |
| `github.com/rodaine/table` | v1.3.0 | MIT | Pulled in by the engine |
| `github.com/fatih/color` | v1.18.0 | MIT | Pulled in by the engine |
| `github.com/mattn/go-colorable` | v0.1.14 | MIT | Transitive |
| `github.com/mattn/go-isatty` | v0.0.20 | MIT | Transitive |
| `github.com/redis/go-redis/v9` | v9.18.0 | BSD-2-Clause | Optional engine index store; unused here |
| `github.com/cespare/xxhash/v2` | v2.3.0 | MIT | Transitive via go-redis |
| `github.com/dgryski/go-rendezvous` | 2020-08-23 | MIT | Transitive via go-redis |
| `go.uber.org/atomic` | v1.11.0 | MIT | Transitive |
| `golang.org/x/net` | v0.58.0 | BSD-3-Clause | HTML tokenizer/parser |
| `golang.org/x/text` | v0.41.0 | BSD-3-Clause | Unicode normalization |
| `golang.org/x/sys` | v0.47.0 | BSD-3-Clause | Transitive |

No third-party code is vendored into this repository. All of the above are
fetched by the Go module system and statically linked into the released
binaries, which is why their notices appear here.

---

## Optional runtime tool

**`speexdec`** (from [Speex](https://speex.org/), BSD-3-Clause) is invoked as an
external process to decode Ogg-Speex pronunciations that macOS cannot play. It
is not linked into the binary and not redistributed by this project; the
Homebrew formula declares it as a dependency, and without it those particular
pronunciations are simply not offered.

---

## Reference material consulted, not copied

- [`terasum/medict`](https://github.com/terasum/medict) (GPL-3.0) — studied for
  MDX/MDD structure and index behaviour.
- [`glowinthedark/mdict-go-web`](https://github.com/glowinthedark/mdict-go-web)
  (LGPL-2.1) — studied for lightweight localhost service design.
- [`yaobinbin333/bob-plugin-cambridge-dictionary`](https://github.com/yaobinbin333/bob-plugin-cambridge-dictionary)
  — studied for how a Bob plugin maps dictionary content onto `toDict`. Its web
  scraping approach is deliberately **not** used; this project's only data
  source is the user's own local files.

No source was copied from any of these into this repository.

---

## Dictionary data

**This project contains no dictionary data, and never will.**

`.mdx` and `.mdd` files are the property of their publishers. Users supply their
own. Nothing in this repository, in the `.bobplugin` package, in the server
binaries, or in any release artifact contains dictionary content. A test
(`TestNoDictionaryContentLeavesTheRepository`) enforces this on every CI run.
