-- Migration v1.6.7 : table dsc_settings (réglages DSC en base) + bump db_version.
--
-- La table dsc_settings est mono-ligne (id=1) et stocke la configuration du
-- renouvellement de certificat DSC (voir doc/dscpull/security.md). Le schéma
-- (schema_mssql.sql) la crée via CREATE TABLE IF NOT EXISTS au démarrage ; ce
-- script est fourni pour les déploiements existants qui souhaitent matérialiser
-- la table (et mettre à jour dsc_infra_info.db_version) sans redémarrer sur le
-- nouveau schéma.

IF NOT EXISTS (SELECT * FROM sysobjects WHERE name='dsc_settings' AND xtype='U')
CREATE TABLE dsc_settings (
    id INT PRIMARY KEY CHECK (id = 1),
    certificate_rotation_enabled BIT NOT NULL DEFAULT 0,
    certificate_renewal_days INT NOT NULL DEFAULT 30,
    legacy_imported BIT NOT NULL DEFAULT 0,
    updated_at DATETIME NOT NULL DEFAULT GETUTCDATE()
);
IF NOT EXISTS (SELECT 1 FROM dsc_settings WHERE id = 1)
    INSERT INTO dsc_settings (id, certificate_rotation_enabled, certificate_renewal_days, legacy_imported) VALUES (1, 0, 30, 1);

IF EXISTS (SELECT 1 FROM dsc_infra_info WHERE id = 1)
    UPDATE dsc_infra_info SET db_version = '1.6.7', updated_at = GETDATE() WHERE id = 1;
ELSE
    INSERT INTO dsc_infra_info (id, web_version, db_version, updated_at) VALUES (1, '1.6.7', '1.6.7', GETDATE());
