-- Fails with a duplicate key error while two registered emails differ only in what
-- utf8mb4_unicode_ci ignores, such as an accent. Emails stay in lower case.
ALTER TABLE users MODIFY COLUMN email VARCHAR(255) CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci NOT NULL;
