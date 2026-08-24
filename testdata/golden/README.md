# Parser golden fixtures

Every file here is **synthetic**. The HTML reproduces the *structural* patterns
found in real MDict dictionaries — class names, nesting, the way translations
and audio links are interleaved — but all headwords, definitions and examples
are invented for this repository.

No dictionary content from any commercial publisher is stored in this
repository, in the plugin package, in the server binary, or in any release
artifact.

Each case is a pair:

- `<name>.html`  — the entry record as the parser receives it
- `<name>.json`  — the expected Entry IR

Regenerate expectations after an intentional parser change with:

    go test ./internal/parser -run TestGolden -update
