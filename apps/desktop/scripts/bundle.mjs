// Bundles the Electron main process, preload and the built web app into apps/desktop/dist.
import { build } from 'esbuild';
import { cpSync, existsSync, rmSync, mkdirSync } from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const root = path.join(path.dirname(fileURLToPath(import.meta.url)), '..');
const dist = path.join(root, 'dist');
rmSync(dist, { recursive: true, force: true });
mkdirSync(dist, { recursive: true });

const common = {
  platform: 'node',
  target: 'node22',
  format: 'cjs',
  bundle: true,
  sourcemap: true,
  logLevel: 'info',
  external: ['electron'],
  legalComments: 'none',
};
await build({
  ...common,
  entryPoints: [path.join(root, 'src/main.ts')],
  outfile: path.join(dist, 'main.cjs'),
});
await build({
  ...common,
  entryPoints: [path.join(root, 'src/preload.ts')],
  outfile: path.join(dist, 'preload.cjs'),
});

const dev = process.argv.includes('--dev'); // dev mode loads the Vite server, no built web app needed
const web = path.join(root, '..', 'web', 'dist');
if (dev) {
  console.log('dev bundle ready:', dist);
  process.exit(0);
}
if (!existsSync(web)) throw new Error('Build the web app first (npm run build -w @lorekeeper/web)');
cpSync(web, path.join(dist, 'web'), { recursive: true });
console.log('desktop bundle ready:', dist);
