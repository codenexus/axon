// See https://svelte.dev/docs/kit/types#app.d.ts
// for information about these interfaces
import type { Db } from '$lib/server/db';
import type { D1Database } from '@cloudflare/workers-types';

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
			};
		}
	}
}

export {};
