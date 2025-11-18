-- Drop materialized view
DROP MATERIALIZED VIEW IF EXISTS subscription_spending_analytics;

-- Drop functions
DROP FUNCTION IF EXISTS calculate_next_renewal_date(UUID);
DROP FUNCTION IF EXISTS update_updated_at_column();

-- Drop tables in reverse order
DROP TABLE IF EXISTS card_funding_history CASCADE;
DROP TABLE IF EXISTS subscription_notifications CASCADE;
DROP TABLE IF EXISTS auto_topup_logs CASCADE;
DROP TABLE IF EXISTS auto_topup_settings CASCADE;
DROP TABLE IF EXISTS subscription_transactions CASCADE;
DROP TABLE IF EXISTS subscriptions CASCADE;
DROP TABLE IF EXISTS subscription_merchants CASCADE;
DROP TABLE IF EXISTS subscription_categories CASCADE;
DROP TABLE IF EXISTS virtual_cards CASCADE;