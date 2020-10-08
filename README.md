# acmewatch

An acme autoformatter configurable via [TOML](https://toml.io/).

Forked from [acmego](https://godoc.org/9fans.net/go/acme/acmego).

## Configuration

File location: `$HOME/.config/acmewatch.toml`.

The file is made up of an array of `formatter` tables with members:

- `match`: String array of globs.
- `cmd`: String command to run.
- `args`: Arguments to pass to the command.

Commands must output the new file contents.

Generally the file contents is passed as stdin to the command. An argument
in `args` that is `$name` will be replaced by the filename and stdin
will no longer be populated.

## Example

```
[[formatter]]
match = [".go"]
cmd = "goimports"
args = ["$name"]

[[formatter]]
match = [".rs"]
cmd = "rustfmt"
args = ["--edition", "2018"]

[[formatter]]
match = [".js", ".jsx", ".tsx", ".ts", ".css", ".html", ".less"]
cmd = "prettier"
args = ["--config-precedence",  "file-override", "--use-tabs", "--single-quote", "$name"]
```
