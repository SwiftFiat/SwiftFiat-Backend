-- name: CreateNewKYC :one
INSERT INTO kyc (
    user_id,
    tier,
    status
) VALUES ($1, $2, 'pending') RETURNING *;

-- name: GetKYCByID :one
SELECT * FROM kyc WHERE id = $1 LIMIT 1;

-- name: GetUserAndKYCByID :one
SELECT 
    u.id as user_id,
    u.first_name as first_name,
    u.last_name as last_name,
    u.email as user_email,
    k.*
FROM kyc k
LEFT JOIN users u ON k.user_id = u.id 
WHERE k.id = $1 LIMIT 1;

-- name: GetKYCByUserID :one
SELECT * FROM kyc WHERE user_id = $1 LIMIT 1;

-- name: UpdateKYCTier :one
UPDATE kyc 
SET 
    tier = $2,
    updated_at = now()
WHERE id = $1 
RETURNING *;

-- name: UpdateKYCStatus :one
UPDATE kyc 
SET 
    status = $2,
    updated_at = now(),
    verification_date = CASE 
        WHEN $2 = 'active' THEN now() 
        ELSE verification_date 
    END
WHERE id = $1 
RETURNING *;

-- name: UpdateKYCLevel1 :one
UPDATE kyc 
SET 
    full_name = $2,
    phone_number = $3,
    email = $4,
    bvn = $5,
    nin = $6,
    gender = $7,
    selfie_url = $8,
    updated_at = now()
WHERE id = $1 
RETURNING *;

-- name: UpdateKYCLevel2 :one
UPDATE kyc 
SET 
    id_type = $2,
    id_number = $3,
    id_image_url = $4,
    state = $5,
    lga = $6,
    house_number = $7,
    street_name = $8,
    nearest_landmark = $9,
    updated_at = now()
WHERE id = $1 
RETURNING *;

-- name: UpdateKYCLevel3 :one
UPDATE kyc 
SET 
    proof_of_address_type = $2,
    proof_of_address_url = $3,
    proof_of_address_date = $4,
    updated_at = now()
WHERE id = $1 
RETURNING *;

-- name: UpdateKYCLimits :one
UPDATE kyc 
SET 
    daily_transfer_limit_ngn = $2,
    wallet_balance_limit_ngn = $3,
    updated_at = now()
WHERE id = $1 
RETURNING *;

-- name: GetPendingKYCRequests :many
SELECT * FROM kyc 
WHERE status = 'pending' 
ORDER BY created_at ASC;

-- name: GetKYCByTier :many
SELECT * FROM kyc 
WHERE tier = $1 
ORDER BY created_at DESC;

-- name: DeleteKYC :execrows
DELETE FROM kyc WHERE id = $1;

-- name: GetKYCStats :one
SELECT 
    COUNT(*) as total_kyc,
    COUNT(CASE WHEN status = 'pending' THEN 1 END) as pending_count,
    COUNT(CASE WHEN status = 'active' THEN 1 END) as active_count,
    COUNT(CASE WHEN status = 'rejected' THEN 1 END) as rejected_count,
    COUNT(CASE WHEN tier = 1 THEN 1 END) as tier1_count,
    COUNT(CASE WHEN tier = 2 THEN 1 END) as tier2_count,
    COUNT(CASE WHEN tier = 3 THEN 1 END) as tier3_count
FROM kyc;