# Contributing to SessionSift

SessionSift is in a private testing phase. Access to the source does not imply
permission for commercial use; contributions are governed by the repository's
[`LICENSE`](LICENSE) and required [`NOTICE`](NOTICE).

## Before proposing a change

1. Use synthetic transcripts and identities in tests, screenshots, issues, and
   logs. Never submit real conversation exports or machine configuration.
2. Keep local analysis usable without the hosted bridge. Remote behavior must
   remain an explicit optional layer.
3. Read `DECISIONS.md` before changing a covered trust boundary or data model.
4. Add a regression test for behavior changes and a `CHANGES.md` entry using the
   existing project format.

## Verification

```bash
npm install
npm test
npm run build
/bin/bash -n scripts/scan-secrets.sh
GITLEAKS_BIN=/path/to/verified/gitleaks scripts/scan-secrets.sh
```

Do not include generated databases, coverage output, build artifacts, local
environment files, or secret-scan reports in a contribution.
