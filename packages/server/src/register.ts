import type { FastifyInstance } from 'fastify';
import { registerAniListRoutes } from './anilist/routes.js';
import { registerIoRoutes } from './io/routes.js';
import type { Store } from './storage/store.js';

/** Every optional route module the desktop app and dev server use. */
export function registerAllRoutes(app: FastifyInstance, store: Store): void {
  registerIoRoutes(app, store);
  registerAniListRoutes()(app, store);
}
