CREATE TABLE `version_catalog_entries` (
	`id` text PRIMARY KEY NOT NULL,
	`game_platform` text NOT NULL,
	`version` text NOT NULL,
	`download_url` text NOT NULL,
	`java_major_version` integer,
	`sort_order` integer NOT NULL,
	`fetched_at` integer NOT NULL,
	`expires_at` integer NOT NULL
);
--> statement-breakpoint
ALTER TABLE `commands` ADD `progress_phase` text;--> statement-breakpoint
ALTER TABLE `pulse_agents` ADD `port_range_start` integer;--> statement-breakpoint
ALTER TABLE `pulse_agents` ADD `port_range_end` integer;--> statement-breakpoint
ALTER TABLE `pulse_agents` ADD `instances_root_dir` text;--> statement-breakpoint
ALTER TABLE `server_instances` ADD `port` integer;