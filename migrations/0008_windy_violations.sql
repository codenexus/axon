CREATE TABLE `pulse_releases` (
	`id` text PRIMARY KEY NOT NULL,
	`version` text NOT NULL,
	`os` text NOT NULL,
	`arch` text NOT NULL,
	`download_url` text NOT NULL,
	`signature_hex` text NOT NULL,
	`created_at` integer NOT NULL
);
