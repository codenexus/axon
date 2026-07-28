// See https://svelte.dev/docs/kit/types#app.d.ts
// for information about these interfaces
import type { Db } from '$lib/server/db';
import type { D1Database, DurableObjectNamespace } from '@cloudflare/workers-types';

declare global {
	namespace App {
		// interface Error {}
		interface Locals {
			db: Db;
			adminAuthenticated: boolean;
		}
		// interface PageData {}
		// interface PageState {}
		interface Platform {
			env?: {
				DB?: D1Database;
				// Bound only on the Cloudflare target -- see
				// $lib/server/realtime/index.ts's getRealtime(), which
				// branches on this the same way db/index.ts branches on DB.
				INSTANCE_HUB?: DurableObjectNamespace;
			};
		}
	}
}

export {};
