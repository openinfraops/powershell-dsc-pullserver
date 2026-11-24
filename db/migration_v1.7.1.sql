-- Migration v1.7.1 : colonnes nullable certificate_public_key / certificate_key_algorithm
-- sur la table agents + bump db_version.
--
-- Ces colonnes sont capturées depuis le certificat client mTLS du nœud (à
-- l'enregistrement / ré-enregistrement) ou depuis le corps du message
-- CertificateRotation (nouveau certificat). Elles ne sont pas encore consommées :
-- réservées à un futur chiffrement de secret par nœud (chiffrement asymétrique
-- avec la clé publique du certificat mTLS du nœud). Voir doc/dscpull/security.md.
--
-- Le schéma (schema_mssql.sql) crée ces colonnes via CREATE TABLE ; ce script
-- est fourni pour les déploiements existants qui souhaitent matérialiser les
-- colonnes sans redémarrer sur le nouveau schéma.

IF NOT EXISTS (SELECT * FROM INFORMATION_SCHEMA.COLUMNS WHERE TABLE_NAME = 'agents' AND COLUMN_NAME = 'certificate_public_key')
    ALTER TABLE agents ADD certificate_public_key NVARCHAR(MAX) NULL;
IF NOT EXISTS (SELECT * FROM INFORMATION_SCHEMA.COLUMNS WHERE TABLE_NAME = 'agents' AND COLUMN_NAME = 'certificate_key_algorithm')
    ALTER TABLE agents ADD certificate_key_algorithm NVARCHAR(16) NULL;

IF EXISTS (SELECT 1 FROM dsc_infra_info WHERE id = 1)
    UPDATE dsc_infra_info SET db_version = '1.7.1', updated_at = GETDATE() WHERE id = 1;
ELSE
    INSERT INTO dsc_infra_info (id, web_version, db_version, updated_at) VALUES (1, '1.7.1', '1.7.1', GETDATE());
