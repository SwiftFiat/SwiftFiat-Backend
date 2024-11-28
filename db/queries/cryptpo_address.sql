-- name: AssignAddressToCustomer :one
INSERT INTO crypto_addresses (customer_id, address_id, coin, balance, status)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: FetchActiveByCustomerID :many
SELECT *
FROM crypto_addresses
WHERE customer_id = $1 AND status = 'active';

-- name: FetchByAddressID :one
SELECT *
FROM crypto_addresses
WHERE address_id = $1;

-- name: UpdateAddressBalanceByAddressID :one
UPDATE crypto_addresses
SET balance = balance + $2,
    updated_at = NOW()
WHERE address_id = $1
RETURNING *;

-- name: DeactivateAddressByID :one
UPDATE crypto_addresses
SET status = 'inactive',
    updated_at = NOW()
WHERE address_id = $1
RETURNING *;

-- name: GetOrphanedAddresses :many
SELECT *
FROM crypto_addresses
WHERE customer_id IS NULL;

-- name: FindTopCustomersByBalance :many
SELECT customer_id, SUM(balance) AS total_balance
FROM crypto_addresses
WHERE customer_id IS NOT NULL
GROUP BY customer_id
ORDER BY total_balance DESC
LIMIT $1;

-- name: FetchAllAddressesAndCustomers :many
SELECT 
    ca.id AS address_id, 
    ca.customer_id, 
    ca.coin, 
    ca.balance, 
    ca.status, 
    u.first_name AS customer_name, 
    u.email AS customer_email
FROM crypto_addresses ca
LEFT JOIN users u ON ca.customer_id = u.id;
