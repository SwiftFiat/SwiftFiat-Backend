--! 
-- A single gift card record in gift_cards can:
-- Reference a brand, category, and country.
-- Have multiple fixed denominations (gift_card_fixed_denominations).
-- Map recipient denominations to sender denominations (gift_card_denomination_map).
-- Include multiple logo URLs (gift_card_logo_urls).
-- Include concise and verbose redeem instructions (redeem_instructions).
--!

-- Up migration script

CREATE TABLE countries (
    id SERIAL PRIMARY KEY,
    iso_name TEXT UNIQUE,
    name TEXT UNIQUE,
    flag_url TEXT
);

CREATE TABLE categories (
    id SERIAL PRIMARY KEY,
    category_id BIGINT UNIQUE NOT NULL,
    name TEXT UNIQUE NOT NULL
);

CREATE TABLE brands (
    id SERIAL PRIMARY KEY,
    brand_id BIGINT UNIQUE NOT NULL,
    brand_name TEXT
);

CREATE TABLE gift_cards (
    id SERIAL PRIMARY KEY,
    product_id BIGINT UNIQUE NOT NULL,
    product_name TEXT,
    denomination_type TEXT,
    discount_percentage FLOAT,
    max_recipient_denomination FLOAT,
    min_recipient_denomination FLOAT,
    max_sender_denomination FLOAT,
    min_sender_denomination FLOAT,
    global BOOLEAN,
    metadata JSONB,
    recipient_currency_code TEXT,
    sender_currency_code TEXT,
    sender_fee FLOAT,
    sender_fee_percentage FLOAT,
    supports_pre_order BOOLEAN,
    brand_id BIGINT REFERENCES brands(id) ON DELETE CASCADE,
    category_id BIGINT REFERENCES categories(id) ON DELETE CASCADE,
    country_id BIGINT REFERENCES countries(id) ON DELETE CASCADE
);

CREATE TABLE gift_card_fixed_denominations (
    id SERIAL PRIMARY KEY,
    gift_card_id BIGINT REFERENCES gift_cards(id) ON DELETE CASCADE,
    type TEXT CHECK (type IN ('recipient', 'sender')), -- Type of denomination
    denomination FLOAT
);

CREATE TABLE gift_card_denomination_map (
    id SERIAL PRIMARY KEY,
    gift_card_id BIGINT REFERENCES gift_cards(id) ON DELETE CASCADE,
    recipient_currency_code TEXT,
    sender_currency_value FLOAT
);

CREATE TABLE gift_card_logo_urls (
    id SERIAL PRIMARY KEY,
    gift_card_id BIGINT REFERENCES gift_cards(id) ON DELETE CASCADE,
    logo_url TEXT
);

CREATE TABLE redeem_instructions (
    id SERIAL PRIMARY KEY,
    gift_card_id BIGINT UNIQUE REFERENCES gift_cards(id) ON DELETE CASCADE,
    concise TEXT,
    detailed_instruction TEXT
);

CREATE INDEX idx_gift_cards_product_id ON gift_cards(product_id);
CREATE INDEX idx_gift_cards_category_id ON gift_cards(category_id);
CREATE INDEX idx_gift_card_denomination_map ON gift_card_denomination_map(gift_card_id);
