-- name: CreateNewKYC :one
INSERT INTO kyc (
    user_id, tier, status, verification_date
) VALUES (
    $1, 'tier_1', 'verified', now()
) RETURNING *;

-- name: GetKYCByUserID :one
SELECT * FROM kyc
WHERE user_id = $1;

-- name: UpdateBVN :one
UPDATE kyc
SET 
    bvn = $2,
    updated_at = now()
WHERE id = $1
RETURNING *;

-- name: UpdateKYCNINInfo :one
UPDATE kyc
SET 
    full_name = $2,
    nin = $3,
    gender = $4,
    selfie_url = $5,
    -- dob = $6,
    phone_number = $6,
    updated_at = now()
WHERE id = $1
RETURNING *;

-- name: UpdateKYCGovernmentID :one
UPDATE kyc
SET 
    id_type = $2,
    id_number = $3,
    id_image_url = $4,
    updated_at = now()
WHERE id = $1
RETURNING *;

-- name: UpdateKYCAddress :one
UPDATE kyc
SET 
    state = $2,
    lga = $3,
    house_number = $4,
    street_name = $5,
    nearest_landmark = $6,
    postal_code = $7,
    city = $8,
    updated_at = now()
WHERE id = $1
RETURNING *;

-- name: UpdateKYCProofOfAddress :one
UPDATE kyc
SET 
    proof_of_address_type = $2,
    proof_of_address_url = $3,
    proof_of_address_date = $4,
    updated_at = now()
WHERE id = $1
RETURNING *;

-- name: GetKYCByStatus :many
SELECT 
    k.*,
    u.email,
    u.first_name,
    u.last_name,
    u.phone_number as user_phone
FROM kyc k
JOIN users u ON u.id = k.user_id
WHERE k.status = $1
ORDER BY k.created_at DESC
LIMIT $2 OFFSET $3;

-- name: GetPendingKYCCount :one
SELECT COUNT(*) FROM kyc
WHERE status = 'pending';

-- name: GetVerifiedKYCCount :one
SELECT COUNT(*) FROM kyc
WHERE status = 'verified';

-- name: GetRejectedKYCCount :one
SELECT COUNT(*) FROM kyc
WHERE status = 'rejected';

-- name: GetKYCStatistics :one
SELECT 
    COUNT(*) FILTER (WHERE status = 'pending') as pending_count,
    COUNT(*) FILTER (WHERE status = 'verified') as verified_count,
    COUNT(*) FILTER (WHERE status = 'rejected') as rejected_count,
    COUNT(*) as total_count,
    AVG(EXTRACT(EPOCH FROM (verification_date - created_at))/3600) FILTER (WHERE verification_date IS NOT NULL) as avg_verification_hours
FROM kyc;

-- name: GetRecentVerifications :many
SELECT 
    k.id,
    k.user_id,
    k.verification_date,
    k.status,
    u.email,
    u.first_name,
    u.last_name
FROM kyc k
JOIN users u ON u.id = k.user_id
WHERE k.verification_date >= $1
ORDER BY k.verification_date DESC;

-- name: UpdateKYCStatus :one
UPDATE kyc
SET 
    status = $2,
    updated_at = now()
WHERE id = $1
RETURNING *;

-- name: ManuallyVerifyKYC :one
UPDATE kyc
SET 
    status = 'verified',
    verification_date = COALESCE(verification_date, now()),
    updated_at = now()
WHERE id = $1
RETURNING *;

-- name: RejectKYC :one
UPDATE kyc
SET 
    status = 'rejected',
    additional_info = jsonb_set(
        additional_info,
        '{rejection_reason}',
        to_jsonb($2::text)
    ),
    updated_at = now()
WHERE id = $1
RETURNING *;

-- name: GetIncompleteKYCFields :one
SELECT 
    CASE WHEN full_name IS NULL OR full_name = '' THEN true ELSE false END as needs_full_name,
    CASE WHEN phone_number IS NULL OR phone_number = '' THEN true ELSE false END as needs_phone,
    CASE WHEN email IS NULL OR email = '' THEN true ELSE false END as needs_email,
    CASE WHEN (bvn IS NULL OR bvn = '') AND (nin IS NULL OR nin = '') THEN true ELSE false END as needs_bvn_or_nin,
    CASE WHEN gender IS NULL OR gender = '' THEN true ELSE false END as needs_gender,
    CASE WHEN selfie_url IS NULL OR selfie_url = '' THEN true ELSE false END as needs_selfie,
    CASE WHEN id_type IS NULL OR id_number IS NULL OR id_image_url IS NULL THEN true ELSE false END as needs_govt_id,
    CASE WHEN state IS NULL OR lga IS NULL OR house_number IS NULL OR street_name IS NULL THEN true ELSE false END as needs_address,
    CASE WHEN proof_of_address_type IS NULL OR proof_of_address_url IS NULL OR proof_of_address_date IS NULL THEN true ELSE false END as needs_proof_of_address,
    CASE WHEN proof_of_address_date IS NOT NULL AND proof_of_address_date < (CURRENT_DATE - INTERVAL '6 months') THEN true ELSE false END as proof_expired
FROM kyc
WHERE id = $1;

-- name: GetUsersNeedingKYCReminder :many
SELECT 
    u.id,
    u.email,
    u.first_name,
    u.last_name,
    k.created_at as kyc_started_at,
    k.status
FROM users u
LEFT JOIN kyc k ON k.user_id = u.id
WHERE 
    u.verified = true 
    AND u.is_kyc_verified = false
    AND (k.status IS NULL OR k.status = 'pending')
    AND k.created_at < (now() - INTERVAL '7 days')
ORDER BY k.created_at ASC;

-- name: UpdateUserKYCVerificationStatus :one
UPDATE users
SET 
    is_kyc_verified = $2,
    updated_at = $3
WHERE id = $1
RETURNING *;

-- name: GetKYCWithUserDetails :one
SELECT 
    k.*,
    u.email,
    u.first_name,
    u.last_name,
    u.phone_number as user_phone,
    u.verified as user_verified,
    u.is_kyc_verified,
    u.created_at as user_created_at
FROM kyc k
JOIN users u ON u.id = k.user_id
WHERE k.id = $1;

-- name: SearchKYCByEmail :many
SELECT 
    k.*,
    u.email,
    u.first_name,
    u.last_name
FROM kyc k
JOIN users u ON u.id = k.user_id
WHERE u.email ILIKE $1
ORDER BY k.created_at DESC;

-- name: GetKYCDocumentExpirations :many
SELECT 
    k.id,
    k.user_id,
    u.email,
    k.proof_of_address_date,
    k.proof_of_address_type,
    (CURRENT_DATE - k.proof_of_address_date) as days_old
FROM kyc k
JOIN users u ON u.id = k.user_id
WHERE 
    k.status = 'verified'
    AND k.proof_of_address_date < (CURRENT_DATE - INTERVAL '5 months')
ORDER BY k.proof_of_address_date ASC;

-- name: BulkRejectExpiredDocuments :exec
UPDATE kyc
SET 
    status = 'rejected',
    additional_info = jsonb_set(
        additional_info,
        '{rejection_reason}',
        '"Proof of address document expired (older than 6 months)"'
    ),
    updated_at = now()
WHERE 
    status = 'verified'
    AND proof_of_address_date < (CURRENT_DATE - INTERVAL '6 months');

-- name: UpdateKYCToTierOne :one
UPDATE kyc
SET 
    tier = 'tier_1',
    updated_at = now()
WHERE id = $1
RETURNING *;

-- name: UpdateKYCToTierTwo :one
UPDATE kyc
SET 
    tier = 'tier_2',
    updated_at = now()
WHERE id = $1
RETURNING *;

-- name: UpdateKYCToTierThree :one
UPDATE kyc
SET 
    tier = 'tier_3',
    updated_at = now()
WHERE id = $1
RETURNING *;
