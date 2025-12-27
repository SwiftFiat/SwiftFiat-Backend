CREATE TABLE system_settings (
    id SERIAL PRIMARY KEY,
    rewards_enabled BOOLEAN DEFAULT TRUE,
    vaults_enabled BOOLEAN DEFAULT TRUE,
    smart_conversions_enabled BOOLEAN DEFAULT TRUE,
    rapid_ramp_enabled BOOLEAN DEFAULT TRUE,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

INSERT INTO system_settings (rewards_enabled, vaults_enabled, smart_conversions_enabled, rapid_ramp_enabled) VALUES (FALSE, TRUE, FALSE, TRUE);