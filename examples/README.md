# futi examples

Working modules you can try right away:

| Module | Commands |
|---|---|
| `git` | `status`, `log`, `switch`, `amend-noedit` |
| `docker` | `ps`, `logs`, `prune` (irreversible) |
| `files` | `large`, `count` |
| `backup` | module package with a bundled script (`backup/scripts/backup.sh`) |

## Installing

Copy the modules into your modules directory:

```bash
mkdir -p ~/.config/futi/modules
cp -r examples/* ~/.config/futi/modules/
```

Or point futi at this directory in `~/.config/futi/config.yaml`:

```yaml
sources:
  - path: /path/to/futi/examples
```

Then look around:

```bash
futi list -r
futi git log --count 5
futi files count -- "my file.txt" other.txt
```

## Previewing a command

`--dry-run` prints the command that would run, in shell quoting, without executing it:

```bash
futi --dry-run git switch --branch feat/x
# git switch -- feat/x
futi --dry-run docker logs --container web --follow
# docker logs --tail 100 --follow -- web
```
