-- lidza:no-transaction
-- Hand-tuned indexes schema.lidza cannot express: a partial index for the
-- due-transmission sweep and case-insensitive uniqueness of mailbox addresses
-- (mailbox_alias.address was unique; mailbox.address was not). Built
-- CONCURRENTLY so a running node keeps serving mail.
CREATE INDEX CONCURRENTLY IF NOT EXISTS mail_transmission_due_idx ON mail_transmission (next_attempt, id) WHERE state IN ('planning','pending');
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS mailbox_address_lower_key ON mailbox (lower(address));
