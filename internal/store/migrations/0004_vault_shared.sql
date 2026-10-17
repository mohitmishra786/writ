-- Verified live (2026-10-17): approving vault setup again for the same payer
-- and merchant returns the same payment token, so several mandates of one
-- principal can share a vault ID. The index stays for lookups (the
-- VAULT.PAYMENT-TOKEN.DELETED handler pauses every mandate using it).
DROP INDEX mandates_vault_id;
CREATE INDEX mandates_vault_id ON mandates (vault_id) WHERE vault_id IS NOT NULL;
