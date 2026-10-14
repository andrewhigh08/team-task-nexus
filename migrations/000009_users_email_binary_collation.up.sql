-- The first release stored emails as typed. Lower-case them so that the exact comparison below
-- still finds them; the case-insensitive unique index already rules out two rows that differ
-- only in case. Setting updated_at to itself keeps ON UPDATE from stamping data repairs as user
-- edits.
UPDATE users
SET email = LOWER(email), updated_at = updated_at
WHERE email COLLATE utf8mb4_bin <> LOWER(email);

-- utf8mb4_unicode_ci took addresses that differ in an accent, in character width or in ß
-- against ss for one address, in the unique index and in every lookup. Emails are stored and
-- looked up trimmed and in lower case, so the column compares them byte for byte.
ALTER TABLE users MODIFY COLUMN email VARCHAR(255) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NOT NULL;
