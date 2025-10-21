# Start the server using AIR for automatic recompilation
start:
	air \
		--build.cmd "go build -o bin/api" \
		--build.bin "./bin/api" \
		--build.exclude_dir "vendor" \
		--build.include_ext "go" \
		--build.kill_delay "0.5s" \
		--build.poll "2s"

# Start the server in debug mode with air and Delve
debug:
	air \
		--build.cmd "go build -gcflags=all=-N -o bin/debug" \
		--build.bin "dlv --listen=:2345 --headless=true --api-version=2 --accept-multiclient exec ./bin/debug" \
   		--build.exclude_dir "vendor"

# Run all tests with verbose output and coverage reporting
test: # run tests
	go test -v -cover ./...

# Create a new database migration file with the specified name
c_m: # create-migration: create migration of name=<migration_name>
	migrate create -ext sql -dir db/migrations -seq $(name)

### Variables for handling migrations
count ?= 1
version ?= 1
db_username ?= postgres
db_password ?= 438a8e2e6c0521233a53602831bf4410add45e52
db_host ?= localhost
db_port ?= 5432
db_name ?= swiftfiat_db
ssl_mode ?= disable

# Run database migrations up to apply pending changes
m_up: # migrate-up
	migrate -path db/migrations -database "postgres://${db_username}:${db_password}@${db_host}:${db_port}/${db_name}?sslmode=${ssl_mode}" up $(count)

# Fix dirty database state by forcing to previous clean version
m_fix: # migrate-fix: fix dirty database state
	migrate -path db/migrations -database "postgres://${db_username}:${db_password}@${db_host}:${db_port}/${db_name}?sslmode=${ssl_mode}" force $(version)

# Check current migration version number
m_version: # migrate-version
	migrate -path db/migrations -database "postgres://${db_username}:${db_password}@${db_host}:${db_port}/${db_name}?sslmode=${ssl_mode}" version

# Force database migration version without running migrations
m_fup: # migreate-force up
	migrate -path db/migrations -database "postgres://${db_username}:${db_password}@${db_host}:${db_port}/${db_name}?sslmode=${ssl_mode}" force $(count)

# Roll back database migrations
m_down: # migrate-down
	migrate -path db/migrations -database "postgres://${db_username}:${db_password}@${db_host}:${db_port}/${db_name}?sslmode=${ssl_mode}" down $(count)

# Start services - PostgreSQL | Bitgo | Redis containers in detached mode
s_up: #
	docker compose -f docker-compose.services.yml up -d

# Stop and remove services - PostgreSQL | Bitgo | Redis container
s_down: #
	docker compose -f docker-compose.services.yml down

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

# Redocly documentation targets
docs_port ?= 8081

docs-preview: # start a local preview of the Redocly docs
	npx redocly preview --project-dir . --port $(docs_port)

docs-bundle: # bundle the OpenAPI into a single file at docs/bundle.yaml
	npx redocly bundle ./docs/swagger.yaml --output docs/bundle.yaml

docs-build: # build static HTML docs into docs/site
	npx redocly build-docs ./docs/swagger.yaml --output docs/site/index.html

docs-lint: # lint the OpenAPI with Redocly
	npx redocly lint ./docs/swagger.yaml

# Swagger helpers
SWAGGER_FILE ?= docs/swagger.yaml
SWAGGER_BUNDLE_OUTPUT ?= docs/swagger.bundle.yaml

swagger-validate: # validate the Swagger/OpenAPI file using @apidevtools/swagger-cli
	npx @apidevtools/swagger-cli validate $(SWAGGER_FILE)

swagger-bundle: # bundle multi-file swagger into a single file using @apidevtools/swagger-cli
	npx @apidevtools/swagger-cli bundle $(SWAGGER_FILE) -o $(SWAGGER_BUNDLE_OUTPUT)

swagger-convert: # convert swagger 2.0 to OpenAPI 3 using swagger2openapi
	npx swagger2openapi --yaml $(SWAGGER_FILE) -o docs/openapi3.yaml

swagger-serve: # serve the swagger using official swagger-ui docker image (binds to 8080)
	docker run --rm -p 8080:8080 -e SWAGGER_JSON=/tmp/swagger.json -v $$(pwd)/$(SWAGGER_FILE):/tmp/swagger.json swaggerapi/swagger-ui

docs-generate: # generate swagger docs using swaggo/swag
	swag init -g ./main.go -o ./docs