-- Schema v22: phone + enterprise name for console SSO dual-write.

ALTER TABLE users ADD COLUMN phone TEXT;
ALTER TABLE users ADD COLUMN enterprise_name TEXT;
CREATE UNIQUE INDEX IF NOT EXISTS idx_users_phone ON users(phone) WHERE phone IS NOT NULL AND phone != '';

UPDATE _schema_version SET version = 22;
