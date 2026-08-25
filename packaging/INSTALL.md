# Manual macOS installation

This bundle is self-contained and contains no dictionary data.

```bash
./install.sh
```

The installer places `bob-mdict` in `~/.local/bin`, registers a user
LaunchAgent, and creates the default dictionary directory. Add your own lawful
MDX/MDD files, then run:

```bash
~/.local/bin/bob-mdict --rescan
~/.local/bin/bob-mdict --check
```

To uninstall the service while preserving dictionaries:

```bash
./uninstall.sh
```
