CREATE TABLE `file_uploads` (
	`id` text PRIMARY KEY NOT NULL,
	`pulse_agent_id` text NOT NULL,
	`instance_id` text NOT NULL,
	`target_path` text NOT NULL,
	`file_path` text NOT NULL,
	`status` text NOT NULL,
	`size_bytes` integer,
	`error_message` text,
	`created_at` integer NOT NULL,
	`expires_at` integer
);
