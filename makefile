# Start the server using CompileDaemon for automatic recompilation
start: # start-server
	CompileDaemon -command="./Swiftfiat-Backend" -color=true

# Start the server in debug mode with Delve debugger
start_d:
	CompileDaemon \
  		-command="dlv --listen=:2345 --headless=true --api-version=2 --accept-multiclient exec ./Swiftfiat-Backend" \
  		-build="go build -gcflags=all=-N" \
  		-color=true

# Run all tests with verbose output and coverage reporting
test: # run tests
	go test -v -cover ./...

# Create a new database migration file with the specified name
c_m: # create-migration: create migration of name=<migration_name>
	migrate create -ext sql -dir db/migrations -seq $(name)

### Variables for handling migrations
count ?= 1
version ?= 1
db_username ?= swift-admin
db_password ?= 438a8e2e6c0521233a53602831bf4410add45e52
db_host ?= localhost
db_port ?= 5432
db_name ?= swiftfiat_db

# Run database migrations up to apply pending changes
m_up: # migrate-up
	migrate -path db/migrations -database "postgres://${db_username}:${db_password}@${db_host}:${db_port}/${db_name}?sslmode=disable" up $(count)

# Fix dirty database state by forcing to previous clean version
m_fix: # migrate-fix: fix dirty database state
	migrate -path db/migrations -database "postgres://${db_username}:${db_password}@${db_host}:${db_port}/${db_name}?sslmode=disable" force $(version)

# Check current migration version number
m_version: # migrate-version
	migrate -path db/migrations -database "postgres://${db_username}:${db_password}@${db_host}:${db_port}/${db_name}?sslmode=disable" version

# Force database migration version without running migrations
m_fup: # migreate-force up
	migrate -path db/migrations -database "postgres://${db_username}:${db_password}@${db_host}:${db_port}/${db_name}?sslmode=disable" force $(count)

# Roll back database migrations
m_down: # migrate-down
	migrate -path db/migrations -database "postgres://${db_username}:${db_password}@${db_host}:${db_port}/${db_name}?sslmode=disable" down $(count)

# Start PostgreSQL container in detached mode
p_up: # postrgres-up: create postgres server -dispatch
	docker-compose up -d

# Stop and remove PostgreSQL container
p_down: # postrgres-up: delete postgres server -dispatch
	docker-compose down

container_name ?= swiftfiat_postgres

# Create a new PostgreSQL database
db_up: # database-up: create a new database
	docker exec -it ${container_name} createdb --username=${db_username} --owner=${db_username} ${db_name}

# Create a full backup of the database
db_backup:
	docker exec -it ${container_name} pg_dump --username=${db_username} ${db_name} > db_backup.sql

# Restore database from a full backup
db_restore:
	docker exec -i ${container_name} psql --username=${db_username} ${db_name} < db_backup.sql

# Backup specific tables from the database
# Usage: make db_backup_specific tables="table1 table2 table3"
db_backup_specific:
	docker exec -it ${container_name} pg_dump --username=${db_username} ${db_name} --table=$(subst $(space),$(,),$(tables)) > db_backup_specific.sql

# Restore specific tables from a backup
# Usage: make db_restore_specific tables="table1 table2 table3" 
db_restore_specific:
	docker exec -i ${container_name} psql --username=${db_username} ${db_name} < db_backup_specific.sql

# Drop/delete the database
db_down: # database-down: drop a database
	docker exec -it ${container_name} dropdb --username=${db_username} ${db_name}

# Generate Go code from SQL using sqlc
sqlc: # sqlc-generate
	sqlc generate