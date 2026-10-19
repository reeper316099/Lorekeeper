import path from 'node:path';
import os from 'node:os';
import { randomBytes } from 'node:crypto';
import { APP_SLUG } from '@lorekeeper/shared';
import { buildApp, Store } from './index.js';

const dir = process.env.LOREKEEPER_DATA_DIR ?? path.join(os.homedir(), `.${APP_SLUG}-dev`);
const token = process.env.LOREKEEPER_TOKEN ?? randomBytes(24).toString('hex');
const store = new Store(dir, { watch: true });
await store.open();
const app = await buildApp({
  store,
  token,
  allowedOrigins: ['http://localhost:5173'],
  logger: true,
});
await app.listen({ host: '127.0.0.1', port: Number(process.env.PORT ?? 4317) });
console.log(`data dir: ${dir}\ntoken: ${token}`);
