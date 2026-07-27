-- DEFAULT 'vanilla' (hand-added, not drizzle-kit's plain output): every
-- row that existed before this migration was, in fact, always vanilla —
-- software_type never existed as a selectable option before this
-- feature, so backfilling existing rows to 'vanilla' is an accurate
-- statement of what they already were, not a guess. Also required by
-- SQLite itself: ALTER TABLE ADD COLUMN NOT NULL fails on a non-empty
-- table without a DEFAULT.
ALTER TABLE `server_definitions` ADD `software_type` text NOT NULL DEFAULT 'vanilla';--> statement-breakpoint
ALTER TABLE `server_definitions` ADD `loader_version` text;--> statement-breakpoint
ALTER TABLE `version_catalog_entries` ADD `software_type` text NOT NULL DEFAULT 'vanilla';--> statement-breakpoint
ALTER TABLE `version_catalog_entries` ADD `loader_version` text;