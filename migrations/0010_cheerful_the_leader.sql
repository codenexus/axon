CREATE TABLE `server_definitions` (
	`id` text PRIMARY KEY NOT NULL,
	`name` text NOT NULL,
	`game_platform` text NOT NULL,
	`version` text NOT NULL,
	`download_url` text NOT NULL,
	`java_major_version` integer,
	`created_at` integer NOT NULL
);
