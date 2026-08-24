# Parser golden fixtures

Every file here is **synthetic and constructed from scratch for one test**. The
HTML retains only the selectors/classes and minimal DOM relationships a parser
rule needs. It does not copy a real entry skeleton and replace its words.

Headwords, definitions, translations, examples and `sound://synthetic/...`
resource paths are invented for this repository. Publisher stylesheets,
internal IDs, original resource directories, UI chrome and unrelated sections
must not appear.

No dictionary content from any commercial publisher is stored in this
repository, in the plugin package, in the server binary, or in any release
artifact.

Each case is a pair:

- `<name>.html`  — the entry record as the parser receives it
- `<name>.json`  — the expected Entry IR

Regenerate expectations after an intentional parser change with:

    go test ./internal/parser -run TestGolden -update
