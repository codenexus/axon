CREATE TABLE `admin_sessions` (
	`id` text PRIMARY KEY NOT NULL,
	`created_at` integer NOT NULL,
	`expires_at` integer NOT NULL
);
--> statement-breakpoint
CREATE TABLE `admin_settings` (
	`id` integer PRIMARY KEY AUTOINCREMENT NOT NULL,
	`password_hash` text NOT NULL,
	`created_at` integer NOT NULL
);
--> statement-breakpoint
CREATE TABLE `commands` (
	`id` text PRIMARY KEY NOT NULL,
	`pulse_agent_id` text NOT NULL,
	`instance_id` text NOT NULL,
	`type` text NOT NULL,
	`status` text NOT NULL,
	`result_message` text,
	`created_at` integer NOT NULL,
	`sent_at` integer,
	`completed_at` integer
);
--> statement-breakpoint
CREATE TABLE `enrollment_tokens` (
	`id` text PRIMARY KEY NOT NULL,
	`token_hash` text NOT NULL,
	`created_at` integer NOT NULL,
	`used_at` integer,
	`expires_at` integer NOT NULL
);
--> statement-breakpoint
CREATE TABLE `pulse_agents` (
	`id` text PRIMARY KEY NOT NULL,
	`hostname` text NOT NULL,
	`os` text NOT NULL,
	`arch` text NOT NULL,
	`device_credential_hash` text NOT NULL,
	`pulse_version` text NOT NULL,
	`last_seen_at` integer,
	`cpu_usage_percent` real,
	`cpu_cores` integer,
	`ram_total_bytes` integer,
	`ram_used_bytes` integer,
	`created_at` integer NOT NULL
);
--> statement-breakpoint
CREATE TABLE `server_instances` (
	`id` text PRIMARY KEY NOT NULL,
	`pulse_agent_id` text NOT NULL,
	`instance_id` text NOT NULL,
	`name` text NOT NULL,
	`game_platform` text NOT NULL,
	`version` text NOT NULL,
	`software_type` text NOT NULL,
	`running_state` text NOT NULL,
	`player_count` integer DEFAULT 0 NOT NULL,
	`uptime_seconds` integer DEFAULT 0 NOT NULL,
	`updated_at` integer NOT NULL
);
