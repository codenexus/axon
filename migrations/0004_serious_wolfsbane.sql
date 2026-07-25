CREATE TABLE `backup_schedules` (
	`server_instance_id` text PRIMARY KEY NOT NULL,
	`pulse_agent_id` text NOT NULL,
	`instance_id` text NOT NULL,
	`interval_hours` integer,
	`keep_count` integer,
	`keep_days` integer,
	`last_run_at` integer,
	`created_at` integer NOT NULL
);
