CREATE TABLE `backup_downloads` (
	`backup_id` text PRIMARY KEY NOT NULL,
	`pulse_agent_id` text NOT NULL,
	`status` text NOT NULL,
	`file_path` text,
	`size_bytes` integer,
	`error_message` text,
	`requested_at` integer NOT NULL,
	`ready_at` integer,
	`expires_at` integer
);
