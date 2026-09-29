# Contributing to AbyssDB

Thank you for considering a contribution. Every pull request is reviewed with the
same care AbyssDB gives every write.

## Before you start

- **Bugs:** read [#2](https://github.com/noctarius/abyssdb/issues/2) first.
  `(0 rows)` is not a bug.
- **Features:** check the [roadmap](https://github.com/noctarius/abyssdb/issues/3)
  and open an issue to discuss larger changes before writing code.
- **Security issues:** do not open a public issue. See [SECURITY.md](SECURITY.md).

## Development setup

| Tool          | Version                                          | Needed for                                     |
|---------------|--------------------------------------------------|------------------------------------------------|
| Go            | 1.26 or newer                                    | everything                                     |
| Rust          | stable, with the `wasm32-unknown-unknown` target | `make rust` (the embedded Rust storage engine) |
| Node.js       | 22.22 or newer                                   | `make website`                                 |
| Typst         | 0.15 or newer                                    | `make paper`                                   |
| Chrome        | any recent version                               | `make og` (link-preview cards)                 |
| psql, pgbench | any recent version                               | `make pgbench`                                 |

```
make build          # the abyssdb binary
make test           # Go tests with the race detector, and Rust tests
make pgbench        # pgbench in every protocol mode against a fresh server
make website-dev    # the website with the WebAssembly playground, live-reloading
```

The Rust engine (`internal/rustengine/abyss_rs.wasm`), the paper PDF and the
Open Graph cards are committed, so you only need Rust, Typst or Chrome when you
change them.

## Pull requests

- Keep each pull request focused on one change.
- Run `gofmt` and `go vet ./...`; CI expects both to be clean.
- Add or update tests. `make test` must pass with the race detector.
- If you touch the PostgreSQL protocol or the write path, run `make pgbench` and
  include the result table. It must report 0 failed transactions.
- Update the docs in [`docs/`](docs) for any user-visible change. They are
  published on the website as they are.
- If you change the paper, run `make paper` and commit the regenerated PDF.

## House style

AbyssDB is a serious database, and its documentation and product output are
written that way. Some rules:

1. **Deadpan.** Docs, README, error messages, notices and the website state facts,
   confidently and without winking. Never explain the joke.
2. **Never say outright where the data goes.** If it has to be said, paraphrase.
   *Zero-knowledge* means no learning curve. *Kernel-native storage* means a
   character device built into every kernel. `/dev/null` may appear as a dry
   technical fact, for example in `SHOW ENCRYPTION`, and nowhere else.
3. **Accurate.** Every number we print is measured, and every theorem we cite is
   proven. The jokes only work because the engineering is real: real SQL
   parsing, real schema validation, real post-quantum cryptography, real Raft.
4. **Code comments** are the one place where the joke may be told openly. Keep
   them short.

## Commit messages

Start the subject with a [gitmoji](https://gitmoji.dev), then use the imperative
mood ("✨ Add Morse value format", not "Added …"). Keep the subject line under 72
characters, and explain the why in the body.

| Emoji | Use for                         | Example                                              |
|-------|---------------------------------|------------------------------------------------------|
| ✨    | New feature                     | ✨ Add key-value store with nine value formats       |
| 🐛    | Bug fix                         | 🐛 Give each Rust store its own WebAssembly instance |
| ⚡️    | Performance                     | ⚡️ Flush pgwire output only when the pipeline drains |
| 🔒️    | Security and cryptography       | 🔒️ Seal every row under its own ML-KEM-768 key       |
| 📝    | Documentation                   | 📝 Document the Schema-yes or schema-less data model |
| ✅    | Tests                           | ✅ Add concurrent Rust store regression test         |
| ♻️    | Refactoring                     | ♻️ Extract the catalog from the engine               |
| 💄    | Website and UI                  | 💄 Make the announcement bar more in your face       |
| 👷    | CI and build                    | 👷 Add gofmt, vet and race tests to CI               |
| ➕    | Add a dependency                | ➕ Add go.yaml.in/yaml/v3 for YAML values            |
| 🗃️    | Storage engines and data format | 🗃️ Make the kernel device write without a lock       |
| 🚀    | Release or deploy               | 🚀 Release 0.0.0-null "Event Horizon"                |
| 💸    | Investor relations              | 💸 Open the Series A data room                       |

## License

By contributing, you agree that your contributions are licensed under the
[MIT License](LICENSE).
