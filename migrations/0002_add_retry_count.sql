-- Add retry tracking columns to permanent_failures for richer backoff metadata.
-- This migration is forward-compatible: columns default to zero/empty.
ALTER TABLE permanent_failures ADD COLUMN next_retry_at TEXT;
ALTER TABLE permanent_failures ADD COLUMN backoff_state TEXT NOT NULL DEFAULT '';
