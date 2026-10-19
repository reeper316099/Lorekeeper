# Lorekeeper

Local-first tracker for manga/manhwa/manhua and anime/donghua/TV. Imports Excel/CSV trackers,
stores plain JSON in the OS app-data folder, and (soon) ships as a desktop app.

Status: M0–M2 in progress (shared schemas, storage + API, importers/exporters).

```bash
npm install
npm test
npm run lint
```

Personal spreadsheets live in the gitignored `private-fixtures/`. A guard
(`npm run guard`, also a pre-commit hook and CI step) rejects any `.xlsx/.xlsm/.csv` outside
`fixtures/synthetic/`.
