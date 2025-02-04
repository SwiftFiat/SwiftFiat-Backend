-- name: InsertNewProofImage :one
INSERT INTO proof_of_address_images (user_id, filename, proof_type, image_data)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: GetProofImage :one
SELECT id, user_id, filename, proof_type, image_data, created_at
FROM proof_of_address_images
WHERE id = $1;

-- name: UpdateProofImage :one
UPDATE proof_of_address_images
SET filename = $2, proof_type = $3, image_data = $4
WHERE id = $1
RETURNING id, filename, proof_type, created_at;

-- name: DeleteProofImage :execrows
DELETE FROM proof_of_address_images
WHERE id = $1;

-- name: ListProofImages :many
SELECT id, filename, proof_type, created_at
FROM proof_of_address_images
ORDER BY created_at DESC;

-- name: ListProofImagesForUser :many
SELECT id, filename, proof_type, created_at
FROM proof_of_address_images
WHERE user_id = $1
ORDER BY created_at DESC;