-- name: UpsertBrand :one
INSERT INTO brands (brand_id, brand_name)
VALUES ($1, $2)
ON CONFLICT (brand_id) DO UPDATE SET brand_name = EXCLUDED.brand_name
RETURNING id;

-- name: UpsertCategory :one
INSERT INTO categories (category_id, name)
VALUES ($1, $2)
ON CONFLICT (category_id) DO UPDATE SET name = EXCLUDED.name
RETURNING id;

-- name: UpsertCountry :one
INSERT INTO countries (iso_name, name, flag_url)
VALUES ($1, $2, $3)
ON CONFLICT (iso_name) DO UPDATE SET name = EXCLUDED.name, flag_url = EXCLUDED.flag_url
RETURNING id;

-- name: UpsertGiftCard :one
INSERT INTO gift_cards (
    product_id, product_name, denomination_type, discount_percentage, 
    max_recipient_denomination, min_recipient_denomination, 
    max_sender_denomination, min_sender_denomination, global, metadata, 
    recipient_currency_code, sender_currency_code, sender_fee, 
    sender_fee_percentage, supports_pre_order, brand_id, category_id, country_id
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18)
ON CONFLICT (product_id) DO UPDATE SET
    product_name = EXCLUDED.product_name, denomination_type = EXCLUDED.denomination_type,
    discount_percentage = EXCLUDED.discount_percentage
RETURNING id;

-- name: UpsertFixedDenominations :exec
INSERT INTO gift_card_fixed_denominations (gift_card_id, type, denomination)
VALUES ($1, $2, $3)
ON CONFLICT DO NOTHING;

-- name: UpsertGiftCardLogoUrl :exec
INSERT INTO gift_card_logo_urls (gift_card_id, logo_url)
VALUES ($1, $2)
ON CONFLICT DO NOTHING;

-- name: UpsertRedeemInstructions :exec
INSERT INTO redeem_instructions (gift_card_id, concise, detailed_instruction)
VALUES ($1, $2, $3)
ON CONFLICT (gift_card_id) DO UPDATE SET
concise = EXCLUDED.concise,
detailed_instruction = EXCLUDED.detailed_instruction;

-- name: FetchGiftCard :one
SELECT 
    gc.product_id, 
    gc.product_name, 
    gc.denomination_type, 
    gc.discount_percentage, 
    gc.max_recipient_denomination, 
    gc.min_recipient_denomination, 
    gc.max_sender_denomination, 
    gc.min_sender_denomination, 
    gc.global, 
    gc.metadata, 
    gc.recipient_currency_code, 
    gc.sender_currency_code, 
    gc.sender_fee, 
    gc.sender_fee_percentage, 
    gc.supports_pre_order, 
    gl.logo_url,
    b.brand_name, 
    c.name AS category_name, 
    co.name AS country_name, 
    co.flag_url
FROM 
    gift_cards gc
LEFT JOIN 
    brands b ON gc.brand_id = b.brand_id
LEFT JOIN 
    categories c ON gc.category_id = c.id
LEFT JOIN 
    countries co ON gc.country_id = co.id
LEFT JOIN 
    gift_card_logo_urls gl ON gc.id = gl.gift_card_id
WHERE 
    gc.product_id = $1;

-- name: FetchGiftCards :many
SELECT 
    gc.id,
    gc.product_id, 
    gc.product_name, 
    gc.denomination_type, 
    gc.discount_percentage, 
    gc.max_recipient_denomination, 
    gc.min_recipient_denomination, 
    gc.max_sender_denomination, 
    gc.min_sender_denomination, 
    -- This ensures that only payable denom in 'USD' is returned
    COALESCE(
        JSON_AGG(DISTINCT gd.denomination) FILTER (WHERE gd.denomination IS NOT NULL AND gd.type = 'sender'),
        '[]'
    )::json AS giftcard_denominations,
    gc.global, 
    gc.metadata, 
    gc.recipient_currency_code, 
    gc.sender_currency_code, 
    gc.sender_fee, 
    gc.sender_fee_percentage, 
    gc.supports_pre_order, 
    COALESCE(JSON_AGG(DISTINCT gl.logo_url) FILTER (WHERE gl.logo_url IS NOT NULL), '[]')::json AS logo_urls,
    b.brand_name, 
    c.name AS category_name,
    co.name AS country_name, 
    co.flag_url
FROM 
    gift_cards gc
LEFT JOIN 
    brands b ON gc.brand_id = b.brand_id
LEFT JOIN 
    gift_card_fixed_denominations gd ON gc.id = gd.gift_card_id
LEFT JOIN 
    categories c ON gc.category_id = c.id
LEFT JOIN 
    countries co ON gc.country_id = co.id
LEFT JOIN 
    gift_card_logo_urls gl ON gc.id = gl.gift_card_id
GROUP BY 
    gc.id,
    gc.product_id, 
    gc.product_name, 
    gc.denomination_type, 
    gc.discount_percentage, 
    gc.max_recipient_denomination, 
    gc.min_recipient_denomination, 
    gc.max_sender_denomination, 
    gc.min_sender_denomination, 
    gc.global, 
    gc.metadata, 
    gc.recipient_currency_code, 
    gc.sender_currency_code, 
    gc.sender_fee, 
    gc.sender_fee_percentage, 
    gc.supports_pre_order, 
    b.brand_name, 
    c.name, 
    co.name, 
    co.flag_url
ORDER BY 
    gc.product_id;



-- name: FetchGiftCardsByBrand :many
SELECT 
    b.id,
    b.brand_id,
    b.brand_name,
    (
        SELECT logo_url 
        FROM gift_card_logo_urls gl 
        JOIN gift_cards gc2 ON gl.gift_card_id = gc2.id
        WHERE gc2.brand_id = b.id
        LIMIT 1
    ) AS brand_logo_url,
    COUNT(gc.id) AS gift_card_count
FROM 
    brands b
LEFT JOIN 
    gift_cards gc ON b.id = gc.brand_id
GROUP BY 
    b.id,
    b.brand_id,
    b.brand_name
ORDER BY 
    b.brand_name ASC;


-- name: FetchGiftCardsByCategory :many
SELECT 
    c.name,
    COUNT(gc.id) AS gift_card_count
FROM 
    categories c 
LEFT JOIN
    gift_cards gc ON gc.category_id = c.id
GROUP BY 
    c.id,
    c.category_id,
    c.name
ORDER BY 
    c.name;