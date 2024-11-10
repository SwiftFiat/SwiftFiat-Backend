-- name: GetUserAndKYCWithProofOfAddress :one
SELECT 
    u.id as user_id,
    u.first_name as first_name,
    u.last_name as last_name,
    u.email as user_email,
    k.*,
    json_agg(pai) as proof_of_address_images
FROM kyc k
LEFT JOIN users u ON k.user_id = u.id 
LEFT JOIN proof_of_address_images pai ON k.user_id = pai.user_id
WHERE k.id = $1
GROUP BY u.id, k.id
LIMIT 1;