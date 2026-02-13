.PHONY: migrate generate run docker-up

migrate:
	# Run goose via `go run` so `goose` binary isn't required on PATH
	# include host/port so goose connects via TCP (docker-postgres exposed on localhost:5432)
	# Use environment variables for driver/DB string to avoid positional parsing issues
	GOOSE_DRIVER=postgres GOOSE_DBSTRING="user=postgres password=postgres host=localhost port=5432 dbname=canglanfu sslmode=disable" \
		go run github.com/pressly/goose/v3/cmd/goose@latest -- -dir internal/db/migrations up

generate:
	# Use `go run` so `sqlc` doesn't need to be installed globally
	# sqlc module moved to github.com/sqlc-dev/sqlc
	# Run sqlc from project root but point to the config file so paths resolve
	go run github.com/sqlc-dev/sqlc/cmd/sqlc@latest generate -f internal/db/sqlc.yaml

run:
	# Start a lightweight dev server (health) without full DB/sqlc generation
	go run cmd/dev/main.go

docker-up:
	docker-compose up -d postgres # redis (disabled)