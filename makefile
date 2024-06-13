start: # start-server
	CompileDaemon -command="./swiftfiat_backend"

test: # run tests
	go test -v -cover ./...

c_m: # create-migration: create migration of name=<migration_name>
	migrate create -ext sql -dir db/migrations -seq $(name)

### Variables for handling migrations
count ?= 1
db_username ?= swift-admin
db_password ?= 438a8e2e6c0521233a53602831bf4410add45e52
db_host ?= localhost
db_port ?= 5432
db_name ?= swiftfiat_db
m_up: # migrate-up
	migrate -path db/migrations -database "postgres://${db_username}:${db_password}@${db_host}:${db_port}/${db_name}?sslmode=disable" up $(count)

m_fup: # migreate-force up
	migrate -path db/migrations -database "postgres://${db_username}:${db_password}@${db_host}:${db_port}/${db_name}?sslmode=disable" force $(count)

m_down: # migrate-down
	migrate -path db/migrations -database "postgres://${db_username}:${db_password}@${db_host}:${db_port}/${db_name}?sslmode=disable" down $(count)

p_up: # postrgres-up: create postgres server -dispatch
	docker-compose up -d

p_down: # postrgres-up: delete postgres server -dispatch
	docker-compose down

container_name ?= swiftfiat_postgres
db_up: # database-up: create a new database
	docker exec -it ${container_name} createdb --username=${db_username} --owner=${db_username} ${db_name}

db_down: # database-down: drop a database
	docker exec -it ${container_name} dropdb --username=${db_username} ${db_name}

sqlc: # sqlc-generate
	sqlc generate