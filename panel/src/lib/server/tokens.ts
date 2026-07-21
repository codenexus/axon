import { randomBytes, scrypt, timingSafeEqual, createHash } from 'node:crypto';
import { promisify } from 'node:util';

const scryptAsync = promisify(scrypt);

export function randomToken(bytes = 32): string {
	return randomBytes(bytes).toString('hex');
}

export function sha256Hex(value: string): string {
	return createHash('sha256').update(value).digest('hex');
}

export async function hashPassword(password: string): Promise<string> {
	const salt = randomBytes(16).toString('hex');
	const derived = ((await scryptAsync(password, salt, 64)) as Buffer).toString('hex');
	return `${salt}:${derived}`;
}

export async function verifyPassword(password: string, stored: string): Promise<boolean> {
	const [salt, hash] = stored.split(':');
	if (!salt || !hash) return false;
	const derived = (await scryptAsync(password, salt, 64)) as Buffer;
	const expected = Buffer.from(hash, 'hex');
	if (derived.length !== expected.length) return false;
	return timingSafeEqual(derived, expected);
}
