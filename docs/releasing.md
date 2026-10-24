# Releasing SessionSift

Releases follow [Semantic Versioning](https://semver.org/spec/v2.0.0.html).
One version is kept in five places, and `scripts/release.py check` fails if
they disagree:

- `VERSION`
- `package.json` and `package-lock.json`
- `currentVersion` and the newest entry in `src/internal/release/catalog.go`
  (the in-app changelog, including its date)
- the first `## [X.Y.Z] — YYYY-MM-DD` heading in `CHANGELOG.md`

## Cut a release

1. Update the five places above and describe user-visible changes in
   `CHANGELOG.md` (Added, Improved, Fixed, Notes). Mirror the same items in
   `catalog.go`.
2. After any dependency change, regenerate the third-party notices with
   `python3 scripts/notices.py --write` (it reads the Go module cache and
   `node_modules`). Then check locally:

   ```bash
   python3 scripts/release.py check
   python3 -m unittest scripts/release_test.py scripts/notices_test.py
   npm ci && npm test
   npm audit --audit-level=moderate
   go run golang.org/x/vuln/cmd/govulncheck@v1.9.0 ./...
   GOMODCACHE="$PWD/.cache/go-mod" python3 scripts/notices.py --check
   bash scripts/package.sh
   ```

   `package.sh` writes reproducible archives, an SPDX inventory and
   `SHA256SUMS` to `dist/release/<version>/`, and smoke-tests the native
   archive offline (`version`, `doctor`, and the ten MCP tools).
3. Merge to `main`, then run the **Release** workflow from the Actions tab.
   It tests, scans dependencies (npm audit, govulncheck), checks `NOTICE` and
   packages natively on Linux amd64 and macOS Intel, requires both native smoke
   tests to pass, publishes `v<version>` with the changelog section as notes,
   and attaches the generated Homebrew formula.
4. The tap's **Update SessionSift** workflow picks up the new release, opens a
   pull request with the regenerated formula and runs the native install test
   on it. Merge it once the checks pass.

## Platforms and trust

Only macOS Intel and Linux amd64 are built. macOS binaries are unsigned and
unnotarized; checksums verify downloads, not publisher identity.
