# Node baseline

Measured on the machine the port is developed on (MacBook Air, Apple Silicon, macOS, Node 22.15.1) with `npx vitest run`. Go numbers in each phase page are compared to these.

## Tests (89 total, all passing)

| File                                           | Tests | Time   |
| ---------------------------------------------- | ----- | ------ |
| `packages/shared/src/parse/parse.test.ts`      | 15    | 0.04s  |
| `packages/server/test/storage.test.ts`         | 18    | 5.16s  |
| `packages/server/test/api.test.ts`             | 10    | 3.79s  |
| `packages/server/test/import.test.ts`          | 17    | 21.93s |
| `packages/server/test/anilist-client.test.ts`  | 14    | 0.21s  |
| `packages/server/test/anilist-service.test.ts` | 9     | 4.44s  |
| `apps/desktop/src/paths.test.ts`               | 3     | <0.01s |
| `apps/web/src/lib/store.test.ts`               | 3     | <0.01s |

Per-file times overlap because Vitest runs files in parallel. Whole run, three runs: **22.0s, 20.9s, 22.5s** (wall clock 22.3-24.6s including start-up). The first run also generates the synthetic workbooks.

## Performance (`npm run perf`, 10,000 synthetic entries)

| Operation                                  | p50      | p95      |
| ------------------------------------------ | -------- | -------- |
| Bulk insert (1 write per category)         | 331 ms   | 331 ms   |
| Cold open (read + validate all JSON)       | 609 ms   | 609 ms   |
| First search (builds the index)            | 231 ms   | 231 ms   |
| Search "crimson tower" (warm)              | 1.9 ms   | 3.4 ms   |
| Search fuzzy "cirmson" (warm)              | 2.0 ms   | 2.6 ms   |
| Search prefix "wand" (warm)                | 1.7 ms   | 2.2 ms   |
| List, sort by title, page 1                | 1.7 ms   | 36.3 ms  |
| List, 2 tags (all) + status filter         | 0.9 ms   | 3.9 ms   |
| Stats overview                             | 28.1 ms  | 52.2 ms  |
| PATCH one entry (persist the big file)     | 148.3 ms | 218.7 ms |
| Search right after a write (incremental)   | 1.5 ms   | 5.1 ms   |
| 20 concurrent progress writes (coalesced)  | 311 ms   | 311 ms   |
| Slowest search while writes were in flight | 2.7 ms   |          |

## Size and memory (filled in at phase 7, macOS arm64)

Measured on the same MacBook Air (Apple M2, macOS 27) with unsigned builds: Electron through `electron-builder --mac dmg --arm64`, Go through the Wails CLI. Memory is the total resident set after the app had been running for 8 seconds with a hidden window, 3 runs each; for the Go app the WebKit helper processes (they are system XPC services, not children) are estimated as the growth of all `com.apple.WebKit.*` processes during the run, so treat that part as approximate.

| Metric                                     | Electron                 | Go + Wails                                         |
| ------------------------------------------ | ------------------------ | -------------------------------------------------- |
| macOS installer (dmg, arm64)               | 130.3 MB                 | 8.1 MB                                             |
| Installed app (`.app`)                     | 296 MB                   | 17.9 MB                                            |
| Memory, all processes                      | 470-520 MB (4 processes) | about 235-245 MB (main 113 MB + WebKit 120-130 MB) |
| Launch until the API answers (median of 5) | 0.38 s                   | 0.22 s                                             |

## Re-measured test time

The Node suite was timed again at the end of the port, after the machine was warm: 89 tests in **9.6-10.8 s** over 3 runs. The 20.9-22.5 s above were the first runs after a clean install (cold caches, workbook generation).
