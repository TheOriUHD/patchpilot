# Third-party notices

patchpilot's own source code is licensed under the [PolyForm Noncommercial License 1.0.0](LICENSE).
It links the open-source Go modules below, each under its own permissive license. None of them is
copyleft.

| Module | License |
|---|---|
| [charmbracelet/bubbletea](https://github.com/charmbracelet/bubbletea), [bubbles](https://github.com/charmbracelet/bubbles), [lipgloss](https://github.com/charmbracelet/lipgloss) and their dependencies | MIT |
| [modernc.org/sqlite](https://gitlab.com/cznic/sqlite) (pure-Go SQLite) and related `modernc.org/*` modules | BSD-3-Clause |
| [golang.org/x/crypto](https://pkg.go.dev/golang.org/x/crypto) (SSH client), `golang.org/x/*` | BSD-3-Clause |
| [gopkg.in/yaml.v3](https://github.com/go-yaml/yaml) | MIT / Apache-2.0 |
| Other modules listed in `go.mod` | MIT, BSD, ISC or Apache-2.0; each module ships its own license file |

The complete list is `go list -m all`. SQLite itself is in the public domain.
