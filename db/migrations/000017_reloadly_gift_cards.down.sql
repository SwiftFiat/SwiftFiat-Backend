-- Down Migration Script

-- Drop Redeem Instructions
DROP TABLE IF EXISTS redeem_instructions;

-- Drop Gift Card Logo URLs
DROP TABLE IF EXISTS gift_card_logo_urls;

-- Drop Gift Card Fixed Denominations
DROP TABLE IF EXISTS gift_card_fixed_denominations;

-- Drop Gift Card Denomination Map
DROP TABLE IF EXISTS gift_card_denomination_map;

-- Drop Gift Cards Table
DROP TABLE IF EXISTS gift_cards;

-- Drop Related Tables
DROP TABLE IF EXISTS brands;
DROP TABLE IF EXISTS categories;
DROP TABLE IF EXISTS countries;
