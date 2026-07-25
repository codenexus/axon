CREATE TABLE `backups` (
	`id` text PRIMARY KEY NOT NULL,
	`pulse_agent_id` text NOT NULL,
	`instance_id` text NOT NULL,
	`server_instance_id` text NOT NULL,
	`status` text NOT NULL,
	`trigger` text NOT NULL,
	`pending_operation` text,
	`command_id` text,
	`size_bytes` integer,
	`checksum_sha256` text,
	`error_message` text,
	`created_at` integer NOT NULL,
	`completed_at` integer
);
--> statement-breakpoint
ALTER TABLE `commands` ADD `payload` text;